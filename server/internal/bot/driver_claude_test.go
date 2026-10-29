package bot

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// fixture 均为 claude 2.1.228 实测捕获的 stream-json 行（原样回放防 schema 漂移回归）。
const (
	fxInit = `{"type":"system","subtype":"init","cwd":"/private/tmp","session_id":"e0dab95b-9724-42a1-a338-e41b3b13abf7","tools":["Task","Bash"],"model":"glm-5.3-flash","permissionMode":"default","claude_code_version":"2.1.228"}`

	fxThinkingNoise = `{"type":"system","subtype":"thinking_tokens","estimated_tokens":1,"estimated_tokens_delta":1,"uuid":"37786709"}`

	fxThinkingDelta = `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"The user"}},"session_id":"553310e2","parent_tool_use_id":null}`

	fxTextDelta = `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"好的"}},"session_id":"553310e2","parent_tool_use_id":null}`

	fxAssistantToolUse = `{"type":"assistant","message":{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}],"stop_reason":null},"session_id":"553310e2"}`

	fxAssistantTextBlock = `{"type":"assistant","message":{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"好的"}]},"session_id":"553310e2"}`

	fxResult = `{"is_error":false,"duration_api_ms":5969,"num_turns":1,"stop_reason":"end_turn","session_id":"553310e2","total_cost_usd":0.12,"subtype":"success","result":"好的","type":"result"}`

	fxResultError = `{"is_error":true,"subtype":"error_max_turns","session_id":"553310e2","result":"stopped","type":"result"}`

	fxUserToolResult = `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"file.txt"}]},"session_id":"553310e2"}`

	fxUserToolResultError = `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","is_error":true,"content":"boom"}]},"session_id":"553310e2"}`
)

func eventsOf(t *testing.T, line string) []TurnEvent {
	t.Helper()
	return parseClaudeLine(line, &turnParseState{toolNames: map[string]string{}})
}

// eventsOfLines 多行共享解析状态回放（工具注册→终态翻译、思考累计等跨行用例）。
func eventsOfLines(t *testing.T, lines ...string) []TurnEvent {
	t.Helper()
	st := &turnParseState{toolNames: map[string]string{}}
	var evs []TurnEvent
	for _, line := range lines {
		evs = append(evs, parseClaudeLine(line, st)...)
	}
	return evs
}

func TestParseClaudeLine(t *testing.T) {
	t.Run("init 捕获会话 id", func(t *testing.T) {
		evs := eventsOf(t, fxInit)
		if len(evs) != 1 || evs[0].Type != TurnInit || evs[0].SessionID != "e0dab95b-9724-42a1-a338-e41b3b13abf7" {
			t.Fatalf("got %+v", evs)
		}
	})
	t.Run("text_delta 增量", func(t *testing.T) {
		evs := eventsOf(t, fxTextDelta)
		if len(evs) != 1 || evs[0].Type != TurnText || evs[0].Delta != "好的" {
			t.Fatalf("got %+v", evs)
		}
	})
	t.Run("thinking 增量事件", func(t *testing.T) {
		evs := eventsOf(t, fxThinkingDelta)
		if len(evs) != 1 || evs[0].Type != TurnThinking || evs[0].Delta != "The user" {
			t.Fatalf("thinking_delta 应转 TurnThinking 增量事件: %+v", evs)
		}
	})
	t.Run("thinking 多增量差分（截取在泵侧）", func(t *testing.T) {
		long := strings.Repeat("思", 300)
		first := `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"起手"}},"session_id":"s"}`
		second, err := json.Marshal(map[string]any{"type": "stream_event", "session_id": "s", "event": map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "thinking_delta", "thinking": long},
		}})
		if err != nil {
			t.Fatalf("构造夹具: %v", err)
		}
		evs := eventsOfLines(t, first, string(second))
		if len(evs) != 2 || evs[0].Type != TurnThinking || evs[0].Delta != "起手" || evs[1].Type != TurnThinking || evs[1].Delta != long {
			t.Fatalf("思考应按水位线差分发增量事件:\n%+v", evs)
		}
	})
	t.Run("thinking_tokens 噪声过滤", func(t *testing.T) {
		if evs := eventsOf(t, fxThinkingNoise); len(evs) != 0 {
			t.Fatalf("噪声应被过滤: %+v", evs)
		}
	})
	t.Run("assistant tool_use", func(t *testing.T) {
		evs := eventsOf(t, fxAssistantToolUse)
		if len(evs) != 1 || evs[0].Type != TurnToolUse || evs[0].ToolName != "Bash" {
			t.Fatalf("got %+v", evs)
		}
	})
	t.Run("assistant 整段 text 不产事件（防与增量重复累计）", func(t *testing.T) {
		if evs := eventsOf(t, fxAssistantTextBlock); len(evs) != 0 {
			t.Fatalf("assistant text 块应忽略: %+v", evs)
		}
	})
	t.Run("result 终态", func(t *testing.T) {
		evs := eventsOf(t, fxResult)
		if len(evs) != 1 || evs[0].Type != TurnDone || evs[0].Result != "好的" || evs[0].IsError {
			t.Fatalf("got %+v", evs)
		}
	})
	t.Run("result 错误终态", func(t *testing.T) {
		evs := eventsOf(t, fxResultError)
		if len(evs) != 1 || evs[0].Type != TurnDone || !evs[0].IsError {
			t.Fatalf("got %+v", evs)
		}
	})
	t.Run("user 工具结果转完成状态行", func(t *testing.T) {
		evs := eventsOfLines(t, fxAssistantToolUse, fxUserToolResult)
		if len(evs) != 2 || evs[0].Type != TurnToolUse || evs[1].Type != TurnStatus ||
			evs[1].Status != "✅ Bash 已完成" || evs[1].ToolName != "Bash" {
			t.Fatalf("tool_result 应经 id→名映射转带工具名的完成终态: %+v", evs)
		}
	})
	t.Run("user 工具失败结果转失败状态行", func(t *testing.T) {
		evs := eventsOfLines(t, fxAssistantToolUse, fxUserToolResultError)
		if len(evs) != 2 || evs[1].Type != TurnStatus || evs[1].Status != "❌ Bash 失败" {
			t.Fatalf("is_error 的 tool_result 应转失败状态行: %+v", evs)
		}
	})
	t.Run("未注册 id 的工具结果回落中性名", func(t *testing.T) {
		evs := eventsOf(t, fxUserToolResult)
		if len(evs) != 1 || evs[0].Type != TurnStatus || evs[0].Status != "✅ 工具 已完成" {
			t.Fatalf("未知 tool_use_id 应回落中性名: %+v", evs)
		}
	})
	t.Run("非法行静默丢弃", func(t *testing.T) {
		for _, line := range []string{"", "   ", "not json", `{"type":"unknown"}`} {
			if evs := eventsOf(t, line); len(evs) != 0 {
				t.Fatalf("%q 应无事件: %+v", line, evs)
			}
		}
	})
}

func TestTurnEnv(t *testing.T) {
	t.Setenv("PATH", "/gui/bin")
	t.Setenv("OCEAN_HARNESS_PORT", "9999")

	// 有 loginPath：原 PATH 被替换为 login PATH。
	env := turnEnv("/login/bin", 9200)
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "PATH=/gui/bin") {
		t.Fatalf("旧 PATH 应被剔除:\n%s", joined)
	}
	if !strings.Contains(joined, "PATH=/login/bin") || !strings.Contains(joined, "OCEAN_HARNESS_PORT=9200") {
		t.Fatalf("缺注入项:\n%s", joined)
	}

	// 无 loginPath（第 1/2 级探测命中）：必须保留原 PATH——否则子进程丢系统路径、Bash 全废。
	env2 := turnEnv("", 9200)
	joined2 := strings.Join(env2, "\n")
	if !strings.Contains(joined2, "PATH=/gui/bin") {
		t.Fatalf("无 loginPath 时应保留原 PATH:\n%s", joined2)
	}
	for _, kv := range env2 {
		if kv == "PATH=" {
			t.Fatalf("不应注入空 PATH")
		}
	}
}

/*
 * TestBuildClaudeArgsDelivery 投递令牌 → argv 注入矩阵：无令牌零注入；带令牌注入
 * --mcp-config（内容指向 im 投递端点），cleanup 删临时配置文件；工具白名单恒不注入。
 */
func TestBuildClaudeArgsDelivery(t *testing.T) {
	args, cleanup, err := buildClaudeArgs(TurnRequest{SystemPrompt: "sp"})
	if err != nil {
		t.Fatalf("无令牌构建: %v", err)
	}
	cleanup()
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--mcp-config") {
		t.Fatalf("无令牌不应注入 MCP 配置: %s", joined)
	}

	args, cleanup, err = buildClaudeArgs(TurnRequest{Port: 9100, DeliveryToken: "tk-1"})
	if err != nil {
		t.Fatalf("带令牌构建: %v", err)
	}
	joined = strings.Join(args, " ")
	if !strings.Contains(joined, "--mcp-config") {
		t.Fatalf("带令牌应注入 MCP 配置: %s", joined)
	}
	if strings.Contains(joined, "--allowedTools") {
		t.Fatalf("headless 不注入工具白名单（权限档全权表达）: %s", joined)
	}
	idx := -1
	for i, a := range args {
		if a == "--mcp-config" {
			idx = i
		}
	}
	data, err := os.ReadFile(args[idx+1])
	if err != nil {
		t.Fatalf("临时配置可读: %v", err)
	}
	cfgText := string(data)
	if !strings.Contains(cfgText, "http://127.0.0.1:9100/mcp/streamableHttp/im/tk-1") ||
		!strings.Contains(cfgText, `"type":"http"`) {
		t.Fatalf("配置应指向 im 投递端点: %s", cfgText)
	}
	cleanup()
	if _, err := os.Stat(args[idx+1]); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cleanup 应删除临时配置文件")
	}
}

// TestBuildClaudeArgsPermissions headless 全自动恒定：argv 恒带 --dangerously-skip-permissions（权限档已全局收敛）。
func TestBuildClaudeArgsPermissions(t *testing.T) {
	args, cleanup, err := buildClaudeArgs(TurnRequest{})
	if err != nil {
		t.Fatalf("构建: %v", err)
	}
	cleanup()
	if joined := strings.Join(args, " "); !strings.Contains(joined, "--dangerously-skip-permissions") {
		t.Fatalf("headless 应恒定全自动: %s", joined)
	}
}

// TestBuildClaudeArgsPresetSessionID headless 预置锚：PresetSessionID 非空注入 --session-id
// （新会话即预置 id）；与 SessionID 并存时 resume 优先（active 锚优先级高于预置注入）。
func TestBuildClaudeArgsPresetSessionID(t *testing.T) {
	args, cleanup, err := buildClaudeArgs(TurnRequest{PresetSessionID: "preset-1"})
	if err != nil {
		t.Fatalf("预置锚构建: %v", err)
	}
	cleanup()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--session-id preset-1") || strings.Contains(joined, "--resume") {
		t.Fatalf("预置锚应注入 --session-id: %s", joined)
	}

	args, cleanup, err = buildClaudeArgs(TurnRequest{SessionID: "sess-1", PresetSessionID: "preset-1"})
	if err != nil {
		t.Fatalf("双锚构建: %v", err)
	}
	cleanup()
	joined = strings.Join(args, " ")
	if !strings.Contains(joined, "--resume sess-1") || strings.Contains(joined, "--session-id") {
		t.Fatalf("active 锚应优先 resume 且不注入预置: %s", joined)
	}
}

// TestBuildClaudeArgsResume headless 续聊锚：SessionID 非空注入 --resume，空不开新参数。
func TestBuildClaudeArgsResume(t *testing.T) {
	args, cleanup, err := buildClaudeArgs(TurnRequest{})
	if err != nil {
		t.Fatalf("新会话构建: %v", err)
	}
	cleanup()
	if joined := strings.Join(args, " "); strings.Contains(joined, "--resume") {
		t.Fatalf("无锚不应带 --resume: %s", joined)
	}

	args, cleanup, err = buildClaudeArgs(TurnRequest{SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("resume 构建: %v", err)
	}
	cleanup()
	joined := strings.Join(args, " ")
	idx := -1
	for i, a := range args {
		if a == "--resume" {
			idx = i
		}
	}
	if idx < 0 || idx+1 >= len(args) || args[idx+1] != "sess-1" {
		t.Fatalf("SessionID 应注入 --resume: %s", joined)
	}
}

// CLI 合成回复过滤：text_delta 与 result 均不作为回合产出（空产出走泵侧吞没重试）。
func TestParseClaudeLineFiltersSyntheticNoReply(t *testing.T) {
	st := &turnParseState{}
	got := parseClaudeLine(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"No response requested."}}}`, st)
	if len(got) != 0 {
		t.Fatalf("合成回复 text_delta 应被过滤: %+v", got)
	}
	done := parseClaudeLine(`{"type":"result","result":"No response requested.","session_id":"s-1"}`, st)
	if len(done) != 1 || done[0].Type != TurnDone || done[0].Result != "" || done[0].SessionID != "s-1" {
		t.Fatalf("合成回复 result 应清空产出（会话 id 保留）: %+v", done)
	}
}
