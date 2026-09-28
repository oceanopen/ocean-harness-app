package bot

import (
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
)

func eventsOf(t *testing.T, line string) []TurnEvent {
	t.Helper()
	return parseClaudeLine(line)
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
	t.Run("thinking 增量过滤", func(t *testing.T) {
		if evs := eventsOf(t, fxThinkingDelta); len(evs) != 0 {
			t.Fatalf("thinking_delta 应被过滤: %+v", evs)
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
	t.Run("user 工具结果不产事件", func(t *testing.T) {
		if evs := eventsOf(t, fxUserToolResult); len(evs) != 0 {
			t.Fatalf("got %+v", evs)
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

func TestParseClaudeProbe(t *testing.T) {
	t.Run("正常形态（rc 噪声不误判）", func(t *testing.T) {
		out := strings.Join([]string{
			"welcome from rc",
			"/usr/bin/python3: No module named pyenv",
			probeSentinelOpen,
			"/Users/x/.nvm/versions/node/v22/bin/claude",
			probeSentinelClose,
			"HOME=/Users/x",
			"PATH=/a:/b",
			"PATH=/x:/usr/bin",
			"",
		}, "\n")
		got := parseClaudeProbe(out)
		if got == nil || got.Bin != "/Users/x/.nvm/versions/node/v22/bin/claude" || got.LoginPath != "/x:/usr/bin" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("builtin（非 / 开头）判失败", func(t *testing.T) {
		out := probeSentinelOpen + "\necho\n" + probeSentinelClose + "\nPATH=/usr/bin\n"
		if got := parseClaudeProbe(out); got != nil {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("哨兵缺失裸路径不采信", func(t *testing.T) {
		if got := parseClaudeProbe("/opt/homebrew/bin/claude\nPATH=/usr/bin\n"); got != nil {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("缺 PATH 行判失败", func(t *testing.T) {
		out := probeSentinelOpen + "\n/bin/cat\n" + probeSentinelClose + "\n"
		if got := parseClaudeProbe(out); got != nil {
			t.Fatalf("got %+v", got)
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
