package mcpservers

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMcpOceanHarnessServerRegistered 守护 init() 的工具注册：go test 加载本包即执行
// init()，全部工具的入出参 schema 在 AddTool 时同步推导——任何 DTO 的 jsonschema tag
// 违反 go-sdk 禁令（首词含 '='，报 "tag must not begin with 'WORD='"）都会在此 panic
// 暴露，而非等到 server 启动。
func TestMcpOceanHarnessServerRegistered(t *testing.T) {
	if mcpServerOceanHarness == nil {
		t.Fatal("mcpServerOceanHarness 单例未初始化")
	}
	if handler := McpOceanHarnessStreamableHTTPHandler(); handler == nil {
		t.Fatal("StreamableHTTPHandler 不应为 nil")
	}
}

// browserToolsRegistered 是 browser 工具面（T2.2）注册全集：原生 6 + 转发 17 + 逃生舱 1。
var browserToolsRegistered = []string{
	"browser_status",
	"browser_profile_open",
	"browser_session_close",
	"browser_auth_state",
	"browser_auth_request",
	"browser_tool_list",
	"browser_navigate",
	"browser_snapshot",
	"browser_click",
	"browser_type",
	"browser_fill_form",
	"browser_select_option",
	"browser_wait_for",
	"browser_evaluate",
	"browser_tabs",
	"browser_take_screenshot",
	"browser_file_upload",
	"browser_console_messages",
	"browser_navigate_back",
	"browser_close",
	"browser_find",
	"browser_press_key",
	"browser_handle_dialog",
	"browser_tool_call",
}

// TestMcpOceanHarnessBrowserToolsRegistered 经 in-memory 会话拉 tools/list，守护
// browser_* 24 工具全数注册且无多余 browser_ 前缀工具（拼错名/漏注册在此显式暴露）。
func TestMcpOceanHarnessBrowserToolsRegistered(t *testing.T) {
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := mcpServerOceanHarness.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatalf("server 建连: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "registered-test", Version: "test"}, nil)
	cs, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client 建连: %v", err)
	}
	defer cs.Close()

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	registered := map[string]bool{}
	browserCount := 0
	for _, tl := range res.Tools {
		registered[tl.Name] = true
		if len(tl.Name) > len("browser_") && tl.Name[:len("browser_")] == "browser_" {
			browserCount++
		}
	}
	for _, want := range browserToolsRegistered {
		if !registered[want] {
			t.Errorf("browser 工具 %s 未注册", want)
		}
	}
	if browserCount != len(browserToolsRegistered) {
		t.Errorf("browser_* 注册数 = %d, want %d（存在名单外 browser_ 前缀工具）", browserCount, len(browserToolsRegistered))
	}
}
