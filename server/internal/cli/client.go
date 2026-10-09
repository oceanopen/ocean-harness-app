package cli

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"ocean-harness/server/internal/buildinfo"
)

// MCP 端点路径（与 server/internal/router/router.go 的挂载点同源，勿单方修改）。
const endpointPath = "/mcp/streamableHttp/oceanHarness"

// WithSession 建立一次 MCP 会话并执行 fn，返回前下发 DELETE 终止服务端会话。
//
// CLI 进程寿命 = 单命令，go-sdk 的 Transport 一次性（at most one Connect），
// 故「每命令新建连接」是唯一形态；initialize 握手在 Connect 内部完成。
// Close（DELETE）是会话释放的首选路径；服务端另配 10 分钟空闲 TTL 兜底回收，
// 即使 Close 缺失也不留永久孤儿会话，生命周期仍收敛在本函数内，调用方无需感知。
func WithSession(ctx context.Context, port int, fn func(*mcp.ClientSession) error) error {
	client := mcp.NewClient(&mcp.Implementation{Name: "ocean-harness-cli", Version: buildinfo.Version}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint: fmt.Sprintf("http://127.0.0.1:%d%s", port, endpointPath),
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	return fn(session)
}

// listTools 拉取全部工具（NextCursor 翻页到尽），供 tools / schema 两命令复用。
func listTools(ctx context.Context, session *mcp.ClientSession) ([]*mcp.Tool, error) {
	var tools []*mcp.Tool
	cursor := ""
	for {
		res, err := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		tools = append(tools, res.Tools...)
		if res.NextCursor == "" {
			return tools, nil
		}
		cursor = res.NextCursor
	}
}
