package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runForTest 构建全新命令树执行一次命令，返回 (退出码, stdout, stderr)。
func runForTest(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errBuf)
	err := root.Execute()
	return mappedExitCode(root, err), out.String(), errBuf.String()
}

// wantOutput 断言退出码与指定流（stdout/stderr）包含全部期望片段。
func wantOutput(t *testing.T, gotCode int, gotStream string, wantCode int, wants ...string) {
	t.Helper()
	if gotCode != wantCode {
		t.Fatalf("exit=%d, want %d (output: %q)", gotCode, wantCode, gotStream)
	}
	for _, w := range wants {
		if !strings.Contains(gotStream, w) {
			t.Fatalf("output %q missing %q", gotStream, w)
		}
	}
}

func TestRunDispatch(t *testing.T) {
	t.Run("无参数输出用法到 stderr 并返回用法错误", func(t *testing.T) {
		code, out, errOut := runForTest(t)
		wantOutput(t, code, errOut, ExitUsage, "用法:")
		if out != "" {
			t.Fatalf("stdout want empty, got %q", out)
		}
	})

	t.Run("未知命令返回用法错误", func(t *testing.T) {
		code, _, errOut := runForTest(t, "nope")
		wantOutput(t, code, errOut, ExitUsage, `未知命令 "nope"`)
	})

	t.Run("未知命令给出相似命令建议", func(t *testing.T) {
		code, _, errOut := runForTest(t, "versio")
		wantOutput(t, code, errOut, ExitUsage, "您是不是想执行", "version")
	})

	t.Run("help 三形态输出到 stdout 返回成功", func(t *testing.T) {
		for _, flag := range []string{"-h", "--help", "help"} {
			code, out, errOut := runForTest(t, flag)
			wantOutput(t, code, out, ExitOK, "用法:")
			if errOut != "" {
				t.Fatalf("%s: stderr want empty, got %q", flag, errOut)
			}
		}
	})

	t.Run("version 输出构建信息（命令名随构建模式）", func(t *testing.T) {
		code, out, _ := runForTest(t, "version")
		wantOutput(t, code, out, ExitOK, "ocean-harness-dev-cli", "(mode: test)")
	})

	t.Run("根 --version 与 version 子命令输出一致", func(t *testing.T) {
		codeV, outV, _ := runForTest(t, "version")
		codeFlag, outFlag, _ := runForTest(t, "--version")
		if codeV != ExitOK || codeFlag != ExitOK || outV != outFlag {
			t.Fatalf("version=%q, --version=%q", outV, outFlag)
		}
	})

	t.Run("mcp 无子命令输出组用法", func(t *testing.T) {
		code, _, errOut := runForTest(t, "mcp")
		wantOutput(t, code, errOut, ExitUsage, "可用命令", "tools")
	})

	t.Run("mcp schema 缺工具名返回用法错误", func(t *testing.T) {
		code, _, errOut := runForTest(t, "mcp", "schema")
		wantOutput(t, code, errOut, ExitUsage, "mcp schema <name>")
	})

	t.Run("mcp call 缺工具名返回用法错误", func(t *testing.T) {
		code, _, errOut := runForTest(t, "mcp", "call")
		wantOutput(t, code, errOut, ExitUsage, "mcp call <name>")
	})

	t.Run("mcp call 的 data 非法 JSON 返回用法错误", func(t *testing.T) {
		code, _, errOut := runForTest(t, "mcp", "call", "issue_update", "--data", "{bad")
		wantOutput(t, code, errOut, ExitUsage, "不是合法 JSON")
	})

	t.Run("mcp call 的 data 非对象 JSON 返回用法错误", func(t *testing.T) {
		code, _, errOut := runForTest(t, "mcp", "call", "issue_update", "--data", `["a"]`)
		wantOutput(t, code, errOut, ExitUsage, "必须是 JSON 对象")
	})

	t.Run("端口 env 非法时在触网前返回用法错误", func(t *testing.T) {
		t.Setenv(EnvPort, "not-a-port")
		code, _, errOut := runForTest(t, "mcp", "tools")
		wantOutput(t, code, errOut, ExitUsage, EnvPort)
	})

	t.Run("端口 flag 非法时在触网前返回用法错误", func(t *testing.T) {
		for _, raw := range []string{"70000", "-5"} {
			code, _, errOut := runForTest(t, "mcp", "tools", "--port", raw)
			wantOutput(t, code, errOut, ExitUsage, "--port")
		}
	})

	t.Run("completion 子命令存在且可产出脚本", func(t *testing.T) {
		code, out, _ := runForTest(t, "completion", "zsh")
		wantOutput(t, code, out, ExitOK, "compdef")
	})
}

// callCmdForTest 构造仅注册 --data flag 的最小 call 命令（parseCallData 单测用，不触网）。
func callCmdForTest(data string, stdin *strings.Reader) *cobra.Command {
	cmd := &cobra.Command{Use: "call"}
	cmd.Flags().String("data", data, "")
	if stdin != nil {
		cmd.SetIn(stdin)
	}
	return cmd
}

func TestParseCallData(t *testing.T) {
	t.Run("空视为空对象", func(t *testing.T) {
		args, err := parseCallData(callCmdForTest("", nil))
		if err != nil || len(args) != 0 {
			t.Fatalf("got (%v, %v)", args, err)
		}
	})

	t.Run("内联对象", func(t *testing.T) {
		args, err := parseCallData(callCmdForTest(`{"a":1}`, nil))
		if err != nil || args["a"] != float64(1) {
			t.Fatalf("got (%v, %v)", args, err)
		}
	})

	t.Run("@file 读文件", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "payload.json")
		if err := os.WriteFile(path, []byte(`{"k":"v"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		args, err := parseCallData(callCmdForTest("@"+path, nil))
		if err != nil || args["k"] != "v" {
			t.Fatalf("got (%v, %v)", args, err)
		}
	})

	t.Run("@文件不存在报错", func(t *testing.T) {
		if _, err := parseCallData(callCmdForTest("@/nonexistent/payload.json", nil)); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("- 读 stdin", func(t *testing.T) {
		args, err := parseCallData(callCmdForTest("-", strings.NewReader(`{"s":true}`)))
		if err != nil || args["s"] != true {
			t.Fatalf("got (%v, %v)", args, err)
		}
	})

	t.Run("非法 JSON 报错", func(t *testing.T) {
		if _, err := parseCallData(callCmdForTest("{bad", nil)); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("非对象 JSON 报错", func(t *testing.T) {
		if _, err := parseCallData(callCmdForTest(`["a"]`, nil)); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestWriteJSON(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := writeJSON(cmd, map[string]any{"名": "值"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output not valid JSON: %q", out.String())
	}
	if got["名"] != "值" {
		t.Fatalf("got %v", got)
	}
}
