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

// maxConcurrentTurns 全局同时在跑的 claude 回合上限（每回合一个真实 claude 子进程）。
const maxConcurrentTurns = 10

// errQueueFull 会话队列已满。
var errQueueFull = errors.New("排队已满")

// errStopped 编排器已停止（sidecar 退出中），不再受理新消息。
var errStopped = errors.New("编排器已停止")

// msgAlreadySeenText 重投已处理消息的统一终帧文案（gate 快路径 / 交互拦截步 / runTurn
// 权威复查三处共用）。
const msgAlreadySeenText = "该消息已处理过，重复推送已忽略。"

// turnJob 一个待执行回合。reply 是受理时已开出并发过「正在思考…」占位帧的回复流——
// 企微 5 秒被动回复窗口在受理瞬间被占住，排队多久都不超窗（两写者一次交接：适配器在
// 回调 goroutine 发占位帧，此后全部帧只由 worker 的回复泵写）。
type turnJob struct {
	cfg   BotRuntimeConfig
	msg   InboundMessage
	reply ReplyStream
}

// Orchestrator 编排核心：入站受理（白名单/快路径/占位帧）→ 每会话串行队列 → prompt →
// claude 回合 → 回复泵 → 会话持久化。无渠道知识；HandleInbound 由适配器回调
// （supervisor 装配）。gate 为审批数字快路径的可选依赖（T2.4；nil = 关闭）；interactions
// 为卡片交互消费者的可选依赖（T3.1 通用拦截骨架；nil = 仅兜底文案）。
type Orchestrator struct {
	store        *ConversationStore
	driver       ClaudeDriver
	gate         pendingGate
	interactions interactionHandler
	log          *zap.Logger

	ctx    context.Context // 生命周期 ctx：Stop 级联取消全部在途回合（杀 claude 子进程）
	cancel context.CancelFunc
	mu     sync.Mutex
	queues map[string]chan turnJob // key: <botID>:<conversationKey>；首消息惰性建，进程期常驻
	turns  chan struct{}           // 全局并发回合信号量（maxConcurrentTurns 个槽位）
	wg     sync.WaitGroup
	closed bool
}

// NewOrchestrator 构造编排器。gate / interactions 可为 nil（无 ACP 会话域的装配与既有
// 测试；interactions nil = 卡片点击仅回兜底文案，T3.2 换 driverRoute 第三角色注入）。
func NewOrchestrator(store *ConversationStore, driver ClaudeDriver, gate pendingGate, interactions interactionHandler, log *zap.Logger) *Orchestrator {
	ctx, cancel := context.WithCancel(context.Background())
	return &Orchestrator{
		store:        store,
		driver:       driver,
		gate:         gate,
		interactions: interactions,
		log:          log,
		ctx:          ctx,
		cancel:       cancel,
		queues:       make(map[string]chan turnJob),
		turns:        make(chan struct{}, maxConcurrentTurns),
	}
}

// HandleInbound 入站唯一入口（适配器回调 goroutine 上执行，须快进快出）。
// 固定时序：白名单判定 → （拒绝：direct 回提示帧 / group 静默）→ 受理开流 → 卡片交互
// 拦截步（恒终帧收口，不入队）→ 审批数字快路径（命中即终帧收口，不入队）→ 占位帧 → 入队。
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
	// 卡片交互拦截步（T3.1 通用拦截骨架）：点击不承载回合正文（企微模板卡无自由文本
	// 控件），组 prompt 出回合无意义——恒在此终结（handled 与否都不入队、不占并发槽，
	// 不回落主路径）。幂等前置到判定之前——交互应答在 T3.2 是变更性动作（RespondPermission
	// CAS + 置灰更新）却不入队，与 gate 快路径同款防御；兜底处理也 MarkSeen（防重投重复
	// 打扰）。置灰更新须以点击事件帧 headers 开出的本流发起（企微 5 秒窗口内），故 update
	// 在终帧之前应用；流无卡能力（纯文本渠道）静默跳过，置灰失败仅告警不阻断终帧。
	if msg.Interaction != nil {
		seen, err := o.store.HasSeen(cfg.BotID, msg.ConversationKey, msg.MessageID)
		if err != nil {
			o.log.Error("bot 幂等查询失败（放行交互拦截）", zap.Error(err))
		}
		if seen {
			_ = rs.Flush(msgAlreadySeenText, true)
			return
		}
		text, update, handled := interactionFallbackText(), (*CardSpec)(nil), false
		if o.interactions != nil {
			text, update, handled = o.interactions.TryHandleInteraction(cfg, msg)
		}
		if !handled {
			text = interactionFallbackText()
		}
		if err := o.store.MarkSeen(cfg.BotID, msg.ConversationKey, msg.MessageID); err != nil {
			o.log.Error("bot markSeen 失败", zap.Error(err))
		}
		if update != nil {
			// 混合承载（卡面+推送）：置灰帧 desc 覆写为应答首行——结果在卡面原地可见
			// （渠道文案截断兜底），完整文案随终帧推送（企微事件流终帧走 aibot_send_msg，
			// 事件 req_id 不可用于 respond_msg）。
			if text != "" {
				update.Description = firstLine(text)
			}
			if cs, ok := rs.(CardReplyStream); ok {
				if err := cs.UpdateCard(*update); err != nil {
					o.log.Warn("bot 卡片置灰失败", zap.Int("botID", cfg.BotID), zap.Error(err))
				}
			}
		}
		_ = rs.Flush(text, true)
		return
	}
	// 审批数字快路径（T2.4）：审批应答消息入队会排在被 pending 挂起的回合之后死锁，
	// 必须在占位帧之前拦截；命中即该消息的终帧（不入队、不占并发槽），未命中落回主路径。
	// 幂等前置到判定之前——快路径产生变更性动作（RespondPermission CAS）却不入队，错过
	// runTurn 的权威幂等复查；渠道重投同 msgid 的数字消息不得二次应答（重放窗口内新开
	// 挂起会被误答）。未命中消息的幂等仍由 runTurn 复查收口（此处 HasSeen 结果弃用）。
	if o.gate != nil {
		seen, err := o.store.HasSeen(cfg.BotID, msg.ConversationKey, msg.MessageID)
		if err != nil {
			o.log.Error("bot 幂等查询失败（放行快路径）", zap.Error(err))
		}
		if seen {
			_ = rs.Flush(msgAlreadySeenText, true)
			return
		}
		if text, handled := o.gate.TryRespondPending(cfg, msg); handled {
			if err := o.store.MarkSeen(cfg.BotID, msg.ConversationKey, msg.MessageID); err != nil {
				o.log.Error("bot markSeen 失败", zap.Error(err))
			}
			_ = rs.Flush(text, true)
			return
		}
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

// bindingKind 绑定卡域别（sendBindingCard 的候选查询与文案分派因子）。
type bindingKind int

const (
	bindingWorkspace bindingKind = iota
	bindingIssue
)

// routeProber 路由探针消费面（T2.2 探针步）：不改状态地探明本会话下一回合会落的引擎与
// 绑定目标。driverRoute 实现（ProbeTarget）；编排器对 o.driver 类型断言获取——headless
// 直连装配与测试替身未实现时探针步自然跳过，主路径行为不变。
type routeProber interface {
	ProbeTarget(cfg BotRuntimeConfig, conversationKey string) (acp bool, boundIssueID string, err error)
}

// workspaceGateLead 门禁指引（T2.2）：说明回合为何未启动。中性措辞不预设出卡——lead 是
// 纯文本前缀，后接域内文本（出卡时的完整名称列表 / 库空教学 / 读库失败文案）。
const workspaceGateLead = "机器人尚未选择工作空间，请选择归属工作空间后重新发送消息；也可在应用「IM 机器人」页编辑机器人。"

// sendBindingCard 绑定卡统一收口（T2.2 统一「搜索 → 卡片 → 提交绑定」模型，指令步/门禁/
// 探针步三入口的唯一出卡口）：查候选 → 组域内文本（出卡同帧完整名称列表 / 零候选教学文案
// / 读库失败文案）→ lead 非空（门禁语境）时指引段前置于一切形态 → 有卡先登记 last_message_card
// 再出卡（终帧收口；last_message_card 闸的 SSOT——写库失败不出卡，防「刚收到的卡一点就提示已
// 失效」）、无卡或 SendCard 失败回落 Flush 纯文本（卡契约：渠道协议「卡同消息仅一次」，
// 已附卡后的调用返回错误）。恒终帧，调用方一律终结本条消息（不入队、不占全局信号量）。
func (o *Orchestrator) sendBindingCard(cfg BotRuntimeConfig, msg InboundMessage, kind bindingKind, keyword, lead string, rs ReplyStream) {
	var spec CardSpec
	var has bool
	var text string
	switch kind {
	case bindingWorkspace:
		wss, err := bindingWorkspaces(o.store.DB, keyword)
		if err != nil {
			o.log.Error("bot workspace 绑定候选查询失败",
				zap.Int("botID", cfg.BotID), zap.String("keyword", keyword), zap.Error(err))
			text = workspaceLookupFailedText
			break
		}
		spec, has = workspaceCardSpec(wss, keyword)
		if has {
			text = workspaceCardText(keyword, wss)
		} else {
			text = workspaceEmptyText(keyword)
		}
	case bindingIssue:
		issues, err := bindingIssueCandidates(o.store.DB, cfg.WorkspaceID, keyword)
		if err != nil {
			o.log.Error("bot 任务绑定候选查询失败",
				zap.Int("botID", cfg.BotID), zap.String("keyword", keyword), zap.Error(err))
			text = issueLookupFailedText
			break
		}
		spec, has = issueCardSpec(issues, keyword)
		if has {
			text = issueCardText(keyword, issues)
		} else {
			text = issueEmptyText(keyword)
		}
	}
	if lead != "" {
		text = lead + "\n\n" + text
	}
	if has {
		if cs, ok := rs.(CardReplyStream); ok {
			// 先登记后发卡：last_message_card 槽是点击消费闸的 SSOT。写库成功而发卡失败时槽指向未
			// 送达卡——旧卡点击按失效收口，用户重发指令即自愈。
			kindName := "wsbind"
			if kind == bindingIssue {
				kindName = "taskbind"
			}
			if err := o.store.SaveLastMessageCard(cfg.BotID, msg.ConversationKey, kindName, spec); err != nil {
				o.log.Error("bot 绑定卡登记失败（回落纯文本）", zap.Int("botID", cfg.BotID), zap.Error(err))
			} else if err := cs.SendCard(spec, text, true); err != nil {
				o.log.Warn("bot 绑定卡发送失败（回落纯文本）", zap.Int("botID", cfg.BotID), zap.Error(err))
			} else {
				return
			}
		}
	}
	_ = rs.Flush(text, true)
}

// sendUnbindCard 解绑确认卡出口（#任务解绑 指令步，与 sendBindingCard 同款收口形态）：
// 读当前绑定 → 未绑定纯文本教学终帧 / 悬空锚按未绑定收口（issue 删除级联清列已交付，
// 行缺失是防御分支）→ 先登记 last_message_card 再出卡（SendCard 终帧收口；无卡能力或发送失败
// 回落 Flush 纯文本——回落链路解绑不可达，与绑定卡同款降级语义）。解绑动作唯一入口是
// taskunbind 卡点击（binding_card.go 域内消费）。恒终帧，调用方一律终结本条消息（不入队、
// 不占全局信号量）。
func (o *Orchestrator) sendUnbindCard(cfg BotRuntimeConfig, msg InboundMessage, rs ReplyStream) {
	bound, err := o.store.BoundIssueID(cfg.BotID, msg.ConversationKey)
	if err != nil {
		o.log.Error("bot 任务绑定读取失败", zap.Int("botID", cfg.BotID), zap.Error(err))
		_ = rs.Flush(issueUnbindFailedText(), true)
		return
	}
	if bound == "" {
		_ = rs.Flush(issueNotBoundText(), true)
		return
	}
	issue, found, err := issueRowByID(o.store.DB, bound)
	if err != nil {
		o.log.Error("bot 解绑卡任务行读取失败", zap.Int("botID", cfg.BotID), zap.Error(err))
		_ = rs.Flush(issueUnbindFailedText(), true)
		return
	}
	if !found {
		_ = rs.Flush(issueNotBoundText(), true) // 悬空锚视为未绑定
		return
	}
	spec := taskunbindCardSpec(issue)
	text := taskunbindCardText(issue)
	if cs, ok := rs.(CardReplyStream); ok {
		// 先登记后发卡（与 sendBindingCard 同款时序语义）。
		if err := o.store.SaveLastMessageCard(cfg.BotID, msg.ConversationKey, "taskunbind", spec); err != nil {
			o.log.Error("bot 解绑卡登记失败（回落纯文本）", zap.Int("botID", cfg.BotID), zap.Error(err))
		} else if err := cs.SendCard(spec, text, true); err != nil {
			o.log.Warn("bot 解绑卡发送失败（回落纯文本）", zap.Int("botID", cfg.BotID), zap.Error(err))
		} else {
			return
		}
	}
	_ = rs.Flush(text, true)
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

// runTurn 执行单回合（会话内串行）。固定时序：幂等复查 → markSeen → #帮助 指令步 →
// #工作空间 指令步 → #任务解绑 指令步（确认卡）→ 工作空间门禁（指引 + workspace 卡
// 同帧）→ #任务 指令步（统一出卡）→ ACP 未绑探针步 → load session → 组 prompt →
// RunTurn → PumpReply → 持久化（含 resume 失效自愈）。指令步/门禁/探针步均终帧收口：
// 不出回合、不占全局信号量。Stop 后排空的残余 job 走「已停止」短路径，不触发失败自愈
// （取消 ≠ 会话失效）。
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
		_ = job.reply.Flush(msgAlreadySeenText, true)
		return
	}
	if err := o.store.MarkSeen(cfg.BotID, key, msg.MessageID); err != nil {
		o.log.Error("bot markSeen 失败", zap.Error(err))
	}

	// #帮助 指令步（置于全部绑定指令与门禁之前）：帮助不依赖任何前置状态（未选工作空间
	// 时恰是用户最需要指令列表的时刻），纯文本终帧收口。
	if parseHelpCommand(msg.Text) {
		_ = job.reply.Flush(helpText(), true)
		return
	}

	// #工作空间 指令步（T2.2，置于门禁之前）：workspace 搜索不依赖已选工作空间（未选域
	// 恰是该指令要解决的场景），恒终帧收口（出卡/空态文案）。
	if cmd := parseWorkspaceCommand(msg.Text); cmd != nil {
		o.sendBindingCard(cfg, msg, bindingWorkspace, cmd.Keyword, "", job.reply)
		return
	}

	// #任务解绑 指令步（置于门禁之前）：清绑定锚不依赖已选工作空间（未选域的 bot 也要
	// 能解绑历史锚点），出确认卡终帧收口（解绑动作唯一入口是 taskunbind 卡点击）。
	if parseIssueUnbindCommand(msg.Text) {
		o.sendUnbindCard(cfg, msg, job.reply)
		return
	}

	// 工作空间未选择（扫码接入先建 bot 后补选的语义）：连接照常、回合不启动——已
	// markSeen（消息已被处理），不算失败、不触发会话自愈。T2.2：指引文本与 workspace
	// 绑定卡同帧（点卡即补选，免开桌面端）；无可选 workspace/查询失败保纯文本指引。
	if strings.TrimSpace(cfg.WorkspaceDir) == "" {
		o.sendBindingCard(cfg, msg, bindingWorkspace, "", workspaceGateLead, job.reply)
		return
	}

	// #任务 指令步（T2.3 → T2.2 统一卡片化）：搜索出卡，恒终帧收口。
	if cmd := parseIssueCommand(msg.Text); cmd != nil {
		o.sendBindingCard(cfg, msg, bindingIssue, cmd.Keyword, "", job.reply)
		return
	}

	// ACP 未绑探针步（T2.2）：ACP 档且本会话未绑 issue 时先递任务绑定卡终帧收口（不出
	// 回合——ACP 引擎对未绑定的同步失败只是一句引导文案，白撞一次回合没有意义）。探针
	// 读库失败不拦截（落回主路径，引擎侧 RunTurn 以同款错误 fail closed 收口）；driver
	// 未实现探针接口（headless 直连装配/测试替身）同样跳过。
	if prober, ok := o.driver.(routeProber); ok {
		if acp, bound, err := prober.ProbeTarget(cfg, key); err == nil && acp && bound == "" {
			o.sendBindingCard(cfg, msg, bindingIssue, "", "", job.reply)
			return
		}
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
		BotID:           cfg.BotID,
		WorkspaceID:     cfg.WorkspaceID,
		WorkspaceDir:    cfg.WorkspaceDir,
		ConversationKey: key,
		SessionID:       prevSessionID,
		Prompt:          prompt,
		SystemPrompt:    ComposeSystemPrompt(),
		Display:         buildTurnDisplay(msg),
		Port:            cfg.Port,
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
		// 引擎受理拒绝（turnNotAcceptedError，如 ACP 模式的引导报错）除外：回合未发生，
		// headless 锚点有效性不受影响，误清会丢既有会话上下文。
		o.log.Error("bot 回合启动失败", zap.Int("botID", cfg.BotID), zap.Error(err))
		_ = job.reply.Flush("❌ 无法启动 claude 会话："+firstLine(err.Error()), true)
		var rejected turnNotAcceptedError
		if !errors.As(err, &rejected) {
			o.healAfterFailure(cfg.BotID, key, prevSessionID)
		}
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
