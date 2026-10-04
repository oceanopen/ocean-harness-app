package bot

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
)

// TestParseIssueCommand 表驱动覆盖 #issue 指令解析的形态域：非指令放行、裸指令、
// 解绑（含撞词收窄）、绑定纯切换、绑定+首回合（正文切分）。
func TestParseIssueCommand(t *testing.T) {
	cases := []struct {
		name string
		text string
		want *issueCommand
	}{
		{"普通文本", "修复一下登录页", nil},
		{"空串", "", nil},
		{"中部出现不解析", "看看 #issue 登录页", nil},
		{"粘连形态 #issues", "#issues 登录页", nil},
		{"粘连形态 #issue-42", "#issue-42 修复", nil},
		{"裸指令", "#issue", &issueCommand{}},
		{"裸指令带空白", "  #issue  ", &issueCommand{}},
		{"解绑", "#issue 解绑", &issueCommand{Unbind: true}},
		{"解绑带前后空白", " #issue   解绑  ", &issueCommand{Unbind: true}},
		{"解绑撞词收窄（多词归关键词路径）", "#issue 解绑 xxx", &issueCommand{Target: "解绑", Body: "xxx"}},
		{"绑定纯切换", "#issue 登录页", &issueCommand{Target: "登录页"}},
		{"绑定+首回合", "#issue 登录页 修复一下样式", &issueCommand{Target: "登录页", Body: "修复一下样式"}},
		{"制表符切分", "#issue\t登录页", &issueCommand{Target: "登录页"}},
		{"ID 前缀形态", "#issue 0198abcd", &issueCommand{Target: "0198abcd"}},
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

// TestIsIssueIDPrefix uuid 短前缀形态判定的边界（去连字符、最短 8 位、全 hex）。
func TestIsIssueIDPrefix(t *testing.T) {
	cases := []struct {
		keyword string
		want    bool
	}{
		{"0198abcd", true},
		{"0198abcd-4f", true}, // 带连字符：去连字符后达标
		{"0198ABCD", true},    // 大写 hex
		{"0198abc", false},    // 不足 8 位
		{"0198abcg", false},   // 含非 hex 字符
		{"登录页", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isIssueIDPrefix(tc.keyword); got != tc.want {
			t.Fatalf("isIssueIDPrefix(%q) = %v, want %v", tc.keyword, got, tc.want)
		}
	}
}

// TestIssueIDLikePrefix LIKE 前缀截断：仅「无连字符且超 8 位」形态截前 8 位。
func TestIssueIDLikePrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0198abcd", "0198abcd"},
		{"0198abcdef12", "0198abcd"},       // 无连字符超 8 位：截断
		{"0198abcd-4f2e", "0198abcd-4f2e"}, // 带连字符：原样（连字符已对位）
	}
	for _, tc := range cases {
		if got := issueIDLikePrefix(tc.in); got != tc.want {
			t.Fatalf("issueIDLikePrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// newIssueTestDB 内存 sqlite + issue 表 + 会话映射表（#issue 指令链路两张表都触达：
// 匹配查 issue、绑定写会话行——编排层用例的 SaveBoundIssueID 依赖后者在场）。
func newIssueTestDB(t *testing.T) *gorm.DB {
	return newBotTestDB(t, &model.ProjectIssue{}, &model.ImBotConversation{})
}

// mkIssue 建行辅助（必填列兜底，SortOrder 显式控制排序断言）。
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

// TestResolveIssueTarget 匹配双通道验收：uuid 前缀通道恒精确（含带连字符形态与跨库
// workspace 隔离）、零命中回落标题子串通道、SortOrder 升序、外域关键词不串扰。
func TestResolveIssueTarget(t *testing.T) {
	db := newIssueTestDB(t)
	const wsA, wsB = 1, 2
	mkIssue(t, db, wsA, "0198abcd-4f2e-7111-8111-3b6ac9e1f201", "登录页样式重构", 1)
	mkIssue(t, db, wsA, "0198ffff-0000-7111-8111-3b6ac9e1f202", "登录页接口联调", 2)
	mkIssue(t, db, wsA, "zzzzzzzz-zzzz-7111-8111-3b6ac9e1f203", "0198abcd 相关任务", 3) // 标题撞前缀
	mkIssue(t, db, wsB, "0198abcd-9999-7111-8111-3b6ac9e1f204", "外域登录页", 4)

	// uuid 前缀通道：只命中 id 前缀，标题撞词者不混入（恒精确）。
	hits, err := resolveIssueTarget(db, wsA, "0198abcd")
	if err != nil || len(hits) != 1 || hits[0].ID != "0198abcd-4f2e-7111-8111-3b6ac9e1f201" {
		t.Fatalf("ID 前缀应精确单命中: hits=%v err=%v", idsOf(hits), err)
	}
	// 带连字符形态去连字符后同通道。
	if hits, err = resolveIssueTarget(db, wsA, "0198abcd-4f"); err != nil || len(hits) != 1 {
		t.Fatalf("带连字符前缀应同通道命中: hits=%v err=%v", idsOf(hits), err)
	}
	// 无连字符超 8 位形态截前 8 位进通道（uuid 第 9 位恒为连字符，原样拼接会恒零命中）。
	if hits, err = resolveIssueTarget(db, wsA, "0198abcdef12"); err != nil || len(hits) != 1 {
		t.Fatalf("无连字符长前缀应截断命中: hits=%v err=%v", idsOf(hits), err)
	}
	// 标题子串通道（多命中按 SortOrder 升序）。
	if hits, err = resolveIssueTarget(db, wsA, "登录页"); err != nil || len(hits) != 2 ||
		hits[0].Name != "登录页样式重构" || hits[1].Name != "登录页接口联调" {
		t.Fatalf("标题通道应按 SortOrder 双命中: hits=%v err=%v", namesOf(hits), err)
	}
	// 前缀形态零命中回落标题通道（keyword 恰为标题子串）。
	if hits, err = resolveIssueTarget(db, wsA, "0198abcd 相关"); err != nil || len(hits) != 1 {
		t.Fatalf("前缀零命中应回落标题通道: hits=%v err=%v", namesOf(hits), err)
	}
	// workspace 隔离：外域同前缀/同标题 issue 不命中。
	if hits, err = resolveIssueTarget(db, wsA, "外域"); err != nil || len(hits) != 0 {
		t.Fatalf("workspace 隔离应零命中: hits=%v err=%v", namesOf(hits), err)
	}
	// 全零命中。
	if hits, err = resolveIssueTarget(db, wsA, "不存在关键词"); err != nil || len(hits) != 0 {
		t.Fatalf("零命中应返回空表: hits=%v err=%v", namesOf(hits), err)
	}
}

func idsOf(hits []*model.ProjectIssue) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

func namesOf(hits []*model.ProjectIssue) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Name)
	}
	return out
}

// TestIssueCandidatesText 多命中候选文案：截前 issueCandidateLimit 条 + 总数注明。
func TestIssueCandidatesText(t *testing.T) {
	hits := make([]*model.ProjectIssue, 0, 7)
	for i := 0; i < 7; i++ {
		hits = append(hits, &model.ProjectIssue{
			ID:   strings.Repeat("a", 8) + string(rune('0'+i)) + "-0000-7111-8111-3b6ac9e1f2ff",
			Name: "候选" + string(rune('A'+i)),
		})
	}
	text := issueCandidatesText("候选", hits)
	if got := strings.Count(text, "\n- 「"); got != issueCandidateLimit {
		t.Fatalf("候选行应截 %d 条, got %d: %s", issueCandidateLimit, got, text)
	}
	if !strings.Contains(text, "共 7 个") {
		t.Fatalf("应注明总数 7: %s", text)
	}
	if !strings.Contains(text, "候选A") || strings.Contains(text, "候选F") {
		t.Fatalf("应保留前 5 条且截去第 6 条起: %s", text)
	}

	// 恰好 5 条不出现总数注明。
	five := hits[:issueCandidateLimit]
	if text = issueCandidatesText("候选", five); strings.Contains(text, "共") {
		t.Fatalf("恰好 %d 条不应注明总数: %s", issueCandidateLimit, text)
	}
}

// TestIssueTextHelpers 关键文案的内容契约（编排器断言引用同款常量语义，锁死关键短语防漂移）。
func TestIssueTextHelpers(t *testing.T) {
	if !strings.Contains(issueUsageText(), "#issue") {
		t.Fatalf("用法文案应含指令 token: %s", issueUsageText())
	}
	issue := &model.ProjectIssue{ID: "0198abcd-4f2e-7111-8111-3b6ac9e1f201", Name: "登录页"}
	if !strings.Contains(issueBoundText(issue), "登录页") || !strings.Contains(issueBoundText(issue), "0198abcd") {
		t.Fatalf("绑定文案应含标题与短 ID: %s", issueBoundText(issue))
	}
	if !strings.Contains(issueZeroHitText("关键词"), "关键词") {
		t.Fatalf("零命中文案应回显关键词: %s", issueZeroHitText("关键词"))
	}
	if !strings.Contains(issueBindFailedText(), "绑定失败") || !strings.Contains(issueUnbindFailedText(), "解绑失败") {
		t.Fatalf("落库失败文案应区分绑定/解绑: %s | %s", issueBindFailedText(), issueUnbindFailedText())
	}
}
