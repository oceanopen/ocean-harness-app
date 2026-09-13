package githubapi

import (
	"encoding/json"
	"testing"
)

// TestPullRequestWireFormat 用 GitHub REST v3 真实响应片段固定反序列化契约
// （T4.1 审查修复：html_url 为 snake_case、head/base 为嵌套对象——tag 漂移曾致
// 链接与分支名恒为空串）。
func TestPullRequestWireFormat(t *testing.T) {
	raw := `{
		"number": 1347,
		"title": "feat: add login",
		"state": "open",
		"html_url": "https://github.com/octocat/Hello-World/pull/1347",
		"head": {"ref": "agent_abc123", "label": "octocat:agent_abc123"},
		"base": {"ref": "main", "label": "octocat:main"}
	}`
	var pr PullRequest
	if err := json.Unmarshal([]byte(raw), &pr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pr.Number != 1347 || pr.Title != "feat: add login" || pr.State != "open" {
		t.Fatalf("基础字段解析错误: %+v", pr)
	}
	if pr.HTMLURL != "https://github.com/octocat/Hello-World/pull/1347" {
		t.Fatalf("html_url 解析错误: %q", pr.HTMLURL)
	}
	if pr.HeadRef() != "agent_abc123" || pr.BaseRef() != "main" {
		t.Fatalf("head/base ref 解析错误: %q / %q", pr.HeadRef(), pr.BaseRef())
	}
}

// TestMergeCIState 覆盖两体系归并的关键场景（T4.1 审查修复：纯 Actions 仓库不再
// 恒 pending、无 CI 仍 pending、failure 优先）。
func TestMergeCIState(t *testing.T) {
	completed := func(conclusion string) CIRecord {
		return CIRecord{Status: "completed", Conclusion: conclusion}
	}
	cases := []struct {
		name          string
		combinedState string
		checks        []CIRecord
		want          string
	}{
		// 纯 Actions 仓库（combined 查询失败或空集归一为 ""）：核心修复场景
		{name: "actions-only-all-success", combinedState: "", checks: []CIRecord{completed("success"), completed("success")}, want: "success"},
		{name: "actions-only-has-failure", combinedState: "", checks: []CIRecord{completed("success"), completed("failure")}, want: "failure"},
		{name: "actions-only-in-progress", combinedState: "", checks: []CIRecord{completed("success"), {Status: "in_progress"}}, want: "pending"},
		{name: "actions-only-neutral-skipped", combinedState: "", checks: []CIRecord{completed("neutral"), completed("skipped")}, want: "success"},
		// 无任何 CI
		{name: "no-ci-at-all", combinedState: "", checks: nil, want: "pending"},
		// 旧体系主导
		{name: "legacy-success", combinedState: "success", checks: nil, want: "success"},
		{name: "legacy-failure", combinedState: "failure", checks: []CIRecord{completed("success")}, want: "failure"},
		{name: "legacy-pending-with-statuses", combinedState: "pending", checks: nil, want: "pending"},
		{name: "legacy-error", combinedState: "error", checks: nil, want: "failure"},
		// 混合
		{name: "mixed-all-success", combinedState: "success", checks: []CIRecord{completed("success")}, want: "success"},
		{name: "mixed-legacy-pending-actions-done", combinedState: "pending", checks: []CIRecord{completed("success")}, want: "pending"},
		{name: "actions-cancelled", combinedState: "", checks: []CIRecord{completed("cancelled")}, want: "failure"},
		{name: "actions-timed-out", combinedState: "", checks: []CIRecord{completed("timed_out")}, want: "failure"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeCIState(tc.combinedState, tc.checks); got != tc.want {
				t.Fatalf("mergeCIState(%q, %v) = %q, want %q", tc.combinedState, tc.checks, got, tc.want)
			}
		})
	}
}
