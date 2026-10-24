package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ocean-harness/server/internal/acpsession"
)

/*
 * acpSessions bot 域消费 acpsession 域的最小面（消费侧缝，惯例同 ClaudeDriver 本身）：*acpsession.Manager 隐式满足（编译期断言紧随定义）；测试以 fake 帧脚本驱动，不依赖真实 Manager 与 agent 进程。
 * 各方法的会话键为 acpsession 会话锚键（issue uuid / conv-<会话行id> 双锚形，语义见 BindingStore）。
 * Ensure 为受理式（同步返回现状快照，spawn 链后台进行），就绪等待与回合输出均由本驱动按帧驱动收集。回合受理走 PromptQueued 排队语义（T2.2 跨入口回合锁）——活动回合中 FIFO 等位、回合空隙自动开跑，与桌面 prompt 入口互斥且有序（桌面即时受理不经队列，用户优先）。
 * Get/RespondPermission 供审批数字快路径（T2.4，pending_gate.go）读写挂起审批，RespondElicitation 供表单卡点击应答（T3.3， permission_card.go）——与桌面 respond* HTTP 面同一 Manager 入口，first-writer-wins 由 acpsession 收敛语义层保证。
 */
type acpSessions interface {
	Ensure(ctx context.Context, sessionKey, pickedLaunchMode string) (acpsession.ViewSnapshot, error)
	Subscribe(sessionKey string) (<-chan acpsession.Frame, func())
	PromptQueued(ctx context.Context, sessionKey, text string, meta acpsession.PromptMeta) error
	Cancel(sessionKey string) error
	Get(sessionKey string) acpsession.ViewSnapshot
	RespondPermission(sessionKey string, pendingID uint64, optionID, source string) error
	RespondElicitation(sessionKey string, pendingID uint64, action string, content map[string]any, source string) error
	// Terminate 主动终结会话（回合失败收尾/活性判死；幂等，语义见 acpsession.Manager.Terminate）。
	Terminate(sessionKey, reason string)
}

// 编译期断言：Manager 满足消费面。
var _ acpSessions = (*acpsession.Manager)(nil)

/*
 * acpDriver ClaudeDriver 的 ACP 实现（T2.1）：bot 回合经 sidecar acpsession 域驱动 claude。会话锚双形（路由层经 TurnRequest.AcpSessionKey 回填）：issue uuid = 与桌面工作台共享同一会话（同一 agent 进程、同一会话视图、同一挂起审批状态，IM 与桌面看到的是同一段对话）；conv- 键 = 未关联任务的 IM 独立会话（cwd 为工作空间根目录），对桌面不可见。与 headless（driver_claude：每回合一个进程，--resume 锚点续聊）的差异：会话 per-锚长驻、无 resume，因此不发 TurnInit、不写 claude_session_id 锚点（ACP session id 与--resume 不兼容）。
 * 权限面（P6）：不消费 bot 级 allowed_tools——会话权限统一由 workspace/issue 的 launch_settings.permissionMode 表达（Ensure 链下发）；
 * 「需要审批」档的挂起审批由桌面端呈现（T1.7），IM 侧交互随 T2.4/T3.2。守则注入（D3，T1.2）：TurnRequest.SystemPrompt（纯固定守则段，提示词定制内容亦经此通道）经回合级 <im_context> 块随受理 prompt 注入（ACP 无 per-turn system prompt 通道；IM 来源回合专属，桌面回合零影响）；会话模型跟随桌面端。
 */
type acpDriver struct {
	sessions acpSessions
	// watchdogIdle/watchdogBudget 看门狗伺服参数（0 = 生产默认；测试装配注入短值驱动判死
	// 时序——阈值经值传递进 collectTurn，不走包级 var 免跨 goroutine 竞争）。
	watchdogIdle   time.Duration
	watchdogBudget time.Duration
}

// NewAcpDriver 构造 ACP 引擎（supervisor 装配）。
func NewAcpDriver(sessions acpSessions) TurnDriver {
	return acpDriver{sessions: sessions}
}

/*
 * RunTurn 实现见 driver.go 契约。受理失败（未绑定 / 配置非法 / 会话起不来 / 排队受理被拒）同步返回 error（复用编排器的启动失败回复路径）；受理成功后异步收集帧直至回合终态。
 * 固定时序：消费路由层回填的目标 issue → Ensure 受理 → 订阅（受理成功后立即订阅——首帧快照新于受理时点，旧会话的失败快照进不来）→ 帧驱动等就绪 → 排队受理（活动回合中 FIFO 等位，回合空隙自动开跑；等位期间会话终结 / ctx 取消则同步报错）→ 受理后重开干净订阅 → 帧循环收集。
 * 两段订阅的分工：第一段只服务等就绪，等位全程挂着不消费（等位窗口里阻塞回合的完整帧序全部作废——重开订阅让 collectTurn 缓冲里只可能有本回合的生命周期帧，外来 turnEnded 误终止、hub 慢订户溢出摘除两个窗口一并消除）。
 */
func (d acpDriver) RunTurn(ctx context.Context, req TurnRequest) (<-chan TurnEvent, error) {
	/*
	 * 会话锚键由路由层解析后回填（routeTarget.SessionKey）；空 = 路由缺陷（正常链路由层
	 * 对 ACP 目标恒给出 conv-/issue 锚键之一），受理拒绝。
	 */
	if req.AcpSessionKey == "" {
		return nil, errors.New("ACP 回合缺少会话锚键（路由层未回填）")
	}
	sessionKey := req.AcpSessionKey
	// bot 恒 ACP（T4.2 单驱动）：显式临场 acp 过门禁——容器 launch_settings.mode 是桌面启动
	// 档位，与 bot 会话通道无关，不得拦截 bot 回合。
	snap, err := d.sessions.Ensure(ctx, sessionKey, "acp")
	if err != nil {
		return nil, err
	}
	frames, stop := d.sessions.Subscribe(sessionKey)
	snap, err = waitSessionReady(ctx, frames, snap)
	if err != nil {
		stop()
		return nil, err
	}
	/*
	 * 排队受理（T2.2）：受理失败（会话终结 / ctx 取消）同步报错，经 driverRoute 包 turnNotAcceptedError，编排器不清锚。
	 * 受理文本 = 围栏全文 + <im_context> 守则块（T1.2）；
	 * meta 来源恒 bot，Display 随 TurnRequest 透传（panel 显示链路：user 条目携带来源标记与展示元数据）。
	 */
	if err := d.sessions.PromptQueued(ctx, sessionKey, acpTurnPrompt(req), acpsession.PromptMeta{Source: acpsession.EntrySourceBot, Display: req.Display}); err != nil {
		stop()
		return nil, err
	}
	stop() // 等位窗口帧全部作废（含受理帧自身——armed 由重开订阅的首帧快照装填）
	turnFrames, stopTurn := d.sessions.Subscribe(sessionKey)
	events := make(chan TurnEvent, 128)
	idle, budget := d.watchdogIdle, d.watchdogBudget
	if idle == 0 {
		idle = watchdogIdleDefault
	}
	if budget == 0 {
		budget = watchdogBudgetDefault
	}
	go collectTurn(ctx, d.sessions, sessionKey, snap.AcpSessionID, turnFrames, stopTurn, events, idle, budget)
	return events, nil
}

/*
 * acpTurnPrompt 组装 ACP 受理全文（T1.2，D3）：围栏 prompt 尾附 <im_context> 块—— SystemPrompt（IM 守则 + 内容边界，ComposeSystemPrompt SSOT 产物；
 * IM 守则 + 内容边界，ComposeSystemPrompt SSOT 产物，提示词定制内容亦经此通道）经 </ 中和防逃逸后随回合注入，仅本引擎（IM 来源回合）生效，桌面回合零影响。
 * SystemPrompt 空则防御性跳过（固定段恒非空，理论不达）。
 */
func acpTurnPrompt(req TurnRequest) string {
	if strings.TrimSpace(req.SystemPrompt) == "" {
		return req.Prompt
	}
	return req.Prompt + "\n\n<im_context>\n" + neutralizeTagClose(req.SystemPrompt) + "\n</im_context>"
}

/*
 * waitSessionReady 帧驱动等会话就绪：从 Ensure 返回的受理快照出发，消费帧直至 ready / failed / terminated / ctx 取消。
 * 订阅后置于 Ensure 受理，首帧快照新于受理时点——状态判定以「受理快照 + 后续状态帧」为准，不会回退到旧会话的失败/终结态（重建路径的旧因不得误报为本回合失败）。
 */
func waitSessionReady(ctx context.Context, frames <-chan acpsession.Frame, snap acpsession.ViewSnapshot) (acpsession.ViewSnapshot, error) {
	for {
		switch snap.Status {
		case acpsession.StatusReady:
			return snap, nil
		case acpsession.StatusFailed, acpsession.StatusTerminated:
			return snap, sessionStateError(snap)
		}
		select {
		case <-ctx.Done():
			return snap, errors.New("等待 ACP 会话就绪被取消")
		case frame, ok := <-frames:
			if !ok {
				return snap, errors.New("ACP 事件流已断开，会话状态未知")
			}
			if s := frameStatus(frame); s != nil {
				/*
				 * 字段合并而非整份替换：sessionStatus/terminated 帧只携带状态投影，保留
				 * 快照其余字段（AcpSessionID 等）免被空值冲掉。
				 */
				snap.Status = s.Status
				snap.Error = s.Error
			}
		}
	}
}

/*
 * frameStatus 提取帧携带的会话状态投影（snapshot / sessionStatus / terminated 三类帧
 * 携带状态；其余帧 nil 不参与就绪等待）。
 */
func frameStatus(frame acpsession.Frame) *acpsession.ViewSnapshot {
	switch frame.Type {
	case acpsession.FrameSnapshot:
		return frame.Snapshot
	case acpsession.FrameSessionStatus, acpsession.FrameTerminated:
		if frame.Status == nil {
			return nil
		}
		return &acpsession.ViewSnapshot{Status: frame.Status.Status, Error: frame.Status.Error}
	}
	return nil
}

// sessionStateError 会话非就绪终态的用户可读错误（failed 落因；terminated 兜底）。
func sessionStateError(snap acpsession.ViewSnapshot) error {
	reason := snap.Error
	if reason == "" {
		reason = "无失败原因"
	}
	if snap.Status == acpsession.StatusFailed {
		return fmt.Errorf("ACP 会话启动失败：%s", reason)
	}
	return fmt.Errorf("ACP 会话已终结：%s", reason)
}

/*
 * ACP 回合活性看门狗生产默认值：连续无任何回合帧超 idle 即软取消（真实长工具执行/限流
 * 退避期间可合法静默，阈值取保守值），死线 budget 内仍无终态帧即判死终结会话。budget 对齐
 * acpsession turnCancelBudget——软取消后 prompt 必在预算内收敛；跨域字面量各自维护。
 */
const (
	watchdogIdleDefault   = 15 * time.Minute
	watchdogBudgetDefault = 10 * time.Second
)

// stopTimer 排空后停止（防已触发残留值再次命中 select 分支）。
func stopTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

// resetTimer 停表排空后重设。
func resetTimer(t *time.Timer, d time.Duration) {
	stopTimer(t)
	t.Reset(d)
}

/*
 * collectTurn 回合输出收集（RunTurn 受理成功后开启；订阅为受理后重开的干净通道，缓冲里只含本回合生命周期帧）。armed 窗口装填：首帧快照 TurnActive（turnStarted 广播早于订阅建立收不到）或收到的 FrameTurnStarted。
 * 终态唯一性：turnEnded/terminated/通道关闭先到先收。差分：回合闸门保证无他回合，agentMessage/agentThought 帧携带本回合累计全量，水位线差分转增量事件（基线经 user 锚从首帧快照装填）。
 * ctx 取消 = 软取消（Manager.Cancel → acp 层强制收口，终态帧必达），不自建超时；取消信号消费一次即摘出 select。
 */
func collectTurn(ctx context.Context, sessions acpSessions, sessionKey, acpSessionID string, frames <-chan acpsession.Frame, stop func(), events chan<- TurnEvent, idle, budget time.Duration) {
	defer close(events)
	defer stop()

	seenTools := map[string]bool{}
	settledTools := map[string]bool{}         // 工具终态去重（completed/failed 覆写帧各只发一次）
	var text string                           // 本回合 agentMessage 累计全量（终态 Result）
	sent := ""                                // 差分水位线：已发增量到达的完整前缀串（校验在 diffWatermark）
	thoughtSent := ""                         // 思考差分水位线（agentThought 滚动摘要）
	var openPendings []acpsession.PendingView // 回合内开放挂起（状态行编号与摘除追踪）
	cardSent := false                         // 本回合卡已出（企微一消息一卡，至多一张——首个可出卡挂起先到先得）
	// elicitCardPendingID 已出表单卡对应的挂起 id（0=未出；次表单虽可表示也不出卡，状态行不得指向已置灰首卡）
	var elicitCardPendingID uint64
	acpSess := acpSessionID // 会话代锚初值来自就绪快照，随后帧快照覆写
	armed := false
	notify := ctx.Done()

	/*
	 * 活性看门狗：帧到达即续期；连续无帧超 idle 先软取消（会话健康时经取消预算收敛出终态帧），
	 * budget 死线内仍无终态帧即判死终结会话（终结帧/关帧随后收口）。死线期普通帧不续期——死线只认终态；
	 * ctx 取消路径停表（acpsession 软取消 giveup 预算保证收敛）。
	 */
	watchdog := time.NewTimer(idle)
	defer watchdog.Stop()
	var watchdogDeadline time.Time // 软取消死线（零值 = 未排）
	// failedReason 非空 = 引擎/协议层失败终态；出口统一终结会话（幂等）——失败后会话不可信，
	// 下条消息 Ensure 重建；cancelled（/stop）不算失败不终结。
	var failedReason string
	defer func() {
		if failedReason != "" {
			sessions.Terminate(sessionKey, failedReason)
		}
	}()

	for {
		select {
		case <-notify:
			sessions.Cancel(sessionKey) // 软取消恰一次；编排器 Stop 后 Manager 可能已收敛，失败则等终态帧/关帧收尾
			notify = nil
			stopTimer(watchdog) // ctx 取消路径由 acpsession giveup 预算保证收敛，看门狗退场
		case <-watchdog.C:
			if watchdogDeadline.IsZero() {
				sessions.Cancel(sessionKey)
				watchdogDeadline = time.Now().Add(budget)
				resetTimer(watchdog, budget)
			} else {
				failedReason = "回合长时间无事件无响应，已强制终结会话"
				sessions.Terminate(sessionKey, failedReason)
			}
		case frame, ok := <-frames:
			if !ok {
				failedReason = "ACP 事件流已断开（会话被关停）"
				events <- TurnEvent{Type: TurnError, Err: errors.New(failedReason)}
				return
			}
			if watchdogDeadline.IsZero() {
				resetTimer(watchdog, idle)
			}
			switch frame.Type {
			case acpsession.FrameSnapshot:
				if frame.Snapshot == nil {
					continue
				}
				/*
				 * 首帧快照（真实 hub 订阅首帧）：TurnActive=true 装填 armed；false = 回合已在订阅建立前收敛（agent 极快；
				 * Subscribe 建立与 PromptText RPC 存在量级差，生产不可达、fake 测试可达），从快照直接合成终态（entries 含全文；
				 * 回合级协议错误不落快照视图，以 StopReason 判 cancelled，其余按正常完成）。快照携带会话代锚（审批卡 TaskID 编码依据）。
				 */
				if id := frame.Snapshot.AcpSessionID; id != "" {
					acpSess = id
				}
				armed = frame.Snapshot.TurnActive
				if !armed {
					failedReason = frame.Snapshot.Error // 非空 = 会话 failed 态（已终，Terminate 幂等 no-op）
					emitTurnDone(events, &acpsession.TurnPayload{
						StopReason: frame.Snapshot.StopReason,
						Error:      frame.Snapshot.Error,
					}, turnAgentText(frame.Snapshot.Entries))
					return
				}
				/*
				 * 基线装填（user 锚过滤）：快照里最后一条 user 条目即本回合 prompt（受理同步写、快照恒含），其后的 agentMessage 才是本回合已产出（订阅建立前落地的理论窗口）——上回合回复不串进基线（对已有历史的会话，受理后 agent 尚无产出是常态，无锚直取最后一条必取到上回合全文）。
				 */
				text = turnAgentText(frame.Snapshot.Entries)
				sent = text
			case acpsession.FrameTurnStarted:
				armed = frame.Turn != nil && frame.Turn.Active
			case acpsession.FrameEntry:
				if !armed || frame.Entry == nil {
					continue
				}
				// kind 取值 SSOT 是 acpsession 视图的 wire 形态（未导出常量；前端同镜像字面量）。
				switch frame.Entry.Kind {
				case "agentMessage":
					/*
					 * 差分流式（T3.4）：帧携带本回合累计全量（视图按 (回合,kind) 聚合、Text 只增长），泵的 TurnText 是增量语义——以已发前缀串差分补发；
					 * 前缀错配（防御：条目覆写换回合的理论态——更短/等长不同/头部不一致）重置全量重发。
					 * 差分不变式在 diffWatermark 单点维护；泵侧正文整回合累计，差分增量纯追加。
					 */
					text = frame.Entry.Text
					delta, nsent := diffWatermark(text, sent)
					sent = nsent
					if delta != "" {
						events <- TurnEvent{Type: TurnText, Delta: delta}
					}
				case "agentThought":
					// 思考增量差分：帧携带累计全量，水位线差分仅在有新增长时发 TurnThinking 增量（组帧在泵侧）。
					delta, nsent := diffWatermark(frame.Entry.Text, thoughtSent)
					thoughtSent = nsent
					if delta != "" {
						events <- TurnEvent{Type: TurnThinking, Delta: delta}
					}
				case "toolCall":
					/*
					 * 工具进度：首见发执行事件（面板工具分区追加一行）；已见工具的终态
					 * 事件发一次、携带工具名（泵就地更新面板对应工具行）。
					 */
					if tc := frame.Entry.ToolCall; tc != nil {
						if !seenTools[tc.ToolCallID] {
							seenTools[tc.ToolCallID] = true
							events <- TurnEvent{Type: TurnToolUse, ToolName: toolDisplayName(tc)}
						}
						if line := toolSettledLine(tc); line != "" && !settledTools[tc.ToolCallID] {
							settledTools[tc.ToolCallID] = true
							events <- TurnEvent{Type: TurnStatus, Status: line, ToolName: toolDisplayName(tc)}
						}
					}
				}
			case acpsession.FramePendingOpened:
				/*
				 * 挂起呈现（T2.4 回合内文本交互 + T3.2 审批卡 + T3.3 表单卡）：permission 出编号状态行（编号与数字快路径同源于视图注册序——快路径按 Get 快照序应答，本处按帧到达序展示，二者同源恒一致），elicitation 按挂起粒度两态（当前挂起可表示且卡位空闲即随状态行出表单卡并提示卡上提交；
				 * 不可表示/卡位被占/首表单已答后的接续表单指回桌面）。
				 * 本回合首个可出卡挂起先到先得（企微一消息一卡，后续挂起回落纯状态行——审批数字应答通道覆盖全部挂起、表单回落桌面应答）。混合挂起合并展示——表单提示附尾，审批的数字应答指引不被覆写。
				 */
				if !armed || frame.Pending == nil {
					continue
				}
				openPendings = append(openPendings, *frame.Pending)
				/*
				 * 出卡尝试（本回合未出过 + 会话代锚在场 + TaskID 过契约校验——sessionKey 含非法
				 * 字符等脏数据、代锚缺失回落纯状态行，审批数字/表单桌面应答兜底仍在）。
				 */
				var card *CardSpec
				if !cardSent && acpSess != "" {
					switch frame.Pending.Kind {
					case "permission":
						if len(frame.Pending.Options) > 0 {
							spec := permissionCardSpec(sessionKey, acpSess, *frame.Pending)
							if ValidateCardTaskID(spec.TaskID) == nil {
								cardSent = true
								card = &spec
							}
						}
					case "elicitation":
						// schema 分类失败（自由输入/越界形态/超上限）即不可表示，不出卡不判错。
						if spec, ok := elicitationCardSpec(sessionKey, acpSess, *frame.Pending); ok && ValidateCardTaskID(spec.TaskID) == nil {
							cardSent, elicitCardPendingID = true, frame.Pending.PendingID
							card = &spec
						}
					}
				}
				/*
				 * 表单状态行按挂起粒度两态：当前挂起即已出卡的表单 → 提示卡上提交；否则指回
				 * 桌面（不可表示 / 卡位被审批卡占用 / 首表单已答后接开的次表单——后者指向首卡
				 * 会误导用户点已置灰卡，decline 探测虽安全但次表单悬空）。
				 */
				elicitLine := elicitPendingDesktopText
				if elicitCardPendingID != 0 && elicitCardPendingID == frame.Pending.PendingID {
					elicitLine = elicitPendingCardText
				}
				perms, hasElicit := splitPendings(openPendings)
				if len(perms) > 0 {
					ev := TurnEvent{Type: TurnStatus, Status: formatPermissionPendingStatus(permissionChoices(perms))}
					if hasElicit {
						ev.Status += "\n" + elicitLine
					}
					ev.Card = card
					events <- ev
				} else {
					events <- TurnEvent{Type: TurnStatus, Status: elicitLine, Card: card}
				}
			case acpsession.FramePendingClosed:
				/*
				 * 已见挂起的关闭：状态行切「已处理，继续执行…」（未见过的关闭不打扰——
				 * armed 窗口外的历史关闭帧）。
				 */
				if !armed {
					continue
				}
				if noun, ok := dropClosedPending(&openPendings, frame.PendingID); ok {
					events <- TurnEvent{Type: TurnStatus, Status: "✅ " + noun + "已处理，继续执行…"}
				}
			case acpsession.FrameTurnEnded:
				if frame.Turn != nil && frame.Turn.Error != "" {
					failedReason = frame.Turn.Error // 协议层回合失败：终结会话，下条消息重建
				}
				emitTurnDone(events, frame.Turn, text)
				return
			case acpsession.FrameTerminated:
				err := terminatedError(frame)
				failedReason = err.Error() // 会话已终：Terminate 幂等 no-op，统一收尾标记
				events <- TurnEvent{Type: TurnError, Err: err}
				return
			}
		}
	}
}

/*
 * turnAgentText 快照 entries 中本回合的 agentMessage 文本（armed=false 极快收敛合成终态与 armed 基线装填共用）：beginTurn 受理即同步写本回合 user 条目——最后一条 user 条目即本回合锚，其后的 agentMessage 才是本回合产出（视图按 (回合,kind) 聚合，锚后至多一条；
 * 取最后非空防御）。
 * 无 user 锚返回空——受理后快照恒含本回合 user 条目，无锚属理论不可达，宁空毋串不回落「最后一条」。
 */
func turnAgentText(entries []acpsession.ConversationEntry) string {
	lastUser := -1
	for i := range entries {
		if entries[i].Kind == "user" {
			lastUser = i
		}
	}
	if lastUser < 0 {
		return ""
	}
	var text string
	for i := lastUser + 1; i < len(entries); i++ {
		if entries[i].Kind == "agentMessage" && entries[i].Text != "" {
			text = entries[i].Text
		}
	}
	return text
}

// terminatedError 会话终结帧的用户可读错误（带因优先）。
func terminatedError(frame acpsession.Frame) error {
	if frame.Status != nil && frame.Status.Error != "" {
		return fmt.Errorf("ACP 会话异常退出：%s", frame.Status.Error)
	}
	return errors.New("ACP 会话异常退出")
}

/*
 * emitTurnDone 回合终态映射：turn.Error 非空 = 协议层失败 → TurnError（泵取首行）；
 * stopReason = cancelled = 软取消中断 → TurnDone{IsError}（泵文案「回合被中断」，已产出
 * 文本随 Result 携带）；其余（end_turn…）= 正常完成，Result 带回复全文。
 */
func emitTurnDone(events chan<- TurnEvent, turn *acpsession.TurnPayload, text string) {
	if turn != nil && turn.Error != "" {
		events <- TurnEvent{Type: TurnError, Err: errors.New(turn.Error)}
		return
	}
	done := TurnEvent{Type: TurnDone, Result: text}
	if turn != nil && turn.StopReason == stopReasonCancelled {
		done.IsError = true
	}
	events <- done
}

/*
 * stopReasonCancelled ACP wire 的回合取消终态（取值域 SSOT 是 ACP 协议 stopReason；
 * acpsession.TurnPayload.StopReason 为其 string 投影，bot 不引 acp-go 保持依赖面窄）。
 */
const stopReasonCancelled = "cancelled"

/*
 * toolDisplayName 工具进度事件名称：wire 的 tool name 优先（对齐 headless 的工具名形态），
 * 缺省回落 title（claude ACP 适配器的工具调用 title 即人类可读名）。
 */
func toolDisplayName(tc *acpsession.ToolCallView) string {
	if tc.Name != "" {
		return tc.Name
	}
	if tc.Title != "" {
		return tc.Title
	}
	return "工具"
}

/*
 * toolSettledLine 工具终态面板行文案：completed/failed 覆写帧对应的进度文案，其余状态返回
 * 空串（不发事件）。
 */
func toolSettledLine(tc *acpsession.ToolCallView) string {
	switch tc.Status {
	case "completed":
		return toolSettledText(toolDisplayName(tc), false)
	case "failed":
		return toolSettledText(toolDisplayName(tc), true)
	}
	return ""
}
