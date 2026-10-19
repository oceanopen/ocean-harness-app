package acp_test

/*
 * 真握手验收为外部测试包（acp_test）：缺省命令取自 internal/agentcatalog 的内嵌
 * catalog——acp 域不感知 catalog（agentcatalog → acp 单向依赖），测试侧经导出面装配。
 */

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ironpark/acp-go/acp1"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/agentcatalog"
	"ocean-harness/server/internal/dal/enums"
)

/*
 * TestRealHandshake 真机验收全链路（含 T2 spike 收编断言）：生产拉起链路（catalog → EnsureVendored → Launch）
 * initialize → NewSession → 两轮 prompt——断言 claudeCode 扩展、modes 双轨形状、_meta.systemPrompt 透传闭环、elicitation 全字段。
 * 需要真实凭据与网络，不进常规测试轮次：pnpm server:vendor:prepare 后以 OCEAN_ACP_REAL_HANDSHAKE=1 go test ./internal/acp -run TestRealHandshake -v 运行；catalog pin 升级验证即重跑本用例；资源源/受管根/拉起命令可经 OCEAN_ACP_RESOURCES_DIR/OCEAN_ACP_ADAPTERS_DIR/OCEAN_ACP_REAL_COMMAND 覆盖。
 */
func TestRealHandshake(t *testing.T) {
	if os.Getenv("OCEAN_ACP_REAL_HANDSHAKE") != "1" {
		t.Skip("一次性真握手验收：需要 OCEAN_ACP_REAL_HANDSHAKE=1 与本机 claude 凭据")
	}
	entry, ok := agentcatalog.GetAgentCatalogInfoByCode(string(enums.AGENT_CODE_CLAUDE_ACP))
	if !ok {
		t.Fatal("catalog 缺映射条目 claude-acp")
	}
	srcRoot := os.Getenv("OCEAN_ACP_RESOURCES_DIR")
	if srcRoot == "" {
		srcRoot = filepath.Join("..", "..", "..", "app", "resources", "acp-adapters")
		if _, err := os.Stat(srcRoot); err != nil {
			t.Fatalf("vendored 资源源缺失（先跑 pnpm server:vendor:prepare）: %v", err)
		}
	}
	dstRoot := os.Getenv("OCEAN_ACP_ADAPTERS_DIR")
	if dstRoot == "" {
		dstRoot = t.TempDir()
	}
	installDir, err := agentcatalog.EnsureVendored(entry, srcRoot, dstRoot)
	if err != nil {
		t.Fatalf("vendored 受管安装: %v", err)
	}
	claudeBin, err := agentcatalog.VendoredClaudeBin(installDir)
	if err != nil {
		t.Fatalf("vendored 自带 claude 解析: %v", err)
	}
	env, err := acp.ClaudeEnvOverrides(entry.Env, claudeBin)
	if err != nil {
		t.Fatalf("claude 路径一致性注入: %v", err)
	}
	cfg, err := entry.VendoredSpawnConfig(srcRoot, installDir, t.TempDir(), env)
	if err != nil {
		t.Fatalf("vendored 入口翻译: %v", err)
	}
	if custom := os.Getenv("OCEAN_ACP_REAL_COMMAND"); custom != "" {
		cfg.Command = strings.Fields(custom)
	}

	ctx := context.Background()
	client, err := acp.Launch(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer client.Close()

	init, err := client.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Logf("agent: %v 协议版本: %d", init.AgentInfo, init.ProtocolVersion)
	if !acp.HasClaudeCodeExtension(init) {
		t.Fatal("agentCapabilities._meta 缺 claudeCode 扩展标记")
	}

	meta := acp1.Meta{}
	if err := meta.Set("systemPrompt", "Your name is "+handshakeSystemToken+". When asked for your name, reply with exactly that token and nothing else."); err != nil {
		t.Fatalf("_meta 构造: %v", err)
	}
	session, err := client.NewSession(ctx, acp.NewSessionParams{Meta: meta})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	assertAvailableModes(t, session.Modes())

	// 轮1：问名验证 _meta.systemPrompt 注入闭环
	text1, _ := runHandshakeTurn(t, client, session, "What is your name? Reply with only your name.", nil)
	if !strings.Contains(text1, handshakeSystemToken) {
		t.Fatalf("_meta.systemPrompt 透传闭环失败: 回复不含注入 token(回复: %q)", text1)
	}

	// 轮2：点名 AskUserQuestion 触发 form elicitation，accept 应答后回合收敛
	var elicWire *acp.ElicitationWire
	text2, toolCalls := runHandshakeTurn(t, client, session, "Use the AskUserQuestion tool to ask me: 'Which database should we use?' with options 'SQLite' (lightweight, file-based) and 'PostgreSQL' (production-grade). Do not proceed until I answer.", func(pending *acp.PendingElicitation) {
		elicWire = pending.Wire()
		answer, err := acp1.NewElicitationContentValue("PostgreSQL")
		if err != nil {
			t.Errorf("构造应答内容: %v", err)
			return
		}
		accept := acp1.NewCreateElicitationResponse(acp1.CreateElicitationResponseAccept{
			Content: map[string]acp1.ElicitationContentValue{"question_0": answer},
		})
		if err := pending.Respond(accept); err != nil {
			t.Errorf("Respond(accept): %v", err)
		}
	})
	assertElicitationWire(t, elicWire, session.ID(), toolCalls)
	if strings.TrimSpace(text2) == "" {
		t.Fatal("elicitation accept 应答后未收到任何文本 chunk")
	}
}

// runHandshakeTurn 发一轮 prompt 并排空事件至回合收敛，返回累积文本与已见 tool_call id；
// onElicitation 非 nil 时对 elicitation 事件回调（应答在回调内完成），权限事件一律取消防挂死。
func runHandshakeTurn(t *testing.T, client *acp.AgentClient, session *acp.Session, prompt string, onElicitation func(*acp.PendingElicitation)) (string, map[acp1.ToolCallID]bool) {
	t.Helper()
	type promptResult struct {
		stop acp1.StopReason
		err  error
	}
	results := make(chan promptResult, 1)
	go func() {
		stop, promptErr := client.PromptText(context.Background(), session, prompt)
		results <- promptResult{stop: stop, err: promptErr}
	}()

	var text strings.Builder
	toolCalls := map[acp1.ToolCallID]bool{}
	handleEvent := func(event acp.SessionEvent) {
		if event.Terminated {
			t.Fatal("会话在回合收敛前终结")
		}
		if event.Update != nil {
			switch v := event.Update.Update.Variant().(type) {
			case acp1.SessionUpdateAgentMessageChunk:
				if chunk, ok := acp1.TextOf(v.Content); ok && strings.TrimSpace(chunk) != "" {
					text.WriteString(chunk)
				}
			case acp1.SessionUpdateToolCall:
				toolCalls[v.ToolCallID] = true
			}
		}
		if event.Permission != nil {
			// 真握手若触发审批（凭据未就绪等），按取消收敛而非挂死
			_ = event.Permission.Cancel()
		}
		if event.Elicitation != nil && onElicitation != nil {
			onElicitation(event.Elicitation)
		}
	}

	deadline := time.After(180 * time.Second)
	settled := false
	for !settled {
		select {
		case event := <-session.Events():
			handleEvent(event)
		case result := <-results:
			if result.err != nil {
				t.Fatalf("Prompt: %v\n进程诊断: %v", result.err, client.Err())
			}
			if result.stop != acp1.StopReasonEndTurn {
				t.Fatalf("stopReason = %q, want end_turn", result.stop)
			}
			/*
			 * 排空尾随事件：chunk 入队先于应答解析，select 竞速可能先收到应答，
			 * 不排空会漏收末端文本。
			 */
			for {
				select {
				case event := <-session.Events():
					handleEvent(event)
				default:
					settled = true
				}
			}
		case <-deadline:
			t.Fatalf("回合 180s 未收敛（进程诊断: %v）", client.Err())
		}
	}
	return text.String(), toolCalls
}

// assertAvailableModes modes 双轨形状断言：基本模式目录齐全且 currentModeId 在表内。
func assertAvailableModes(t *testing.T, modes *acp1.SessionModeState) {
	t.Helper()
	if modes == nil {
		t.Fatal("session/new 应答缺 modes")
	}
	available := map[acp1.SessionModeID]bool{}
	for _, mode := range modes.AvailableModes {
		available[mode.ID] = true
	}
	for _, want := range []acp1.SessionModeID{"default", "acceptEdits", "plan", "auto"} {
		if !available[want] {
			t.Fatalf("availableModes 缺基本模式 %q(实际: %v)", want, modes.AvailableModes)
		}
	}
	if !available[modes.CurrentModeID] {
		t.Fatalf("currentModeId %q 不在 availableModes 内", modes.CurrentModeID)
	}
}

// assertElicitationWire elicitation wire 全字段断言：会话归属、toolCall 归属、form 载荷保真。
func assertElicitationWire(t *testing.T, wire *acp.ElicitationWire, sessionID acp1.SessionID, toolCalls map[acp1.ToolCallID]bool) {
	t.Helper()
	if wire == nil {
		t.Fatal("轮2 未触发 elicitation/create")
	}
	if wire.SessionID != string(sessionID) {
		t.Fatalf("elicitation sessionId = %q, want %q", wire.SessionID, sessionID)
	}
	if wire.ToolCallID == "" {
		t.Fatal("elicitation 缺 toolCallId")
	}
	if !toolCalls[acp1.ToolCallID(wire.ToolCallID)] {
		t.Fatalf("elicitation toolCallId %q 未在先前 tool_call update 中出现", wire.ToolCallID)
	}
	schema := string(wire.RequestedSchema)
	if !strings.Contains(schema, `"object"`) || !strings.Contains(schema, `"question_0"`) {
		t.Fatalf("requestedSchema 缺 form 载荷要素: %s", schema)
	}
}

// handshakeSystemToken _meta.systemPrompt 注入的名字 token（轮1 问名验证透传闭环）。
const handshakeSystemToken = "OCEAN-HANDSHAKE-5C2B"
