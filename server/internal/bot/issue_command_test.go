package bot

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
)

// TestParseIssueCommand 表驱动覆盖 #任务 指令解析的形态域（统一卡片化口径）：非指令
// 放行、裸指令、关键词整段（旧正文切分与 ID 通道退役、解绑子指令退役归关键词）、全角
// 空格分隔（首 rune 解码回归）、旧 token 退役（#issue 不再是指令）。
func TestParseIssueCommand(t *testing.T) {
	cases := []struct {
		name string
		text string
		want *issueCommand
	}{
		{"普通文本", "修复一下登录页", nil},
		{"空串", "", nil},
		{"中部出现不解析", "看看 #任务 登录页", nil},
		{"粘连形态 #任务列表", "#任务列表 修复", nil},
		{"粘连形态 #任务解绑（独立指令接管）", "#任务解绑", nil},
		{"旧 token 已退役", "#issue 登录页", nil},
		{"裸指令", "#任务", &issueCommand{}},
		{"裸指令带空白", "  #任务  ", &issueCommand{}},
		{"解绑词归关键词（子指令退役）", "#任务 解绑", &issueCommand{Keyword: "解绑"}},
		{"解绑词带前后空白", " #任务   解绑  ", &issueCommand{Keyword: "解绑"}},
		{"多词整段关键词", "#任务 解绑 xxx", &issueCommand{Keyword: "解绑 xxx"}},
		{"关键词", "#任务 登录页", &issueCommand{Keyword: "登录页"}},
		{"整段皆为关键词（旧正文切分退役）", "#任务 登录页 修复一下样式", &issueCommand{Keyword: "登录页 修复一下样式"}},
		{"制表符形态", "#任务\t登录页", &issueCommand{Keyword: "登录页"}},
		{"全角空格形态（首 rune 解码）", "#任务　登录页", &issueCommand{Keyword: "登录页"}},
		{"ID 前缀形态也归名称模糊（ID 通道退役）", "#任务 0198abcd", &issueCommand{Keyword: "0198abcd"}},
		{"关键词超限截断", "#任务 " + strings.Repeat("任", 20), &issueCommand{Keyword: strings.Repeat("任", 13)}},
	}
	for _, tc := range cases {
		got := parseIssueCommand(tc.text)
		if tc.want == nil {
			if got != nil {
				t.Fatalf("%s: 期望非指令（nil），got %+v", tc.name, got)
			}
			continue
		}
		if got == nil || *got != *tc.want {
			t.Fatalf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestParseIssueUnbindCommand 解绑指令恰等判定（同 #帮助 惯例）：裸 token（含首尾空白）
// 命中；带后缀/粘连/中部出现/其它指令一律不命中（带参数形态无解绑语义，按普通文本走
// 主路径）。
func TestParseIssueUnbindCommand(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"裸指令", "#任务解绑", true},
		{"裸指令带空白", "  #任务解绑  ", true},
		{"带后缀不解析", "#任务解绑 xxx", false},
		{"粘连形态", "#任务解绑了", false},
		{"中部出现不解析", "看看 #任务解绑", false},
		{"#任务 裸指令不命中", "#任务", false},
		{"#任务 带解绑词归搜索", "#任务 解绑", false},
		{"空串", "", false},
	}
	for _, tc := range cases {
		if got := parseIssueUnbindCommand(tc.text); got != tc.want {
			t.Fatalf("%s: parseIssueUnbindCommand(%q)=%v, want %v", tc.name, tc.text, got, tc.want)
		}
	}
}

// TestParseWorkspaceCommand 表驱动覆盖 #工作空间 指令解析的形态域（与 #任务 同款惯例；
// 无子指令——解绑词也是普通关键词；旧 token 退役）。
func TestParseWorkspaceCommand(t *testing.T) {
	cases := []struct {
		name string
		text string
		want *workspaceCommand
	}{
		{"普通文本", "切换工作空间", nil},
		{"空串", "", nil},
		{"中部出现不解析", "看看 #工作空间", nil},
		{"粘连形态 #工作空间们", "#工作空间们", nil},
		{"旧 token 已退役", "#workspace 前端", nil},
		{"裸指令", "#工作空间", &workspaceCommand{}},
		{"裸指令带空白", "  #工作空间  ", &workspaceCommand{}},
		{"关键词", "#工作空间 前端", &workspaceCommand{Keyword: "前端"}},
		{"整段关键词", "#工作空间 前端 仓", &workspaceCommand{Keyword: "前端 仓"}},
		{"无子指令（解绑词归关键词）", "#工作空间 解绑", &workspaceCommand{Keyword: "解绑"}},
		{"全角空格形态（首 rune 解码）", "#工作空间　前端", &workspaceCommand{Keyword: "前端"}},
		{"关键词超限截断", "#工作空间 " + strings.Repeat("仓", 20), &workspaceCommand{Keyword: strings.Repeat("仓", 13)}},
	}
	for _, tc := range cases {
		got := parseWorkspaceCommand(tc.text)
		if tc.want == nil {
			if got != nil {
				t.Fatalf("%s: 期望非指令（nil），got %+v", tc.name, got)
			}
			continue
		}
		if got == nil || *got != *tc.want {
			t.Fatalf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestParseHelpCommand 帮助指令恰等于判定：裸 token（含首尾空白）命中；带后缀/粘连/中部
// 出现/其它指令一律不命中（带参数形态无帮助语义，按普通文本走主路径）。
func TestParseHelpCommand(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"裸指令", "#帮助", true},
		{"裸指令带空白", "  #帮助  ", true},
		{"带参数不解析", "#帮助 一下", false},
		{"粘连形态", "#帮助我", false},
		{"中部出现不解析", "看看 #帮助", false},
		{"其它指令", "#任务", false},
		{"空串", "", false},
	}
	for _, tc := range cases {
		if got := parseHelpCommand(tc.text); got != tc.want {
			t.Fatalf("%s: parseHelpCommand(%q)=%v, want %v", tc.name, tc.text, got, tc.want)
		}
	}
}

// TestNormalizeBindingKeyword 关键词规整边界：空白收敛与 rune 安全截断（不切半个字符）。
func TestNormalizeBindingKeyword(t *testing.T) {
	if got := normalizeBindingKeyword("  前端  "); got != "前端" {
		t.Fatalf("应 TrimSpace: %q", got)
	}
	if got := normalizeBindingKeyword(strings.Repeat("a", 50)); got != strings.Repeat("a", bindingKeywordMaxBytes) {
		t.Fatalf("ASCII 应截到 %d 字节: n=%d", bindingKeywordMaxBytes, len(got))
	}
	if got := normalizeBindingKeyword(strings.Repeat("任", 20)); got != strings.Repeat("任", 13) {
		t.Fatalf("多字节截断应回到 rune 边界: %q", got)
	}
}

// newIssueTestDB 内存 sqlite + issue 表 + 会话映射表（绑定指令链路两张表都触达：候选查
// issue、解绑写会话行——编排层用例的 SaveBoundIssueID 依赖后者在场）。
func newIssueTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newBotTestDB(t, &model.ProjectIssue{}, &model.ImBotConversation{})
}

// mkIssue 建行辅助（必填列兜底，SortOrder 显式控制排序断言；ParentID 缺省空串 = 顶级）。
func mkIssue(t *testing.T, db *gorm.DB, workspaceID int, id, name string, sortOrder float64) {
	t.Helper()
	issue := &model.ProjectIssue{
		ID: id, ProjectID: 1, WorkspaceID: workspaceID, Name: name,
		StateCode: enums.STATE_CODE_BACKLOG, Priority: enums.PRIORITY_NONE,
		SortOrder: sortOrder, TypeID: 1,
	}
	if err := db.Create(issue).Error; err != nil {
		t.Fatalf("建 issue %s: %v", id, err)
	}
}

// TestIssueUnbindTextHelpers 解绑文案契约（触发侧与点击侧断言引用同款语义，锁死关键短语
// 防漂移）。
func TestIssueUnbindTextHelpers(t *testing.T) {
	if !strings.Contains(issueNotBoundText(), "未绑定任务") {
		t.Fatalf("未绑定教学文案应含「未绑定任务」: %s", issueNotBoundText())
	}
	if !strings.Contains(issueUnbindFailedText(), "解绑失败") {
		t.Fatalf("解绑失败文案应含「解绑失败」: %s", issueUnbindFailedText())
	}
}
