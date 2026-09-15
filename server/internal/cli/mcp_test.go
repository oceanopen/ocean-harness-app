package cli

import (
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
