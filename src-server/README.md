# HTTP 本地服务

we-claude-terminal 的 HTTP 本地服务（Go + gin 实现，位于 `src-server/`），提供系统信息等能力。前端 `ServerStatusPage`（`src/windows/panel/ServerStatusPage.tsx`）通过 IPC 查询运行态并 `fetch` 直连调用。Rust 侧进程管理位于 `src-tauri/src/shared/http_server.rs`。

采用分层结构（config / initialize / router / controller / service），**配置全部走环境变量**（不读配置文件）、**数据库用 sqlite**（纯 Go 驱动，无 CGO）。

## 目录结构

遵循 Go 社区 `cmd/` + `internal/` 约定（`internal/` 强制仅本模块可见）：

```
src-server/
├── go.mod / go.sum
├── cmd/server/main.go              # 服务入口：初始化序列 + 优雅退出
└── internal/
    ├── config/config.go            # MustLoadConfig()：从环境变量加载 + 校验（替代配置文件）
    ├── global/global.go            # 进程级单例：Config / Logger / SqliteDB
    ├── initialize/
    │   ├── logger.go               # MustInitZapLogger：zap + lumberjack（日志目录来自 env）
    │   ├── sqlite.go               # MustInitSQLite：gorm + glebarez/sqlite（目录来自 env）
    │   └── gin_writer.go           # InitGinLoggerWriter：gin 日志桥接到 zap
    ├── response/response.go        # 统一响应封装 { code, data, msg }
    ├── types/base_info.go          # /api/baseInfo 模块的请求/返回 DTO
    ├── service/base_info.go        # 业务逻辑层
    ├── controller/base_info.go     # HTTP 处理层（参数 + 响应封装）
    ├── middleware/recovery.go      # panic 恢复（zap 记录 + 统一 500）
    └── router/router.go            # SetupRouter：gin.New + 中间件 + 路由
```

> 分层约定：`router` → `controller`（参数/响应封装）→ `service`（业务逻辑）→ `dal/global`（数据）。新增模块按 `<module>.go` 在 `types`/`service`/`controller` 下组织，路由按 `/api/<module>/<action>` 分组。

## 命名约定

同一业务概念在不同分层有不同数据载体，按后缀区分职责（统一维护在 `types/<module>.go`）：

| 后缀                | 全称                 | 用途                                                                 |
| ------------------- | -------------------- | -------------------------------------------------------------------- |
| `Request` / `Query` | —                    | 接口**入参**                                                         |
| `ResponseData`      | 响应数据             | 返回前端/展示层的**出参**（外层 `{code,msg,data}` 中的 `data` 载荷） |
| `DTO`               | Data Transfer Object | 跨层传输（service ↔ controller）                                     |
| `BO`                | Business Object      | service 内的业务领域对象                                             |
| `PO`                | Persistent Object    | 数据库实体（gorm 表结构，建表时用）                                  |

示例：`SysInfoRequest`（入参）↔ `SysInfoResponseData`（出参）。出参即外层 `{code,msg,data}` 中的 `data` 载荷，外层封装由 `response.Response` 统一处理。

## 配置：环境变量

不读取任何配置文件，全部由 Rust spawn 时注入（见 `src-tauri/src/shared/http_server.rs`）。环境变量名统一大写，沿用 `GO_SERVER_` 前缀：

| 变量                   | 含义            | 校验                                            |
| ---------------------- | --------------- | ----------------------------------------------- |
| `GO_SERVER_MODE`       | gin 运行模式    | 必填，仅 `debug` / `release` / `test`           |
| `GO_SERVER_PORT`       | HTTP 监听端口   | 必填，1–65535 整数；dev=9000、build=9100        |
| `GO_SERVER_LOG_DIR`    | 日志目录        | 必填，自动 `MkdirAll`                           |
| `GO_SERVER_SQLITE_DIR` | sqlite 数据目录 | 必填，自动 `MkdirAll`；库文件 `<dir>/server.db` |

任一缺失或非法 → 启动失败（`log.Fatalf` 给出明确原因）。

## 运行模式与端口

服务由 Tauri（Rust）在启动时**自动拉起**（默认开关 ON），无需手动运行：

| 模式              | `GO_SERVER_MODE` | 端口   | 说明                        |
| ----------------- | ---------------- | ------ | --------------------------- |
| dev（本地启动）   | `debug`          | `9000` | 调试模式，`gin.DebugMode`   |
| build（打包构建） | `release`        | `9100` | 随包分发，`gin.ReleaseMode` |

端口暂不支持配置化/修改，由模式决定，直接通过环境变量注入。前端从 Rust `http_server_status` 命令获取服务地址（`http://127.0.0.1:<port>`），不硬编码端口。

## 初始化序列（main.go）

```
config.MustLoadConfig()        # 读环境变量 + 校验 + gin.SetMode（失败即退出）
initialize.MustInitZapLogger   # zap 三路 tee：app.error.log / app.log / 控制台 + lumberjack 轮转
initialize.MustInitSQLite      # gorm + sqlite，全局 SqliteDB，ping 验活（暂不建表）
initialize.InitGinLoggerWriter # gin.DefaultWriter/ErrorWriter → zap（文件 + 控制台）
printRuntimeConfig             # 启动前用 zap 打印完整环境变量信息
router.SetupRouter             # gin.New + recovery + cors + /api/baseInfo/getSysInfo
*http.Server + SIGTERM/SIGINT  # 优雅退出（Rust 退出时发 SIGTERM）
zap 打印 listening 地址        # 文件 + 控制台都有
```

## API

- `GET /api/baseInfo/getSysInfo` → `{ code, msg, data: { hostname, goVersion, os, arch, mode } }`

`code` 取值：`0` 成功，`1` 失败（中文 Go 生态主流约定）。无需登录/鉴权。CORS 放行所有 origin（webview 与 `127.0.0.1` 不同源）。

## 日志与数据落盘

由 Rust 从 `app_data_dir` 派生并注入（dev/release 自动隔离到独立 `app_data_dir`）：

- 日志：`<app_data_dir>/go-server/logs/{app.log, app.error.log}`（lumberjack 轮转：20MB/文件、保留 30 天）
- sqlite：`<app_data_dir>/go-server/db/server.db`

## 手动调试

```bash
cd src-server
GO_SERVER_MODE=debug \
GO_SERVER_PORT=9000 \
GO_SERVER_LOG_DIR=/tmp/wect-logs \
GO_SERVER_SQLITE_DIR=/tmp/wect-db \
go run ./cmd/server

# 访问：
curl http://127.0.0.1:9000/api/baseInfo/getSysInfo
```

> 依赖为纯 Go（sqlite 用 `glebarez/sqlite`，无 CGO），构建脚本 `scripts/build-server.mjs` 显式 `CGO_ENABLED=0`，支持 CI 跨平台交叉编译。
