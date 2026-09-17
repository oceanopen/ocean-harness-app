package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFindTool(t *testing.T) {
	tools := []*mcp.Tool{{Name: "issue_get_info"}, {Name: "issue_update"}}
	if _, ok := findTool(tools, "issue_update"); !ok {
		t.Fatal("want found")
	}
	if _, ok := findTool(tools, "nope"); ok {
		t.Fatal("want not found")
	}
}

func TestToolBriefs(t *testing.T) {
	// 空列表：输出 [] 而非 null
	if got := toolBriefs(nil); len(got) != 0 || got == nil {
		t.Fatalf("nil 输入应得非 nil 空切片，得到 %#v", got)
	}
	tools := []*mcp.Tool{
		{Name: "issue_get_info", Description: "获取 issue 详情。\n第二行说明。"},
		{Name: "issue_update", Description: "部分更新 issue。"},
	}
	got := toolBriefs(tools)
	if len(got) != 2 {
		t.Fatalf("应得 2 项，得到 %d", len(got))
	}
	// 字段保真（含多行 description 原文）
	if got[0].Name != "issue_get_info" || got[0].Description != tools[0].Description {
		t.Fatalf("首项摘要失真：%#v", got[0])
	}
	if got[1].Name != "issue_update" || got[1].Description != "部分更新 issue。" {
		t.Fatalf("次项摘要失真：%#v", got[1])
	}
	// JSON 序列化后 name 在 description 前（map 键序陷阱回归防护）
	b, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), `{"name":`) {
		t.Fatalf("name 应为首个 JSON 字段，得到 %s", b)
	}
}
