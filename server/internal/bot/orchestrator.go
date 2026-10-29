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

	"ocean-harness/server/internal/appdb"
	"ocean-harness/server/internal/dal/enums"

	"go.uber.org/zap"
)

// queueCapacity 每会话排队上限（fail closed：满即拒，绝不静默积压）。
const queueCapacity = 8

// maxConcurrentTurns 全局同时在跑的 claude 回合上限（每回合一个真实 claude 子进程）。
const maxConcurrentTurns = 10

/*
 * BotIncomingPolicy bot 忙时新消息处理策略（应用设置页写入 app.db，编排器受理忙时现读）：
 * queue = 排到队尾 FIFO；priority = 中断在途回合、新消息插队首、原队列保留继续；
 * interrupt = 中断在途回合并排空队列（各收 ⏹ 提示），仅处理最新消息。
 */
type BotIncomingPolicy string

const (
	BotIncomingQueue    BotIncomingPolicy = "queue"
	BotIncomingPriority BotIncomingPolicy = "priority"
	BotIncomingLatest   BotIncomingPolicy = "interrupt"
)

// defaultBotIncomingPolicy 默认档「优先处理（队列保留）」：空值/读失败/非法值统一回落。
const defaultBotIncomingPolicy = BotIncomingPriority

// parseBotIncomingPolicy 值域外回落默认（与前端 appConfig.ts parse 对齐约定）。
func parseBotIncomingPolicy(value string) BotIncomingPolicy {
	switch BotIncomingPolicy(strings.TrimSpace(value)) {
	case BotIncomingQueue, BotIncomingPriority, BotIncomingLatest:
		return BotIncomingPolicy(strings.TrimSpace(value))
	}
	return defaultBotIncomingPolicy
}

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
 * resume/resumePartial 标记暂停恢复轮（priority 抢占）：resume 跳过幂等复查，resumePartial
 * 注入续写上下文（做到一半的内容带入下轮）。
 */
type turnJob struct {
	cfg           BotRuntimeConfig
	msg           InboundMessage
	reply         ReplyStream
	resume        bool
	resumePartial string
}

/*
 * Orchestrator 编排核心：入站受理（白名单/快路径/占位帧）→ 每会话串行队列 → prompt → claude 回合 → 回复泵 → 会话持久化。
 * 无渠道知识；HandleInbound 由适配器回调（supervisor 装配）。gate 为审批数字快路径的可选依赖（T2.4；
 * nil = 关闭）；interactions 为卡片交互消费者的可选依赖（T3.1 通用拦截骨架；nil = 仅兜底文案）。
 */
type Orchestrator struct {
	store        *ConversationStore
	driver       TurnDriver
	gate         pendingGate
	interactions interactionHandler
	log          *zap.Logger

	/*
	 * Deliveries 会话内文件投递注册表（装配根注入；nil = 投递链路禁用，既有测试零改动）。
	 * 投递渠道每回合登记 token → ArtifactReplyStream，MCP im 投递端点经 token 取回推送。
	 */
	Deliveries *DeliveryRegistry

	// AppDbPath Rust 共享 app.db 路径（忙时新消息策略读取；空 = 未注入，策略回落默认）。
	AppDbPath string

	ctx         context.Context // 生命周期 ctx：Stop 级联取消全部在途回合（杀 claude 子进程）
	cancel      context.CancelFunc
	mu          sync.Mutex
	queues      map[string]chan turnJob       // key: <botID>:<conversationKey>；首消息惰性建，进程期常驻
	turnCancels map[string]context.CancelFunc // key 同上；在途回合取消句柄（/stop /clear 拦截步消费）
	pauseSlots  map[string]*pauseNoticeHolder // key 同上；在途回合暂停通知槽（priority 抢占写入）
	busy        map[string]bool               // key 同上；worker 执行中标记（排队占位文案分流）
	turns       chan struct{}                 // 全局并发回合信号量（maxConcurrentTurns 个槽位）
	wg          sync.WaitGroup
	closed      bool
}

/*
 * NewOrchestrator 构造编排器。gate / interactions 可为 nil（无 ACP 会话域的装配与既有
 * 测试；interactions nil = 卡片点击仅回兜底文案，T3.2 换 driverRoute 第三角色注入）。
 */
func NewOrchestrator(store *ConversationStore, driver TurnDriver, gate pendingGate, interactions interactionHandler, log *zap.Logger) *Orchestrator {
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
		pauseSlots:   make(map[string]*pauseNoticeHolder),
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
	 * 控制指令拦截步（/stop /clear）：打断在途回合/变更会话锚点却不承载正文，恒在
	 * 受理现场终帧收口（不入队——排队执行会让 /stop 失去即时性，且被打断的正是本队列）。
	 * 幂等前置同快路径——本路径变更状态却不过 runTurn 的权威复查。
	 */
	if token := parseSlashCommand(msg.Text); token == slashStopToken || token == slashClearToken {
		if o.seenOrFlush(cfg, msg.ConversationKey, msg.MessageID, rs, "控制指令") {
			return
		}
		text := o.applyControlCommand(cfg, msg.ConversationKey, token)
		o.markSeen(cfg, msg.ConversationKey, msg.MessageID)
		_ = rs.Flush(text, true)
		return
	}
	// 活动时间戳顺延（受理即触碰）：ACP 等无锚回写链路的唯一时基来源，防闲置误报逐条重放。
	if err := o.store.TouchLastMessage(cfg, msg.ConversationKey, time.Now()); err != nil {
		o.log.Warn("bot 会话活动时间戳写入失败", zap.Error(err))
	}
	/*
	 * 忙时新消息策略分流（仅忙时读配置，无争用零开销）：priority = 中断在途回合 + 队首插入
	 * （原队列保留，入列即完成，不再走队尾）；interrupt = 中断在途回合 + 排空队列（被排空
	 * 消息各收 ⏹ 提示）；queue = 队尾 FIFO 现状。在途回合的 ⏹ 提示由回复泵取消路径覆盖。
	 */
	qKey := queueKey(cfg.BotID, msg.ConversationKey)
	headEnqueued := false
	interrupting := false
	var enqueueErr error
	if o.queueBusy(qKey) {
		switch o.botIncomingPolicy() {
		case BotIncomingPriority:
			// 先入队后暂停（失败不波及在途回合）：队满拒绝时在途回合照常存活。
			cancelTurn, pause := o.captureTurnHandle(qKey)
			enqueueErr = o.enqueueHead(cfg, msg, rs)
			if enqueueErr == nil {
				if pause != nil && cancelTurn != nil {
					pause.set(pauseNoticeText)
					cancelTurn() // 被抢占回合发 ⏸ 非终帧保持流开启，worker 侧重新入队待恢复
				}
				headEnqueued = true
			}
		case BotIncomingLatest:
			o.stopConversation(qKey)
			interrupting = true
		}
	}

	// 占位帧走 quote 样式（思考面板同款），标识非正文；分支标记判定而非事后探测
	// queueBusy——interrupt 取消后 busy 标记要等 worker 收尾才清除，探测会误报排队文案。
	placeholder := "收到，处理中…"
	switch {
	case headEnqueued:
		placeholder = "收到。上一条回复已暂停，本条将优先处理，完成后自动继续上一条…"
	case interrupting:
		placeholder = "收到。已中断上一条消息的处理，正在处理本条…"
	case enqueueErr == nil && o.queueBusy(qKey):
		placeholder = "收到。当前有任务处理中，排队中，稍后自动继续…"
	}
	if err := rs.Flush(quoteAware(rs, placeholder), false); err != nil {
		o.log.Warn("bot 占位帧发送失败（继续回合）", zap.Error(err))
	}

	if enqueueErr == nil && !headEnqueued {
		enqueueErr = o.enqueue(cfg, msg, rs)
	}
	if enqueueErr != nil {
		text := "当前消息较多，排队已满，请稍后再发。"
		if errors.Is(enqueueErr, errStopped) {
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

// seenOrFlush 幂等闸：已见即终帧回统一文案并返回 true。
func (o *Orchestrator) seenOrFlush(cfg BotRuntimeConfig, key, msgID string, rs ReplyStream, _ string) bool {
	if o.store.HasSeen(cfg, key, msgID) {
		_ = rs.Flush(msgAlreadySeenText, true)
		return true
	}
	return false
}

// markSeen 幂等收口：登记已处理（拦截步/权威复查共用）。
func (o *Orchestrator) markSeen(cfg BotRuntimeConfig, key, msgID string) {
	o.store.MarkSeen(cfg, key, msgID)
}

/*
 * routeProber 路由探针消费面：不改状态地探明本会话下一回合会落的引擎。driverRoute 实现
 * （ProbeTarget）；编排器对 o.driver 类型断言获取——headless 直连装配与测试替身未实现时
 * 探测按 headless 处理，主路径行为不变。
 */
type routeProber interface {
	ProbeTarget(cfg BotRuntimeConfig, conversationKey string) (acp bool, err error)
}

// workspaceGateLead 门禁指引：说明回合为何未启动（工作空间未选择）。
const workspaceGateLead = "机器人尚未选择工作空间，请选择归属工作空间后重新发送消息；也可在应用「IM 机器人」页编辑机器人。"

// taskUnboundGateLead 门禁指引：项目档未关联任务（D8 提示后拦截）。
const taskUnboundGateLead = "本项目机器人尚未关联任务：请在应用「IM 机器人」页编辑机器人并选择要协作的任务后，重新发送消息。"

// enqueue 入队（不存在则惰性建会话队列 + worker）。已停止 / 队列满 fail closed。
func (o *Orchestrator) enqueue(cfg BotRuntimeConfig, msg InboundMessage, rs ReplyStream) error {
	return o.enqueueJob(turnJob{cfg: cfg, msg: msg, reply: rs})
}

// enqueueJob 按 job 入队（resume 轮与普通消息共用通道与容量语义）。
func (o *Orchestrator) enqueueJob(job turnJob) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return errStopped
	}
	key := queueKey(job.cfg.BotID, job.msg.ConversationKey)
	ch, ok := o.queues[key]
	if !ok {
		ch = make(chan turnJob, queueCapacity)
		o.queues[key] = ch
		o.wg.Add(1)
		go o.worker(ch)
	}
	select {
	case ch <- job:
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
	pauseSlot := &pauseNoticeHolder{}
	turnCtx = withPauseNotice(turnCtx, pauseSlot)
	o.mu.Lock()
	o.turnCancels[qKey] = cancel
	o.pauseSlots[qKey] = pauseSlot
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		delete(o.turnCancels, qKey)
		delete(o.pauseSlots, qKey)
		o.mu.Unlock()
		cancel()
	}()

	/*
	 * 权威幂等复查（受理与执行之间可能已有同 msgid 入队）：resume 轮豁免——首跑已登记，
	 * 复查会把恢复轮误判为重投直接收口。
	 */
	if !job.resume {
		if o.seenOrFlush(cfg, key, msg.MessageID, job.reply, "执行") {
			return
		}
		o.markSeen(cfg, key, msg.MessageID)
	}

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
	 * 工作空间未选择（扫码接入先建 bot 后补选的语义）：连接照常、回合不启动——已
	 * markSeen（消息已被处理），不算失败、不触发会话自愈，纯指引文本终帧收口。
	 */
	if strings.TrimSpace(cfg.WorkspaceDir) == "" {
		_ = job.reply.Flush(workspaceGateLead, true)
		return
	}

	// 项目档未关联任务（D8）：提示后拦截——已 markSeen（消息已被处理），纯指引终帧收口，
	// 不算失败、不触发会话自愈。
	if cfg.ConversationType == enums.CONVERSATION_TYPE_PROJECT && strings.TrimSpace(cfg.TaskID) == "" {
		_ = job.reply.Flush(taskUnboundGateLead, true)
		return
	}

	/*
	 * headless 驱动锚二择（读失败按新会话处理）：active → resumeID 续聊；pending → 预置 id
	 * 开新会话（--session-id，jsonl 文件名即预置 id）。ACP 路由不消费。
	 */
	resumeID, presetID, anchorErr := o.store.HeadlessAnchor(cfg, key)
	if anchorErr != nil {
		o.log.Error("bot 会话读取失败（按新会话处理）", zap.Error(anchorErr))
	}

	// 超长全文落盘：.bot-outbox/overflow/<ts>-<msgid 前 8 位>.md，帧内附路径。
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
		dir := filepath.Join(cfg.WorkspaceDir, outboxDirName, "delivery", nonce)
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
	if job.resumePartial != "" {
		prompt += pausedContextSuffix(job.resumePartial)
	}
	events, err := o.driver.RunTurn(turnCtx, TurnRequest{
		BotID:            cfg.BotID,
		WorkspaceID:      cfg.WorkspaceID,
		ConversationType: cfg.ConversationType,
		ProjectID:        cfg.ProjectID,
		TaskID:           cfg.TaskID,
		WorkspaceDir:     cfg.WorkspaceDir,
		ConversationKey:  key,
		SessionID:        resumeID,
		PresetSessionID:  presetID,
		Prompt:           prompt,
		SystemPrompt:     systemPrompt,
		Display:          buildTurnDisplay(msg, string(cfg.Channel)),
		Port:             cfg.Port,
		DeliveryToken:    deliveryToken,
	})
	if err != nil {
		<-o.turns
		/*
		 * 取消竞态（select 命中槽位后 ctx 才取消）：暂停通知在挂 = priority 建立期抢占——
		 * 泵未启动，与泵暂停收口同款（⏸ 非终帧 + 重新入队待恢复），消息不丢；恢复轮不重排
		 * （防抢占反复下的重排环）。其余取消（/stop、编排器停止）等同 Stop 短路径，不清锚点。
		 */
		if turnCtx.Err() != nil {
			if pauseNoticeFromCtx(turnCtx) != "" && !job.resume {
				_ = job.reply.Flush(quoteAware(job.reply, pauseNoticeText), false)
				o.enqueuePaused(job, "")
			} else {
				_ = job.reply.Flush("机器人已停止服务。", true)
			}
			return
		}
		// 引擎同步失败：终态帧收口。受理拒绝（turnNotAcceptedError：ACP 引导失败等，回合未发生）
		// 跳过自愈——误清会丢 headless 既有会话锚点；其余（headless spawn 失败）若带 resume 尝试则清锚自愈。
		o.log.Error("bot 回合启动失败", zap.String("botID", cfg.BotID), zap.Error(err))
		_ = job.reply.Flush(turnFailureReply("❌ 无法启动机器人会话：", err), true)
		var rejected turnNotAcceptedError
		if !errors.As(err, &rejected) {
			o.healAfterFailure(cfg, key, prevAnchorTried(resumeID, presetID))
		}
		return
	}

	outcome := PumpReply(turnCtx, job.reply, events, sink, o.log)
	<-o.turns
	if outcome.Err != nil {
		o.log.Warn("bot 回合失败", zap.String("botID", cfg.BotID),
			zap.String("conversation", key), zap.Error(outcome.Err))
	}
	// 会话锚回写（outcome.SessionID 仅 headless 引擎捕获；ACP 不产出则不触碰）。预置注入
	// 场景校验一致性：捕获值偏离预置 id 以实际值为准（绝对一致铁律），偏离仅告警观测。
	if outcome.SessionID != "" {
		if presetID != "" && outcome.SessionID != presetID {
			o.log.Warn("headless 会话 id 偏离预置锚，以实际值为准",
				zap.String("preset", presetID), zap.String("actual", outcome.SessionID))
		}
		if err := o.store.FinishTurn(cfg, key, outcome.SessionID, time.Now()); err != nil {
			o.log.Error("bot 会话回写失败", zap.Error(err))
		}
	}
	/*
	 * 暂停收口（priority 抢占）：被抢占回合带部分正文重新队尾入列，新消息与原队列之后自动
	 * 恢复（resume 轮在同一回复流上继续流式）；锚已回写，恢复轮 resume 到最新会话。
	 */
	if outcome.Paused {
		o.enqueuePaused(job, outcome.FinalText)
		return
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
		o.healAfterFailure(cfg, key, prevAnchorTried(resumeID, presetID))
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

// pauseNoticeText priority 抢占写入被暂停回复的过渡帧（非终帧，流保持开启待恢复续写）。
const pauseNoticeText = "⏸ 检测到新消息，本回复先暂停，待新消息处理完毕后自动继续。"

/*
 * captureTurnHandle 在 mu 下取在途回合句柄（取消函数 + 暂停通知槽）：priority 档入队成功后
 * 置暂停文案并取消，入队失败（队满/停止）不波及既有回合。
 */
func (o *Orchestrator) captureTurnHandle(key string) (context.CancelFunc, *pauseNoticeHolder) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.turnCancels[key], o.pauseSlots[key]
}

/*
 * enqueueHead 队首插入（priority 档）：mu 下排空存量 → 插入新 job → 按原序放回（存量 FIFO
 * 不变，多次插入自然形成后到先服务）；存量已达 queueCapacity 回落 errQueueFull（fail closed）。
 * 仅忙时调用（忙必有队列）；发送全程持 o.mu（与 enqueue 同款纪律，容量余量保证不阻塞）。
 */
func (o *Orchestrator) enqueueHead(cfg BotRuntimeConfig, msg InboundMessage, rs ReplyStream) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return errStopped
	}
	ch := o.queues[queueKey(cfg.BotID, msg.ConversationKey)]
	if ch == nil {
		return errStopped // 理论不可达（忙必有队列），fail closed 防御 nil 通道发送
	}
	var held []turnJob
drain:
	for {
		select {
		case job := <-ch:
			held = append(held, job)
		default:
			break drain
		}
	}
	if len(held) >= queueCapacity {
		for i := len(held) - 1; i >= 0; i-- {
			ch <- held[i]
		}
		return errQueueFull
	}
	ch <- turnJob{cfg: cfg, msg: msg, reply: rs}
	for i := len(held) - 1; i >= 0; i-- {
		ch <- held[i]
	}
	return nil
}

/*
 * applyControlCommand 控制指令的消费（拦截步专用，恒返回终帧文案）：/stop 打断在途回合 +
 * 排空队列（上下文保留）；/clear 打断 + 清主锚。
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
		if err := o.store.ResetSessionID(cfg, conversationKey); err != nil {
			o.log.Error("bot 会话锚点清理失败", zap.Error(err))
		}
		return "🧹 上下文已清空，下一条消息将开启新会话。"
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

/*
 * enqueuePaused 被暂停回合重新队尾入列（resume 轮）：暂停与新消息插入之间队列容量必有空位，
 * 满时降级终帧收口（理论边缘，fail visible 不静默丢消息）。
 */
func (o *Orchestrator) enqueuePaused(job turnJob, partial string) {
	resumed := job
	resumed.resume = true
	resumed.resumePartial = partial
	if err := o.enqueueJob(resumed); err != nil {
		o.log.Warn("bot 暂停回合恢复入列失败", zap.Error(err))
		_ = resumed.reply.Flush("⏹ 本回复恢复失败（排队已满），请重新发送。", true)
	}
}

// pausedContextSuffix 恢复轮的暂停上下文注入：已输出到一半的内容供衔接，不重复已有内容。
func pausedContextSuffix(partial string) string {
	return "\n\n<paused_context>\n本条消息此前的回复因新消息插队而暂停。以下是你此前已输出到一半的内容（用户已看到），请从中断处自然继续，不要重复已有内容：\n" + partial + "\n</paused_context>"
}

// queueBusy 会话队列忙碌（worker 执行中或有积压）——排队占位文案分流与策略分流的依据。
func (o *Orchestrator) queueBusy(key string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.busy[key] || (o.queues[key] != nil && len(o.queues[key]) > 0)
}

// botIncomingPolicy 忙时新消息策略现读（仅忙时调用，无争用零开销；读失败回落默认）。
func (o *Orchestrator) botIncomingPolicy() BotIncomingPolicy {
	value, err := appdb.ReadAppConfigValue(o.AppDbPath, appdb.KeyBotIncomingPolicy)
	if err != nil {
		return defaultBotIncomingPolicy
	}
	return parseBotIncomingPolicy(value)
}

/*
 * healAfterFailure 回合失败后的 headless 会话自愈：本轮尝试过锚驱动（resume/preset 任一非空）
 * → 锚重置（换新预置 id 回 pending），用户重发即开新会话（失败文案已提示重试）。本轮本是
 * 裸新会话则无事可做。
 */
func (o *Orchestrator) healAfterFailure(cfg BotRuntimeConfig, conversationKey string, anchorTried bool) {
	if !anchorTried {
		return
	}
	if err := o.store.ResetSessionID(cfg, conversationKey); err != nil {
		o.log.Error("bot 会话锚点清理失败", zap.Error(err))
	}
}

// prevAnchorTried 本轮是否尝试过锚驱动（resume/preset 任一在场）。
func prevAnchorTried(resumeID, presetID string) bool {
	return resumeID != "" || presetID != ""
}

// overflowSink 生成超长全文落盘回调（懒创建目录；失败时返回空路径降级为静默截断）。
func (o *Orchestrator) overflowSink(cfg BotRuntimeConfig, msg InboundMessage) OverflowSink {
	return func(fullText string) string {
		dir := filepath.Join(cfg.WorkspaceDir, outboxDirName, "overflow")
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
 * SanitizeFileName 文件名净化：剔除路径分隔符/控制字符/Windows 保留字符，去首尾空白点与
 * 首部 '_-'（防与 msgId 前缀拼出 '-_'/'--' 怪名），保留 Unicode（中文合法）；空结果回落 fallback。
 * 核心与渠道适配器共用，单一 SSOT。
 */
func SanitizeFileName(s, fallback string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/\:*?"<>|`, r) {
			continue
		}
		b.WriteRune(r)
	}
	name := strings.TrimLeft(strings.Trim(b.String(), " ."), "_-")
	if name == "" {
		return fallback
	}
	return name
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
