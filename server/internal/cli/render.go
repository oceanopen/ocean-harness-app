package cli

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
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

// writeJSON 把数据以格式化 JSON（换行 + 2 空格缩进）写入 stdout：终端人读为主轨，
// 格式化后仍是合法 JSON、jq/解析器消费不受空白影响；SetEscapeHTML(false) 保持中文/符号
// 原样（编码器自带换行收尾）。输出流经 cobra 注入点（OutOrStdout），测试可 SetOut 替换。
func writeJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return usageErr("输出序列化失败：%v", err)
	}
	return nil
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
