package cli

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCallPayload(t *testing.T) {
	t.Run("StructuredContent 优先", func(t *testing.T) {
		res := &mcp.CallToolResult{
			StructuredContent: map[string]any{"id": "uuid-1"},
			Content:           []mcp.Content{&mcp.TextContent{Text: `{"id":"uuid-1"}`}},
		}
		got := callPayload(res)
		m, ok := got.(map[string]any)
		if !ok || m["id"] != "uuid-1" {
			t.Fatalf("got %#v, want map with id=uuid-1", got)
		}
	})

	t.Run("缺 Structured 时解析 TextContent JSON", func(t *testing.T) {
		res := &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: `{"name":"x"}`}},
		}
		got := callPayload(res)
		m, ok := got.(map[string]any)
		if !ok || m["name"] != "x" {
			t.Fatalf("got %#v, want parsed map", got)
		}
	})

	t.Run("TextContent 非 JSON 时退化为原文字符串", func(t *testing.T) {
		res := &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "纯文本结果"}},
		}
		if got := callPayload(res); got != "纯文本结果" {
			t.Fatalf("got %#v, want raw string", got)
		}
	})

	t.Run("无内容时为 nil", func(t *testing.T) {
		if got := callPayload(&mcp.CallToolResult{}); got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
}

func TestErrText(t *testing.T) {
	t.Run("取首条 TextContent", func(t *testing.T) {
		res := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{
			&mcp.TextContent{Text: "issue 不存在"},
		}}
		if got := errText(res); got != "issue 不存在" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("无内容兜底文案", func(t *testing.T) {
		if got := errText(&mcp.CallToolResult{IsError: true}); got == "" {
			t.Fatal("want non-empty fallback text")
		}
	})
}

func TestFindTool(t *testing.T) {
	tools := []*mcp.Tool{{Name: "issue_get_info"}, {Name: "issue_update"}}
	if _, ok := findTool(tools, "issue_update"); !ok {
		t.Fatal("want found")
	}
	if _, ok := findTool(tools, "nope"); ok {
		t.Fatal("want not found")
	}
}
