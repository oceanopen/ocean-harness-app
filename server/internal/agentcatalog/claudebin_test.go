package agentcatalog

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeClaudeBinAt 在 installDir 内造当前平台的 SDK 平台二进制包（node_modules/
// @anthropic-ai/claude-agent-sdk-<triple>/claude），返回二进制绝对路径。
func writeClaudeBinAt(t *testing.T, installDir string) string {
	t.Helper()
	triple := claudePlatformTriple()
	if triple == "" {
		t.Skipf("平台 %s/%s 无 SDK 自带 claude 二进制形态", runtime.GOOS, runtime.GOARCH)
	}
	bin := filepath.Join(installDir, "node_modules", "@anthropic-ai",
		"claude-agent-sdk-"+triple, claudeBinName())
	mustWriteFile(t, bin, "#!/bin/sh\necho claude\n", 0o755)
	return bin
}

func TestVendoredClaudeBinHit(t *testing.T) {
	installDir := t.TempDir()
	want := writeClaudeBinAt(t, installDir)
	got, err := VendoredClaudeBin(installDir)
	if err != nil {
		t.Fatalf("解析 vendored claude: %v", err)
	}
	if got != want {
		t.Fatalf("路径形态: got %s want %s", got, want)
	}
}

func TestVendoredClaudeBinMissing(t *testing.T) {
	if claudePlatformTriple() == "" {
		t.Skipf("平台 %s/%s 无 SDK 自带 claude 二进制形态", runtime.GOOS, runtime.GOARCH)
	}
	_, err := VendoredClaudeBin(t.TempDir())
	if err == nil {
		t.Fatal("空安装目录应报二进制缺失")
	}
	if !strings.Contains(err.Error(), "vendored claude 二进制缺失") {
		t.Fatalf("错误文案应可用户直读: %v", err)
	}
}

func TestClaudePlatformTripleKnown(t *testing.T) {
	triple := claudePlatformTriple()
	switch {
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		if triple != "darwin-arm64" {
			t.Fatalf("darwin/arm64 映射: %q", triple)
		}
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		if triple != "darwin-x64" {
			t.Fatalf("darwin/amd64 映射: %q", triple)
		}
	case runtime.GOOS == "windows":
		if triple != "win32-x64" && triple != "win32-arm64" {
			t.Fatalf("windows 映射: %q", triple)
		}
		if claudeBinName() != "claude.exe" {
			t.Fatalf("windows 二进制名: %q", claudeBinName())
		}
	case runtime.GOOS == "linux":
		if triple != "linux-x64" && triple != "linux-arm64" {
			t.Fatalf("linux 映射: %q", triple)
		}
	default:
		if triple != "" {
			t.Fatalf("未支持平台应返回空: %q", triple)
		}
	}
}

func TestResolveVendoredClaudeBin(t *testing.T) {
	entry, ok := GetAgentCatalogInfoByCode("claude-acp")
	if !ok {
		t.Fatal("catalog 应含 claude-acp 条目")
	}
	srcRoot, dstRoot := t.TempDir(), t.TempDir()
	writeClaudeBinAt(t, filepath.Join(srcRoot, entry.ID, entry.Version))

	// 目录注入 + 缓存均为进程级状态，测试间互不污染：先重置，结束还原。
	t.Cleanup(func() {
		claudeBinCached = ""
		resourcesDir, adaptersDir = "", ""
	})
	claudeBinCached = ""
	SetVendoredDirs(srcRoot, dstRoot)

	bin, err := ResolveVendoredClaudeBin(entry.Code)
	if err != nil {
		t.Fatalf("解析 vendored claude: %v", err)
	}
	// 返回受管落地路径（staging 的内容经 EnsureVendored 复制，非资源源本体）。
	want := filepath.Join(dstRoot, entry.ID, entry.Version,
		"node_modules", "@anthropic-ai", "claude-agent-sdk-"+claudePlatformTriple(), claudeBinName())
	if bin != want {
		t.Fatalf("受管落地路径: got %s want %s", bin, want)
	}

	// 命中进程级缓存：资源源移除后仍可解析（成功缓存口径，失败才重探）。
	if err := os.RemoveAll(srcRoot); err != nil {
		t.Fatalf("清理资源源: %v", err)
	}
	if again, err := ResolveVendoredClaudeBin(entry.Code); err != nil || again != bin {
		t.Fatalf("缓存命中: got %s, %v", again, err)
	}
}
