# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# Ocean Harness

监听本地 Claude Code 终端运行状态的桌面应用（Tauri）。用户文档（安装提示、SQLite 数据库位置）见 `README.md`。

## Workspace 布局（pnpm 多包）

```
ocean-harness-app/
├── package.json          # workspace 根：编排脚本（全部转发）+ lint 工具链
├── pnpm-workspace.yaml   # packages: ['packages/*'] + catalog（公共 TS 依赖版本 SSOT）
├── app/                  # Rust/Tauri 端（原 src-tauri，不进 pnpm workspace）
├── packages/
│   ├── web/              # 前端（workspace 包 "web"，vite 多页构建，产物 packages/web/dist）
│   └── docs/             # vitepress 文档项目占位（.gitkeep，后续嵌入 app）
├── server/               # Go sidecar（原 src-server，module ocean-harness/server）
├── docs/                 # 临时开发文档（agent_dev_*.md，非 workspace 包，与 packages/docs 无关）
└── scripts/              # 根编排辅助脚本（build-server.ts）
```

- `app/`、`server/`、`docs/` 游离于 pnpm workspace 之外（非 Node 包/纯文档），由根脚本 `go -C server` / `cargo --manifest-path app/Cargo.toml` 编排；后续 `cli/`、`bot/` 等新 Node 包落 `packages/` 下即被 `packages/*` glob 自动收编。
- 版本号 SSOT 在根 `package.json`，`pnpm release`（bumpp）同步 `app/tauri.conf.json` + `app/Cargo.{toml,lock}`；`web` 包 private 无版本。
- 前端依赖装在 `packages/web/package.json`，公共版本（typescript/vite/vitest/@types/node）走 `pnpm-workspace.yaml` 的 catalog（包内以 `catalog:` 引用）。

## 常用命令

```bash
pnpm setup                # 安装依赖 + go mod tidy（首次克隆后）
pnpm tauri:dev            # 开发运行（拉起 Go sidecar + Tauri 窗口）
pnpm tauri:build:local    # 本地发布构建（overlay 关 updater 签名故无需私钥；CI 走 tauri:build:ci）

# 前端
pnpm web:build            # tsc -b + vite build（类型检查入口）
pnpm web:lint             # eslint（自动修复：web:lint:fix）
pnpm web:test             # vitest run（当前无测试文件；跑单个：pnpm exec vitest run <file>）

# Go 旁路服务（server/）
pnpm server:dev           # air 热重载本地自测（端口 9200）
pnpm server:test          # 构建 dev 二进制供 tauri:dev 拉起（端口 9000）
pnpm server:release       # 构建发布二进制（端口 9100，CGO_ENABLED=0）
pnpm server:gorm:gen      # goose 迁移 + gorm/gen 重新生成 DO 层（改表后必跑）

# Rust ↔ 前端桥接
pnpm gen:bindings         # 从 Rust command 重新生成 packages/web/src/shared/bindings.ts
pnpm verify:bindings      # CI 校验 bindings.ts 与 Rust 无漂移

pnpm tauri:fmt            # cargo fmt；Go 格式化：pnpm server:fmt
```

## 架构总览

三端协作，职责严格分层：

```
React 前端（多窗口 webview）─ IPC ─ Rust（Tauri shell）─ spawn/HTTP ─ Go sidecar（业务 HTTP 服务）
```

### 前端（`packages/web/src/`）

- **多窗口入口**（vite 多页构建，各自独立 JS realm，QueryClient 缓存不共享）：`panel.html`（主控台，含系统设置页 `/settings`——隐藏菜单页，分区路由在 `SettingsPage/routes.ts`）、`pet-session-*.html`（悬浮窗）。路由在 `packages/web/src/windows/<window>/routes.ts`。
- **页面切走即卸载**：`PanelApp` 声明式路由，切菜单 = 整页卸载重建（状态放 store 不丢）。
- **状态管理**（`packages/web/src/state/`，约定见其 README.md）：一业务域一目录（store + keys + queries + index），server 状态用 TanStack Query、client 选中态用 zustand；跨窗口同步一律走后端 SSOT + Tauri 事件，前端不做。
- **Go API 客户端**：`packages/web/src/services/`；服务地址从 Rust `http_server_status` 命令获取，不硬编码端口。
- **配置体系**：key 常量与默认值的 SSOT 是 `packages/web/src/shared/appConfig.ts`。`useConfigValue` 订阅单 key（**初值为同步默认值、真实值异步回填**）；挂载期消费度量/编排类配置的组件用 `useConfigReady` 闸门等就绪（见「编码规则 1」）。
- **Tauri 桥**：所有 IPC command 类型在 `packages/web/src/shared/bindings.ts`（生成物，勿手改）——**改 Rust command 后必须 `pnpm gen:bindings`**，否则 CI `verify:bindings` 漂移失败。生成流程末尾剥离生成器注入的块注释（`scripts/strip-bindings-comments.mjs`），生成物除文件头外零注释（Rust 源码不使用文档注释，见编码规则 3）。

### 终端链路（本项目核心，跨 React ↔ Rust）

- 前端：`DevWorkbenchPage/components/EmbeddedTerminal/` 下 `EmbeddedTerminal`（配置组装/会话锚点 `${issueId}::${paneId}`）→ `TerminalView`（xterm.js 唯一封装，FitAddon/WebGL）→ `usePtySession`（attach 编排：exists → reattach / spawn）。
- Rust：`app/src/pty/`（`session.rs` 会话 + ring buffer，`local_provider.rs` spawn/resize）。**会话生命周期在后端常驻**：前端卸载只断订阅，重挂载走 reattach 并一次性回放 scrollback。
- 终端分屏：`TerminalPanes/`（布局二叉树持久化于 `packages/web/src/state/terminalPanes/`）。
- 固定编排时序（勿破坏）：配置就绪闸门 → TerminalView mount fit 实测尺寸 → 以实测尺寸 spawn → attach 后仅在尺寸不一致时校正一次。**先测量后生胎，禁止「占位 spawn → 事后补发纠正」**——每次事后纠正都是一次打在已绘制提示符上的 SIGWINCH 重绘伪影。

### Rust 侧其余模块（`app/src/`）

`sessions/`（Claude Code 会话发现/轮询/解析）、`terminal/`（iTerm2/Terminal.app 外部跳转）、`windows/`（各窗口创建 + 托盘）、`shared/http_server.rs`（Go sidecar 进程管理与端口注入，`OCEAN_HARNESS_PORT` 环境变量供 MCP 使用）。

### Go 旁路服务（`server/`）

Gin + GORM + sqlite（纯 Go，无 CGO），分层规范/API 范式/配置优先级见 `server/README.md`（tracker 域接口范式是后续模块基线）。改表流程：改 `internal/migrations/migrations/*.sql` → `pnpm server:gorm:gen`（迁移 + 重新生成 DO 一气呵成）。内嵌 MCP Server（`/mcp/streamableHttp/oceanHarness`）供 Claude CLI Skill 读写 issue/子任务/工作空间。

## 编码规则

### 1. 执行时序固定优先（杜绝「先临时值、后纠正」）

异步数据（配置、查询结果）**就绪之后**才挂载消费组件、才发起依赖它的副作用编排。禁止「先用默认值渲染/生胎，数据到达后再纠正」——每次纠正都是一次副作用重放。

- 消费异步配置的组件挂载前，用就绪闸门（`packages/web/src/shared/useConfigReady.ts`）等待相关 key 读取完成，**首帧即终值**。
- 副作用编排按固定时序逐项处理（参照 `usePtySession.ts`：闸门 → fit 实测 → 以实测尺寸 spawn → 收尾校正一次），出问题时直接定位到时序中的某一步。**不引入防抖/节流这类时间性手段掩盖时序缺陷**。
- 尺寸/度量类状态遵循「先测量后使用」。

### 2. 函数式/过程式优先

代码逻辑能用函数式、面向过程的方式实现，就不要引入面向对象的类与有状态封装（class、可变实例字段）。仅在确有收益（多态、框架约束等）时使用类。副作用编排用显式参数传递的纯函数 + 明确的生命周期时序表达，而非散落在对象内部状态里。

### 3. 注释要求

所有注释行数 ≤3：常规一行，内容确多两行，特别注意点放第三行补充。注释仅描述功能，极简风：不与代码中的变量名、方法名、属性名等实现细节绑定，不写过程性叙述（演变史归 git）；只写「这是什么/有什么约束」。

**Web（TS/TSX）**

- 单行：

  ```
  // 单行注释
  ```

- 多行：

  ```
  /**
   * 多行注释
   */
  ```

- JSX 内部同规格：

  ```jsx
  {/* 单行注释 */}

  {/*
  多行注释
  */}
  ```

**Rust**

- 单行：`//`（文档注释 `///`，inner `//!`）
- 多行一律普通块（不区分文档/普通注释，应用型 crate 不发布 rustdoc）：

  ```
  /*
   * 多行注释
   */
  ```

- 生成物豁免（`bindings.ts`等生成代码不改）。

**Go**

- 单行：`//`
- 多行（函数文档注释同格式）：

  ```
  /*
   * 多行注释
   */
  ```

- 超过 3 行的存量注释按语义压缩内容收敛，不机械截断。
- 生成物豁免（`gorm/gen` DO 层等生成代码不改）。

### 4. UI 尺寸一律显式 px 字符串

sx 中所有能明确 px 的地方一律写 `'14px'` 字符串，**禁止写数字**。

- 根因：MUI sx 的 padding/margin 系属性传数字是 theme.spacing 倍数（×8px），`paddingBottom: 10` 实渲染 80px；统一字符串后歧义归零。
- 适用范围：MUI sx 及一切内联样式对象（`style={{...}}` 同样禁裸数字 px）。
- theme 倍乘属性一律保渲染换算：spacing 系（padding/margin/gap 及 p/m 简写）数字是 ×8 倍乘、borderRadius 数字是 ×shape.borderRadius（当前主题 4）倍乘，均按实渲染值换算为显式 `'Npx'` 字符串（零渲染变化）；本项目无响应式 spacing 需求，禁止保留倍数写法，仅确需跟随主题缩放的特殊场景可例外并注明理由。
- 例外：库 API 的数值参数（类型即 number、单位即 px，如 virtua `startMargin: 12`）照传数字；无单位属性（`flex`/`zIndex`/`fontWeight`/`opacity`/`lineHeight` 等）与百分比/`calc()` 值不属「明确 px」，不在约束内。
- px 语义属性的数值 0 统一写 `'0px'`。
- fontSize 最小 12、禁小数渲染值，不依赖 MUI 变体默认字号（主题 12/14 系数使变体实渲染全为小数，caption ≈10.29px）；变体仅用于行高/语义。
- 存量数字写法不主动批量翻新，改到哪里就地把该处收敛为字符串 px。
