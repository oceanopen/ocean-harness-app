package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"ocean-harness/server/internal/buildinfo"
)

// 单命令统一超时：本地回环 + 一次工具调用足够；服务未启动时连接拒绝即刻返回，不会挂满。
const commandTimeout = 30 * time.Second

// withMcpSession 直连 sidecar 的共用编排（mcp 及未来 task 等命名空间同模式）：解析端口
// （失败 exit 2）、套统一超时并建立 MCP 会话执行 fn；连接/协议层错误统一经 connErr
// 分类（不可达附加启动引导）。
func withMcpSession(cmd *cobra.Command, fn func(ctx context.Context, session *mcp.ClientSession) error) error {
	port, err := resolvePortFrom(cmd)
	if err != nil {
		return usageErr("%v", err)
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), commandTimeout)
	defer cancel()
	if err := WithSession(ctx, port, func(session *mcp.ClientSession) error { return fn(ctx, session) }); err != nil {
		return connErr(port, err)
	}
	return nil
}

// connErr 报告连接/协议层失败；「服务不可达类」错误附加启动引导文案。一律 exit 2。
func connErr(port int, err error) error {
	msg := fmt.Sprintf("调用 MCP 服务失败：%v", err)
	if isUnreachable(err) {
		msg += fmt.Sprintf("\nOcean Harness 本地服务不可达（127.0.0.1:%d）。请先启动 Ocean Harness 桌面应用；若服务端口非默认值，可用 --port 指定或设置环境变量 %s 后重试。", port, EnvPort)
		if buildinfo.Mode != "release" {
			msg += fmt.Sprintf("（dev 应用注册的命令名为 %s）", commandName())
		}
	}
	return usageErr("%s", msg)
}

// isUnreachable 判断错误是否为「服务不可达类」（连接拒绝 / 超时），决定是否附加引导文案；
// 其余错误（如工具不存在）保持原始报错，不误导用户去启动应用。
func isUnreachable(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr) && urlErr.Timeout()
}
