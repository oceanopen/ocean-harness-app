package acp_test

// 真握手验收为外部测试包（acp_test）：缺省命令取自 internal/agentcatalog 的内嵌
// catalog——acp 域不感知 catalog（agentcatalog → acp 单向依赖），测试侧经导出面装配。

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/BrokkAi/acp-go/schema"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/agentcatalog"
	"ocean-harness/server/internal/dal/enums"
)

// TestRealHandshake 一次性真握手验收（T1.1 验收 + T1.2 升级验证流程收口）：拉起真实
// claude ACP adapter 完成 initialize → session/new → session/prompt 一轮完整链路。
// 需要真实凭据与网络，不进常规测试轮次：
//
//	OCEAN_ACP_REAL_HANDSHAKE=1 go test ./internal/acp -run TestRealHandshake -v
//
// 缺省命令 = 内嵌 agent catalog 的 claude-acp 条目翻译结果——catalog pin 升级后跑本
// 用例即完成验证（改 pin → pnpm server:catalog:refresh → 本用例 → 提交）。可经
// OCEAN_ACP_REAL_COMMAND 覆盖拉起命令（空格分隔 argv）。
func TestRealHandshake(t *testing.T) {
	if os.Getenv("OCEAN_ACP_REAL_HANDSHAKE") != "1" {
		t.Skip("一次性真握手验收：需要 OCEAN_ACP_REAL_HANDSHAKE=1 与本机 claude 凭据")
	}
	entry, ok := agentcatalog.GetAgentCatalogInfoByCode(string(enums.AGENT_CODE_CLAUDE_ACP))
	if !ok {
		t.Fatal("catalog 缺映射条目 claude-acp")
	}
	cfg, err := entry.SpawnConfig(t.TempDir())
	if err != nil {
		t.Fatalf("catalog 条目翻译: %v", err)
	}
	if custom := os.Getenv("OCEAN_ACP_REAL_COMMAND"); custom != "" {
		cfg.Command = strings.Fields(custom)
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatalf("本机未探测到 claude 可执行文件（真握手前置条件）: %v", err)
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
	t.Logf("agent: %v 协议版本: %d claudeCode 扩展: %v", init.AgentInfo, init.ProtocolVersion, acp.HasClaudeCodeExtension(init))

	session, err := client.NewSession(ctx, acp.NewSessionParams{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	type promptResult struct {
		stop schema.StopReason
		err  error
	}
	results := make(chan promptResult, 1)
	go func() {
		stop, promptErr := client.PromptText(context.Background(), session, "只回复两个字符：OK")
		results <- promptResult{stop: stop, err: promptErr}
	}()

	deadline := time.After(120 * time.Second)
	var sawText bool
	for {
		select {
		case event := <-session.Events():
			if event.Terminated {
				t.Fatal("会话在回合收敛前终结")
			}
			if event.Update != nil && event.Update.Update.AgentMessageChunk != nil {
				if text := event.Update.Update.AgentMessageChunk.Content.Text.Text; strings.TrimSpace(text) != "" {
					sawText = true
					t.Logf("chunk: %s", text)
				}
			}
			if event.Permission != nil {
				// 真握手若触发审批（凭据未就绪等），按取消收敛而非挂死
				_ = event.Permission.Cancel()
			}
		case result := <-results:
			if result.err != nil {
				t.Fatalf("Prompt: %v\n进程诊断: %v", result.err, client.Err())
			}
			if result.stop != schema.StopReasonEndTurn {
				t.Fatalf("stopReason = %q, want end_turn", result.stop)
			}
			if !sawText {
				t.Fatal("回合收敛但未收到任何文本 chunk")
			}
			return
		case <-deadline:
			t.Fatalf("真握手 120s 未收敛（进程诊断: %v）", client.Err())
		}
	}
}
