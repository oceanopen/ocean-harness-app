# HTTP 本地服务

we-claude-terminal 的 HTTP 本地服务（Go + gin 实现，位于 `src-server/`）。
Rust 侧进程管理位于 `src-tauri/src/shared/http_server.rs`。

采用分层结构（config / initialize / router / controller / service），**配置优先级：环境变量 > yaml 配置文件**（本地调试用 `config/settings.dev.yaml`）、**数据库用 sqlite**（纯 Go 驱动，无 CGO）。

## 目录结构

遵循 Go 社区 `cmd/` + `internal/` 约定（`internal/` 强制仅本模块可见）：

```
src-server/
├── go.mod / go.sum
├── .air.toml                       # air 热重载配置（本地自测，pnpm server:dev）
├── config/
│   └── settings.dev.yaml           # 本地调试默认配置（环境变量缺失时回退于此）
├── cmd/server/main.go              # 服务入口：初始化序列 + 优雅退出
├── cmd/gormgen/                    # gorm/gen 代码生成器（pnpm server:gorm:gen，见「gorm/gen 代码生成」）
└── internal/
    ├── config/config.go            # MustLoadConfig()：yaml 默认 + 环境变量覆盖（env 优先）+ 校验
    ├── global/global.go            # 进程级单例：Config / Logger / SqliteDB
    ├── initialize/
    │   ├── logger.go               # MustInitZapLogger：zap + lumberjack（日志目录来自配置）
    │   ├── sqlite.go               # MustInitSQLite：gorm + glebarez/sqlite（目录来自配置）
    │   └── gin_writer.go           # InitGinLoggerWriter：gin 日志桥接到 zap
    ├── apis/                        # API 管道：Api/Service 链式基类 + 统一响应封装（JsonOK/JsonFail/Response）
    ├── service/base_info.go        # 业务逻辑层
    ├── controller/base_info.go     # HTTP 处理层（参数 + 响应封装）
    ├── middleware/recovery.go      # panic 恢复（zap 记录 + 统一 500）
    ├── router/router.go            # SetupRouter：gin.New + 中间件 + 路由
    └── dal/                        # 数据访问层（gorm/gen 生成 DO/DAO + 类型/DTO 定义）
        ├── model/                  # gorm/gen 生成：各表 PO 结构体（model.Workspace）
        ├── query/                  # gorm/gen 生成：类型安全 CRUD（query.Use(db)）
        └── types/                  # 请求/返回 DTO（types.BaseInfoRequest）
```

> 分层约定：`router` → `controller`（嵌 `apis.Api`，参数解析/校验/响应）→ `service`（嵌 `apis.Service`，业务逻辑）→ `dal`（query/model）。tracker 业务域接口规范见下「API 统一规范」；新增模块按 `<module>.go` 在 `dal/types`、`service`、`controller` 下组织。

## 命名约定

同一业务概念在不同分层有不同数据载体，按后缀区分职责（统一维护在 `dal/types/<module>.go`）：

| 后缀                | 全称                 | 用途                                                                                                                                                       |
| ------------------- | -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Request` / `Query` | —                    | 接口**入参**                                                                                                                                               |
| `ResponseData`      | 响应数据             | 返回前端/展示层的**出参**（外层 `{code,msg,data}` 中的 `data` 载荷）                                                                                       |

示例：`SysInfoRequest`（入参）↔ `SysInfoResponseData`（出参）。出参即外层 `{code,msg,data}` 中的 `data` 载荷，外层封装由 `apis.Response` 统一处理。

## API 统一规范（tracker 域）

tracker 业务域（workspace / project / issue / state / label）的所有接口遵循统一范式，作为后续模块的基线。

**数据流**：`router → controller（嵌 apis.Api）→ service（嵌 apis.Service）→ dal(query/model)`。运行期依赖（ctx / Orm / Logger）由 controller 链式灌入 service，service 方法只收 request DTO，不用全局态。

**路由**：`/api/tracker/<module>/<action>`，**一律 POST**（避免同一对象 json/form 并存；getList/getInfo/create/update/delete 均走 POST，便于后续加参）。action 命名固定：`getList / getInfo / create / update / delete`。

**请求类型**：每个 action 一个独立 Request（`<Module><Action>Request`，如 `WorkspaceGetInfoRequest`、`WorkspaceDeleteRequest`，不复用）。常规校验用 gin `binding` tag（`required`/`max` 等，`ShouldBindJSON` 自动触发）；跨字段/复杂场景追加 `vd` tag（go-tagexpr，`@:expr; msg:'中文'`，由 `apis.Api.Validate` 生效）。

**Controller 模板**（嵌入 `apis.Api`，链式装配 + 分层错误）：

```go
type Workspace struct { apis.Api }

func (api Workspace) GetInfo(ctx *gin.Context) {
    req := &types.WorkspaceGetInfoRequest{}
    svc := service.Workspace{}
    if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
        api.JsonFail(err) // 绑定/校验失败
        return
    }
    data, err := svc.GetInfo(req)
    if err != nil {
        api.JsonFail(err) // 业务错误
        return
    }
    api.JsonOK(data)
}
```

**Service 模板**（嵌入 `apis.Service`，只收 req，返原始 model——不返 ResponseData，由 controller 直 JsonOK）：

```go
type Workspace struct { apis.Service }

func (svc Workspace) GetInfo(req *types.WorkspaceGetInfoRequest) (*model.Workspace, error) {
    q := query.Use(svc.Orm)
    return q.Workspace.WithContext(svc.Context).Where(q.Workspace.ID.Eq(req.ID)).First()
}
```

**响应**：`apis.JsonOK(ctx, data)` / `apis.JsonFail(ctx, err)` / `apis.JsonFailWithCode(ctx, code, msg)`，结构 `{code,msg,data}`（成功 0 / 失败 1，业务错误统一 HTTP 200）。

**分页**：**大列表**（如 issue，可能上千条）用 `getListByPage`——请求 DTO 内嵌 `apis.PageInfo{pageSize, currentPage}`，service 用 gen `q.Xxx.WithContext(ctx).FindByPage(pageInfo.GetOffset(), pageInfo.GetPageSize())` 返 `(list, count, err)`、回填 `pageInfo.TotalCount` 后返 `(list, *apis.PageInfo, error)`，controller `api.JsonPageOK(list, pageInfo)`（data=`{list, pageInfo}`）。**小列表**（如 workspace，个位数）用 `getList` 返全量、不分页。

**命名**：context 变量统一 `ctx`（不简写 `c`）；controller / service / model 同名分属不同包（`controller.Workspace` / `service.Workspace` / `model.Workspace`）。基类落 `internal/apis/`（`api.go` + `service.go`）。

## 配置：环境变量 + yaml 配置文件

优先级：**环境变量 > yaml 配置文件**。环境变量名统一大写，沿用 `GO_SERVER_` 前缀。

- **生产环境**：由 Rust spawn 时注入环境变量（见 `src-tauri/src/shared/http_server.rs`），覆盖配置文件；不传 `-config`、不读文件，与改造前完全一致。
- **本地调试**：经 `-config config/settings.dev.yaml` 提供默认值，环境变量缺失时回退于此（air 自测不注入环境变量，故取文件值，如 port=9200）。

| 变量                   | 含义            | 校验                                            |
| ---------------------- | --------------- | ----------------------------------------------- |
| `GO_SERVER_MODE`       | gin 运行模式    | 必填，仅 `debug` / `release` / `test`           |
| `GO_SERVER_PORT`       | HTTP 监听端口   | 必填，1–65535 整数                              |
| `GO_SERVER_LOG_DIR`    | 日志目录        | 必填，自动 `MkdirAll`                           |
| `GO_SERVER_SQLITE_DIR` | sqlite 数据目录 | 必填，自动 `MkdirAll`；库文件 `<dir>/server.db` |

`config/settings.dev.yaml` 字段同上（`mode` / `port` / `logDir` / `sqliteDir`，camelCase）。合并后任一必填字段缺失或非法 → 启动失败（`log.Fatalf` 给出明确原因）。

## 运行模式与端口

三种运行场景：

| 场景              | 命令                  | mode / 端口来源    | 端口   | 说明                                      |
| ----------------- | --------------------- | ------------------ | ------ | ----------------------------------------- |
| 本地 air 自测     | `pnpm server:dev`     | yaml（不注入 env） | `9200` | air 热重载，读 `config/settings.dev.yaml` |
| 客户端联调（dev） | `pnpm server:test`    | Rust 注入 env      | `9000` | 构建 dev 二进制，`tauri:dev` 拉起 sidecar |
| 正式构建（build） | `pnpm server:release` | Rust 注入 env      | `9100` | 随包分发，`gin.ReleaseMode`               |

- 客户端/生产场景：端口由 Rust 按模式注入环境变量（覆盖 yaml）。前端从 Rust `http_server_status` 命令获取服务地址（`http://127.0.0.1:<port>`），不硬编码端口。
- 本地 air 自测：端口 9200 来自 `config/settings.dev.yaml`，与客户端的 9000 隔离，互不影响。

## API

- `GET /api/baseInfo/getServerRunInfo` → `{ code, msg, data: { sysInfo: { hostname, goVersion, os, arch }, serverInfo: { mode, address, logDir, sqliteDir } } }`

`code` 取值：`0` 成功，`1` 失败（中文 Go 生态主流约定）。无需登录/鉴权。CORS 放行所有 origin（webview 与 `127.0.0.1` 不同源）。

## 日志与数据落盘

由 Rust 从 `app_data_dir` 派生并注入（dev/release 自动隔离到独立 `app_data_dir`）：

- 日志：`<app_data_dir>/app-server/logs/{app.log, app.error.log}`（lumberjack 轮转：20MB/文件、保留 30 天）
- sqlite：`<app_data_dir>/app-server/db/server.db`

## 本地调试

**方式一：air 热重载（推荐，本地 http 自测）**

```bash
# 前置：安装 air
go install github.com/air-verse/air@latest

# 仓库根执行（等价 cd src-server && air），改 .go 自动重建重启
pnpm server:dev

# 访问（端口来自 config/settings.dev.yaml = 9200）：
curl http://127.0.0.1:9200/api/baseInfo/getServerRunInfo
```

**方式二：手动环境变量（模拟生产注入）**

```bash
cd src-server
GO_SERVER_MODE=debug \
GO_SERVER_PORT=9000 \
GO_SERVER_LOG_DIR=/tmp/wect-logs \
GO_SERVER_SQLITE_DIR=/tmp/wect-db \
go run ./cmd/server

curl http://127.0.0.1:9000/api/baseInfo/getServerRunInfo
```

**方式三：客户端联调（构建 sidecar + 打开应用）**

```bash
pnpm server:test   # 构建 dev 二进制（原 server:dev）
pnpm tauri:dev     # Rust 拉起 sidecar 并打开客户端，端口 9000
```

> 依赖为纯 Go（sqlite 用 `glebarez/sqlite`，无 CGO），构建脚本 `scripts/build-server.mjs` 显式 `CGO_ENABLED=0`，支持 CI 跨平台交叉编译。

## gorm/gen 代码生成（DO 层）

业务表（workspace/project/state/issue/label 等）的 DO 层（`PO` 结构体 + 类型安全 CRUD）由 [gorm/gen](https://github.com/go-gorm/gen) 按**当前 sqlite 实际表结构**自动生成，落在 `internal/dal/`：

- `internal/dal/query/`（package `query`）：query 层 + `gen.go`（`Use(db)` / `Query` / `WithContext` / `Transaction`）。
- `internal/dal/model/`（package `model`）：各表 `PO` 结构体（如 `model.Workspace`）。

生成器源码在 `cmd/gormgen/`（`main.go` + `init_gen.go` + `gen_model_tracker.go`）。它复用服务 initialize 序列（config → zap → sqlite → goose 迁移），确保库与表就绪后再 introspect 生成，故**单条命令即可完成迁移 + 生成**。

**运行：**

```bash
pnpm server:gorm:gen   # 等价 cd src-server && go run ./cmd/gormgen -config config/settings.dev.yaml
```

```bash
GOPROXY=https://goproxy.cn,direct go -C src-server mod tidy
```

**改表流程：** 改 `internal/migrations/migrations/*.sql` → 跑一次 `pnpm server:gorm:gen`（goose 自动向前迁移建表）→ 同条命令随即重新生成 DO（迁移 + 生成一气呵成）。

**调用范式（service 层）：** 不生成全局 `Q`/`SetDefault`，每次调用 `Use` 取一个 query 对象（无全局态、可注入事务）：

```go
import (
    "we-claude-terminal/go-server/internal/global"
    "we-claude-terminal/go-server/internal/dal/query"
    "we-claude-terminal/go-server/internal/dal/model"
)

q := query.Use(global.SqliteDB)
// 建表后 id 自动回填；全部表物理删除（无 deleted_at）
if err := q.Workspace.WithContext(ctx).Create(&model.Workspace{Name: "个人", Slug: "personal"}); err != nil { ... }
ws, err := q.Workspace.WithContext(ctx).Where(q.Workspace.Slug.Eq("personal")).First()
```

> nullable 列（`parent_id`/`completed_at`/`start_date`/`target_date` 等）未开 `FieldNullable`，按零值表「未设置」（id 自增从 1 起，0 即未设置；时间用 `IsZero()`）。清空（如 issue 流转出 completed 时 `completed_at`）由 service 层显式 `Update("completed_at", nil)` 处理。

**依赖版本：** `gorm.io/gen` 生态目前锁定 `gorm v1.25.x`（不支持 gorm v1.31）。`go.mod` 固定 `gorm v1.25.12` + `gen v0.3.28` + `dbresolver v1.5.3`（v0.3.28 修复了 sqlite 下 `ScanType` 为空导致生成期 panic 的问题）。`is_*` 列映射为 `bool`、JSON 标签为小驼峰（全部表物理删除，无 deleted_at 列）。
