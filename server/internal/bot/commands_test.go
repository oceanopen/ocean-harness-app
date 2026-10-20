package bot

import (
	"strings"
	"testing"
)

// slash 指令形态判定矩阵：全等命中、TrimSpace 容忍、粘连/前后缀不命中。
func TestParseSlashCommand(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"/help", "/help"},
		{"/stop", "/stop"},
		{"/clear", "/clear"},
		{"/继续", "/继续"},
		{" /stop ", "/stop"},
		{"/stopx", ""},
		{"#帮助", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := parseSlashCommand(tc.text); got != tc.want {
			t.Fatalf("parseSlashCommand(%q)=%q, want %q", tc.text, got, tc.want)
		}
	}
}

/*
 * 未识别 slash 指令形态矩阵：顶格纯 ASCII 字母 token 且非已知才命中；
 * 参数形态、中文/混合 token、非顶格均不命中（不误伤含 “/” 的英文正文）。
 */
func TestParseUnknownSlashCommand(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"/helpp", true},
		{"/ABC", true},
		{"/stop", false},  // 已知指令
		{"/help", false},  // 已知指令
		{"/clear", false}, // 已知指令
		{"/继续", false},    // 已知指令（且中文不命中纯字母形态）
		{"/stop now", false},
		{"x/stop", false},
		{"/", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := parseUnknownSlashCommand(tc.text); got != tc.want {
			t.Fatalf("parseUnknownSlashCommand(%q)=%v, want %v", tc.text, got, tc.want)
		}
	}
}

// helpText 覆盖 / 系指令条目。
func TestHelpTextSlashEntries(t *testing.T) {
	text := helpText()
	for _, token := range []string{slashStopToken, slashClearToken, slashResumeToken, slashHelpToken} {
		if !strings.Contains(text, token) {
			t.Fatalf("帮助文案缺 %s 条目:\n%s", token, text)
		}
	}
}
