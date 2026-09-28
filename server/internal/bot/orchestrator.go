package bot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// queueCapacity 每会话排队上限（fail closed：满即拒，绝不静默积压）。
const queueCapacity = 8

// maxConcurrentTurns 全局同时在跑的 claude 回合上限（跨所有 bot/会话共享的资源护栏：
// 每回合一个真实 claude 子进程）。个人场景常量足够；将来需按 bot 配置时提为表列是一步迁移。
const maxConcurrentTurns = 10

// errQueueFull 会话队列已满。
var errQueueFull = errors.New("排队已满")

// errStopped 编排器已停止（sidecar 退出中），不再受理新消息。
var errStopped = errors.New("编排器已停止")

// turnJob 一个待执行回合。reply 是受理时已开出并发过「正在思考…」占位帧的回复流——
// 企微 5 秒被动回复窗口在受理瞬间被占住，排队多久都不超窗（两写者一次交接：适配器在
// 回调 goroutine 发占位帧，此后全部帧只由 worker 的回复泵写）。
type turnJob struct {
	cfg   BotRuntimeConfig
	msg   InboundMessage
	reply ReplyStream
}

// Orchestrator 编排核心：入站受理（白名单/占位帧）→ 每会话串行队列 → prompt → claude 回合 →
// 回复泵 → 会话持久化。无渠道知识；HandleInbound 由适配器回调（supervisor 装配）。
type Orchestrator struct {
	store  *ConversationStore
	driver ClaudeDriver
	log    *zap.Logger

	ctx    context.Context // 生命周期 ctx：Stop 级联取消全部在途回合（杀 claude 子进程）
	cancel context.CancelFunc
	mu     sync.Mutex
	queues map[string]chan turnJob // key: <botID>:<conversationKey>；首消息惰性建，进程期常驻
	turns  chan struct{}           // 全局并发回合信号量（maxConcurrentTurns 个槽位）
	wg     sync.WaitGroup
	closed bool
}

// NewOrchestrator 构造编排器。
func NewOrchestrator(store *ConversationStore, driver ClaudeDriver, log *zap.Logger) *Orchestrator {
	ctx, cancel := context.WithCancel(context.Background())
	return &Orchestrator{
		store:  store,
		driver: driver,
		log:    log,
		ctx:    ctx,
		cancel: cancel,
		queues: make(map[string]chan turnJob),
		turns:  make(chan struct{}, maxConcurrentTurns),
	}
}

// HandleInbound 入站唯一入口（适配器回调 goroutine 上执行，须快进快出）。
// 固定时序：白名单判定 → （拒绝：direct 回提示帧 / group 静默）→ 受理开流发占位帧 → 入队。
// openStream 由适配器提供（闭包持有渠道连接），核心决定是否/何时调用——策略拒绝与群聊
// 静默不产生任何流。
func (o *Orchestrator) HandleInbound(cfg BotRuntimeConfig, msg InboundMessage, openStream func() (ReplyStream, error)) {
	allowed, reason := EvaluateAccess(cfg.AccessPolicy, msg.SenderID)
	if !allowed {
		o.log.Warn("bot 消息被访问策略拒绝",
			zap.Int("botID", cfg.BotID), zap.String("conversation", msg.ConversationKey),
			zap.String("reason", reason))
		if msg.ChatType == ChatDirect {
			// 单聊拒绝回一句提示（fail closed 语义可见）；群聊静默（不向陌生群成员泄露策略）。
			if rs, err := openStream(); err == nil {
				_ = rs.Flush(accessDeniedText(reason), true)
			}
		}
		return
	}

	rs, err := openStream()
	if err != nil {
		o.log.Error("bot 开启回复流失败", zap.Int("botID", cfg.BotID), zap.Error(err))
		return
	}
	if err := rs.Flush("正在思考…", false); err != nil {
		o.log.Warn("bot 占位帧发送失败（继续回合）", zap.Error(err))
	}

	if err := o.enqueue(cfg, msg, rs); err != nil {
		text := "当前消息较多，排队已满，请稍后再发。"
		if errors.Is(err, errStopped) {
			text = "机器人已停止服务。"
		}
		_ = rs.Flush(text, true)
	}
}

// accessDeniedText 白名单拒绝提示（单聊）。
func accessDeniedText(reason string) string {
	if reason == AccessReasonPolicyInvalid {
		return "机器人访问策略配置异常，暂无法处理消息，请检查 bot 配置。"
	}
	return "您不在该机器人的使用白名单内，请联系机器人管理员。"
}

// enqueue 入队（不存在则惰性建会话队列 + worker）。已停止 / 队列满 fail closed。
func (o *Orchestrator) enqueue(cfg BotRuntimeConfig, msg InboundMessage, rs ReplyStream) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return errStopped
	}
	key := queueKey(cfg.BotID, msg.ConversationKey)
	ch, ok := o.queues[key]
	if !ok {
		ch = make(chan turnJob, queueCapacity)
		o.queues[key] = ch
		o.wg.Add(1)
		go o.worker(ch)
	}
	select {
	case ch <- turnJob{cfg: cfg, msg: msg, reply: rs}:
		return nil
	default:
		return errQueueFull
	}
}

// worker 会话串行消费者：逐 job 执行回合；Stop 关闭队列后自然退出。
func (o *Orchestrator) worker(ch chan turnJob) {
	defer o.wg.Done()
	for job := range ch {
		o.runTurn(job)
	}
}

// queueKey 队列键。
func queueKey(botID int, conversationKey string) string {
	return strconv.Itoa(botID) + ":" + conversationKey
}

// runTurn 执行单回合（会话内串行）。固定时序：幂等复查 → markSeen → load session →
// 组 prompt → RunTurn → PumpReply → 持久化（含 resume 失效自愈）。
// Stop 后排空的残余 job 走「已停止」短路径，不触发失败自愈（取消 ≠ 会话失效）。
func (o *Orchestrator) runTurn(job turnJob) {
	// Stop 短路径：ctx 已取消（含 Stop 后队列残余 job）——只回停止文案，不碰会话锚点。
	if o.ctx.Err() != nil {
		_ = job.reply.Flush("机器人已停止服务。", true)
		return
	}

	cfg, msg := job.cfg, job.msg
	key := msg.ConversationKey

	// 权威幂等复查（受理与执行之间可能已有同 msgid 入队）。
	seen, err := o.store.HasSeen(cfg.BotID, key, msg.MessageID)
	if err != nil {
		o.log.Error("bot 幂等查询失败（放行执行）", zap.Error(err))
	}
	if seen {
		_ = job.reply.Flush("该消息已处理过，重复推送已忽略。", true)
		return
	}
	if err := o.store.MarkSeen(cfg.BotID, key, msg.MessageID); err != nil {
		o.log.Error("bot markSeen 失败", zap.Error(err))
	}

	// 工作目录未配置（扫码接入先建 bot 后补配置的语义）：连接照常、回合不启动，
	// 提示用户补配置——已 markSeen（消息已被处理），不算失败、不触发会话自愈。
	if strings.TrimSpace(cfg.WorkspaceDir) == "" {
		_ = job.reply.Flush("机器人尚未配置工作目录，请先在应用「设置 → IM 机器人」中完成配置，再重新发送消息。", true)
		return
	}

	prevSessionID, err := o.store.SessionID(cfg.BotID, key)
	if err != nil {
		o.log.Error("bot 会话读取失败（按新会话处理）", zap.Error(err))
	}

	// 超长全文落盘：.bot-outbox/<ts>-<msgid 前 8 位>.md，帧内附路径。
	sink := o.overflowSink(cfg, msg)

	// 全局并发护栏：拿槽位才 spawn（排队等待可被 Stop 取消）。会话队列已保证同会话串行，
	// 信号量只约束跨会话的总进程数。
	select {
	case o.turns <- struct{}{}:
	case <-o.ctx.Done():
		_ = job.reply.Flush("机器人已停止服务。", true)
		return
	}
	prompt := BuildTurnPrompt(msg)
	events, err := o.driver.RunTurn(o.ctx, TurnRequest{
		WorkspaceDir: cfg.WorkspaceDir,
		SessionID:    prevSessionID,
		Prompt:       prompt,
		Model:        cfg.Model,
		SystemPrompt: ComposeSystemPrompt(cfg.SystemPrompt),
		AllowedTools: cfg.AllowedTools,
		Port:         cfg.Port,
	})
	if err != nil {
		<-o.turns
		// 取消竞态（select 命中槽位后 ctx 才取消）：等同 Stop 短路径，不清锚点。
		if o.ctx.Err() != nil {
			_ = job.reply.Flush("机器人已停止服务。", true)
			return
		}
		// spawn 即失败（如 claude 不在场）：终态帧 + 自愈（若带 resume 尝试则清锚点，
		// 下条消息开新会话——失败后不复用旧锚点是确定性单步恢复，不做脆弱的文案匹配）。
		o.log.Error("bot 回合启动失败", zap.Int("botID", cfg.BotID), zap.Error(err))
		_ = job.reply.Flush("❌ 无法启动 claude 会话："+firstLine(err.Error()), true)
		o.healAfterFailure(cfg.BotID, key, prevSessionID)
		return
	}

	outcome := PumpReply(o.ctx, job.reply, events, sink, o.log)
	<-o.turns
	if outcome.Err != nil {
		o.log.Warn("bot 回合失败", zap.Int("botID", cfg.BotID),
			zap.String("conversation", key), zap.Error(outcome.Err))
	}
	if outcome.SessionID != "" {
		if err := o.store.FinishTurn(cfg.BotID, key, outcome.SessionID, time.Now()); err != nil {
			o.log.Error("bot 会话回写失败", zap.Error(err))
		}
	}
	// 取消（SIGTERM/Stop）导致的回合中断不是会话失效：claude 侧锚点仍可用，不做自愈清除。
	if outcome.Err != nil && o.ctx.Err() == nil {
		o.healAfterFailure(cfg.BotID, key, prevSessionID)
	}
}

// healAfterFailure 回合失败后的会话自愈：本轮尝试过 resume（prev 非空）→ 清空锚点，
// 用户重发即开新会话（失败文案已提示重试）。本轮本是新会话则无事可做。
func (o *Orchestrator) healAfterFailure(botID int, conversationKey, prevSessionID string) {
	if prevSessionID == "" {
		return
	}
	if err := o.store.ClearSessionID(botID, conversationKey); err != nil {
		o.log.Error("bot 会话锚点清理失败", zap.Error(err))
	}
}

// overflowSink 生成超长全文落盘回调（懒创建目录；失败时返回空路径降级为静默截断）。
func (o *Orchestrator) overflowSink(cfg BotRuntimeConfig, msg InboundMessage) OverflowSink {
	return func(fullText string) string {
		dir := filepath.Join(cfg.WorkspaceDir, ".bot-outbox")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			o.log.Error("bot outbox 目录创建失败", zap.Error(err))
			return ""
		}
		name := fmt.Sprintf("%d-%s.md", time.Now().UnixMilli(), shortID(msg.MessageID))
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(fullText), 0o644); err != nil {
			o.log.Error("bot 超长全文落盘失败", zap.Error(err))
			return ""
		}
		return path
	}
}

// shortID 消息 id 前 8 位（文件名用）；不足 8 位原样。
func shortID(id string) string {
	return SanitizeFileName(id[:min(len(id), 8)], "msg")
}

// SanitizeFileName 文件名净化：仅保留字母数字与 -_.（跨平台安全），空结果回落 fallback。
// 核心与渠道适配器共用（wecom 附件落盘同名过滤），单一 SSOT。
func SanitizeFileName(s, fallback string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return fallback
	}
	return b.String()
}

// Stop 优雅停止：关队列（停止受理）→ 取消生命周期 ctx（杀在途回合）→ 等 worker 收尾。
func (o *Orchestrator) Stop() {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return
	}
	o.closed = true
	for _, ch := range o.queues {
		close(ch)
	}
	o.mu.Unlock()
	o.cancel()
	o.wg.Wait()
}
