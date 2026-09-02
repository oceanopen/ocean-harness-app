# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# Ocean Harness

监听本地 Claude Code 终端运行状态的桌面应用（Tauri）。用户文档（安装提示、SQLite 数据库位置）见 `README.md`。

## 常用命令

```bash
pnpm setup                # 安装依赖 + go mod tidy（首次克隆后）
pnpm tauri:dev            # 开发运行（拉起 Go sidecar + Tauri 窗口）

# 前端
pnpm web:build            # tsc -b + vite build（类型检查入口）
pnpm web:lint             # eslint（自动修复：web:lint:fix）
pnpm web:test             # vitest run（当前无测试文件；跑单个：pnpm exec vitest run <file>）

# Go 旁路服务（src-server/）
pnpm server:dev           # air 热重载本地自测（端口 9200）
pnpm server:test          # 构建 dev 二进制供 tauri:dev 拉起（端口 9000）
pnpm server:release       # 构建发布二进制（端口 9100，CGO_ENABLED=0）
pnpm server:gorm:gen      # goose 迁移 + gorm/gen 重新生成 DO 层（改表后必跑）

# Rust ↔ 前端桥接
pnpm gen:bindings         # 从 Rust command 重新生成 src/shared/bindings.ts
pnpm verify:bindings      # CI 校验 bindings.ts 与 Rust 无漂移

pnpm tauri:fmt            # cargo fmt；Go 格式化：pnpm server:fmt
```

## 架构总览

三端协作，职责严格分层：

```
React 前端（多窗口 webview）─ IPC ─ Rust（Tauri shell）─ spawn/HTTP ─ Go sidecar（业务 HTTP 服务）
```

### 前端（`src/`）

- **多窗口入口**（vite 多页构建，各自独立 JS realm，QueryClient 缓存不共享）：`panel.html`（主控台）、`settings.html`、`pet-claude-sessions-*.html`（悬浮窗）。路由在 `src/windows/<window>/routes.ts`。
- **页面切走即卸载**：`PanelApp` 声明式路由，切菜单 = 整页卸载重建（状态放 store 不丢）。
- **状态管理**（`src/state/`，约定见其 README.md）：一业务域一目录（store + keys + queries + index），server 状态用 TanStack Query、client 选中态用 zustand；跨窗口同步一律走后端 SSOT + Tauri 事件，前端不做。
- **Go API 客户端**：`src/services/`；服务地址从 Rust `http_server_status` 命令获取，不硬编码端口。
- **配置体系**：key 常量与默认值的 SSOT 是 `src/shared/appConfig.ts`。`useConfigValue` 订阅单 key（**初值为同步默认值、真实值异步回填**）；挂载期消费度量/编排类配置的组件用 `useConfigReady` 闸门等就绪（见「编码规则 1」）。
- **Tauri 桥**：所有 IPC command 类型在 `src/shared/bindings.ts`（生成物，勿手改）——**改 Rust command 后必须 `pnpm gen:bindings`**，否则 CI `verify:bindings` 漂移失败。

### 终端链路（本项目核心，跨 React ↔ Rust）

- 前端：`DevWorkbenchPage/components/EmbeddedTerminal/` 下 `EmbeddedTerminal`（配置组装/会话锚点 `${issueId}::${paneId}`）→ `TerminalView`（xterm.js 唯一封装，FitAddon/WebGL）→ `usePtySession`（attach 编排：exists → reattach / spawn）。
- Rust：`src-tauri/src/pty/`（`session.rs` 会话 + ring buffer，`local_provider.rs` spawn/resize）。**会话生命周期在后端常驻**：前端卸载只断订阅，重挂载走 reattach 并一次性回放 scrollback。
- 终端分屏：`TerminalPanes/`（布局二叉树持久化于 `src/state/terminalPanes/`）。
- 固定编排时序（勿破坏）：配置就绪闸门 → TerminalView mount fit 实测尺寸 → 以实测尺寸 spawn → attach 后仅在尺寸不一致时校正一次。**先测量后生胎，禁止「占位 spawn → 事后补发纠正」**——每次事后纠正都是一次打在已绘制提示符上的 SIGWINCH 重绘伪影。

### Rust 侧其余模块（`src-tauri/src/`）

`sessions/`（Claude Code 会话发现/轮询/解析）、`terminal/`（iTerm2/Terminal.app 外部跳转）、`windows/`（各窗口创建 + 托盘）、`shared/http_server.rs`（Go sidecar 进程管理与端口注入，`OCEAN_HARNESS_PORT` 环境变量供 MCP 使用）。

### Go 旁路服务（`src-server/`）

Gin + GORM + sqlite（纯 Go，无 CGO），分层规范/API 范式/配置优先级见 `src-server/README.md`（tracker 域接口范式是后续模块基线）。改表流程：改 `internal/migrations/migrations/*.sql` → `pnpm server:gorm:gen`（迁移 + 重新生成 DO 一气呵成）。内嵌 MCP Server（`/mcp/streamableHttp/oceanHarness`）供 Claude CLI Skill 读写 issue/子任务/工作空间。

### 任务文档（`docs/`）

`agent_dev_00_overview.md`（Agent 驱动开发流程技术方案）+ `agent_dev_01_tasks.md`（模块任务清单，进度 SSOT）。**状态回写规则（内置）**：执行任务清单中的任务并实现完成后，总结阶段直接把对应任务 `**状态**` 改为 ✅ 并补带日期的「实施定稿」段落（记录方案变更/偏离），不等用户指示。

## 编码规则

### 1. 执行时序固定优先（杜绝「先临时值、后纠正」）

异步数据（配置、查询结果）**就绪之后**才挂载消费组件、才发起依赖它的副作用编排。禁止「先用默认值渲染/生胎，数据到达后再纠正」——每次纠正都是一次副作用重放。

- 消费异步配置的组件挂载前，用就绪闸门（`src/shared/useConfigReady.ts`）等待相关 key 读取完成，**首帧即终值**。
- 副作用编排按固定时序逐项处理（参照 `usePtySession.ts`：闸门 → fit 实测 → 以实测尺寸 spawn → 收尾校正一次），出问题时直接定位到时序中的某一步。**不引入防抖/节流这类时间性手段掩盖时序缺陷**。
- 尺寸/度量类状态遵循「先测量后使用」。

### 2. 函数式/过程式优先

代码逻辑能用函数式、面向过程的方式实现，就不要引入面向对象的类与有状态封装（class、可变实例字段）。仅在确有收益（多态、框架约束等）时使用类。副作用编排用显式参数传递的纯函数 + 明确的生命周期时序表达，而非散落在对象内部状态里。
