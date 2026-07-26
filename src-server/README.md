# HTTP 本地服务

we-claude-terminal 的 HTTP 本地服务（Go 实现，位于 `src-server/`），提供系统信息等能力。前端通过 `fetch` 直接调用。Rust 侧进程管理位于 `src-tauri/src/shared/http_server.rs`，前端页面 `src/windows/panel/HttpServerPage.tsx`。

## 目录结构

遵循 Go 社区主流约定（`cmd/` 放入口、`internal/` 放仅包内可见的业务代码）：

```
src-server/
├── go.mod                      # module we-claude-terminal/go-server
├── cmd/server/main.go          # 服务入口（标准库 net/http，零依赖）
├── internal/handler/sysinfo.go # GET /api/sysinfo handler
└── README.md
```

## 运行模式

服务由 Tauri（Rust）应用在启动时**自动拉起**，无需手动运行。两种模式：

| 模式    | 启动方式                                  | 说明                                  |
| ------- | ----------------------------------------- | ------------------------------------- |
| `dev`   | Rust `go build` 编译后 spawn 二进制       | 调试模式，依赖本机 Go 环境            |
| `build` | Rust spawn 编译后的二进制 `go-server-bin` | 由 `pnpm server:build` 产出，随包分发 |

运行模式通过环境变量 `GO_SERVER_MODE`（`dev` / `build`）注入，体现在 `/api/sysinfo` 返回的 `mode` 字段。

## API

- `GET /api/sysinfo` → `{ hostname, goVersion, os, arch, mode }`

固定监听 `127.0.0.1:9000`，前端（`HttpServerPage`）直接 `fetch('http://127.0.0.1:9000/api/sysinfo')`，无需获取端口。

## 手动调试

```bash
cd src-server
GO_SERVER_MODE=dev go run ./cmd/server
# 访问：
curl http://127.0.0.1:9000/api/sysinfo
```
