package acpsession

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	acpgo "github.com/BrokkAi/acp-go"
	"github.com/BrokkAi/acp-go/schema"

	"ocean-harness/server/internal/acp"
)

// textChunkUpdate 构造 agent_message_chunk / agent_thought_chunk 的 update。
func textChunkUpdate(sessionID string, thought bool, text string) acp.SessionEvent {
	chunk := &schema.ContentChunk{Content: schema.ContentBlock{Text: &schema.TextContent{Text: text}}}
	update := schema.SessionUpdate{}
	if thought {
		update.AgentThoughtChunk = chunk
	} else {
		update.AgentMessageChunk = chunk
	}
	return acp.SessionEvent{Update: &acpgo.Update{SessionID: schema.SessionId(sessionID), Update: update}}
}

func framesOf(t *testing.T, view *sessionView, events ...acp.SessionEvent) []Frame {
	t.Helper()
	var frames []Frame
	for i := range events {
		frames = append(frames, view.applyEvent(&events[i])...)
	}
	return frames
}

func TestViewChunkAggregation(t *testing.T) {
	view := newSessionView()
	if _, err := view.beginTurn("帮我看看"); err != nil {
		t.Fatalf("beginTurn: %v", err)
	}
	frames := framesOf(t, view,
		textChunkUpdate("s1", false, "第一段"),
		textChunkUpdate("s1", false, "，第二段"),
		textChunkUpdate("s1", true, "思考中"),
		textChunkUpdate("s1", false, "，第三段"),
	)
	// 同回合同类 chunk 聚合进同一 entry：每个 chunk 事件出一帧 entry（3 次 agent + 1 次
	// thought），条目级最新态覆写。
	entryFrames := 0
	for _, f := range frames {
		if f.Type != FrameEntry {
			t.Fatalf("chunk 事件应只出 entry 帧，got %s", f.Type)
		}
		entryFrames++
	}
	if entryFrames != 4 {
		t.Fatalf("应出 4 个 entry 帧（3 次 agent + 1 次 thought），got %d", entryFrames)
	}
	snap := view.snapshot()
	if len(snap.Entries) != 3 {
		t.Fatalf("应有 user+agent+thought 三条目，got %+v", snap.Entries)
	}
	var agent, thought *ConversationEntry
	for i := range snap.Entries {
		switch snap.Entries[i].Kind {
		case entryKindAgentMessage:
			agent = &snap.Entries[i]
		case entryKindAgentThought:
			thought = &snap.Entries[i]
		}
	}
	if agent == nil || agent.Text != "第一段，第二段，第三段" {
		t.Fatalf("agent chunk 应聚合，got %+v", agent)
	}
	if thought == nil || thought.Text != "思考中" {
		t.Fatalf("thought chunk 应独立聚合，got %+v", thought)
	}
	if snap.Entries[0].Kind != entryKindUser || snap.Entries[0].Text != "帮我看看" {
		t.Fatalf("用户条目应首位，got %+v", snap.Entries[0])
	}
}

func TestViewToolCallPartialMerge(t *testing.T) {
	view := newSessionView()
	// 初建（tool_call 变体，Title 为值类型）。
	call := &schema.ToolCall{
		ToolCallID: "tc-1",
		Title:      "Bash: echo",
		Kind:       kindPtr(schema.ToolKindExecute),
		Status:     statusPtr(schema.ToolCallStatusPending),
		RawInput:   json.RawMessage(`{"cmd":"echo"}`),
	}
	view.applyEvent(&acp.SessionEvent{Update: &acpgo.Update{
		SessionID: "s1",
		Update:    schema.SessionUpdate{ToolCall: call},
	}})
	// 部分更新：仅 status + rawOutput 非空，其余保留。
	upd := &schema.ToolCallUpdate{
		ToolCallID: "tc-1",
		Status:     statusPtr(schema.ToolCallStatusCompleted),
		RawOutput:  json.RawMessage(`{"ok":true}`),
	}
	view.applyEvent(&acp.SessionEvent{Update: &acpgo.Update{
		SessionID: "s1",
		Update:    schema.SessionUpdate{ToolCallUpdate: upd},
	}})
	snap := view.snapshot()
	if len(snap.Entries) != 1 {
		t.Fatalf("同一 toolCallId 应单条目，got %+v", snap.Entries)
	}
	tc := snap.Entries[0].ToolCall
	if tc == nil || tc.ToolCallID != "tc-1" {
		t.Fatalf("工具条目缺失，got %+v", snap.Entries[0])
	}
	if tc.Title != "Bash: echo" || string(tc.Kind) != string(schema.ToolKindExecute) || string(tc.RawInput) != `{"cmd":"echo"}` {
		t.Fatalf("未更新字段应保留，got %+v", tc)
	}
	if tc.Status != string(schema.ToolCallStatusCompleted) || string(tc.RawOutput) != `{"ok":true}` {
		t.Fatalf("更新字段应覆盖，got %+v", tc)
	}

	// update 先于初建到达（异常时序）：建空壳再合并。
	orphan := &schema.ToolCallUpdate{ToolCallID: "tc-2", Title: strPtr("孤儿")}
	view.applyEvent(&acp.SessionEvent{Update: &acpgo.Update{
		SessionID: "s1",
		Update:    schema.SessionUpdate{ToolCallUpdate: orphan},
	}})
	snap = view.snapshot()
	if len(snap.Entries) != 2 {
		t.Fatalf("孤儿 update 应建条目，got %+v", snap.Entries)
	}
}

func TestViewSnapshotReplacement(t *testing.T) {
	view := newSessionView()
	plan := &schema.Plan{Entries: []schema.PlanEntry{{Content: "步骤一", Priority: schema.PlanEntryPriorityMedium, Status: schema.PlanEntryStatusPending}}}
	usage := &schema.UsageUpdate{Size: 100, Used: 42}
	commands := []schema.AvailableCommand{{Name: "create_plan", Description: "创建计划"}}
	events := []acp.SessionEvent{
		{Update: &acpgo.Update{SessionID: "s1", Update: schema.SessionUpdate{Plan: plan}}},
		{Update: &acpgo.Update{SessionID: "s1", Update: schema.SessionUpdate{UsageUpdate: usage}}},
		{Update: &acpgo.Update{SessionID: "s1", Update: schema.SessionUpdate{AvailableCommandsUpdate: &schema.AvailableCommandsUpdate{AvailableCommands: commands}}}},
		{Update: &acpgo.Update{SessionID: "s1", Update: schema.SessionUpdate{CurrentModeUpdate: &schema.CurrentModeUpdate{CurrentModeID: "acceptEdits"}}}},
	}
	frames := framesOf(t, view, events...)
	wantTypes := []FrameType{FramePlanUpdated, FrameUsageUpdated, FrameCommandsUpdated, FrameModeUpdated}
	for i, f := range frames {
		if f.Type != wantTypes[i] {
			t.Fatalf("帧[%d] = %s, want %s", i, f.Type, wantTypes[i])
		}
	}
	snap := view.snapshot()
	if snap.Plan != plan || snap.Usage != usage || len(snap.AvailableCommands) != 1 || snap.CurrentModeID != "acceptEdits" {
		t.Fatalf("快照类字段应最新替换，got %+v", snap)
	}
	// 最新替换：plan 二次推送覆盖。
	plan2 := &schema.Plan{Entries: []schema.PlanEntry{{Content: "步骤二", Priority: schema.PlanEntryPriorityHigh, Status: schema.PlanEntryStatusCompleted}}}
	framesOf(t, view, acp.SessionEvent{Update: &acpgo.Update{SessionID: "s1", Update: schema.SessionUpdate{Plan: plan2}}})
	if view.snapshot().Plan != plan2 {
		t.Fatal("plan 应被最新替换")
	}
}

func TestViewTurnGateAndEndFrames(t *testing.T) {
	view := newSessionView()
	frames, err := view.beginTurn("第一问")
	if err != nil {
		t.Fatalf("首回合应受理，got %v", err)
	}
	if frames[0].Type != FrameEntry || frames[0].Entry.Text != "第一问" {
		t.Fatalf("首帧应为用户条目，got %+v", frames[0])
	}
	if frames[1].Type != FrameTurnStarted || !frames[1].Turn.Active {
		t.Fatalf("次帧应为 turnStarted，got %+v", frames[1])
	}
	// 活动回合中再受理拒绝（与 acp.ErrTurnActive 同语义）。
	if _, err := view.beginTurn("第二问"); err != acp.ErrTurnActive {
		t.Fatalf("并发回合应拒绝，got %v", err)
	}
	// 终态合成 turnEnded（StopReason 只从 Prompt 返回值可见的 gap 补齐）。
	frames = view.endTurn(string(schema.StopReasonEndTurn), "")
	if len(frames) != 1 || frames[0].Type != FrameTurnEnded || frames[0].Turn.Active {
		t.Fatalf("应出 turnEnded 帧，got %+v", frames)
	}
	snap := view.snapshot()
	if snap.TurnActive || snap.StopReason != string(schema.StopReasonEndTurn) {
		t.Fatalf("终态应落快照，got %+v", snap)
	}
	// 二次 endTurn（无活动回合）无帧。
	if frames := view.endTurn(string(schema.StopReasonEndTurn), ""); frames != nil {
		t.Fatalf("非活动回合 endTurn 应无帧，got %+v", frames)
	}
}

func TestViewTerminatedIdempotent(t *testing.T) {
	view := newSessionView()
	frames := view.setTerminated("进程退出")
	if len(frames) != 1 || frames[0].Type != FrameTerminated {
		t.Fatalf("应出 terminated 帧，got %+v", frames)
	}
	if again := view.setTerminated("重复收尾"); again != nil {
		t.Fatalf("setTerminated 应幂等，got %+v", again)
	}
	snap := view.snapshot()
	if snap.Status != StatusTerminated || snap.Error != "进程退出" {
		t.Fatalf("终态与原因应落快照，got %+v", snap)
	}
}

// closed-history 环形上限与登记/查询语义（T2.4）。
func TestViewClosedHistoryRing(t *testing.T) {
	view := newSessionView()
	for id := uint64(1); id <= closedHistoryCap+8; id++ {
		view.recordClosedLocked(id, closedPending{source: PendingSourceBot, label: "允许"})
	}
	if len(view.closed) != closedHistoryCap || len(view.closedOrder) != closedHistoryCap {
		t.Fatalf("closed-history 应封顶 %d，got %d/%d", closedHistoryCap, len(view.closed), len(view.closedOrder))
	}
	if _, ok := view.closed[1]; ok {
		t.Fatal("最旧记录应被逐出")
	}
	if _, ok := view.closed[closedHistoryCap+8]; !ok {
		t.Fatal("最新记录应在环内")
	}
	// 同 ID 重复登记只覆写详情，不重排队序。
	view.recordClosedLocked(closedHistoryCap+8, closedPending{settled: true})
	if info, ok := view.closed[closedHistoryCap+8]; !ok || !info.settled || info.source != "" {
		t.Fatalf("重复登记应覆写详情，got %+v ok=%v", info, ok)
	}
	if len(view.closedOrder) != closedHistoryCap {
		t.Fatalf("重复登记不应扩序，got %d", len(view.closedOrder))
	}
}

// 收敛文案与选项标签 SSOT（桌面哨兵文案与 IM 状态行共用的取值域）。
func TestPendingConvergenceLabels(t *testing.T) {
	cases := []struct{ kind, name, want string }{
		{"allow_once", "Allow", "允许"},
		{"allow_always", "Allow always", "允许并记住"},
		{"reject_once", "Reject", "拒绝"},
		{"reject_always", "Always reject", "始终拒绝"},
		{"custom_kind", "自定义", "自定义"},
	}
	for _, c := range cases {
		if got := PermissionOptionLabel(c.kind, c.name); got != c.want {
			t.Fatalf("PermissionOptionLabel(%q) = %q, want %q", c.kind, got, c.want)
		}
	}
	err := &PendingAlreadyHandledError{Kind: "审批", Source: PendingSourcePanel, Label: "允许"}
	if msg := err.Error(); msg != "该审批已由桌面端应答（允许）" || !errors.Is(err, ErrPendingAlreadyHandled) {
		t.Fatalf("后到方哨兵文案/判别不符，got %q", msg)
	}
	if msg := (&PendingAlreadyHandledError{Kind: "表单", Settled: true}).Error(); msg != "该表单已随回合结束自动结算" {
		t.Fatalf("结算文案不符，got %q", msg)
	}
	if msg := (&PendingAlreadyHandledError{Kind: "审批", Source: "weird", Label: "x"}).Error(); !strings.Contains(msg, "另一端") {
		t.Fatalf("未知入口应中性兜底，got %q", msg)
	}
}

func TestViewSetReadyAndSnapshot(t *testing.T) {
	view := newSessionView()
	cfg := SessionConfig{AgentCode: "claude-acp", PermissionMode: acp.PermissionModeAcceptEdits}
	modes := &schema.SessionModeState{
		AvailableModes: []schema.SessionMode{{ID: "acceptEdits", Name: "Accept Edits"}, {ID: "bypassPermissions", Name: "Bypass"}},
		CurrentModeID:  "acceptEdits",
	}
	frames := view.setReady("sess-1", cfg, modes)
	if len(frames) != 1 || frames[0].Type != FrameSessionStatus || frames[0].Status.Status != StatusReady {
		t.Fatalf("应出 ready 帧且带 sessionId，got %+v", frames)
	}
	snap := view.snapshot()
	if snap.Status != StatusReady || snap.AcpSessionID != "sess-1" || snap.PermissionMode != string(acp.PermissionModeAcceptEdits) {
		t.Fatalf("ready 快照不符，got %+v", snap)
	}
	if snap.CurrentModeID != "acceptEdits" || len(snap.AvailableModes) != 2 {
		t.Fatalf("模式目录应入快照，got %+v", snap)
	}
}

func kindPtr(k schema.ToolKind) *schema.ToolKind               { return &k }
func statusPtr(s schema.ToolCallStatus) *schema.ToolCallStatus { return &s }
func strPtr(s string) *string                                  { return &s }
