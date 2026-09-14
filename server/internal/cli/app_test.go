package cli

import (
	"bytes"
	"strings"
	"testing"
)

// runForTest 执行 Run 并返回 (退出码, stdout, stderr)。
func runForTest(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code := Run(args, Streams{Stdout: &out, Stderr: &errBuf})
	return code, out.String(), errBuf.String()
}

func TestRunDispatch(t *testing.T) {
	t.Run("无参数输出帮助到 stderr 并返回用法错误", func(t *testing.T) {
		code, out, errOut := runForTest(t)
		if code != ExitUsage || out != "" || !strings.Contains(errOut, "用法:") {
			t.Fatalf("got (%d, %q, %q)", code, out, errOut)
		}
	})

	t.Run("未知命令返回用法错误", func(t *testing.T) {
		code, _, errOut := runForTest(t, "nope")
		if code != ExitUsage || !strings.Contains(errOut, `未知命令 "nope"`) {
			t.Fatalf("got (%d, %q)", code, errOut)
		}
	})

	t.Run("help 输出到 stdout 返回成功", func(t *testing.T) {
		for _, flag := range []string{"-h", "--help", "help"} {
			code, out, errOut := runForTest(t, flag)
			if code != ExitOK || !strings.Contains(out, "用法:") || errOut != "" {
				t.Fatalf("%s: got (%d, %q, %q)", flag, code, out, errOut)
			}
		}
	})

	t.Run("version 输出构建信息", func(t *testing.T) {
		code, out, _ := runForTest(t, "version")
		if code != ExitOK || !strings.Contains(out, "ocean-harness-cli") || !strings.Contains(out, "(mode: test)") {
			t.Fatalf("got (%d, %q)", code, out)
		}
	})

	t.Run("mcp 无子命令输出组用法", func(t *testing.T) {
		code, _, errOut := runForTest(t, "mcp")
		if code != ExitUsage || !strings.Contains(errOut, "mcp tools") {
			t.Fatalf("got (%d, %q)", code, errOut)
		}
	})

	t.Run("mcp schema 缺工具名返回用法错误", func(t *testing.T) {
		code, _, errOut := runForTest(t, "mcp", "schema")
		if code != ExitUsage || !strings.Contains(errOut, "mcp schema <name>") {
			t.Fatalf("got (%d, %q)", code, errOut)
		}
	})

	t.Run("mcp call 缺工具名返回用法错误", func(t *testing.T) {
		code, _, errOut := runForTest(t, "mcp", "call")
		if code != ExitUsage || !strings.Contains(errOut, "mcp call <name>") {
			t.Fatalf("got (%d, %q)", code, errOut)
		}
	})

	t.Run("mcp call 的 data 非法 JSON 返回用法错误", func(t *testing.T) {
		code, _, errOut := runForTest(t, "mcp", "call", "issue_update", "--data", "{bad")
		if code != ExitUsage || !strings.Contains(errOut, "不是合法 JSON") {
			t.Fatalf("got (%d, %q)", code, errOut)
		}
	})

	t.Run("mcp call 的 data 非对象 JSON 返回用法错误", func(t *testing.T) {
		code, _, errOut := runForTest(t, "mcp", "call", "issue_update", "--data", `["a"]`)
		if code != ExitUsage || !strings.Contains(errOut, "必须是 JSON 对象") {
			t.Fatalf("got (%d, %q)", code, errOut)
		}
	})

	t.Run("端口 env 非法时在触网前返回用法错误", func(t *testing.T) {
		t.Setenv(EnvPort, "not-a-port")
		code, _, errOut := runForTest(t, "mcp", "tools")
		if code != ExitUsage || !strings.Contains(errOut, EnvPort) {
			t.Fatalf("got (%d, %q)", code, errOut)
		}
	})
}
