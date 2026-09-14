package cli

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callPayload 归一化成功结果供 stdout 输出：StructuredContent 优先（服务端 McpOK 双挂载
// 保证非 nil 且结构干净，见 server/internal/mcpservers/mcp_util/result.go），缺失时尝试把
// 首条 TextContent 解析为 JSON，再退化为原文字符串（三级退化，见单测）。
func callPayload(res *mcp.CallToolResult) any {
	if res.StructuredContent != nil {
		return res.StructuredContent
	}
	if text, ok := firstText(res); ok {
		var v any
		if err := json.Unmarshal([]byte(text), &v); err == nil {
			return v
		}
		return text
	}
	return nil
}

// errText 取 IsError 结果的首条 TextContent 文本（服务端 McpFail 的中文文案通道）。
func errText(res *mcp.CallToolResult) string {
	if text, ok := firstText(res); ok {
		return text
	}
	return "工具执行失败（无错误详情）"
}

// findTool 按名查找工具；schema 命令用于校验未知工具名。
func findTool(tools []*mcp.Tool, name string) (*mcp.Tool, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return nil, false
}

// writeJSON 把数据以紧凑 JSON 写入 stdout（机器消费主轨，SetEscapeHTML(false) 保持中文/符号原样；
// 编码器自带换行收尾）。
func writeJSON(streams Streams, v any) int {
	enc := json.NewEncoder(streams.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(streams.Stderr, "输出序列化失败：%v\n", err)
		return ExitUsage
	}
	return ExitOK
}

// firstText 取结果内首个 TextContent 的文本。
func firstText(res *mcp.CallToolResult) (string, bool) {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text, true
		}
	}
	return "", false
}
