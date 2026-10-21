package bot

import (
	"errors"
	"strings"
	"testing"
)

// TestClassifyTurnError 分类映射：额度优先于连接（stderr 摘要与 exit status 同串）、认证、未命中回落。
func TestClassifyTurnError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"额度优先于进程退出", errors.New("claude 回合失败: exit status 1；stderr: …usage limit reached…"), "额度"},
		{"限流", errors.New("api error: 429 rate limit exceeded"), "额度"},
		{"认证", errors.New("claude 未登录：请先在终端运行 claude 完成登录，再重试"), "认证"},
		{"连接", errors.New("ACP 回合失败: connection closed"), "连接中断"},
		{"未知回落", errors.New("something odd happened"), ""},
	}
	for _, tc := range cases {
		got := classifyTurnError(tc.err)
		if tc.want == "" {
			if got != "" {
				t.Fatalf("%s: 未命中应返回空串，got %q", tc.name, got)
			}
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Fatalf("%s: 期望含 %q，got %q", tc.name, tc.want, got)
		}
	}
}

// TestTurnFailureReply 统一失败文案：分类命中带原始首行；未命中回落通用指引。
func TestTurnFailureReply(t *testing.T) {
	got := turnFailureReply("❌ 处理失败：", errors.New("usage limit reached"))
	if !strings.Contains(got, "额度") || !strings.Contains(got, "原始错误：usage limit reached") {
		t.Fatalf("分类命中应含说明与原始首行: %q", got)
	}
	got = turnFailureReply("❌ 无法启动 claude 会话：", errors.New("vendored 目录未配置"))
	if !strings.Contains(got, "请重试或调整后重新发送") || strings.Contains(got, "原始错误") {
		t.Fatalf("未命中应回落通用文案: %q", got)
	}
}
