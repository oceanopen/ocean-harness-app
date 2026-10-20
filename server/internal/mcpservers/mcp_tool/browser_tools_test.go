package mcptool

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpdto "ocean-harness/server/internal/mcpservers/mcp_dto"
)

/*
 * browser 工具面纯函数单测：profile 归一、引擎入参抽取（剥服务端自有字段）、归因元数据追加、
 * 逃生舱黑名单前置拒绝（后者走 handler 真实入口——黑名单校验先于链式装配，无需测试环境装配）。
 */

func TestResolveProfile(t *testing.T) {
	if got := resolveProfile("", "", ""); got != defaultBrowserProfile {
		t.Fatalf(`resolveProfile("", "", "") = %q, want %q`, got, defaultBrowserProfile)
	}
	if got := resolveProfile("work", "", ""); got != "work" {
		t.Fatalf(`resolveProfile("work", "", "") = %q, want 原值`, got)
	}
	// 任务派生约定：profile 留空且 issueId 在手 → issue-<issueId>；显式 profile 不被覆写。
	if got := resolveProfile("", "abc-123", ""); got != "issue-abc-123" {
		t.Fatalf(`resolveProfile("", "abc-123", "") = %q, want issue-abc-123`, got)
	}
	if got := resolveProfile("work", "abc-123", ""); got != "work" {
		t.Fatalf(`resolveProfile("work", "abc-123", "") = %q, want 原值`, got)
	}
	// 工作空间派生约定：profile 与 issueId 均留空 → workspace-<workspaceId>；issueId 优先。
	if got := resolveProfile("", "", "ws-1"); got != "workspace-ws-1" {
		t.Fatalf(`resolveProfile("", "", "ws-1") = %q, want workspace-ws-1`, got)
	}
	if got := resolveProfile("", "abc-123", "ws-1"); got != "issue-abc-123" {
		t.Fatalf(`resolveProfile("", "abc-123", "ws-1") = %q, want issue-abc-123（issueId 优先）`, got)
	}
}

func TestEngineArgs(t *testing.T) {
	in := mcpdto.BrowserNavigateArgs{
		BrowserProfileArgs: mcpdto.BrowserProfileArgs{Profile: "work", IssueID: "issue-1"},
		URL:                "https://example.com",
	}
	args := engineArgs(&in)
	if args["url"] != "https://example.com" {
		t.Fatalf("url = %v, want 引擎参数保留", args["url"])
	}
	for _, banned := range []string{"profile", "issueId"} {
		if _, ok := args[banned]; ok {
			t.Fatalf("args 含服务端自有字段 %s: %v", banned, args)
		}
	}
}

func TestEngineArgsPointerFields(t *testing.T) {
	depth := 3
	in := mcpdto.BrowserSnapshotArgs{
		BrowserProfileArgs: mcpdto.BrowserProfileArgs{Profile: "default"},
		Depth:              &depth,
		Boxes:              nil, // 未传字段不出现在引擎入参（omitempty）
	}
	args := engineArgs(&in)
	if args["depth"] != float64(3) { // JSON 往返数值统一 float64
		t.Fatalf("depth = %v, want 3", args["depth"])
	}
	if _, ok := args["boxes"]; ok {
		t.Fatalf("未传的指针字段不应出现在引擎入参: %v", args)
	}
}

func TestWithForwardMeta(t *testing.T) {
	engine := &mcp.TextContent{Text: "engine-result"}

	res := &mcp.CallToolResult{Content: []mcp.Content{engine}}
	withForwardMeta(res, "work", "")
	last, ok := res.Content[1].(*mcp.TextContent)
	if !ok || res.Content[0] != mcp.Content(engine) || last.Text != "[profile: work]" {
		t.Fatalf("无 issueId 元数据 = %+v, want 引擎 Content 保持首位、[profile: work] 尾部追加", res.Content)
	}

	res = &mcp.CallToolResult{Content: []mcp.Content{engine}}
	withForwardMeta(res, "work", "issue-9")
	last, ok = res.Content[1].(*mcp.TextContent)
	if !ok || last.Text != "[profile: work, issueId: issue-9]" {
		t.Fatalf("带 issueId 元数据 = %v, want [profile: work, issueId: issue-9] 尾部追加", res.Content[1])
	}
}

func TestToolCallBlacklistRejected(t *testing.T) {
	// 黑名单校验先于链式装配：无 MakeContext 环境也可断言拒绝文案。
	mt := McpBrowserTool{}
	_, _, err := mt.ToolCall(context.Background(), nil, mcpdto.BrowserToolCallArgs{
		BrowserProfileArgs: mcpdto.BrowserProfileArgs{Profile: "default"},
		Name:               "browser_run_code_unsafe",
	})
	if err == nil || !strings.Contains(err.Error(), "禁止直调") {
		t.Fatalf("err = %v, want 黑名单中文拒绝", err)
	}
}
