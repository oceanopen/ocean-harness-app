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
		if len(evs) != 2 || evs[0].Type != TurnToolUse || evs[1].Type != TurnStatus || evs[1].Status != "🔧 Bash 已完成" {
			t.Fatalf("tool_result 应经 id→名映射转完成状态行: %+v", evs)
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
		if len(evs) != 1 || evs[0].Type != TurnStatus || evs[0].Status != "🔧 工具 已完成" {
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
 * --mcp-config（内容指向 im 投递端点）+ send_file 白名单位，cleanup 删临时配置文件。
 */
func TestBuildClaudeArgsDelivery(t *testing.T) {
	args, cleanup, err := buildClaudeArgs(TurnRequest{SystemPrompt: "sp"})
	if err != nil {
		t.Fatalf("无令牌构建: %v", err)
	}
	cleanup()
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--mcp-config") || strings.Contains(joined, imSendFileToolName) {
		t.Fatalf("无令牌不应注入 MCP 配置: %s", joined)
	}

	args, cleanup, err = buildClaudeArgs(TurnRequest{Port: 9100, DeliveryToken: "tk-1"})
	if err != nil {
		t.Fatalf("带令牌构建: %v", err)
	}
	joined = strings.Join(args, " ")
	if !strings.Contains(joined, "--mcp-config") || !strings.Contains(joined, imSendFileToolName) {
		t.Fatalf("带令牌应注入 MCP 配置与白名单: %s", joined)
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
