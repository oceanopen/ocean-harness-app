package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasProfileLock(t *testing.T) {
	dir := t.TempDir()
	if HasProfileLock(dir) {
		t.Fatal("空目录不应有锁")
	}
	for _, name := range singletonLockNames {
		sub := t.TempDir()
		if err := os.WriteFile(filepath.Join(sub, name), nil, 0o644); err != nil {
			t.Fatalf("造锁文件 %s: %v", name, err)
		}
		if !HasProfileLock(sub) {
			t.Errorf("存在 %s 应识别为锁", name)
		}
	}
}

func TestHasEmbeddedBrowsers(t *testing.T) {
	// 目录不存在/为空 = 不在位（覆盖构建期未装与安装损坏）。
	installDir := t.TempDir()
	if HasEmbeddedBrowsers(installDir) {
		t.Fatal("空安装目录不应判在位")
	}
	browsers := filepath.Join(installDir, embeddedBrowsersDirName)
	if err := os.MkdirAll(browsers, 0o755); err != nil {
		t.Fatalf("建 browsers 目录: %v", err)
	}
	if HasEmbeddedBrowsers(installDir) {
		t.Fatal("空 browsers 目录不应判在位")
	}
	if err := os.WriteFile(filepath.Join(browsers, "chromium-1247"), nil, 0o644); err != nil {
		t.Fatalf("造条目: %v", err)
	}
	if !HasEmbeddedBrowsers(installDir) {
		t.Fatal("非空 browsers 目录应判在位")
	}
}

func TestDialFailureSuffix(t *testing.T) {
	// 干净基准：无锁、内置浏览器在位、无 stderr → 无后缀。
	dir := t.TempDir()
	installDir := t.TempDir()
	browsers := filepath.Join(installDir, embeddedBrowsersDirName)
	if err := os.MkdirAll(browsers, 0o755); err != nil {
		t.Fatalf("造内置浏览器目录: %v", err)
	}
	if err := os.WriteFile(filepath.Join(browsers, "chromium-1247"), nil, 0o644); err != nil {
		t.Fatalf("造内置浏览器条目: %v", err)
	}
	if s := dialFailureSuffix(dir, installDir, ""); s != "" {
		t.Fatalf("干净 profile 的 suffix = %q, want 空", s)
	}
	// 内置浏览器缺失 → 缺失提示（构建期未装/安装损坏）。
	if s := dialFailureSuffix(dir, t.TempDir(), ""); !strings.Contains(s, "内置浏览器缺失") {
		t.Fatalf("suffix = %q, want 内置浏览器缺失提示", s)
	}
	// 有锁出残留会话提示（前置空格拼接语义）。
	if err := os.WriteFile(filepath.Join(dir, "SingletonLock"), nil, 0o644); err != nil {
		t.Fatalf("造锁文件: %v", err)
	}
	if s := dialFailureSuffix(dir, installDir, ""); !strings.HasPrefix(s, " ") || !strings.Contains(s, "残留会话") {
		t.Fatalf("suffix = %q, want 残留会话提示", s)
	}
	// stderr tail 证据拼装在探测提示之后。
	if s := dialFailureSuffix(dir, installDir, "boom trace"); !strings.Contains(s, "引擎 stderr: boom trace") {
		t.Fatalf("suffix = %q, want 含 stderr 证据", s)
	}
	// 仅 stderr、无探测提示时也产出后缀。
	if s := dialFailureSuffix(t.TempDir(), installDir, "only stderr"); !strings.Contains(s, "only stderr") {
		t.Fatalf("suffix = %q, want 含 stderr 证据", s)
	}
}
