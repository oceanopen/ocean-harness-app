# 嵌入式终端（issue 任务目录直连）

> 前身：worktree 锚点版设计（`docs/dev_terminal.md`，随 worktree 概念移除已废弃并删除，需要时从 git 历史 `62ee7d4^` 取回）。
>
> **本模块定位**：开发工作台（DevWorkbenchPage）右侧内容区的嵌入式终端。选中 issue 即进入该 issue 的专属终端，会话常驻抗前端刷新。
> **锚点**：issue uuid（`t_project_issues.id`，TEXT uuid v7，同 claude session_id 格式）。参考项目 orca 锚点 worktreeId（`${repoId}::${path}`），本项目锚点更简单——**每 issue 恰一个终端**。
> **cwd**：`工作空间根目录 / <issue-uuid>`。根目录来自 `workspace_base_dir` 配置（appConfig），目录派生见 §3.2。
> **范围（本期）**：仅终端嵌入——Rust PTY + xterm 组件 + 会话常驻/reattach。**不含**：终端内自动执行 `claude`（独立后续模块，另立文档）、目录/仓库准备（skills 集成）、PR/清理编排。
> **交互与终端方案**：与 orca 保持一致的体验（多标签不需要——单 issue 单终端；抗刷新常驻、scrollback 重载、resize、backpressure 均对齐）。

---

## 1. 模块概览

**要解决什么**：用户在开发工作台选中 issue 后，需要切外部终端（`openInTerminal` 打开 iTerm2）进入任务目录，需切出应用。引入应用内 xterm 终端直达 `工作空间根目录/<issue-uuid>`，shell 常驻抗刷新。

**接入点**：`DevWorkbenchPage.tsx:158-183` 内容区空态（「AI 驱动开发流程即将上线」占位）→ 替换为嵌入式终端。左栏选中 issue 后右侧即展示该 issue 的终端。

**涉及端**：Rust（PTY）、Web（xterm）。

**非目标**：自动执行 claude（后续模块）；远程终端/SSH relay（仅预留 `PtyProvider`）；daemon 常驻（关 app 终端即断）；多标签/split pane；目录不存在时自动创建/git 准备（skills 层做，本模块仅报错提示）；per-issue shell history 持久化。

---

## 2. 现状基线

### 2.1 已有
| 能力 | 位置 |
|---|---|
| 工作台骨架 + 左栏任务树 | `DevWorkbenchPage/`（选中 issue、uuid 尾 8 位展示均已就位） |
| issue uuid 主键 | `t_project_issues.id` TEXT uuid；前端 `ProjectIssueService.ts:15` `id: string // uuid 字符串` |
| 工作空间根目录配置 | `appConfig.ts:91` `WORKSPACE_BASE_DIR_KEY = 'workspace_base_dir'`（ProjectConfigPage 可配置，空串=未设置） |
| State 范式 | `shared/state/claude_sessions.rs`（`Mutex<HashMap>` + `Default` + `init(app)`） |
| 子进程退出回收范式 | `shared/http_server.rs`（`RunEvent::Exit` → shutdown：SIGTERM→kill 兜底 + `aborted` 标志防退出竞态孤儿） |
| 命令注册 SSOT | `lib.rs:12 build_specta_builder`（`collect_commands!` + `.typ::<T>()`） |
| 事件双份维护 | `shared/events.rs` ↔ `src/shared/events.ts`（SSOT 注释明确要求同步） |
| bindings 生成 | `cli/export_bindings.rs` → `pnpm gen:bindings` → `src/shared/bindings.ts` |
| 外部终端模块 | `src-tauri/src/terminal/`（AppleScript 打开 iTerm2/Terminal.app，**外部**，与本模块并存不冲突） |

### 2.2 缺口
| 缺口 | 端 |
|---|---|
| `portable-pty` 接入 + `PtyProvider` trait + `LocalPtyProvider` | Rust |
| PTY 命令（spawn/write/resize/shutdown/exists/reattach）+ 会话存储 | Rust |
| 每会话 ring buffer + reattach（刷新重载 scrollback） | Rust |
| 输出通道：Channel（首选，需 spike specta）或 emit（备选） | Rust→Web |
| xterm 组件（TerminalView + usePtySession）+ 双向流 | Web |
| 工作台内容区接入 + app 退出回收 | Rust+Web |

---

## 3. 设计

### 3.1 `PtyProvider` 接口（为远程终端预留）

```rust
trait PtyProvider: Send + Sync {
    fn spawn(&self, opts: SpawnOpts) -> Result<String, String>; // 返回 sessionId
    fn write(&self, id: &str, data: &[u8]) -> Result<(), String>;
    fn resize(&self, id: &str, cols: u16, rows: u16) -> Result<(), String>;
    fn shutdown(&self, id: &str) -> Result<(), String>;
    fn list(&self) -> Vec<PtySessionInfo>;
    // 输出/退出通过 Channel/emit 回传（§3.5）
}
```

对比旧版：删除 `stop_for_worktree`（worktree 联动已移除；issue 删除时的终端清理由 `shutdown` 显式调用即可，见 §5.4）。本期只实现 `LocalPtyProvider`。

### 3.2 锚点与目录派生（替代 orca 的 worktreeId）

- **会话标识**：`sessionId = issueId`（uuid）。一 issue 一终端，`PtySessionStore: Mutex<HashMap<String /* issueId */, PtySession>>`。不需要 orca 的 `@@`/`::` 复合分隔符（无 tab×worktree 多对多）。
- **cwd 派生**（前端做，spawn 时传绝对路径）：
  ```
  dir = `${workspace_base_dir}/${issueId}`
  ```
  `workspace_base_dir` 空串（未配置）→ 前端拦截：不 spawn，展示「请先在设置 → 项目配置中设置工作空间根目录」提示 + 跳设置按钮。
- **目录存在性**：本模块**不创建目录**。cwd 不存在时 portable-pty spawn 会失败 → 前端捕获错误展示「任务目录不存在：<路径>」+「在外部终端打开父目录」按钮（复用 `open_in_terminal`）。目录创建/git 准备属 skills 集成，后续模块做。

### 3.3 Rust 模块结构

```
src-tauri/src/pty/
  mod.rs            // 命令 + dispatch + shutdown_all
  provider.rs       // PtyProvider trait
  local_provider.rs // LocalPtyProvider（portable-pty）
  session.rs        // PtySession（id/issueId/cwd/handle/ring buffer/exit flag）
  state.rs          // PtySessionStore: Mutex<HashMap<issueId, PtySession>> + init(app)
```

命令（注册进 `lib.rs` `collect_commands!`）：

| 命令 | 签名 | 说明 |
|---|---|---|
| `pty_spawn` | `(opts: PtySpawnOpts) -> Result<PtySpawned, String>` | opts 含 issueId/cwd/cols/rows；已存在同 issueId 会话则直接返回现有（幂等） |
| `pty_write` | `(issueId: String, data: String) -> Result<(), String>` | 键盘输入 |
| `pty_resize` | `(issueId: String, cols: u16, rows: u16) -> Result<(), String>` | |
| `pty_shutdown` | `(issueId: String) -> Result<(), String>` | 关闭单个终端 |
| `pty_exists` | `(issueId: String) -> bool` | 挂载时探测 |
| `pty_reattach` | `(issueId: String, ...) -> Result<Option<PtyReattached>, String>` | 重放 ring buffer；不存在返回 None（前端转 spawn） |
| `pty_list_sessions` | `() -> Vec<PtySessionInfo>` | 调试/后续状态栏用 |

类型约束（tauri-specta 实证，见旧文档 §5.6）：计数 u32、时间戳 i64 需 `#[specta(type = Number)]`、cols/rows u16、入参 struct 带 `#[derive(Clone,Serialize,Deserialize,Type)]` + `#[serde(rename_all="camelCase")]`、纯 payload 类型 `.typ::<T>()` 注册。

### 3.4 会话生命周期

```
spawn → 存 store → 读线程（try_clone reader）→ ring buffer + 推前端
webview 刷新（Rust 不死）→ 前端挂载：pty_exists → pty_reattach（重放 ring）→ 接实时流
issue 关闭终端 → pty_shutdown → kill shell → 移出 store
issue 删除 → 前端调 pty_shutdown（§5.4）
app 退出 → RunEvent::Exit → pty::shutdown_all（仿 http_server：SIGTERM→kill 兜底）
```

**幂等 spawn**：同一 issueId 重复 spawn（双击、快速切换）直接返回现有会话信息，不重复起 shell。

### 3.5 输出通道：Channel（首选）或 emit（备选）

- **首选 `Channel<PtyData>`**：`pty_spawn`/`pty_reattach` 由前端传 Channel，Rust `channel.send()` 定向流式。项目零 Channel 先例，tauri-specta 对 `Channel<T>` 导出**需任务 1.2 先 spike**，不行回退 emit。
- **备选 emit**：`EVENT_PTY_DATA` / `EVENT_PTY_EXIT` 追加进 `events.rs` ↔ `events.ts`（双份维护），payload 带 issueId，前端按 id 过滤。

### 3.6 ring buffer + reattach（刷新重载 scrollback）

- 每 session 有界环形缓冲（默认 **256 KB**，可调常量），输出先入 ring 再推 listener。
- `pty_reattach`：一次性 replay ring 全量 → 切实时流。全屏 TUI（vim 等）原始字节 replay 可能轻微错位，TUI 自重绘，可接受（与 orca 行为一致）。
- 前端挂载顺序固定：`pty_exists` → 存在则 `pty_reattach`，不存在才 `pty_spawn`。

### 3.7 数据流

```
[键盘] xterm.onData → pty_write → PtySessionStore → portable_pty handle.write
[输出] reader 线程 → 入 ring → Channel.send / emit → usePtySession → xterm.write
[尺寸] xterm.onResize（addon-fit）→ pty_resize
[退出] shell 退出 → 置 exit flag + 通知前端（Channel/emit）→ 前端展示「会话已结束」+ 重开按钮
```

### 3.8 前端组件

```
src/windows/panel/DevWorkbenchPage/components/EmbeddedTerminal/
  EmbeddedTerminal.tsx          // 容器：读 workspace_base_dir、派生 cwd、空态/错误态处理、writeData ref 桥
  TerminalView.tsx              // xterm 封装（onData/onResize/输出桥上抛；import '@xterm/xterm/css/xterm.css'）
  TerminalErrorBoundary.tsx     // 终端区错误边界（xterm 崩溃降级为卡片，不白屏整页）
  usePtySession.ts              // 会话 hook：exists→reattach→spawn 顺序编排、Channel 订阅、尺寸补发
```

依赖：`@xterm/xterm@6`、`@xterm/addon-fit`、`@xterm/addon-webgl`（渲染失败自动回退 dom）。

组件规范遵循项目约定：目录 PascalCase、密集 UI 不挂 Tooltip（用 aria-label）、报错/提示硬编码中文、按钮文案不需要 i18n（非菜单/路由）。

### 3.9 前端范式约定（函数式 / 面向过程，强制）

> 本节为终端嵌入模块的**前端强制规范**，后续终端相关改动（含自动执行 claude 模块）一律遵守。来源：2026-08-17 调试实战教训。

**核心原则：事件处理一律函数式、显式顺序；不做响应式 ref 转发层。**

1. **回调链直传，不做 ref 代理**：`terminal.onData(onData)` 直接用 props/参数——父层负责传**稳定引用**（`useCallback`，deps 收敛到最小）。禁止「onDataRef.current = onData 每渲染同步」这类 ref 转发层（响应式味道，时序不透明）。唯一例外见第 3 条。
2. **mount effect 按显式顺序建齐，cleanup 严格逆序**：建实例 → loadAddon → open → 事件接线 → 桥上抛 → focus → observer → 初始 fit；卸载反向逐个拆除。禁止依赖「React 某时刻会帮我同步」的隐式时序。
3. **Channel 数据桥必须是 ref**（唯一例外）：PTY 输出可在任何时刻到达（含 StrictMode 双挂载窗口、attach 完成前），`writeDataRef.current` 直读直写、**绝不经 React state**——state 桥在 React 19 StrictMode 下实测出现「fn→null→fn 连续 setState 后回调闭包仍读到 null」，输出全丢（黑屏假死）。教训：**外部推送型数据源 → ref；用户交互型数据流 → state。**
4. **effect 内同步 setState 仅限状态机重置**（connecting 重入），其余状态变更由异步回调（attach.then/catch、Channel onEvent）驱动。
5. **错误处理直白化**：`unwrap(...).catch(warn)` 一处一责；可预期失败降级 `console.debug`（如 attach 未就绪期间的 resize not found），真错误 `console.warn/info` 带 issueId。
6. **诊断日志规范**（长期保留，排障生命线）：
   - 前端：`[pty] attach failed / session exit / write|resize|shutdown failed`（warn）、`resize skipped (session not ready)`（debug）；逐块数据/逐键输入**不打**（噪声）。
   - Rust（dev 日志）：`spawn ok|failed / reattach / shutdown / shutdown_all` 各一行；逐 write 不打。
   - 约定前缀 `[pty]`，grep 友好。

---

## 4. 任务清单

> 按序执行；每个任务独立实现 + 验证。
>
> 状态图例：✅ 已完成 · 🔄 进行中 · ⬜ 待办

### ✅ 任务 1 — portable-pty 接入 + PtyProvider 骨架
- **文件**：`src-tauri/Cargo.toml`（加 `portable-pty = "0.8"`，**不引 tokio/anyhow**）+ `src-tauri/src/pty/{provider,local_provider,session,state}.rs`
- **目标**：`PtyProvider` trait + `LocalPtyProvider`；`PtySession`（id/cwd/handle/ring 占位）；`PtySessionStore` 照 `ClaudeSessionStore` 范式（`Mutex<HashMap>` + Default + `init(app)`）。
- **验证**：编译通过；临时目录 spawn shell 不崩。

### ✅ 任务 2 — PTY 命令 + 输出通道 spike + 退出回收
- **文件**：`pty/mod.rs`（§3.3 七命令）+ `lib.rs`（注册命令、`.typ::<T>()` 注册 payload 类型、`RunEvent::Exit:158` 加 `pty::shutdown_all`）
- **目标**：spike `Channel<PtyData>` 过 tauri-specta；不行回退 emit（`events.rs`/`events.ts` 双份追加 `EVENT_PTY_DATA`/`EVENT_PTY_EXIT`）。`pty_spawn` 幂等。
- **验证**：`pnpm gen:bindings` 无 panic、bindings.ts 含新命令；临时目录 spawn shell 打字有回显、resize 生效；退出 app 无孤儿 shell 进程。
- **落地记录**：Channel spike 通过（tauri-specta rc.25 原生支持参数 `Channel<T>`，含 `mapChannel` 反序列化注入），emit 备选未采用。输出/退出走单 `Channel<PtyEvent>`（`{kind:"data",data}`/`{kind:"exit"}`，UTF-8 边界切分见 session.rs `Utf8Tail`）；命令注册 5 个（spawn/write/resize/shutdown/list_sessions），exists/reattach 留任务 3；`RunEvent::Exit` 已挂 `pty::shutdown_all`。

### ✅ 任务 3 — ring buffer + reattach
- **文件**：`pty/session.rs`（环形缓冲）+ `pty/mod.rs`（`pty_exists`/`pty_reattach`）
- **目标**：输出先入有界 ring 再推 listener；reattach replay 后切实时流。
- **验证**：spawn → 刷新前端 → scrollback 重载后接实时流；`find /` 高频输出不撑爆（backpressure：ring 有界覆盖旧数据）。
- **落地记录**：ring = `VecDeque<String>`（存 Utf8Tail 切分后完整块，256KB 有界、超限队首丢整块保证 replay 合法 UTF-8）；ring 与 listener 合并一把锁（`push_and_emit` 入 ring→推流原子序，reattach 快照+换装同临界区无缝续流）；`PtyReattached{issueId,exited,scrollback}` scrollback 随命令返回值一次性送达；已退出会话 reattach 返回 exited=true+退出前 scrollback（Exit 事件不重发，靠 exited 字段）。前端真实刷新场景验证留任务 4 组件接入后。

### ✅ 任务 4 — xterm 组件 + 工作台接入
- **文件**：`package.json`（xterm 三依赖）+ `EmbeddedTerminal/{EmbeddedTerminal,TerminalView,usePtySession}.tsx`
- **目标**：替换 `DevWorkbenchPage.tsx:158-183` 空态为终端；`workspace_base_dir` 未配置/目录不存在两个错误态（§3.2）；切换 issue 时旧终端 unmount 不销毁会话（仅断订阅），回切 reattach。
- **验证**：选中 issue 开终端、双向流、resize 正常；切走再切回 scrollback 还在；F5 刷新 scrollback 重载；关闭终端按钮干净退出。
- **落地记录**：三组件齐（EmbeddedTerminal 容器/TerminalView xterm 封装/usePtySession 编排 hook，目录 PascalCase 文件 camelCase）；webgl 渲染器 + 失败自动回退 dom；工具栏「关闭终端」+ exited 态「重开」；`useConfigValue` 读 base_dir 派生 cwd；目录不存在错误态仅提示+重试（外部终端入口按用户决策砍掉）；DevWorkbenchPage 以 `key={issue.id}` 挂载（切换即重挂载，unmount 仅断订阅）。tsc/eslint(0 error)/web:build 通过；dev 真机交互验证由用户后续手动执行（2026-08-17 跳过）。

### ⬜ 任务 5 — issue 删除联动（可选，小）
- **文件**：issue 删除调用链（前端 tracker queries → 先 `pty_shutdown(issueId)` 再走删除接口）
- **目标**：删 issue 时同步关其终端，避免孤儿会话占 store。
- **验证**：删除 issue 后 `pty_list_sessions` 无该 id。

---

## 5. 工程约束

### 5.1 输出 backpressure
高频输出（`find /`）撑爆前端：ring 有界（覆盖旧数据）+ 前端 xterm webgl addon 渲染。Rust 侧 reader 线程读满时阻塞在 ring 写入（丢旧保新），不无限堆内存。

### 5.2 会话退出通知
shell 正常退出（`exit`/Ctrl-D）或被 kill：置 exit flag、推 `PtyExit` 通知前端，**会话保留在 store**（用户可重开；重开 = 移除旧会话重新 spawn）。避免「shell 退出 → store 立即清 → 前端误判刷新场景 reattach 失败又 spawn 新 shell」的歧义。

### 5.3 tauri-specta 类型约束（项目实证）
计数/长度用 `u32`/`i32`（禁 usize/u64/isize/i64）；时间戳 i64 字段加 `#[specta(type = Number)]`；cols/rows 用 u16；入参 struct 需 `#[derive(Clone,Serialize,Deserialize,Type)]` + `#[serde(rename_all="camelCase")]`；仅事件 payload 的类型在 `lib.rs:44-49` 区用 `.typ::<T>()` 显式注册。

### 5.4 与 issue 删除的联动
issue 删除（物理删除）后其终端会话若存活：前端删除流程先 `pty_shutdown`。不做级联监听（issue 删除入口收敛在 tracker queries，一处接线足够）。

### 5.5 刷新续接边界
scrollback 有界（256 KB）；**整 app 退出终端即断**（非 daemon）；目录不存在不自动创建（skills 层职责）。

### 5.6 orca → 本项目对照（附录）

| orca（Electron） | 本项目（Tauri） |
|---|---|
| `node-pty` spawn，锚点 worktreeId `${repoId}::${path}` | Rust `portable-pty`，锚点 issueId（uuid，无需复合分隔符） |
| `ipcMain.handle` + `webContents.send('pty:data')` | `#[tauri::command]` + `Channel<PtyData>`（或 emit） |
| xterm.js renderer + tab/leaf 多级会话映射 | xterm.js，一 issue 一终端，无 tab 映射 |
| daemon 常驻 + 冷恢复（checkpoint 文件） | Rust `tauri::State` 抗 webview 刷新 + ring replay（**不做 daemon**） |
| `StartupCommandDelivery: 'fast' \| 'shell-ready'` | 本期不注入命令；自动执行 claude 属后续独立模块（届时引入 shell-ready marker） |
| worktree 目录为 cwd | `工作空间根目录/<issue-uuid>` 为 cwd |

**远程终端扩展预留**：`PtyProvider` trait + `SpawnOpts` 抽象保留；将来加 SSH provider 时命令层与前端组件不变。

### 5.7 真机调试实录与坑位档案（2026-08-17）

任务 4 真机接入时连续踩坑，以下按「现象 → 根因 → 修复」归档（全部实证，勿凭直觉回退）：

| # | 现象 | 根因 | 修复（保留） |
|---|---|---|---|
| 1 | 点击任务白屏，`TypeError e.length`（xterm.write 内部） | 响应式 state 桥：StrictMode 双挂载下 `setWriteData(fn→null→fn)` 后闭包仍读到 null → `terminal.write(null)` | writeData 改 **ref 桥**（§3.9 第 3 条）；另加 write 非字符串守卫 + TerminalErrorBoundary 兜底 |
| 2 | shell 起在家目录（cwd 错） | portable-pty 对不存在 cwd **静默回退**父进程 cwd，不报错 | Rust spawn 前显式 `Path::is_dir` 预检 → Err「任务目录不存在：<路径>」 |
| 3 | 黑屏无 prompt（数据链通） | StrictMode 双 spawn：第二遍复用会话不回放 ring，早期输出随第一遍已死 listener 丢失 | spawn 复用分支走 `reattach`（快照+换装同临界区）；`PtySpawned` 增 `scrollback` 字段；前端 `fresh=false` 时回放 |
| 4 | `Couldn't find callback id` 刷屏 + 无输出 | 并发 spawn 败者只 kill 自己 shell，未把现有会话 listener 换成存活前端的 Channel | 败者路径 `existing.io.set_listener(listener)` |
| 5 | 键盘无反应 | xterm 6 `open()` 不自动聚焦 | mount 即 `terminal.focus()` + 容器 mousedown 兜底 |
| 6 | resize `not found` ×2（挂载期） | fit 上报先于 spawn 完成，良性竞态 | attach 就绪后按积压尺寸补发一次（`pendingSizeRef`） |
| 7 | （已昭雪）webgl 渲染器曾疑似致渲染循环死 | 真凶是 #1 的 state 桥，webgl 无罪；恢复后真机验证正常 | 已恢复 webgl + 失败自动回退 dom（try/catch）；若再出现渲染异常先查 #1 同款桥问题 |

**排障方法论**（下次少走弯路）：
- 先分层数定位：`ps + lsof`（进程树/cwd）→ Rust dev 日志（命令进出）→ 前端 console（事件链）——本轮 80% 的时间耗在跳层猜测上
- `[pty]` 前缀日志（§3.9 第 6 条）是分层定位的基础设施，保留勿删
- `Couldn't find callback id` 警告 = Tauri 官方提示 reload 期间旧异步操作推已销毁回调，**无害**

---

## 6. 后续模块（不在本文档范围）

- **终端自动执行 claude**：spawn 后在 shell ready 时自动执行 `claude`（shell-ready marker，参考 orca `StartupCommandDelivery`）。依赖本模块的 `pty_spawn`/`pty_write`。**另立文档规划。**
- **skills 集成**：任务目录创建、仓库准备、claude 会话与 issue 的关联等。
