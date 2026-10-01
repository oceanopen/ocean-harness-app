package acp

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/BrokkAi/acp-go/schema"
)

// TestRealHandshake 一次性真握手验收（T1.1 验收）：拉起真实 claude ACP adapter 完成
// initialize → session/new → session/prompt 一轮完整链路。需要真实凭据与网络，
// 不进常规测试轮次：
//
//	OCEAN_ACP_REAL_HANDSHAKE=1 pnpm exec go test ./internal/acp -run TestRealHandshake -v
//
// 可经 OCEAN_ACP_REAL_COMMAND 覆盖拉起命令（空格分隔 argv），缺省为 Zed 官方
// claude-code-acp adapter。
func TestRealHandshake(t *testing.T) {
	if os.Getenv("OCEAN_ACP_REAL_HANDSHAKE") != "1" {
		t.Skip("一次性真握手验收：需要 OCEAN_ACP_REAL_HANDSHAKE=1 与本机 claude 凭据")
	}
	command := "npx -y @zed-industries/claude-code-acp"
	if custom := os.Getenv("OCEAN_ACP_REAL_COMMAND"); custom != "" {
		command = custom
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatalf("本机未探测到 claude 可执行文件（真握手前置条件）: %v", err)
	}

	ctx := context.Background()
	client, err := Launch(ctx, SpawnConfig{
		Command: strings.Fields(command),
		Cwd:     t.TempDir(),
	}, nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer client.Close()

	init, err := client.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Logf("agent: %v 协议版本: %d claudeCode 扩展: %v", init.AgentInfo, init.ProtocolVersion, HasClaudeCodeExtension(init))

	session, err := client.NewSession(ctx, NewSessionParams{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	results := promptAsync(client, session, "只回复两个字符：OK")
	deadline := time.After(120 * time.Second)
	var sawText bool
	for {
		select {
		case event := <-session.Events():
			if event.Terminated {
				t.Fatal("会话在回合收敛前终结")
			}
			if text, ok := chunkText(event); ok && strings.TrimSpace(text) != "" {
				sawText = true
				t.Logf("chunk: %s", text)
			}
			if event.Permission != nil {
				// 真握手若触发审批（凭据未就绪等），按取消收敛而非挂死
				_ = event.Permission.cancel()
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
