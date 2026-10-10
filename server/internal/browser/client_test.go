package browser

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// withFakeNode 注入假 node（版本探测返回固定串），收尾恢复生产替换点。
func withFakeNode(t *testing.T, version string) {
	t.Helper()
	oldBin, oldVer := resolveNodeBin, nodeVersionOf
	resolveNodeBin = func() (string, error) { return "/fake/node", nil }
	nodeVersionOf = func(context.Context, string) (string, error) { return version, nil }
	t.Cleanup(func() { resolveNodeBin, nodeVersionOf = oldBin, oldVer })
}

// fakeStaging 构造伪 vendored 资源源（srcRoot/playwright-mcp/<version>/node_modules/
// @playwright/mcp/{package.json,cli.js}），保证 EnsureVendored → VendoredSpawnConfig
// 全链可走通（入口文件存在即可，不真正执行）。
func fakeStaging(t *testing.T) Dirs {
	t.Helper()
	version, err := EngineVersion()
	if err != nil {
		t.Fatalf("EngineVersion: %v", err)
	}
	srcRoot := t.TempDir()
	pkgDir := filepath.Join(srcRoot, "playwright-mcp", version, "node_modules", "@playwright", "mcp")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("伪 staging: %v", err)
	}
	manifest := fmt.Sprintf(`{"name":"@playwright/mcp","version":%q,"bin":"cli.js"}`, version)
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("伪 package.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "cli.js"), []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatalf("伪 cli.js: %v", err)
	}
	return Dirs{Resources: srcRoot, Adapters: t.TempDir()}
}

// withDialFunc 注入自定义拨号（收尾恢复生产替换点）。
func withDialFunc(t *testing.T, fn func(ctx context.Context, cmd *exec.Cmd) (*mcp.ClientSession, error)) {
	t.Helper()
	old := dialEngine
	dialEngine = fn
	t.Cleanup(func() { dialEngine = old })
}

// withFakeDial 注入 in-memory 假引擎：注册一个 browser_navigate 工具原样回显 url。
func withFakeDial(t *testing.T) {
	t.Helper()
	withDialFunc(t, func(ctx context.Context, _ *exec.Cmd) (*mcp.ClientSession, error) {
		serverTransport, clientTransport := mcp.NewInMemoryTransports()
		srv := mcp.NewServer(&mcp.Implementation{Name: "fake-engine", Version: "test"}, nil)
		mcp.AddTool(srv, &mcp.Tool{Name: "browser_navigate", Description: "fake"},
			func(ctx context.Context, req *mcp.CallToolRequest, in map[string]any) (*mcp.CallToolResult, struct{}, error) {
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: "navigated: " + fmt.Sprint(in["url"])}},
				}, struct{}{}, nil
			})
		if _, err := srv.Connect(ctx, serverTransport, nil); err != nil {
			return nil, fmt.Errorf("假引擎建连: %w", err)
		}
		client := mcp.NewClient(&mcp.Implementation{Name: "ocean-harness-browser-test", Version: "test"}, nil)
		return client.Connect(ctx, clientTransport, nil)
	})
}

func TestLaunchEngineNodeTooLow(t *testing.T) {
	withTestRoot(t)
	withFakeNode(t, "v16.20.2")
	_, err := LaunchEngine(context.Background(), fakeStaging(t), "default", false, nil)
	if err == nil || !strings.Contains(err.Error(), "Node.js") {
		t.Fatalf("err = %v, want node 版本过低报错", err)
	}
}

func TestLaunchEngineVendoredMissing(t *testing.T) {
	withTestRoot(t)
	withFakeNode(t, "v20.11.0")
	dirs := Dirs{Resources: filepath.Join(t.TempDir(), "nope"), Adapters: t.TempDir()}
	_, err := LaunchEngine(context.Background(), dirs, "default", false, nil)
	if err == nil || !strings.Contains(err.Error(), "资源缺失") {
		t.Fatalf("err = %v, want vendored 资源缺失报错", err)
	}
}

func TestLaunchEngineProfileLockHint(t *testing.T) {
	root := withTestRoot(t)
	withFakeNode(t, "v20.11.0")
	// dial 失败 + profile 残留锁 → 错误文案带「残留会话」。
	withDialFunc(t, func(ctx context.Context, _ *exec.Cmd) (*mcp.ClientSession, error) {
		return nil, fmt.Errorf("boom")
	})

	profileDir := filepath.Join(root, "profiles", "default")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("建 profile 目录: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "SingletonLock"), nil, 0o644); err != nil {
		t.Fatalf("造锁: %v", err)
	}

	_, err := LaunchEngine(context.Background(), fakeStaging(t), "default", false, nil)
	if err == nil || !strings.Contains(err.Error(), "残留会话") {
		t.Fatalf("err = %v, want 残留会话提示", err)
	}
}

func TestLaunchEngineHappyPathAndClose(t *testing.T) {
	withTestRoot(t)
	withFakeNode(t, "v20.11.0")
	withFakeDial(t)

	h, err := LaunchEngine(context.Background(), fakeStaging(t), "default", true, nil)
	if err != nil {
		t.Fatalf("LaunchEngine: %v", err)
	}

	// CallTool 打通假引擎。
	res, err := h.Session().CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "browser_navigate",
		Arguments: map[string]any{"url": "https://example.com"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if res.IsError || !strings.Contains(text, "https://example.com") {
		t.Fatalf("CallTool 结果异常: isError=%v text=%q", res.IsError, text)
	}

	// Close 收敛 + 退出守护信号关闭。
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !h.ClosedByCaller() {
		t.Fatal("ClosedByCaller 应为 true")
	}
	select {
	case <-h.Exited():
	case <-time.After(5 * time.Second):
		t.Fatal("Close 后 5s 内 Exited 未关闭")
	}
}
