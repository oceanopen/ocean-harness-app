package acpsession

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ironpark/acp-go/acp1"

	"ocean-harness/server/internal/acp"
)

/*
 * golden SSE 契约测试：Frame 的 JSON 序列化形态在此钉死，internal/acp 换底（BrokkAi → ironpark）
 * 期间 golden 文件不许变——字段名、omitempty 取舍、嵌套 wire 结构漂移即测试红。
 * 比对按解码后语义等价（键序无关），golden 文件为缩进格式便于 diff 审阅；-update 回写基线。
 */
var goldenUpdate = flag.Bool("update", false, "回写 golden 快照文件")

// goldenPtr 样例装配辅助（指针型字段的取址）。
func goldenPtr[T any](v T) *T { return &v }

func goldenSampleModes() []acp1.SessionMode {
	return []acp1.SessionMode{
		{ID: "code", Name: "Code", Description: goldenPtr("直接执行编码任务")},
		{ID: "architect", Name: "Architect"},
	}
}

func goldenSamplePlan() *PlanView {
	return &PlanView{Entries: []acp1.PlanEntry{
		{Content: "定位回归根因", Priority: acp1.PlanEntryPriorityHigh, Status: acp1.PlanEntryStatusCompleted},
		{Content: "补回归测试", Priority: acp1.PlanEntryPriorityMedium, Status: acp1.PlanEntryStatusInProgress},
		{Content: "更新文档", Priority: acp1.PlanEntryPriorityLow, Status: acp1.PlanEntryStatusPending},
	}}
}

func goldenSampleUsage() *UsageView {
	return &UsageView{
		Size: 200000,
		Used: 123456,
		Cost: &acp1.Cost{Amount: 0.42, Currency: "USD"},
	}
}

func goldenSampleCommands() []acp1.AvailableCommand {
	return []acp1.AvailableCommand{
		{Name: "review", Description: "审查当前变更"},
		{Name: "plan", Description: "制定计划", Input: &acp1.AvailableCommandInput{Hint: "输入计划目标"}},
	}
}

func goldenSampleToolCallView() *ToolCallView {
	return &ToolCallView{
		ToolCallID: "call-1",
		Title:      "读取文件",
		Kind:       string(acp1.ToolKindRead),
		Status:     string(acp1.ToolCallStatusInProgress),
		Name:       "Read",
		Content: []acp1.ToolCallContent{
			acp1.NewToolCallContent(acp1.ToolCallContentContent{Content: acp1.NewContentBlock(acp1.ContentBlockText{Text: "文件前 42 行"})}),
			acp1.NewToolCallContent(acp1.ToolCallContentDiff{Path: "src/main.go", OldText: goldenPtr("func old()"), NewText: "func new()"}),
			acp1.NewToolCallContent(acp1.ToolCallContentTerminal{TerminalID: "term-1"}),
		},
		Locations: []acp1.ToolCallLocation{
			{Path: "src/main.go", Line: goldenPtr(uint32(12))},
			{Path: "src/util.go"},
		},
		RawInput:  json.RawMessage(`{"file_path":"src/main.go"}`),
		RawOutput: json.RawMessage(`{"lines":42}`),
	}
}

func goldenSamplePermissionPending() PendingView {
	return PendingView{
		PendingID: 7,
		Kind:      "permission",
		Options: []acp1.PermissionOption{
			{OptionID: "opt-allow", Kind: acp1.PermissionOptionKindAllowOnce, Name: "Allow"},
			{OptionID: "opt-reject-always", Kind: acp1.PermissionOptionKindRejectAlways, Name: "Reject"},
		},
		ToolCall: &acp1.ToolCallUpdate{
			ToolCallID: "call-1",
			Title:      goldenPtr("读取文件"),
			Kind:       goldenPtr(acp1.ToolKindRead),
			Status:     goldenPtr(acp1.ToolCallStatusPending),
			Name:       goldenPtr("Read"),
			Content: []acp1.ToolCallContent{
				acp1.NewToolCallContent(acp1.ToolCallContentContent{Content: acp1.NewContentBlock(acp1.ContentBlockText{Text: "工具调用详情"})}),
			},
			Locations: []acp1.ToolCallLocation{{Path: "src/main.go"}},
			RawInput:  json.RawMessage(`{"file_path":"src/main.go"}`),
		},
	}
}

func goldenSampleElicitationForm() *acp.ElicitationWire {
	return &acp.ElicitationWire{
		Mode:            "form",
		Message:         "请填写部署参数",
		SessionID:       "acp-sess-1",
		ToolCallID:      "call-2",
		RequestID:       "req-1",
		RequestedSchema: json.RawMessage(`{"type":"object","properties":{"region":{"type":"string"}}}`),
		ElicitationID:   "elc-1",
	}
}

func goldenSampleSnapshot() *ViewSnapshot {
	return &ViewSnapshot{
		Status:         StatusReady,
		AgentCode:      "claude-code",
		AcpSessionID:   "acp-sess-1",
		PermissionMode: "approve-per-tool",
		CurrentModeID:  "code",
		AvailableModes: goldenSampleModes(),
		Entries: []ConversationEntry{
			{
				EntryID: "t1-user",
				Kind:    entryKindUser,
				Text:    "修复登录回归",
				Source:  EntrySourceBot,
				Display: &EntryDisplay{
					Text:  "修复登录回归",
					Quote: &EntryQuote{Author: "张三", Text: "登录页 500", Truncated: true},
					Files: []EntryFile{{Name: "report.log", Path: "/tmp/report.log"}},
				},
			},
			{EntryID: "t1-agentMessage", Kind: entryKindAgentMessage, Text: "已定位到会话过期判断缺陷"},
			{EntryID: "tool:call-1", Kind: entryKindToolCall, ToolCall: goldenSampleToolCallView()},
		},
		Pendings:          []PendingView{goldenSamplePermissionPending(), {PendingID: 8, Kind: "elicitation", Request: goldenSampleElicitationForm()}},
		Plan:              goldenSamplePlan(),
		Usage:             goldenSampleUsage(),
		AvailableCommands: goldenSampleCommands(),
		TurnActive:        true,
		StopReason:        "end_turn",
	}
}

var goldenCases = []struct {
	name  string
	frame Frame
}{
	{
		name:  "snapshot",
		frame: Frame{Seq: 1, IssueID: "iss-1", Type: FrameSnapshot, Snapshot: goldenSampleSnapshot()},
	},
	{
		name:  "session_status_starting",
		frame: Frame{Seq: 2, IssueID: "iss-1", Type: FrameSessionStatus, Status: &StatusPayload{Status: StatusStarting, AgentCode: "claude-code"}},
	},
	{
		name:  "session_status_ready",
		frame: Frame{Seq: 3, IssueID: "iss-1", Type: FrameSessionStatus, Status: &StatusPayload{Status: StatusReady, AgentCode: "claude-code", AcpSessionID: "acp-sess-1"}},
	},
	{
		name:  "session_status_failed",
		frame: Frame{Seq: 4, IssueID: "iss-1", Type: FrameSessionStatus, Status: &StatusPayload{Status: StatusFailed, AgentCode: "claude-code", Error: "spawn 失败: 进程退出"}},
	},
	{
		name:  "terminated",
		frame: Frame{Seq: 5, IssueID: "iss-1", Type: FrameTerminated, Status: &StatusPayload{Status: StatusTerminated, AgentCode: "claude-code", AcpSessionID: "acp-sess-1", Error: "进程意外退出"}},
	},
	{
		name:  "entry_user_display",
		frame: Frame{Seq: 6, IssueID: "iss-1", Type: FrameEntry, Entry: goldenPtr(goldenSampleSnapshot().Entries[0])},
	},
	{
		name:  "entry_tool_call",
		frame: Frame{Seq: 7, IssueID: "iss-1", Type: FrameEntry, Entry: goldenPtr(goldenSampleSnapshot().Entries[2])},
	},
	{
		name:  "plan_updated",
		frame: Frame{Seq: 8, IssueID: "iss-1", Type: FramePlanUpdated, Plan: goldenSamplePlan()},
	},
	{
		name:  "usage_updated",
		frame: Frame{Seq: 9, IssueID: "iss-1", Type: FrameUsageUpdated, Usage: goldenSampleUsage()},
	},
	{
		name:  "commands_updated",
		frame: Frame{Seq: 10, IssueID: "iss-1", Type: FrameCommandsUpdated, Commands: goldenSampleCommands()},
	},
	{
		name:  "mode_updated",
		frame: Frame{Seq: 11, IssueID: "iss-1", Type: FrameModeUpdated, Mode: &acp1.SessionModeState{CurrentModeID: "architect", AvailableModes: goldenSampleModes()}},
	},
	{
		name:  "pending_opened_permission",
		frame: Frame{Seq: 12, IssueID: "iss-1", Type: FramePendingOpened, Pending: goldenPtr(goldenSamplePermissionPending())},
	},
	{
		name:  "pending_opened_elicitation_form",
		frame: Frame{Seq: 13, IssueID: "iss-1", Type: FramePendingOpened, Pending: &PendingView{PendingID: 8, Kind: "elicitation", Request: goldenSampleElicitationForm()}},
	},
	{
		name:  "pending_opened_elicitation_url",
		frame: Frame{Seq: 14, IssueID: "iss-1", Type: FramePendingOpened, Pending: &PendingView{PendingID: 9, Kind: "elicitation", Request: &acp.ElicitationWire{Mode: "url", Message: "前往授权页", SessionID: "acp-sess-1", URL: "https://example.com/auth"}}},
	},
	{
		name:  "pending_closed",
		frame: Frame{Seq: 15, IssueID: "iss-1", Type: FramePendingClosed, PendingID: 7},
	},
	{
		name:  "turn_started",
		frame: Frame{Seq: 16, IssueID: "iss-1", Type: FrameTurnStarted, Turn: &TurnPayload{Active: true}},
	},
	{
		name:  "turn_ended",
		frame: Frame{Seq: 17, IssueID: "iss-1", Type: FrameTurnEnded, Turn: &TurnPayload{Active: false, StopReason: "end_turn"}},
	},
	{
		name:  "turn_ended_error",
		frame: Frame{Seq: 18, IssueID: "iss-1", Type: FrameTurnEnded, Turn: &TurnPayload{Active: false, Error: "上游连接中断"}},
	},
}

// goldenPretty 解码值重排为缩进 JSON（map 键序归一，供 golden 落盘与漂移 diff 展示）。
func goldenPretty(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func TestGoldenSSEFrames(t *testing.T) {
	for _, c := range goldenCases {
		t.Run(c.name, func(t *testing.T) {
			got, err := json.Marshal(c.frame)
			if err != nil {
				t.Fatalf("帧序列化失败: %v", err)
			}
			path := filepath.Join("testdata", "sse_golden", c.name+".json")
			if *goldenUpdate {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("创建 golden 目录失败: %v", err)
				}
				if err := os.WriteFile(path, []byte(goldenPretty(json.RawMessage(got))+"\n"), 0o644); err != nil {
					t.Fatalf("回写 golden 失败: %v", err)
				}
				return
			}
			wantRaw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读取 golden 失败（基线缺失时以 -update 生成）: %v", err)
			}
			var gotVal, wantVal any
			if err := json.Unmarshal(got, &gotVal); err != nil {
				t.Fatalf("实际帧解码失败: %v", err)
			}
			if err := json.Unmarshal(wantRaw, &wantVal); err != nil {
				t.Fatalf("golden 解码失败: %v", err)
			}
			if !reflect.DeepEqual(gotVal, wantVal) {
				t.Fatalf("SSE 契约漂移\n--- golden ---\n%s\n--- 实际 ---\n%s", goldenPretty(wantVal), goldenPretty(gotVal))
			}
		})
	}
}
