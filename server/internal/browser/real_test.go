package browser

// 真实拉起冒烟验收（T1.2）：不进常规测试轮次，需要本机 staging 与系统 Chrome：
//
//	staging=$(mktemp -d)/playwright-mcp 0.0.83 目录内 pnpm add @playwright/mcp@0.0.83
//	OCEAN_ACP_RESOURCES_DIR=<staging 根> OCEAN_BROWSER_REAL_SMOKE=1 \
//	  go test ./internal/browser -run TestRealEngineSmoke -v -timeout 300s
//
// staging 布局与 acp-adapters 同构（<srcRoot>/playwright-mcp/<version>/node_modules）；
// T3.1 打包 vendoring 落地后缺省取 app/resources/acp-adapters 即无需手工构造。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRealEngineSmoke(t *testing.T) {
	if os.Getenv("OCEAN_BROWSER_REAL_SMOKE") != "1" {
		t.Skip("真实拉起冒烟：需要 OCEAN_BROWSER_REAL_SMOKE=1、本机 staging 与系统 Chrome")
	}
	srcRoot := os.Getenv("OCEAN_ACP_RESOURCES_DIR")
	if srcRoot == "" {
		srcRoot = filepath.Join("..", "..", "..", "app", "resources", "acp-adapters")
		if _, err := os.Stat(srcRoot); err != nil {
			t.Fatalf("vendored 资源源缺失（T3.1 前需经 OCEAN_ACP_RESOURCES_DIR 指向本地 pnpm 安装的 staging）: %v", err)
		}
	}
	// profile 目录指向一次性临时根（不污染 ~/.ocean-harness/browser）。
	withTestRoot(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	h, err := LaunchEngine(ctx, Dirs{Resources: srcRoot, Adapters: t.TempDir()}, "smoke", true, nil)
	if err != nil {
		t.Fatalf("LaunchEngine: %v", err)
	}
	defer h.Close()

	call := func(name string, args map[string]any) string {
		t.Helper()
		res, err := h.Session().CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("CallTool %s: %v", name, err)
		}
		var text string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				text += tc.Text
			}
		}
		if res.IsError {
			t.Fatalf("CallTool %s 引擎报错: %s", name, text)
		}
		return text
	}

	// 导航 + 回读 title（browser_evaluate 为引擎 core 常驻工具，不受 --caps 收窄影响）。
	call("browser_navigate", map[string]any{"url": "https://example.com"})
	title := call("browser_evaluate", map[string]any{"function": "() => document.title"})
	t.Logf("navigate 回读 title: %q", title)
	if !strings.Contains(title, "Example Domain") {
		t.Fatalf("title = %q, want 含 Example Domain", title)
	}

	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-h.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("Close 后 10s 内 Exited 未关闭（引擎未收敛）")
	}
}
