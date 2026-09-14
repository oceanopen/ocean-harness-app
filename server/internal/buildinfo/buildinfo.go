// Package buildinfo 持有 CLI 二进制的编译期注入信息。唯一写入方是 scripts/build-server.mjs
// 的 -ldflags -X（TAURI_RUN_MODE=dev|build 决定取值）；绕过脚本直接 go build 时保留源码默认值，
// 此时可用 OCEAN_HARNESS_PORT 环境变量在运行期纠正目标服务（见 internal/cli/port.go）。
package buildinfo

// Mode 构建模式：dev 构建 = "test"（默认端口 9000）、正式构建 = "release"（默认端口 9100）。
// 与 Rust 注入 sidecar 的 GO_SERVER_MODE 取值口径一致（见 app/src/shared/http_server.rs）。
var Mode = "test"

// Version CLI 版本，构建时从仓库根 package.json 的 version 注入（bumpp release 流的 SSOT）。
var Version = "dev"
