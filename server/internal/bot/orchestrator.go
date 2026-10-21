package bot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

/*
 * msgAlreadySeenText 重投已处理消息的统一终帧文案（gate 快路径 / 交互拦截步 / runTurn
 * 权威复查三处共用）。
 */
const msgAlreadySeenText = "该消息已处理过，重复推送已忽略。"

/*
 * turnJob 一个待执行回合。reply 是受理时已开出并发过占位帧的回复流——企微 5 秒被动回复
 * 窗口在受理瞬间被占住，排队多久都不超窗（写者交接恒单写者：占位帧 → 状态帧 → 回复泵）。
 * freshSession 闲置过期产物：本轮以新会话启动（上一会话锚已备份，/继续 可接回）。
 */
type turnJob struct {
	cfg          BotRuntimeConfig
	msg          InboundMessage
	reply        ReplyStream
	freshSession bool
}

/*
 * Orchestrator 编排核心：入站受理（白名单/快路径/占位帧）→ 每会话串行队列 → prompt → claude 回合 → 回复泵 → 会话持久化。
 * 无渠道知识；HandleInbound 由适配器回调（supervisor 装配）。gate 为审批数字快路径的可选依赖（T2.4；
 * nil = 关闭）；interactions 为卡片交互消费者的可选依赖（T3.1 通用拦截骨架；nil = 仅兜底文案）。
 */
type Orchestrator struct {
	store        *ConversationStore
	driver       ClaudeDriver
	gate         pendingGate
	interactions interactionHandler
	log          *zap.Logger

	/*
	 * Deliveries 会话内文件投递注册表（装配根注入；nil = 投递链路禁用，既有测试零改动）。
	 * 投递渠道每回合登记 token → ArtifactReplyStream，MCP im 投递端点经 token 取回推送。
	 */
	Deliveries *DeliveryRegistry

	ctx         context.Context // 生命周期 ctx：Stop 级联取消全部在途回合（杀 claude 子进程）
	cancel      context.CancelFunc
	mu          sync.Mutex
	queues      map[string]chan turnJob       // key: <botID>:<conversationKey>；首消息惰性建，进程期常驻
	turnCancels map[string]context.CancelFunc // key 同上；在途回合取消句柄（/stop /clear 拦截步消费）
	busy        map[string]bool               // key 同上；worker 执行中标记（排队占位文案分流）
	turns       chan struct{}                 // 全局并发回合信号量（maxConcurrentTurns 个槽位）
	wg          sync.WaitGroup
	closed      bool
}

/*
 * NewOrchestrator 构造编排器。gate / interactions 可为 nil（无 ACP 会话域的装配与既有
 * 测试；interactions nil = 卡片点击仅回兜底文案，T3.2 换 driverRoute 第三角色注入）。
 */
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
		turnCancels:  make(map[string]context.CancelFunc),
		busy:         make(map[string]bool),
		turns:        make(chan struct{}, maxConcurrentTurns),
	}
}

/*
 * HandleInbound 入站唯一入口（适配器回调 goroutine 上执行，须快进快出）。
 * 固定时序：白名单判定 → （拒绝：direct 回提示帧 / group 静默）→ 受理开流 → 卡片交互拦截步（恒终帧收口，不入队）→ 审批数字快路径（命中即终帧收口，不入队）→ 占位帧 → 入队。
 * openStream 由适配器提供（闭包持有渠道连接），核心决定是否/何时调用——策略拒绝与群聊静默不产生任何流。
 */
func (o *Orchestrator) HandleInbound(cfg BotRuntimeConfig, msg InboundMessage, openStream func() (ReplyStream, error)) {
	allowed, reason := EvaluateAccess(cfg.AccessPolicy, msg.SenderID)
	if !allowed {
		o.log.Warn("bot 消息被访问策略拒绝",
			zap.String("botID", cfg.BotID), zap.String("conversation", msg.ConversationKey),
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
		o.log.Error("bot 开启回复流失败", zap.String("botID", cfg.BotID), zap.Error(err))
		return
	}
	/*
	 * 卡片交互拦截步（T3.1 通用拦截骨架）：点击不承载回合正文（企微模板卡无自由文本控件），组 prompt 出回合无意义——恒在此终结（handled 与否都不入队、不占并发槽，不回落主路径）。
	 * 幂等前置到判定之前——交互应答在 T3.2 是变更性动作（RespondPermission CAS + 置灰更新）却不入队，与 gate 快路径同款防御；兜底处理也 MarkSeen（防重投重复打扰）。
	 * 置灰更新须以点击事件帧 headers 开出的本流发起（企微 5 秒窗口内），故 update 在终帧之前应用；流无卡能力（纯文本渠道）静默跳过，置灰失败仅告警不阻断终帧。
	 */
	if msg.Interaction != nil {
		if o.seenOrFlush(cfg, msg.ConversationKey, msg.MessageID, rs, "交互拦截") {
			return
		}
		text, update, handled := interactionFallbackText(), (*CardSpec)(nil), false
		if o.interactions != nil {
			text, update, handled = o.interactions.TryHandleInteraction(cfg, msg)
		}
		if !handled {
			text = interactionFallbackText()
		}
		o.markSeen(cfg, msg.ConversationKey, msg.MessageID)
		if update != nil {
			/*
			 * 混合承载（卡面+推送）：置灰帧 desc 覆写为应答首行——结果在卡面原地可见
			 * （渠道文案截断兜底），完整文案随终帧推送（企微事件流终帧走 aibot_send_msg，
			 * 事件 req_id 不可用于 respond_msg）。
			 */
			if text != "" {
				update.Description = firstLine(text)
			}
			if cs, ok := rs.(CardReplyStream); ok {
				if err := cs.UpdateCard(*update); err != nil {
					o.log.Warn("bot 卡片置灰失败", zap.String("botID", cfg.BotID), zap.Error(err))
				}
			}
		}
		_ = rs.Flush(text, true)
		return
	}
	/*
	 * 审批数字快路径（T2.4）：审批应答消息入队会排在被 pending 挂起的回合之后死锁，必须在占位帧之前拦截；命中即该消息的终帧（不入队、不占并发槽），未命中落回主路径。
	 * 幂等前置到判定之前——快路径产生变更性动作（RespondPermission CAS）却不入队，错过 runTurn 的权威幂等复查；
	 * 渠道重投同 msgid 的数字消息不得二次应答（重放窗口内新开挂起会被误答）。未命中消息的幂等仍由 runTurn 复查收口（此处 HasSeen 结果弃用）。
	 */
	if o.gate != nil {
		if o.seenOrFlush(cfg, msg.ConversationKey, msg.MessageID, rs, "快路径") {
			return
		}
		if text, handled := o.gate.TryRespondPending(cfg, msg); handled {
			o.markSeen(cfg, msg.ConversationKey, msg.MessageID)
			_ = rs.Flush(text, true)
			return
		}
	}
	/*
	 * 绑定卡编号快路径（无卡渠道的绑定选择降级）：纯数字 + 本流无卡能力 + 会话槽内未消费
	 * 绑定族卡 → 合成卡片点击交互走统一消费链，命中即终帧收口（不入队不占并发槽）；未命中
	 * 落回主路径。幂等前置与数字快路径同款——本路径变更状态（绑定落库）却不入队。
	 */
	if o.interactions != nil {
		if n, ok := parseNumberedSelection(msg.Text); ok {
			if _, isCard := rs.(CardReplyStream); !isCard {
				if in, ok := o.synthNumberedInteraction(cfg, msg, n); ok {
					msg.Interaction = in
					if o.seenOrFlush(cfg, msg.ConversationKey, msg.MessageID, rs, "编号快路径") {
						return
					}
					if text, _, handled := o.interactions.TryHandleInteraction(cfg, msg); handled {
						o.markSeen(cfg, msg.ConversationKey, msg.MessageID)
						_ = rs.Flush(text, true)
						return
					}
				}
			}
		}
	}
	/*
	 * 控制指令拦截步（/stop /clear /继续）：打断在途回合/变更会话锚点却不承载正文，恒在
	 * 受理现场终帧收口（不入队——排队执行会让 /stop 失去即时性，且被打断的正是本队列）。
	 * 幂等前置同快路径——本路径变更状态却不过 runTurn 的权威复查。
	 */
	if token := parseSlashCommand(msg.Text); token == slashStopToken || token == slashClearToken || token == slashResumeToken {
		if o.seenOrFlush(cfg, msg.ConversationKey, msg.MessageID, rs, "控制指令") {
			return
		}
		text := o.applyControlCommand(cfg, msg.ConversationKey, token)
		o.markSeen(cfg, msg.ConversationKey, msg.MessageID)
		_ = rs.Flush(text, true)
		return
	}
	/*
	 * 会话闲置边界（2h）：命中即先发独立提示气泡（会话结束提醒 + 新会话标题 + /继续 指引），
	 * 再走正文流——「先提示、后正文」的消息序。headless 本轮回合以新会话启动（锚已备份，
	 * /继续 可接回）；ACP 共享会话（桌面共用）不切断、仅提醒。
	 */
	freshSession := false
	if _, expired, err := o.store.ExpireIdleSession(cfg.BotID, msg.ConversationKey, time.Now()); err != nil {
		o.log.Error("bot 会话闲置判定失败（按未过期处理）", zap.Error(err))
	} else if expired {
		acp := o.isACPRoute(cfg, msg.ConversationKey)
		if ns, ok := rs.(NoticeReplyStream); ok {
			if acp {
				_ = ns.SendNotice("⏰ 距上次对话已超过 2 小时，会话仍在延续。回复 /help 可查看支持的指令。")
			} else {
				_ = ns.SendNotice("⏰ 距上次对话已超过 2 小时，上一会话已结束。已开启新会话《" + sessionTitle(msg.Text) + "》；要继续上次话题，回复 /继续。")
			}
		}
		freshSession = !acp
	}
	// 活动时间戳顺延（受理即触碰）：ACP 等无锚回写链路的唯一时基来源，防闲置误报逐条重放。
	if err := o.store.TouchLastMessage(cfg.BotID, msg.ConversationKey, time.Now()); err != nil {
		o.log.Warn("bot 会话活动时间戳写入失败", zap.Error(err))
	}
	// 占位帧走 quote 样式（思考面板同款），标识非正文。
	placeholder := quoteAware(rs, "收到，处理中…")
	if o.queueBusy(queueKey(cfg.BotID, msg.ConversationKey)) {
		placeholder = quoteAware(rs, "收到。当前有任务处理中，排队中，稍后自动继续…")
	}
	if err := rs.Flush(placeholder, false); err != nil {
		o.log.Warn("bot 占位帧发送失败（继续回合）", zap.Error(err))
	}

	if err := o.enqueue(cfg, msg, rs, freshSession); err != nil {
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

// seenOrFlush 幂等闸：已见即终帧回统一文案并返回 true；查询失败按放行处理（what 为放行场景标注）。
func (o *Orchestrator) seenOrFlush(cfg BotRuntimeConfig, key, msgID string, rs ReplyStream, what string) bool {
	seen, err := o.store.HasSeen(cfg.BotID, key, msgID)
	if err != nil {
		o.log.Error("bot 幂等查询失败（放行"+what+"）", zap.Error(err))
	}
	if seen {
		_ = rs.Flush(msgAlreadySeenText, true)
		return true
	}
	return false
}

// markSeen 幂等收口：登记已处理（拦截步/权威复查共用），失败仅告警不阻断。
func (o *Orchestrator) markSeen(cfg BotRuntimeConfig, key, msgID string) {
	if err := o.store.MarkSeen(cfg.BotID, key, msgID); err != nil {
		o.log.Error("bot markSeen 失败", zap.Error(err))
	}
}

/*
 * synthNumberedInteraction 把编号选择合成为绑定卡点击交互（DeliveryID/TaskID = 槽内最近卡
 * 锚，编号越界交卡域消费链的越界防御收口）。ok=false = 无槽/槽已消费/非绑定族——落回主路径。
 */
func (o *Orchestrator) synthNumberedInteraction(cfg BotRuntimeConfig, msg InboundMessage, n int) (*Interaction, bool) {
	lc, ok := o.store.LastMessageCard(cfg.BotID, msg.ConversationKey)
	if !ok || lc.Status == lastMessageCardProcessed || bindingCardKind(lc.Spec.TaskID) == "" {
		return nil, false
	}
	return &Interaction{DeliveryID: lc.Spec.TaskID, ActionIndex: n - 1, TaskID: lc.Spec.TaskID}, true
}

// bindingKind 绑定卡域别（sendBindingCard 的候选查询与文案分派因子）。
type bindingKind int

const (
	bindingWorkspace bindingKind = iota
	bindingIssue
)

/*
 * routeProber 路由探针消费面（T2.2 探针步）：不改状态地探明本会话下一回合会落的引擎与
 * 绑定目标。driverRoute 实现（ProbeTarget）；编排器对 o.driver 类型断言获取——headless
 * 直连装配与测试替身未实现时探针步自然跳过，主路径行为不变。
 */
type routeProber interface {
	ProbeTarget(cfg BotRuntimeConfig, conversationKey string) (acp bool, boundIssueID string, err error)
}

/*
 * workspaceGateLead 门禁指引（T2.2）：说明回合为何未启动。中性措辞不预设出卡——lead 是
 * 纯文本前缀，后接域内文本（出卡时的完整名称列表 / 库空教学 / 读库失败文案）。
 */
const workspaceGateLead = "机器人尚未选择工作空间，请选择归属工作空间后重新发送消息；也可在应用「IM 机器人」页编辑机器人。"

/*
 * sendCardOrFlush 出卡统一收口：无卡能力直接纯文本终帧；先登记 last_message_card（写库失败
 * 不出卡——点击消费闸 SSOT，防「刚收到的卡一点就提示已失效」），SendCard 终帧，任一失败
 * 回落 Flush 纯文本（渠道协议「卡同消息仅一次」，回落语义由卡契约保证）。
 */
func (o *Orchestrator) sendCardOrFlush(cfg BotRuntimeConfig, key, kindName string, spec CardSpec, text string, rs ReplyStream) {
	cs, ok := rs.(CardReplyStream)
	if !ok {
		_ = rs.Flush(text, true)
		return
	}
	if err := o.store.SaveLastMessageCard(cfg.BotID, key, kindName, spec); err != nil {
		o.log.Error("bot 卡登记失败（回落纯文本）", zap.String("botID", cfg.BotID), zap.String("kind", kindName), zap.Error(err))
	} else if err := cs.SendCard(spec, text, true); err != nil {
		o.log.Warn("bot 卡发送失败（回落纯文本）", zap.String("botID", cfg.BotID), zap.String("kind", kindName), zap.Error(err))
	} else {
		return
	}
	_ = rs.Flush(text, true)
}

/*
 * sendBindingCard 绑定卡统一收口（T2.2 统一「搜索 → 卡片 → 提交绑定」模型，指令步/门禁/ 探针步三入口的唯一出卡口）：查候选 → 组域内文本（出卡同帧完整名称列表 / 零候选教学文案/ 读库失败文案）→ lead 非空（门禁语境）时指引段前置于一切形态 → 出卡收口走 sendCardOrFlush。
 * 恒终帧，调用方一律终结本条消息（不入队、不占全局信号量）。
 */
func (o *Orchestrator) sendBindingCard(cfg BotRuntimeConfig, msg InboundMessage, kind bindingKind, keyword, lead string, rs ReplyStream) {
	var spec CardSpec
	var has bool
	var text string
	_, cardCapable := rs.(CardReplyStream) // 无卡渠道：编号文本渲染，选择走编号快路径
	switch kind {
	case bindingWorkspace:
		wss, err := bindingWorkspaces(o.store.DB, keyword)
		if err != nil {
			o.log.Error("bot workspace 绑定候选查询失败",
				zap.String("botID", cfg.BotID), zap.String("keyword", keyword), zap.Error(err))
			text = workspaceLookupFailedText
			break
		}
		spec, has = workspaceCardSpec(wss, keyword)
		if has {
			text = workspaceCardText(keyword, wss)
			if !cardCapable {
				text = workspaceNumberedText(keyword, wss)
			}
		} else {
			text = workspaceEmptyText(keyword)
		}
	case bindingIssue:
		issues, err := bindingIssueCandidates(o.store.DB, cfg.WorkspaceID, keyword)
		if err != nil {
			o.log.Error("bot 任务绑定候选查询失败",
				zap.String("botID", cfg.BotID), zap.String("keyword", keyword), zap.Error(err))
			text = issueLookupFailedText
			break
		}
		spec, has = issueCardSpec(issues, keyword)
		if has {
			text = issueCardText(keyword, issues)
			if !cardCapable {
				text = issueNumberedText(keyword, issues)
			}
		} else {
			text = issueEmptyText(keyword)
		}
	}
	if lead != "" {
		text = lead + "\n\n" + text
	}
	if has {
		kindName := "wsbind"
		if kind == bindingIssue {
			kindName = "taskbind"
		}
		o.sendCardOrFlush(cfg, msg.ConversationKey, kindName, spec, text, rs)
		return
	}
	_ = rs.Flush(text, true)
}

/*
 * sendUnbindCard 解绑确认卡出口（#任务解绑 指令步，出卡收口走 sendCardOrFlush）：读当前绑定 →
 * 未绑定纯文本教学终帧 / 悬空锚按未绑定收口（issue 删除级联清列已交付，行缺失是防御分支）。
 * 解绑动作唯一入口是 taskunbind 卡点击（binding_card.go 域内消费）。恒终帧收口。
 */
func (o *Orchestrator) sendUnbindCard(cfg BotRuntimeConfig, msg InboundMessage, rs ReplyStream) {
	bound, err := o.store.BoundIssueID(cfg.BotID, msg.ConversationKey)
	if err != nil {
		o.log.Error("bot 任务绑定读取失败", zap.String("botID", cfg.BotID), zap.Error(err))
		_ = rs.Flush(issueUnbindFailedText(), true)
		return
	}
	if bound == "" {
		_ = rs.Flush(issueNotBoundText(), true)
		return
	}
	issue, found, err := issueRowByID(o.store.DB, bound)
	if err != nil {
		o.log.Error("bot 解绑卡任务行读取失败", zap.String("botID", cfg.BotID), zap.Error(err))
		_ = rs.Flush(issueUnbindFailedText(), true)
		return
	}
	if !found {
		_ = rs.Flush(issueNotBoundText(), true) // 悬空锚视为未绑定
		return
	}
	spec := taskunbindCardSpec(issue)
	text := taskunbindCardText(issue)
	if _, ok := rs.(CardReplyStream); !ok {
		text = taskunbindNumberedText(issue)
	}
	o.sendCardOrFlush(cfg, msg.ConversationKey, "taskunbind", spec, text, rs)
}

// enqueue 入队（不存在则惰性建会话队列 + worker）。已停止 / 队列满 fail closed。
func (o *Orchestrator) enqueue(cfg BotRuntimeConfig, msg InboundMessage, rs ReplyStream, freshSession bool) error {
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
	case ch <- turnJob{cfg: cfg, msg: msg, reply: rs, freshSession: freshSession}:
		return nil
	default:
		return errQueueFull
	}
}

// worker 会话串行消费者：逐 job 执行回合（busy 标记供排队占位文案分流）；Stop 关闭队列后自然退出。
func (o *Orchestrator) worker(ch chan turnJob) {
	defer o.wg.Done()
	for job := range ch {
		key := queueKey(job.cfg.BotID, job.msg.ConversationKey)
		o.mu.Lock()
		o.busy[key] = true
		o.mu.Unlock()
		o.runTurn(job)
		o.mu.Lock()
		delete(o.busy, key)
		o.mu.Unlock()
	}
}

// queueKey 队列键。
func queueKey(botID string, conversationKey string) string {
	return botID + ":" + conversationKey
}

/*
 * runTurn 执行单回合（会话内串行），固定时序：Stop 短路径 → per-turn ctx → 幂等复查 →
 * markSeen → 各指令步/门禁/探针步（均终帧收口，不出回合不占信号量）→ load session → 组
 * prompt → RunTurn → PumpReply → 持久化（含 resume 失效自愈）。残余 job 走停止短路径。
 */
func (o *Orchestrator) runTurn(job turnJob) {
	// Stop 短路径：ctx 已取消（含 Stop 后队列残余 job）——只回停止文案，不碰会话锚点。
	if o.ctx.Err() != nil {
		_ = job.reply.Flush("机器人已停止服务。", true)
		return
	}

	cfg, msg := job.cfg, job.msg
	key := msg.ConversationKey
	qKey := queueKey(cfg.BotID, key)

	/*
	 * per-turn 可取消 ctx：注册/注销随回合生命周期（/stop /clear 打断的落点）；
	 * defer 注销兜底全部退出路径。
	 */
	turnCtx, cancel := context.WithCancel(o.ctx)
	o.mu.Lock()
	o.turnCancels[qKey] = cancel
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		delete(o.turnCancels, qKey)
		o.mu.Unlock()
		cancel()
	}()

	// 权威幂等复查（受理与执行之间可能已有同 msgid 入队）。
	if o.seenOrFlush(cfg, key, msg.MessageID, job.reply, "执行") {
		return
	}
	o.markSeen(cfg, key, msg.MessageID)

	/*
	 * #帮助 指令步（置于全部绑定指令与门禁之前）：帮助不依赖任何前置状态（未选工作空间
	 * 时恰是用户最需要指令列表的时刻），纯文本终帧收口。
	 */
	if parseHelpCommand(msg.Text) {
		_ = job.reply.Flush(helpText(), true)
		return
	}

	/*
	 * 未识别 slash 指令步：/ 顶格纯字母 token 且非已知——指回 /help 终帧收口
	 * （非指令意图的英文正文不命中形态，照常出回合）。
	 */
	if parseUnknownSlashCommand(msg.Text) {
		_ = job.reply.Flush("未识别的指令。回复 /help 查看支持的指令。", true)
		return
	}

	/*
	 * #工作空间 指令步（T2.2，置于门禁之前）：workspace 搜索不依赖已选工作空间（未选域
	 * 恰是该指令要解决的场景），恒终帧收口（出卡/空态文案）。
	 */
	if cmd := parseWorkspaceCommand(msg.Text); cmd != nil {
		o.sendBindingCard(cfg, msg, bindingWorkspace, cmd.Keyword, "", job.reply)
		return
	}

	/*
	 * #任务解绑 指令步（置于门禁之前）：清绑定锚不依赖已选工作空间（未选域的 bot 也要
	 * 能解绑历史锚点），出确认卡终帧收口（解绑动作唯一入口是 taskunbind 卡点击）。
	 */
	if parseIssueUnbindCommand(msg.Text) {
		o.sendUnbindCard(cfg, msg, job.reply)
		return
	}

	/*
	 * 工作空间未选择（扫码接入先建 bot 后补选的语义）：连接照常、回合不启动——已
	 * markSeen（消息已被处理），不算失败、不触发会话自愈。T2.2：指引文本与 workspace
	 * 绑定卡同帧（点卡即补选，免开桌面端）；无可选 workspace/查询失败保纯文本指引。
	 */
	if strings.TrimSpace(cfg.WorkspaceDir) == "" {
		o.sendBindingCard(cfg, msg, bindingWorkspace, "", workspaceGateLead, job.reply)
		return
	}

	// #任务 指令步（T2.3 → T2.2 统一卡片化）：搜索出卡，恒终帧收口。
	if cmd := parseIssueCommand(msg.Text); cmd != nil {
		o.sendBindingCard(cfg, msg, bindingIssue, cmd.Keyword, "", job.reply)
		return
	}

	/*
	 * ACP 未绑探针步（T2.2）：ACP 档且本会话未绑 issue 时先递任务绑定卡终帧收口（不出回合——ACP 引擎对未绑定的同步失败只是一句引导文案，白撞一次回合没有意义）。
	 * 探针读库失败不拦截（落回主路径，引擎侧 RunTurn 以同款错误 fail closed 收口）；
	 * driver 未实现探针接口（headless 直连装配/测试替身）同样跳过。
	 */
	if prober, ok := o.driver.(routeProber); ok {
		if acp, bound, err := prober.ProbeTarget(cfg, key); err == nil && acp && bound == "" {
			o.sendBindingCard(cfg, msg, bindingIssue, "", "", job.reply)
			return
		}
	}

	// 闲置过期轮次以新会话启动（受理时锚已备份），跳过 resume 锚读取。
	var prevSessionID string
	if !job.freshSession {
		var err error
		prevSessionID, err = o.store.SessionID(cfg.BotID, key)
		if err != nil {
			o.log.Error("bot 会话读取失败（按新会话处理）", zap.Error(err))
		}
	}

	// 超长全文落盘：.bot-outbox/<ts>-<msgid 前 8 位>.md，帧内附路径。
	sink := o.overflowSink(cfg, msg)

	/*
	 * 产物交付目录（仅 ArtifactReplyStream 渠道启用）：回合内 claude 把待交付文件写进
	 * <workspaceDir>/.bot-outbox/delivery/<nonce>/ 顶层，回合成功后按文件名序逐个投递，
	 * 收尾整目录清理（defer 覆盖失败路径）；wecom 等未实现该接口的渠道零变化。
	 */
	var ars ArtifactReplyStream
	var deliveryDir, deliveryToken string
	if a, ok := job.reply.(ArtifactReplyStream); ok {
		ars = a
		nonce := fmt.Sprintf("%d-%s", time.Now().UnixMilli(), shortID(msg.MessageID))
		dir := filepath.Join(cfg.WorkspaceDir, ".bot-outbox", "delivery", nonce)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			o.log.Warn("bot 交付目录创建失败（本轮跳过产物投递）", zap.Error(err))
		} else {
			deliveryDir = dir
			defer os.RemoveAll(deliveryDir) // 投递完成/回合失败/Stop 取消统一清理
			if o.Deliveries != nil {
				/*
				 * 回合内投递接线（任务 8）：nonce 兼作 MCP im 投递端点的 token，回合收尾注销；
				 * MCP 工具经它取回本投递面，把任意路径文件即时推给用户（不占流）。
				 */
				deliveryToken = nonce
				o.Deliveries.Register(nonce, a)
				defer o.Deliveries.Unregister(nonce)
			}
		}
	}
	systemPrompt := ComposeSystemPrompt()
	if deliveryDir != "" {
		systemPrompt += deliveryPromptSuffix(deliveryDir)
	}

	/*
	 * 执行期状态帧：填充占位帧到首个流式内容之间的静默期（排队 + 会话冷启动）。此刻
	 * PumpReply 未启动，worker 是回复流唯一写者，交接纪律不破。
	 */
	// 状态词走 quote 样式（思考面板同款），标识非正文。
	if err := job.reply.Flush(quoteAware(job.reply, "已开始处理，正在连接会话…"), false); err != nil {
		o.log.Warn("bot 执行期状态帧发送失败（继续回合）", zap.Error(err))
	}

	/*
	 * 全局并发护栏：拿槽位才 spawn（排队等待可被 Stop 取消）。会话队列已保证同会话串行，
	 * 信号量只约束跨会话的总进程数。
	 */
	select {
	case o.turns <- struct{}{}:
	case <-turnCtx.Done():
		_ = job.reply.Flush("机器人已停止服务。", true)
		return
	}
	prompt := BuildTurnPrompt(msg)
	events, err := o.driver.RunTurn(turnCtx, TurnRequest{
		BotID:           cfg.BotID,
		WorkspaceID:     cfg.WorkspaceID,
		WorkspaceDir:    cfg.WorkspaceDir,
		ConversationKey: key,
		SessionID:       prevSessionID,
		Prompt:          prompt,
		SystemPrompt:    systemPrompt,
		Display:         buildTurnDisplay(msg, string(cfg.Channel)),
		Port:            cfg.Port,
		DeliveryToken:   deliveryToken,
	})
	if err != nil {
		<-o.turns
		// 取消竞态（select 命中槽位后 ctx 才取消）：等同 Stop 短路径，不清锚点。
		if turnCtx.Err() != nil {
			_ = job.reply.Flush("机器人已停止服务。", true)
			return
		}
		/*
		 * spawn 即失败（如 claude 不在场）：终态帧 + 自愈（若带 resume 尝试则清锚点，下条消息开新会话——失败后不复用旧锚点是确定性单步恢复，不做脆弱的文案匹配）。
		 * 引擎受理拒绝（turnNotAcceptedError，如 ACP 模式的引导报错）除外：回合未发生， headless 锚点有效性不受影响，误清会丢既有会话上下文。
		 */
		o.log.Error("bot 回合启动失败", zap.String("botID", cfg.BotID), zap.Error(err))
		_ = job.reply.Flush("❌ 无法启动 claude 会话："+firstLine(err.Error()), true)
		var rejected turnNotAcceptedError
		if !errors.As(err, &rejected) {
			o.healAfterFailure(cfg.BotID, key, prevSessionID)
		}
		return
	}

	outcome := PumpReply(turnCtx, job.reply, events, sink, o.log)
	<-o.turns
	if outcome.Err != nil {
		o.log.Warn("bot 回合失败", zap.String("botID", cfg.BotID),
			zap.String("conversation", key), zap.Error(outcome.Err))
	}
	if outcome.SessionID != "" {
		if err := o.store.FinishTurn(cfg.BotID, key, outcome.SessionID, time.Now()); err != nil {
			o.log.Error("bot 会话回写失败", zap.Error(err))
		}
	}
	/*
	 * 产物投递（仅交付目录就绪且回合成功）：终帧之后按文件名序逐个 SendArtifact——
	 * 失败会话不投（可能是不完整产物），取消（/stop / Stop）同样跳过。
	 */
	if deliveryDir != "" && outcome.Err == nil && turnCtx.Err() == nil {
		o.deliverArtifacts(ars, deliveryDir)
	}
	// 取消（/stop 或 SIGTERM/Stop）导致的回合中断不是会话失效：claude 侧锚点仍可用，不做自愈清除。
	if outcome.Err != nil && turnCtx.Err() == nil {
		o.healAfterFailure(cfg.BotID, key, prevSessionID)
	}
}

/*
 * deliveryPromptSuffix 注入 SystemPrompt 的交付指引（绝对路径 + 约束）：
 * 仅顶层文件参与投递，回合结束自动送达用户。
 */
func deliveryPromptSuffix(dir string) string {
	return "\n\n## 产物交付\n本轮产生的待交付文件（生成物、导出结果等）直接写入目录：" + dir +
		"\n仅写顶层文件（子目录不投递），回合结束时会自动投递给用户，无需自行发送。"
}

// deliverArtifacts 交付目录顶层常规文件按文件名序逐个投递；单个失败 SendNotice 交代后继续。
func (o *Orchestrator) deliverArtifacts(ars ArtifactReplyStream, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		o.log.Warn("bot 交付目录读取失败", zap.Error(err))
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	nonce := filepath.Base(dir)
	for _, name := range names {
		if err := ars.SendArtifact(nonce+"/"+name, name, filepath.Join(dir, name)); err != nil {
			o.log.Warn("bot 产物投递失败", zap.String("file", name), zap.Error(err))
			_ = ars.SendNotice("文件「" + name + "」投递失败：" + firstLine(err.Error()))
		}
	}
}

/*
 * applyControlCommand 控制指令的消费（拦截步专用，恒返回终帧文案）：/stop 打断在途回合 +
 * 排空队列（上下文保留）；/clear 打断 + 清主锚与备份锚；/继续 把备份锚回填主锚。
 */
func (o *Orchestrator) applyControlCommand(cfg BotRuntimeConfig, conversationKey, token string) string {
	switch token {
	case slashStopToken:
		if !o.stopConversation(queueKey(cfg.BotID, conversationKey)) {
			return "当前没有正在处理的回复。"
		}
		return "⏹ 已停止当前回复，会话上下文保留，继续对话即可。"
	case slashClearToken:
		o.stopConversation(queueKey(cfg.BotID, conversationKey))
		if err := o.store.ClearSessionID(cfg.BotID, conversationKey); err != nil {
			o.log.Error("bot 会话锚点清理失败", zap.Error(err))
		}
		if err := o.store.ClearPrevSessionID(cfg.BotID, conversationKey); err != nil {
			o.log.Error("bot 会话备份锚清理失败", zap.Error(err))
		}
		return "🧹 上下文已清空，下一条消息将开启新会话。"
	case slashResumeToken:
		ok, err := o.store.ResumePrevSession(cfg.BotID, conversationKey)
		if err != nil {
			o.log.Error("bot 会话接回失败", zap.Error(err))
			return "❌ 会话接回失败，请稍后重试。"
		}
		if !ok {
			return "没有可接回的上一会话。"
		}
		// 接回即活动：顺延时基，防紧随消息把刚接回的锚再次判过期备份（接回被架空）。
		if err := o.store.TouchLastMessage(cfg.BotID, conversationKey, time.Now()); err != nil {
			o.log.Warn("bot 会话活动时间戳写入失败", zap.Error(err))
		}
		return "🔗 已接回上一会话，继续对话即可。"
	}
	return ""
}

/*
 * stopConversation 打断本会话在途回合（取消 per-turn ctx）并排空队列（被排空消息各自终帧
 * 收口，不再执行）。返回是否确有在途回合被取消。
 */
func (o *Orchestrator) stopConversation(key string) bool {
	o.mu.Lock()
	cancelTurn := o.turnCancels[key]
	var drained []turnJob
	if ch := o.queues[key]; ch != nil {
	draining:
		for {
			select {
			case job := <-ch:
				drained = append(drained, job)
			default:
				break draining
			}
		}
	}
	o.mu.Unlock()
	if cancelTurn != nil {
		cancelTurn()
	}
	for _, job := range drained {
		_ = job.reply.Flush("⏹ 已停止，本条消息未执行。", true)
	}
	return cancelTurn != nil
}

// isACPRoute 探测本会话回合将落的引擎是否 ACP（共享会话域；探测失败按 headless 处理）。
func (o *Orchestrator) isACPRoute(cfg BotRuntimeConfig, conversationKey string) bool {
	prober, ok := o.driver.(routeProber)
	if !ok {
		return false
	}
	acp, _, err := prober.ProbeTarget(cfg, conversationKey)
	return err == nil && acp
}

// queueBusy 会话队列忙碌（worker 执行中或有积压）——排队占位文案分流的依据。
func (o *Orchestrator) queueBusy(key string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.busy[key] || (o.queues[key] != nil && len(o.queues[key]) > 0)
}

// sessionTitle 新会话标题：正文首行前 20 字（超长省略号收尾；空正文回落占位）。
func sessionTitle(text string) string {
	line := firstLine(strings.TrimSpace(text))
	if line == "" {
		return "新会话"
	}
	if rs := []rune(line); len(rs) > 20 {
		return string(rs[:20]) + "…"
	}
	return line
}

/*
 * healAfterFailure 回合失败后的会话自愈：本轮尝试过 resume（prev 非空）→ 清空锚点，
 * 用户重发即开新会话（失败文案已提示重试）。本轮本是新会话则无事可做。
 */
func (o *Orchestrator) healAfterFailure(botID string, conversationKey, prevSessionID string) {
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

/*
 * SanitizeFileName 文件名净化：仅保留字母数字与 -_.（跨平台安全），空结果回落 fallback。
 * 核心与渠道适配器共用（wecom 附件落盘同名过滤），单一 SSOT。
 */
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
