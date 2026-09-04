package mcpservers

import "testing"

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
