package bot

import (
	"context"
	"errors"
	"fmt"

	"ocean-harness/server/internal/acpsession"
)

// acpSessions bot 域消费 acpsession 域的最小面（消费侧缝，惯例同 ClaudeDriver 本身）：
// *acpsession.Manager 隐式满足（编译期断言紧随定义）；测试以 fake 帧脚本驱动，不依赖
// 真实 Manager 与 agent 进程。Ensure 为受理式（同步返回现状快照，spawn 链后台进行），
// 就绪等待与回合输出均由本驱动按帧驱动收集。回合受理走 PromptQueued 排队语义（T2.2
// 跨入口回合锁）——活动回合中 FIFO 等位、回合空隙自动开跑，与桌面 prompt 入口互斥且
// 有序（桌面即时受理不经队列，用户优先）。Get/RespondPermission 供审批数字快路径
// （T2.4，pending_gate.go）读写挂起审批——与桌面 respondPermission HTTP 面同一 Manager
// 入口，first-writer-wins 由 acpsession 收敛语义层保证。
type acpSessions interface {
	Ensure(ctx context.Context, issueID, pickedLaunchMode string) (acpsession.ViewSnapshot, error)
	Subscribe(issueID string) (<-chan acpsession.Frame, func())
	PromptQueued(ctx context.Context, issueID, text string) error
	Cancel(issueID string) error
	Get(issueID string) acpsession.ViewSnapshot
	RespondPermission(issueID string, pendingID uint64, optionID, source string) error
}

// 编译期断言：Manager 满足消费面。
var _ acpSessions = (*acpsession.Manager)(nil)

// acpDriver ClaudeDriver 的 ACP 实现（T2.1）：bot 回合经 sidecar acpsession 域驱动 claude，
// 与桌面 issue 主窗口共享同一 ACP 会话（同一 agent 进程、同一会话视图、同一挂起审批状态，
// IM 与桌面看到的是同一段对话）。
// 与 headless（driver_claude：每回合一个进程，--resume 锚点续聊）的差异：会话 per-issue
// 长驻、无 resume，因此不发 TurnInit、不写 claude_session_id 锚点（ACP session id 与
// --resume 不兼容）。
// 目标 issue（T2.3）：路由层解析会话显式绑定后经 TurnRequest.IssueID 回填，本驱动是纯
// 执行者——空值 = 未绑定，只负责回引导文案（绑定知识不进引擎）。
// 权限面（P6）：不消费 bot 级 allowed_tools——会话权限统一由 workspace/issue 的
// launch_settings.permissionMode 表达（Ensure 链下发）；「需要审批」档的挂起审批由桌面端
// 呈现（T1.7），IM 侧交互随 T2.4/T3.2。
// 已知限制：TurnRequest.Model / SystemPrompt 不消费——ACP 链路无 per-turn system prompt
// 通道，且会话与桌面共享，bot 人设不宜做会话级注入（bot prompt 配置存废另行决策）。
type acpDriver struct {
	sessions acpSessions
}

// NewAcpDriver 构造 ACP 引擎（supervisor 装配）。
func NewAcpDriver(sessions acpSessions) ClaudeDriver {
	return acpDriver{sessions: sessions}
}

// RunTurn 实现见 driver.go 契约。受理失败（未绑定 / 配置非法 / 会话起不来 / 排队受理
// 被拒）同步返回 error（复用编排器的启动失败回复路径）；受理成功后异步收集帧直至回合
// 终态。固定时序：消费路由层回填的目标 issue → Ensure 受理 → 订阅（受理成功后立即订
// 阅——首帧快照新于受理时点，旧会话的失败快照进不来）→ 帧驱动等就绪 → 排队受理（活动
// 回合中 FIFO 等位，回合空隙自动开跑；等位期间会话终结 / ctx 取消则同步报错）→ 受理后
// 重开干净订阅 → 帧循环收集。两段订阅的分工：第一段只服务等就绪，等位全程挂着不消费
// （等位窗口里阻塞回合的完整帧序全部作废——重开订阅让 collectTurn 缓冲里只可能有本回合
// 的生命周期帧，外来 turnEnded 误终止、hub 慢订户溢出摘除两个窗口一并消除）。
func (d acpDriver) RunTurn(ctx context.Context, req TurnRequest) (<-chan TurnEvent, error) {
	// 目标 issue 由路由层解析会话绑定后回填（routeTarget.IssueID，T2.3）；空 = 未绑定
	// （含挂空绑定降级），本驱动不持绑定知识，只负责引导文案把用户带回正轨。
	if req.IssueID == "" {
		return nil, errors.New("该工作空间已切换为 ACP 模式，但本会话尚未绑定 issue：发送「#issue <标题关键词 或 ID前8位>」完成绑定（发送「#issue」查看用法），或把工作空间启动模式切回终端模式")
	}
	issueID := req.IssueID
	snap, err := d.sessions.Ensure(ctx, issueID, "")
	if err != nil {
		return nil, err
	}
	frames, stop := d.sessions.Subscribe(issueID)
	snap, err = waitSessionReady(ctx, frames, snap)
	if err != nil {
		stop()
		return nil, err
	}
	// 排队受理（T2.2）：受理失败（会话终结 / ctx 取消）同步报错，经 driverRoute 包
	// turnNotAcceptedError，编排器不清锚。
	if err := d.sessions.PromptQueued(ctx, issueID, req.Prompt); err != nil {
		stop()
		return nil, err
	}
	stop() // 等位窗口帧全部作废（含受理帧自身——armed 由重开订阅的首帧快照装填）
	turnFrames, stopTurn := d.sessions.Subscribe(issueID)
	events := make(chan TurnEvent, 128)
	go collectTurn(ctx, d.sessions, issueID, turnFrames, stopTurn, events)
	return events, nil
}

// waitSessionReady 帧驱动等会话就绪：从 Ensure 返回的受理快照出发，消费帧直至 ready /
// failed / terminated / ctx 取消。订阅后置于 Ensure 受理，首帧快照新于受理时点——状态
// 判定以「受理快照 + 后续状态帧」为准，不会回退到旧会话的失败/终结态（重建路径的旧因
// 不得误报为本回合失败）。
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
				snap = *s
			}
		}
	}
}

// frameStatus 提取帧携带的会话状态投影（snapshot / sessionStatus / terminated 三类帧
// 携带状态；其余帧 nil 不参与就绪等待）。
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

// collectTurn 回合输出收集（RunTurn 受理成功后开启，订阅与退订所有权在此；订阅为受理
// 后重开的干净通道，缓冲里只可能有本回合生命周期帧）。armed 窗口装填：首帧快照的
// TurnActive（本回合 turnStarted 帧广播早于订阅建立，收不到）或收到的 FrameTurnStarted；
// 窗口内视图侧回合闸门保证不存在别的回合，agentMessage entry 即本轮回复（帧携带累计
// 全量文本，取最新即可）；toolCall 按 id 首见发进度事件。终态唯一性：turnEnded /
// terminated / 帧通道关闭三者先到先收，收即返回。
// ctx 取消 = 软取消（Manager.Cancel → acp 层预算内强制收口，终态帧必达），继续消费直至
// 终态帧；不自建超时——终结时序统一由 acp 层取消预算与帧到达表达，不引入时间性兜底。
// 取消信号消费一次即摘出 select（置 nil）——Done 恒就绪，留在集合里会在等终态帧期间
// 空转烧 CPU。
func collectTurn(ctx context.Context, sessions acpSessions, issueID string, frames <-chan acpsession.Frame, stop func(), events chan<- TurnEvent) {
	defer close(events)
	defer stop()

	seenTools := map[string]bool{}
	var text string
	var openPendings []acpsession.PendingView // 回合内开放挂起（状态行编号与摘除追踪）
	armed := false
	notify := ctx.Done()

	for {
		select {
		case <-notify:
			sessions.Cancel(issueID) // 软取消恰一次；编排器 Stop 后 Manager 可能已收敛，失败则等终态帧/关帧收尾
			notify = nil
		case frame, ok := <-frames:
			if !ok {
				events <- TurnEvent{Type: TurnError, Err: errors.New("ACP 事件流已断开（会话被关停）")}
				return
			}
			switch frame.Type {
			case acpsession.FrameSnapshot:
				if frame.Snapshot == nil {
					continue
				}
				// 首帧快照（真实 hub 订阅首帧）：TurnActive=true 装填 armed；false =
				// 回合已在订阅建立前收敛（agent 极快；Subscribe 建立与 PromptText RPC
				// 存在量级差，生产不可达、fake 测试可达），从快照直接合成终态（entries
				// 含全文；回合级协议错误不落快照视图，以 StopReason 判 cancelled，其余
				// 按正常完成）。
				armed = frame.Snapshot.TurnActive
				if !armed {
					emitTurnDone(events, &acpsession.TurnPayload{
						StopReason: frame.Snapshot.StopReason,
						Error:      frame.Snapshot.Error,
					}, lastAgentText(frame.Snapshot.Entries))
					return
				}
			case acpsession.FrameTurnStarted:
				armed = frame.Turn != nil && frame.Turn.Active
			case acpsession.FrameEntry:
				if !armed || frame.Entry == nil {
					continue
				}
				// kind 取值 SSOT 是 acpsession 视图的 wire 形态（未导出常量；前端同镜像字面量）。
				switch frame.Entry.Kind {
				case "agentMessage":
					text = frame.Entry.Text
				case "toolCall":
					if tc := frame.Entry.ToolCall; tc != nil && !seenTools[tc.ToolCallID] {
						seenTools[tc.ToolCallID] = true
						events <- TurnEvent{Type: TurnToolUse, ToolName: toolDisplayName(tc)}
					}
				}
			case acpsession.FramePendingOpened:
				// 审批挂起呈现（T2.4 回合内文本交互）：permission 出编号状态行（编号与
				// 数字快路径同源于视图注册序——快路径按 Get 快照序应答，本处按帧到达序
				// 展示，二者同源恒一致）；elicitation 指回桌面（应答面随 T3.3）。混合挂起
				// 合并展示——表单提示附尾，审批的数字应答指引不被覆写。
				if !armed || frame.Pending == nil {
					continue
				}
				openPendings = append(openPendings, *frame.Pending)
				perms, hasElicit := splitPendings(openPendings)
				if len(perms) > 0 {
					status := formatPermissionPendingStatus(permissionChoices(perms))
					if hasElicit {
						status += "\n" + elicitationPendingText
					}
					events <- TurnEvent{Type: TurnStatus, Status: status}
				} else {
					events <- TurnEvent{Type: TurnStatus, Status: elicitationPendingText}
				}
			case acpsession.FramePendingClosed:
				// 已见挂起的关闭：状态行切「已处理，继续执行…」（未见过的关闭不打扰——
				// armed 窗口外的历史关闭帧）。
				if !armed {
					continue
				}
				if noun, ok := dropClosedPending(&openPendings, frame.PendingID); ok {
					events <- TurnEvent{Type: TurnStatus, Status: "✅ " + noun + "已处理，继续执行…"}
				}
			case acpsession.FrameTurnEnded:
				emitTurnDone(events, frame.Turn, text)
				return
			case acpsession.FrameTerminated:
				events <- TurnEvent{Type: TurnError, Err: terminatedError(frame)}
				return
			}
		}
	}
}

// lastAgentText 快照 entries 中最后一条 agentMessage 文本（极快收敛合成终态用——帧语义
// 的「累计全量取最新」在快照投影上即取最后一条非空）。
func lastAgentText(entries []acpsession.ConversationEntry) string {
	var text string
	for i := range entries {
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

// emitTurnDone 回合终态映射：turn.Error 非空 = 协议层失败 → TurnError（泵取首行）；
// stopReason = cancelled = 软取消中断 → TurnDone{IsError}（泵文案「回合被中断」，已产出
// 文本随 Result 携带）；其余（end_turn…）= 正常完成，Result 带回复全文。
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

// stopReasonCancelled ACP wire 的回合取消终态（取值域 SSOT 是 ACP 协议 stopReason；
// acpsession.TurnPayload.StopReason 为其 string 投影，bot 不引 acp-go 保持依赖面窄）。
const stopReasonCancelled = "cancelled"

// toolDisplayName 工具进度事件名称：wire 的 tool name 优先（对齐 headless 的工具名形态），
// 缺省回落 title（claude ACP 适配器的工具调用 title 即人类可读名）。
func toolDisplayName(tc *acpsession.ToolCallView) string {
	if tc.Name != "" {
		return tc.Name
	}
	if tc.Title != "" {
		return tc.Title
	}
	return "工具"
}
