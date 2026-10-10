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

func TestDialFailureSuffix(t *testing.T) {
	// 无锁无 stderr 无后缀。
	dir := t.TempDir()
	if s := dialFailureSuffix(dir, ""); s != "" {
		t.Fatalf("干净 profile 的 suffix = %q, want 空", s)
	}
	// 有锁出残留会话提示（前置空格拼接语义）。
	if err := os.WriteFile(filepath.Join(dir, "SingletonLock"), nil, 0o644); err != nil {
		t.Fatalf("造锁文件: %v", err)
	}
	if s := dialFailureSuffix(dir, ""); !strings.HasPrefix(s, " ") || !strings.Contains(s, "残留会话") {
		t.Fatalf("suffix = %q, want 残留会话提示", s)
	}
	// stderr tail 证据拼装在探测提示之后。
	if s := dialFailureSuffix(dir, "boom trace"); !strings.Contains(s, "引擎 stderr: boom trace") {
		t.Fatalf("suffix = %q, want 含 stderr 证据", s)
	}
	// 仅 stderr、无探测提示时也产出后缀。
	if s := dialFailureSuffix(t.TempDir(), "only stderr"); !strings.Contains(s, "only stderr") {
		t.Fatalf("suffix = %q, want 含 stderr 证据", s)
	}
}
