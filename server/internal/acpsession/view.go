package acpsession

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	acpgo "github.com/BrokkAi/acp-go"
	"github.com/BrokkAi/acp-go/schema"

	"ocean-harness/server/internal/acp"
)

// SessionStatus 会话视图状态（ViewSnapshot.Status 与 sessionStatus 帧的取值域）。
type SessionStatus string

const (
	StatusIdle       SessionStatus = "idle"       // 无会话（从未 ensure / entry 已被 Discard）
	StatusStarting   SessionStatus = "starting"   // ensure 已受理，spawn 链进行中
	StatusReady      SessionStatus = "ready"      // 会话就绪（可 prompt / 订阅事件流）
	StatusFailed     SessionStatus = "failed"     // spawn 链失败（error 有因，可重试 ensure）
	StatusTerminated SessionStatus = "terminated" // 会话终结（进程死亡 / 显式收尾 / discard）
)

// 会话记录条目的 kind 取值域。
const (
	entryKindUser         = "user"
	entryKindAgentMessage = "agentMessage"
	entryKindAgentThought = "agentThought"
	entryKindToolCall     = "toolCall"
)

// ConversationEntry 会话记录条目（四类：用户消息 / agent 消息 / agent 思考 / 工具调用）。
// 同类同回合的流式 chunk 聚合进同一 entry，工具调用按 toolCallId 覆写式 upsert。
type ConversationEntry struct {
	EntryID  string        `json:"entryId"`
	Kind     string        `json:"kind"` // user | agentMessage | agentThought | toolCall
	Text     string        `json:"text,omitempty"`
	ToolCall *ToolCallView `json:"toolCall,omitempty"`
}

// ToolCallView 工具调用条目（ACP wire 原样透传，前端按 kind/status 选择呈现形态）。
type ToolCallView struct {
	ToolCallID string                    `json:"toolCallId"`
	Title      string                    `json:"title,omitempty"`
	Kind       string                    `json:"kind,omitempty"`
	Status     string                    `json:"status,omitempty"`
	Name       string                    `json:"name,omitempty"`
	Content    []schema.ToolCallContent  `json:"content,omitempty"`
	Locations  []schema.ToolCallLocation `json:"locations,omitempty"`
	RawInput   json.RawMessage           `json:"rawInput,omitempty"`
	RawOutput  json.RawMessage           `json:"rawOutput,omitempty"`
}

// PendingView 挂起交互投影（permission / elicitation 二选一，字段按 kind 取舍）。
type PendingView struct {
	PendingID uint64                    `json:"pendingId"`
	Kind      string                    `json:"kind"`               // permission | elicitation
	Options   []schema.PermissionOption `json:"options,omitempty"`  // permission 专用
	ToolCall  *schema.ToolCallUpdate    `json:"toolCall,omitempty"` // permission 专用：审批详情
	Request   *acp.ElicitationWire      `json:"request,omitempty"`  // elicitation 专用：全保真 wire（acp-go 生成模型丢 requestedSchema/url，见 acp/elicitation.go）
}

// ToolTitle 挂起审批的工具呈现标题（Title 优先，Name 兜底，无则空）——投影出口同 label
// SSOT：bot 域经此取值，不引 acp-go（依赖面口径见 bot/driver_acp.go）。
func (p PendingView) ToolTitle() string {
	if p.ToolCall == nil {
		return ""
	}
	if p.ToolCall.Title != nil && *p.ToolCall.Title != "" {
		return *p.ToolCall.Title
	}
	if p.ToolCall.Name != nil && *p.ToolCall.Name != "" {
		return *p.ToolCall.Name
	}
	return ""
}

// ViewSnapshot 会话视图快照——/api/acpSession 响应与 SSE snapshot 帧的 shape SSOT
// （前端按此镜像 TS 类型）。嵌套的 ACP wire 结构原样透传不转译。
type ViewSnapshot struct {
	Status            SessionStatus             `json:"status"`
	AgentCode         string                    `json:"agentCode,omitempty"`
	AcpSessionID      string                    `json:"acpSessionId,omitempty"`
	PermissionMode    string                    `json:"permissionMode,omitempty"`
	CurrentModeID     string                    `json:"currentModeId,omitempty"`
	AvailableModes    []schema.SessionMode      `json:"availableModes,omitempty"`
	Entries           []ConversationEntry       `json:"entries"`
	Pendings          []PendingView             `json:"pendings"`
	Plan              *schema.Plan              `json:"plan,omitempty"`
	Usage             *schema.UsageUpdate       `json:"usage,omitempty"`
	AvailableCommands []schema.AvailableCommand `json:"availableCommands,omitempty"`
	TurnActive        bool                      `json:"turnActive"`
	StopReason        string                    `json:"stopReason,omitempty"` // 最近回合终态
	Error             string                    `json:"error,omitempty"`      // failed/terminated 原因
}

// pendingSlot 挂起交互注册表槽位：视图投影 + acp 挂起句柄（投影走 view，应答走句柄）。
type pendingSlot struct {
	view        PendingView
	permission  *acp.PendingPermission
	elicitation *acp.PendingElicitation
}

// 应答入口标识（T2.4 双入口审批收敛）：closed-history 登记与后到方文案的来源维度。
// HTTP 桌面面恒 panel（service 层收口硬编码），bot IM 快路径传 bot；source 是服务端
// 内部追踪字段，不进任何 client DTO。
const (
	PendingSourcePanel = "panel" // 桌面 issue 主窗口（HTTP 应答面）
	PendingSourceBot   = "bot"   // IM bot（企微回合内数字快路径）
)

// ErrPendingAlreadyHandled 挂起已被一端应答或已随回合结算（T2.4 first-writer-wins 的
// 后到方判别哨兵）：acp 层 ErrAlreadyResolved 只说明「CAS 拒绝」，本哨兵补齐「谁、以何
// 选项处理」的收敛语义。消费方 errors.Is 判别，文案（*PendingAlreadyHandledError）用户
// 可读直出。
var ErrPendingAlreadyHandled = errors.New("ACP 挂起已处理")

// PendingAlreadyHandledError 后到方应答已处理挂起的用户可读错误：Error() 即展示文案
// （「该审批已由桌面端应答（允许）」/「该表单已随回合结束自动结算」），Unwrap 到哨兵
// 供 errors.Is 判别（文案零前缀污染，前端可按「该审批已/该表单已」前缀识别中性呈现）。
type PendingAlreadyHandledError struct {
	Kind    string // 展示名词：审批 | 表单
	Source  string // 应答入口（panel/bot；结算路径为空）
	Label   string // 应答选项/动作的展示标签
	Settled bool   // true = 随回合结束/取消自动结算（非人为应答）
}

func (e *PendingAlreadyHandledError) Error() string {
	if e.Settled {
		return fmt.Sprintf("该%s已随回合结束自动结算", e.Kind)
	}
	return fmt.Sprintf("该%s已由%s应答（%s）", e.Kind, pendingSourceLabel(e.Source), e.Label)
}

func (e *PendingAlreadyHandledError) Unwrap() error { return ErrPendingAlreadyHandled }

// pendingSourceLabel 应答入口展示名（panel=桌面端 / bot=IM；未知值中性兜底「另一端」）。
func pendingSourceLabel(source string) string {
	switch source {
	case PendingSourcePanel:
		return "桌面端"
	case PendingSourceBot:
		return "IM"
	}
	return "另一端"
}

// closedHistoryCap closed-history 环形上限（近期已关闭挂起的收敛记录窗口）。
const closedHistoryCap = 32

// closedPending 已关闭挂起的收敛记录（closed-history 条目，T2.4）：应答成功登记
// {source, label}；回合收尾/自动结算登记 {settled}。后到方应答命中历史即回判别哨兵，
// 未命中（pendingID 写错/超窗出环）才回落「不存在或已关闭」。
type closedPending struct {
	source  string // 应答入口；空 = 非人为应答
	label   string // 应答选项/动作展示标签
	settled bool   // true = 回合收尾/取消自动结算
}

// PermissionOptionLabel 审批选项展示标签（label SSOT，桌面哨兵文案与 IM 状态行共用）：
// kind → 中文（allow_once=允许 / allow_always=允许并记住 / reject_once=拒绝 /
// reject_always=始终拒绝），未知 kind 回落 agent 原始 name。
func PermissionOptionLabel(kind, name string) string {
	switch schema.PermissionOptionKind(kind) {
	case schema.PermissionOptionKindAllowOnce:
		return "允许"
	case schema.PermissionOptionKindAllowAlways:
		return "允许并记住"
	case schema.PermissionOptionKindRejectOnce:
		return "拒绝"
	case schema.PermissionOptionKindRejectAlways:
		return "始终拒绝"
	}
	return name
}

// findPermissionOption 按 optionId 查审批选项目录（应答预检与标签取值共用一次查找；
// 不存在返回 ok=false）。
func findPermissionOption(options []schema.PermissionOption, optionID string) (schema.PermissionOption, bool) {
	for i := range options {
		if string(options[i].OptionID) == optionID {
			return options[i], true
		}
	}
	return schema.PermissionOption{}, false
}

// elicitationActionLabel elicitation 三态应答动作的展示标签。
func elicitationActionLabel(action string) string {
	switch action {
	case "accept":
		return "接受"
	case "decline":
		return "拒绝"
	case "cancel":
		return "取消"
	}
	return action
}

// sessionView 单会话的积累投影：manager 是唯一写者（受理/收尾编排 + pump 按 wire 顺序
// 喂入），读经 snapshot。快照类字段（plan/usage/commands/modes）最新替换；条目覆写式
// upsert。回合两端帧由本视图合成（StopReason 只从 acp Prompt 返回值可见的 gap 补齐）。
type sessionView struct {
	mu sync.RWMutex

	status    SessionStatus
	agentCode string
	sessionID string
	permMode  string
	errText   string

	modes    *schema.SessionModeState
	plan     *schema.Plan
	usage    *schema.UsageUpdate
	commands []schema.AvailableCommand

	entries    []ConversationEntry
	entryIndex map[string]int // entryId → entries 下标（覆写式 upsert）

	// pendings 挂起交互注册表（acp 层无枚举 API，视图侧自登记；应答/结算后摘除）。
	pendings []pendingSlot

	// closed 已关闭挂起的收敛记录（pendingID → 详情 + closedOrder FIFO 序，环形上限
	// closedHistoryCap，跨回合常驻——回合结束后抵达的迟到应答仍可判别；Ensure 重建随
	// 新视图重置）。后到方应答命中历史回 ErrPendingAlreadyHandled 判别哨兵，未命中才
	// 回落「不存在或已关闭」。
	closed      map[uint64]closedPending
	closedOrder []uint64

	turn       int // 回合序号（entryId 命名空间：t<turn>-user / t<turn>-agent / ...）
	turnActive bool
	stopReason string
}

func newSessionView() *sessionView {
	return &sessionView{entryIndex: map[string]int{}, closed: map[uint64]closedPending{}}
}

// setStarting 受理即置态（agentCode/permissionMode 预填，前端受理反馈可见实际取值）。
func (v *sessionView) setStarting(cfg SessionConfig) []Frame {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.status = StatusStarting
	v.agentCode = cfg.AgentCode
	v.permMode = string(cfg.PermissionMode)
	return []Frame{statusFrame(FrameSessionStatus, StatusPayload{
		Status:    StatusStarting,
		AgentCode: cfg.AgentCode,
	})}
}

// setReady 会话就绪：记 sessionId + 初始模式目录（ACP wire 原样）。
func (v *sessionView) setReady(sessionID string, cfg SessionConfig, modes *schema.SessionModeState) []Frame {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.status = StatusReady
	v.sessionID = sessionID
	v.agentCode = cfg.AgentCode
	v.permMode = string(cfg.PermissionMode)
	v.modes = modes
	return []Frame{statusFrame(FrameSessionStatus, StatusPayload{
		Status:       StatusReady,
		AgentCode:    cfg.AgentCode,
		AcpSessionID: sessionID,
	})}
}

// setFailed spawn 链失败落因（可重试 ensure 重建）。
func (v *sessionView) setFailed(reason string) []Frame {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.status = StatusFailed
	v.errText = reason
	return []Frame{statusFrame(FrameSessionStatus, StatusPayload{
		Status: StatusFailed,
		Error:  reason,
	})}
}

// setTerminated 会话终结（幂等：pump 哨兵与 Discard/StopAll 收尾并发只出一次帧）。
func (v *sessionView) setTerminated(reason string) []Frame {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.status == StatusTerminated {
		return nil
	}
	v.status = StatusTerminated
	if reason != "" {
		v.errText = reason
	}
	return []Frame{statusFrame(FrameTerminated, StatusPayload{
		Status:       StatusTerminated,
		AgentCode:    v.agentCode,
		AcpSessionID: v.sessionID,
		Error:        v.errText,
	})}
}

// applyEvent 会话事件喂入（Update / Permission / Elicitation；Terminated 哨兵由 manager
// 显式处理——需查进程死亡证据，视图不感知 client）。
func (v *sessionView) applyEvent(event *acp.SessionEvent) []Frame {
	v.mu.Lock()
	defer v.mu.Unlock()
	if event.Update != nil {
		return v.applyUpdateLocked(event.Update)
	}
	if event.Permission != nil {
		toolCall := event.Permission.ToolCall()
		return v.registerPendingLocked(PendingView{
			PendingID: uint64(event.Permission.ID()),
			Kind:      "permission",
			Options:   event.Permission.Options(),
			ToolCall:  &toolCall,
		}, &pendingSlot{permission: event.Permission})
	}
	if event.Elicitation != nil {
		wire := event.Elicitation.Wire()
		if wire == nil {
			wire = acp.WireFromRequest(event.Elicitation.Request())
		}
		return v.registerPendingLocked(PendingView{
			PendingID: uint64(event.Elicitation.ID()),
			Kind:      "elicitation",
			Request:   wire,
		}, &pendingSlot{elicitation: event.Elicitation})
	}
	return nil
}

// applyUpdateLocked session/update 变体消费（wire 顺序即调用顺序）。
func (v *sessionView) applyUpdateLocked(update *acpgo.Update) []Frame {
	switch inner := update.Update; {
	case inner.AgentMessageChunk != nil:
		return v.appendChunkLocked(entryKindAgentMessage, textOfChunk(inner.AgentMessageChunk))
	case inner.AgentThoughtChunk != nil:
		return v.appendChunkLocked(entryKindAgentThought, textOfChunk(inner.AgentThoughtChunk))
	case inner.ToolCall != nil:
		return v.upsertToolCallLocked(toolCallViewFromCall(inner.ToolCall))
	case inner.ToolCallUpdate != nil:
		return v.applyToolCallUpdateLocked(inner.ToolCallUpdate)
	case inner.Plan != nil:
		v.plan = inner.Plan
		return []Frame{{Type: FramePlanUpdated, Plan: inner.Plan}}
	case inner.UsageUpdate != nil:
		v.usage = inner.UsageUpdate
		return []Frame{{Type: FrameUsageUpdated, Usage: inner.UsageUpdate}}
	case inner.AvailableCommandsUpdate != nil:
		v.commands = inner.AvailableCommandsUpdate.AvailableCommands
		return []Frame{{Type: FrameCommandsUpdated, Commands: v.commands}}
	case inner.CurrentModeUpdate != nil:
		// 替换式回写：mode 帧共享 v.modes 指针在锁外序列化，就地改 CurrentModeID 同构
		// 数据竞态——以值拷贝新建（AvailableModes 目录创建后只读，共享安全），帧携带新
		// 指针；acp 层 setReady 交来的快照副本同样不被就地触碰。
		modes := &schema.SessionModeState{CurrentModeID: inner.CurrentModeUpdate.CurrentModeID}
		if v.modes != nil {
			modes.AvailableModes = v.modes.AvailableModes
		}
		v.modes = modes
		return []Frame{{Type: FrameModeUpdated, Mode: v.modes}}
	default:
		// user_message_chunk（用户输入的本地回显）/ config_option_update /
		// session_info_update：一期不积累不转发，消费需要时在此扩展。
		return nil
	}
}

// appendChunkLocked 文本 chunk 按 (kind, 当前回合) 聚合进会话条目（agent 流式输出的合帧
// 投影；ContentChunk.MessageID 的消息边界细分一期不消费，回合粒度聚合足够）。entry 帧
// 整体覆写（快照类语义：最新态替换）。
func (v *sessionView) appendChunkLocked(kind, text string) []Frame {
	if text == "" {
		return nil
	}
	entryID := fmt.Sprintf("t%d-%s", v.turn, kind)
	idx, ok := v.entryIndex[entryID]
	if !ok {
		v.entries = append(v.entries, ConversationEntry{EntryID: entryID, Kind: kind})
		idx = len(v.entries) - 1
		v.entryIndex[entryID] = idx
	}
	v.entries[idx].Text += text
	entry := v.entries[idx]
	return []Frame{{Type: FrameEntry, Entry: &entry}}
}

// upsertToolCallLocked 工具调用条目按 toolCallId 覆写式 upsert（tool: 前缀命名空间，
// 与回合文本条目互不冲突；update 先于初建到达的异常时序同样成立——建空壳再合并）。
func (v *sessionView) upsertToolCallLocked(view *ToolCallView) []Frame {
	entryID := "tool:" + view.ToolCallID
	idx, ok := v.entryIndex[entryID]
	if !ok {
		v.entries = append(v.entries, ConversationEntry{EntryID: entryID, Kind: entryKindToolCall})
		idx = len(v.entries) - 1
		v.entryIndex[entryID] = idx
	}
	v.entries[idx].ToolCall = view
	entry := v.entries[idx]
	return []Frame{{Type: FrameEntry, Entry: &entry}}
}

// applyToolCallUpdateLocked 部分更新合并。已发布的 *ToolCallView 被帧与快照共享、在锁外
// 序列化，就地 merge 构成数据竞态——先克隆再 merge，经 upsert 整体换指针（恢复
// 「发布后不可变」不变量；浅拷贝足够：切片/RawMessage 字段 merge 中只整体替换）。
func (v *sessionView) applyToolCallUpdateLocked(upd *schema.ToolCallUpdate) []Frame {
	entryID := "tool:" + string(upd.ToolCallID)
	var view *ToolCallView
	if idx, ok := v.entryIndex[entryID]; ok && v.entries[idx].ToolCall != nil {
		cloned := *v.entries[idx].ToolCall
		view = &cloned
	}
	if view == nil {
		view = &ToolCallView{ToolCallID: string(upd.ToolCallID)}
	}
	mergeToolCallUpdate(view, upd)
	return v.upsertToolCallLocked(view)
}

// mergeToolCallUpdate 部分合并语义：指针/切片字段非空才覆盖（ACP wire 部分更新契约），
// 其余保留。wire 解码对象不可变，字段引用直接共享安全。
func mergeToolCallUpdate(view *ToolCallView, upd *schema.ToolCallUpdate) {
	if upd.Title != nil {
		view.Title = *upd.Title
	}
	if upd.Kind != nil {
		view.Kind = string(*upd.Kind)
	}
	if upd.Status != nil {
		view.Status = string(*upd.Status)
	}
	if upd.Name != nil {
		view.Name = *upd.Name
	}
	if len(upd.Content) > 0 {
		view.Content = upd.Content
	}
	if len(upd.Locations) > 0 {
		view.Locations = upd.Locations
	}
	if upd.RawInput != nil {
		view.RawInput = upd.RawInput
	}
	if upd.RawOutput != nil {
		view.RawOutput = upd.RawOutput
	}
}

func toolCallViewFromCall(call *schema.ToolCall) *ToolCallView {
	view := &ToolCallView{ToolCallID: string(call.ToolCallID), Title: call.Title}
	if call.Kind != nil {
		view.Kind = string(*call.Kind)
	}
	if call.Status != nil {
		view.Status = string(*call.Status)
	}
	if call.Name != nil {
		view.Name = *call.Name
	}
	view.Content = call.Content
	view.Locations = call.Locations
	view.RawInput = call.RawInput
	view.RawOutput = call.RawOutput
	return view
}

// registerPendingLocked 挂起登记 + pendingOpened 帧。
func (v *sessionView) registerPendingLocked(view PendingView, slot *pendingSlot) []Frame {
	slot.view = view
	v.pendings = append(v.pendings, *slot)
	return []Frame{{Type: FramePendingOpened, Pending: &view}}
}

// respondPending 收敛应答骨架（permission/elicitation 共用，差异经 precheck/respond
// 闭包传入）。固定时序：定位挂起 → closed-history 判别哨兵 → precheck（kind 错配/非法
// 选项在摘除前拒绝——摘除后应答失败会让前后端挂起视图与 agent 等待态三方失同步）→
// 摘除 → CAS 应答 → 登记 closed-history → pendingClosed 帧。CAS 被拒（errors.Is 判
// acp.ErrAlreadyResolved：回合收尾/取消竞态先结算）回填槽位 + 登记 settled + 回 settled
// 哨兵——回填维持 endTurn 兜底补发 pendingClosed 的原语义（订阅端不挂死在已消失的审批
// 上）；其余应答错误回填后原样透传（挂起在视图与 agent 侧均仍开放，可重试）。
// pendingId 命中 closed-history 回 ErrPendingAlreadyHandled 判别哨兵（T2.4 双入口
// first-writer-wins：另一端已应答/已随回合结算），未命中才是「不存在或已关闭」。
func (v *sessionView) respondPending(pendingID uint64, noun, source string, precheck func(slot pendingSlot) error, respond func(slot pendingSlot) (string, error)) ([]Frame, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	idx := pendingIndexOfLocked(v.pendings, pendingID)
	if idx < 0 {
		if err := v.alreadyClosedLocked(noun, pendingID); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("挂起%s不存在或已关闭: %d", noun, pendingID)
	}
	slot := v.pendings[idx]
	if err := precheck(slot); err != nil {
		return nil, err
	}
	v.pendings = append(v.pendings[:idx], v.pendings[idx+1:]...)
	label, err := respond(slot)
	if err != nil {
		v.pendings = append(v.pendings, slot)
		if errors.Is(err, acp.ErrAlreadyResolved) {
			v.recordClosedLocked(pendingID, closedPending{settled: true})
			return nil, &PendingAlreadyHandledError{Kind: noun, Settled: true}
		}
		return nil, err
	}
	v.recordClosedLocked(pendingID, closedPending{source: source, label: label})
	return []Frame{pendingClosedFrame(pendingID)}, nil
}

// respondPermission 以 optionId 应答挂起权限审批（差异步：句柄预检 + 选项预检与标签
// 取值共用一次目录查找）。
func (v *sessionView) respondPermission(pendingID uint64, optionID, source string) ([]Frame, error) {
	var pick schema.PermissionOption
	return v.respondPending(pendingID, "审批", source,
		func(slot pendingSlot) error {
			if slot.permission == nil {
				return fmt.Errorf("挂起 %d 不是权限审批", pendingID)
			}
			var ok bool
			if pick, ok = findPermissionOption(slot.view.Options, optionID); !ok {
				return fmt.Errorf("未知审批选项: %s", optionID)
			}
			return nil
		},
		func(slot pendingSlot) (string, error) {
			if err := slot.permission.Respond(optionID); err != nil {
				return "", err
			}
			return PermissionOptionLabel(string(pick.Kind), pick.Name), nil
		},
	)
}

// respondElicitation 以完整三态应答挂起 elicitation（构造见 acpgo.AcceptElicitation 等；
// 差异步仅句柄预检，应答标签取动作三态）。
func (v *sessionView) respondElicitation(pendingID uint64, action string, response schema.CreateElicitationResponse, source string) ([]Frame, error) {
	return v.respondPending(pendingID, "表单", source,
		func(slot pendingSlot) error {
			if slot.elicitation == nil {
				return fmt.Errorf("挂起 %d 不是表单", pendingID)
			}
			return nil
		},
		func(slot pendingSlot) (string, error) {
			return elicitationActionLabel(action), slot.elicitation.Respond(response)
		},
	)
}

// alreadyClosedLocked 已关闭挂起的判别错误构造（历史未命中返回 nil——回落「不存在或已
// 关闭」原语义）。
func (v *sessionView) alreadyClosedLocked(kind string, pendingID uint64) error {
	info, ok := v.closed[pendingID]
	if !ok {
		return nil
	}
	return &PendingAlreadyHandledError{Kind: kind, Source: info.source, Label: info.label, Settled: info.settled}
}

// recordClosedLocked 登记已关闭挂起（环形上限：超限逐出最旧；同 ID 重复登记只覆写详情
// 不重排队序）。
func (v *sessionView) recordClosedLocked(pendingID uint64, info closedPending) {
	if _, exists := v.closed[pendingID]; !exists {
		v.closedOrder = append(v.closedOrder, pendingID)
		if len(v.closedOrder) > closedHistoryCap {
			delete(v.closed, v.closedOrder[0])
			v.closedOrder = v.closedOrder[1:]
		}
	}
	v.closed[pendingID] = info
}

// pendingIndexOfLocked 按 pendingId 定位挂起槽位（不存在返回 -1）。
func pendingIndexOfLocked(pendings []pendingSlot, pendingID uint64) int {
	for i := range pendings {
		if pendings[i].view.PendingID == pendingID {
			return i
		}
	}
	return -1
}

// beginTurn 受理一轮回合：用户消息条目 + turnStarted 合成。已有活动回合拒绝（单会话
// 回合串行，与 acp.ErrTurnActive 同语义的视图侧前置闸门）。
func (v *sessionView) beginTurn(text string) ([]Frame, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.turnActive {
		return nil, acp.ErrTurnActive
	}
	v.turn++
	v.turnActive = true
	v.stopReason = ""
	entry := ConversationEntry{EntryID: fmt.Sprintf("t%d-%s", v.turn, entryKindUser), Kind: entryKindUser, Text: text}
	v.entries = append(v.entries, entry)
	return []Frame{
		{Type: FrameEntry, Entry: &entry},
		turnFrame(FrameTurnStarted, true, "", ""),
	}, nil
}

// endTurn 回合终态：turnEnded 合成（stopReason / 错误）+ 未决交互对齐清空（acp 侧
// settlePendings 已结算但无事件通道通知，视图补发 pendingClosed——其余订阅端不挂死在
// 已消失的审批上）。残余挂起登记 closed-history（settled）——回合结束后抵达的迟到
// 应答可判别（T2.4）。
func (v *sessionView) endTurn(stop string, errText string) []Frame {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.turnActive {
		return nil
	}
	v.turnActive = false
	v.stopReason = stop
	frames := []Frame{turnFrame(FrameTurnEnded, false, stop, errText)}
	for i := range v.pendings {
		v.recordClosedLocked(v.pendings[i].view.PendingID, closedPending{settled: true})
		frames = append(frames, pendingClosedFrame(v.pendings[i].view.PendingID))
	}
	v.pendings = nil
	return frames
}

// snapshot 投影快照。entries 浅拷贝安全：条目字段一经写入只被整体替换（chunk 聚合是
// string 追加 + 工具调用是整体指针替换），单写者 + 锁互斥下无共享可变态。
func (v *sessionView) snapshot() ViewSnapshot {
	v.mu.RLock()
	defer v.mu.RUnlock()
	snap := ViewSnapshot{
		Status:            v.status,
		AgentCode:         v.agentCode,
		AcpSessionID:      v.sessionID,
		PermissionMode:    v.permMode,
		Entries:           append([]ConversationEntry(nil), v.entries...),
		Pendings:          pendingViewsOf(v.pendings),
		Plan:              v.plan,
		Usage:             v.usage,
		AvailableCommands: v.commands,
		TurnActive:        v.turnActive,
		StopReason:        v.stopReason,
		Error:             v.errText,
	}
	if v.modes != nil {
		snap.AvailableModes = v.modes.AvailableModes
		snap.CurrentModeID = string(v.modes.CurrentModeID)
	}
	return snap
}

func pendingViewsOf(slots []pendingSlot) []PendingView {
	views := make([]PendingView, 0, len(slots))
	for i := range slots {
		views = append(views, slots[i].view)
	}
	return views
}

// textOfChunk 提取文本内容块文本（非文本块返回空串——一期仅文本流）。
func textOfChunk(chunk *schema.ContentChunk) string {
	if chunk != nil && chunk.Content.Text != nil {
		return chunk.Content.Text.Text
	}
	return ""
}

// 帧构造辅助（载荷与 Type 的配对集中在此，防错配）。

func statusFrame(t FrameType, payload StatusPayload) Frame {
	return Frame{Type: t, Status: &payload}
}

func pendingClosedFrame(pendingID uint64) Frame {
	return Frame{Type: FramePendingClosed, PendingID: pendingID}
}

func turnFrame(t FrameType, active bool, stop, errText string) Frame {
	return Frame{Type: t, Turn: &TurnPayload{Active: active, StopReason: stop, Error: errText}}
}
