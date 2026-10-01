package acp

import (
	"strings"
	"testing"
)

func TestClassifyStderr(t *testing.T) {
	cases := []struct {
		name string
		line string
		want stderrKind
	}{
		{"npm error 前缀", "npm error could not determine executable to run", stderrFailure},
		{"npm err! 变体", "npm ERR! code ENOENT", stderrFailure},
		{"ENOENT errno", "spawn /usr/bin/claude ENOENT", stderrFailure},
		{"enospc errno", "write stdout ENOSPC", stderrFailure},
		{"eacces errno", "Error: EACCES: permission denied, open '/usr/local/bin'", stderrFailure},
		{"eperm errno", "EPERM: operation not permitted", stderrFailure},
		{"npm 旧配置键噪音", "npm warn Unknown user config value", stderrNoise},
		{"session 查询前缀噪音", "[session/query] listing sessions", stderrNoise},
		{"普通诊断", "claude-acp listening on stdio", stderrDiagnostic},
		{"空行", "", stderrDiagnostic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyStderr(tc.line); got != tc.want {
				t.Fatalf("classifyStderr(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestStderrTailKeepsTailBytes(t *testing.T) {
	tail := newStderrTail(16)
	if n, err := tail.Write([]byte("aaaaaaaaaaaaaaaaaaaa")); n != 20 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if got := tail.Text(); got != strings.Repeat("a", 16) {
		t.Fatalf("Text = %q, want 16 个尾部字节", got)
	}
}

func TestStderrTailUTF8Safe(t *testing.T) {
	tail := newStderrTail(4)
	// "中" 占 3 字节：写入 两字 + 尾部裁剪落在多字节中间时，残缺前缀应被替换而非 panic。
	if _, err := tail.Write([]byte("中中中")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := tail.Text()
	if got != strings.ToValidUTF8("中中中"[len("中中中")-4:], "�") {
		t.Fatalf("Text = %q", got)
	}
	if got == "" {
		t.Fatal("Text 不应为空")
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("abcdef", 10); got != "abcdef" {
		t.Fatalf("未超限应原样返回，got %q", got)
	}
	if got := truncateRunes("abcdef", 3); got != "…def" {
		t.Fatalf("超限应尾部保留，got %q", got)
	}
	if got := truncateRunes("中文中文中", 2); got != "…文中" {
		t.Fatalf("多字节截断应尾部保留，got %q", got)
	}
}

func TestMergeEnvOverrideWins(t *testing.T) {
	merged := mergeEnv(map[string]string{"OCEAN_TEST_KEY": "override", "OCEAN_TEST_NEW": "new"})
	seen := make(map[string]string)
	counts := make(map[string]int)
	for _, kv := range merged {
		key, value, _ := strings.Cut(kv, "=")
		counts[key]++
		seen[key] = value
	}
	if counts["OCEAN_TEST_KEY"] != 1 {
		t.Fatalf("覆盖键应去重为一份，got %d 份", counts["OCEAN_TEST_KEY"])
	}
	if seen["OCEAN_TEST_KEY"] != "override" {
		t.Fatalf("覆盖应生效，got %q", seen["OCEAN_TEST_KEY"])
	}
	if seen["OCEAN_TEST_NEW"] != "new" {
		t.Fatalf("新增应生效，got %q", seen["OCEAN_TEST_NEW"])
	}
	if _, ok := seen["PATH"]; !ok {
		t.Fatal("sidecar 环境应作为基底保留")
	}
}

func TestMergeEnvStripsClaudeNestedGuard(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	merged := mergeEnv(map[string]string{"OCEAN_TEST_KEY": "override", "CLAUDECODE": "1"})
	seen := make(map[string]string)
	for _, kv := range merged {
		key, value, _ := strings.Cut(kv, "=")
		seen[key] = value
	}
	if _, ok := seen["CLAUDECODE"]; ok {
		t.Fatal("CLAUDECODE 应被恒剥离（含 overrides 试图重新注入的情形）")
	}
	if seen["OCEAN_TEST_KEY"] != "override" {
		t.Fatalf("剥离不应影响覆盖语义，got %q", seen["OCEAN_TEST_KEY"])
	}
}
