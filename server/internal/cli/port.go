package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"ocean-harness/server/internal/buildinfo"
)

// 端口约定（与 app/src/shared/http_server.rs 的 HTTP_SERVER_PORT_TEST/RELEASE 同源，勿单方修改）：
// dev 构建（Mode=test）默认 9000、正式构建（Mode=release）默认 9100。
// 编译期注入见 scripts/build-server.mjs 的 -ldflags -X。
const (
	portTest    = 9000
	portRelease = 9100
)

// EnvPort 服务端口逃生通道环境变量：Rust pty_spawn 向会话终端注入（app/src/pty/mod.rs）；
// air 自测（pnpm server:dev，端口 9200）等非默认场景手动设置。设置后覆盖编译期默认。
const EnvPort = "OCEAN_HARNESS_PORT"

// resolvePort 解析目标服务端口：--port flag > OCEAN_HARNESS_PORT env > 编译期模式默认。
// flagPort 为 0 视作未设置；非法 flag/env 值返回错误（用法错误，exit 2）。
func resolvePort(flagPort int) (int, error) {
	if flagPort != 0 {
		if flagPort < 0 || flagPort > 65535 {
			return 0, fmt.Errorf("--port 值 %d 非法（需要 1-65535 的整数）", flagPort)
		}
		return flagPort, nil
	}
	if raw := strings.TrimSpace(os.Getenv(EnvPort)); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil || p <= 0 || p > 65535 {
			return 0, fmt.Errorf("环境变量 %s 值 %q 非法（需要 1-65535 的整数）", EnvPort, raw)
		}
		return p, nil
	}
	if buildinfo.Mode == "release" {
		return portRelease, nil
	}
	return portTest, nil
}

// resolvePortFrom 取 --port persistent flag 并按优先级解析（mcp 各子命令共用）。
func resolvePortFrom(cmd *cobra.Command) (int, error) {
	flagPort, _ := cmd.Flags().GetInt("port")
	return resolvePort(flagPort)
}
