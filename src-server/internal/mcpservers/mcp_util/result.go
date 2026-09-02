package mcputil

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"ocean-harness/src-server/internal/global"
)

// McpOK 把业务出参包装为工具成功结果：TextContent（JSON 文本，LLM 消费主轨）+
// StructuredContent（同一 Go 对象直接挂载，供支持结构化读取的客户端；类型即反射推导
// OutputSchema 的来源，字段描述见 mcp_dto 的 jsonschema tag）。单次 Marshal 双挂载。
func McpOK[T any](data T) (*mcp.CallToolResultFor[T], error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("工具结果序列化失败: %w", err) // 程序缺陷 → 协议层 error
	}
	return &mcp.CallToolResultFor[T]{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(raw)}},
		StructuredContent: data,
	}, nil
}

// McpFail 把业务失败包装为 isError=true 的工具结果（SDK 推荐语义：业务错误放结果内，
// LLM 能读到中文文案并自我纠正；协议层 error 仅留给序列化失败等程序缺陷）。
func McpFail[T any](err error) (*mcp.CallToolResultFor[T], error) {
	if global.Logger != nil {
		global.Logger.Warn("[mcp] tool failed", zap.Error(err))
	}
	return &mcp.CallToolResultFor[T]{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}, nil
}
