# 模块 2：嵌入式终端（开发流程增强）

> 配套：[`docs/worktree_lifecycle.md`](worktree_lifecycle.md)（模块 1 worktree 生命周期，**本模块依赖其先做通**）。
>
> **本模块定位**：把「嵌入式终端」作为开发阶段（D2 developing）里与「外部终端/编辑器」并列的另一种开发方式，替换 `DevelopingStep.tsx` 的「嵌入式终端（P2）」占位框。
> **范围**：应用内 xterm 终端直达 worktree 目录，shell 常驻抗前端刷新（非 daemon）。本期仅本地；远程终端（SSH relay）预留 `PtyProvider` 接口。
> **现状基线**：DevelopingStep 占位框 + Rust `pty_stop_for_worktree` 桩已存在。本模块任务是「P1 占位 → P2 真实现」。

---

## 1. 模块概览

**要解决什么**：模块 1 创建 worktree 后，用户要切外部终端（`openInTerminal` 打开 iTerm2）开发，需切出应用。引入应用内 xterm 直达 worktree 目录，shell 常驻抗刷新。

**4 步状态机中的位置**：D2 developing（与模块 1 共用状态机，见 worktree_lifecycle.md §1）。本模块只增强 D2 的终端形态，不改状态推进。

**先后顺序**：模块 1（worktree 生命周期）先做通 → 本模块后补。靠 `worktreeId` 绑定 PTY、`pty_stop_for_worktree` 真实现联动模块 1 D4 清理。

**涉及端**：Rust（PTY）、Web（xterm）。

**非目标**：远程终端/SSH relay（仅预留 `PtyProvider`）；daemon 常驻（关 app 也断）；per-worktree shell history；pane split、多 agent 编排。

---

## 2. 现状基线（P1 已有 / P2 缺口）

### 2.1 P1 已有
| 能力 | 位置 |
|---|---|
| D2 占位框 | `DevelopingStep.tsx:54-65`（「嵌入式终端(P2)」Box + 外部打开按钮 + `[开发完成]`） |
| PTY 停止桩 | `src-tauri/src/pty/mod.rs pty_stop_for_worktree`（恒返 0，返回 u32） |
| 命令注册 SSOT | `src-tauri/src/lib.rs:13 build_specta_builder`（`pty_stop_for_worktree` 已在 `:42`） |
| State 范式 | `shared/state/claude_sessions.rs`（最简）/ `shared/http_server.rs`（子进程生命周期回收） |
| 事件双份维护 | `shared/events.rs` ↔ `src/shared/events.ts`（const &str 不被 specta 导出，手维护两份） |
| bindings 生成 | `cli/export_bindings.rs` → `src/shared/bindings.ts`（`pnpm gen:bindings`） |
| 外部终端模块 | `src-tauri/src/terminal/`（AppleScript 打开 iTerm2/Terminal.app，**外部**，与新 PTY 并存不冲突） |

### 2.2 P2 缺口
| 缺口 | 端 |
|---|---|
| `portable-pty` 接入 + `PtyProvider` trait + `LocalPtyProvider` | Rust |
| PTY 命令（spawn/write/resize/shutdown/list/exists/reattach）+ `stop_for_worktree` 真实现 | Rust |
| 每会话 ring buffer + reattach（刷新重载 scrollback） | Rust |
| 输出通道：Channel（首选，需验证 specta）或 emit（备选） | Rust→Web |
| xterm 组件（TerminalView + usePtySession）+ 双向流接线 | Web |
| 接入 D2（替换占位框）+ app 退出回收（lib.rs `RunEvent::Exit`） | Rust+Web |

---

## 3. 任务清单（主线）

> 依赖模块 1 完成（1.6 后即可启动）。按序执行；每个任务独立实现 + 验证。
>
> 状态图例：✅ 已完成 · 🔄 进行中 · ⬜ 待办

### ⬜ 任务 2.1 — portable-pty 接入 + PtyProvider 骨架
- **文件**：`src-tauri/Cargo.toml`（加 `portable-pty = "0.8"`，**不引 tokio/anyhow**）+ `src-tauri/src/pty/{provider,local_provider,session,state}.rs`（拆分）
- **当前**：`pty/mod.rs` 仅 `pty_stop_for_worktree` 桩；portable-pty 未引入；项目明确不用 tokio（异步走 `std::thread::spawn`）。
- **目标**：`PtyProvider` trait + `LocalPtyProvider`（portable-pty 实现）；`PtySession`（id/worktreeId/cwd/handle）；`PtySessionStore`（照 `ClaudeSessionStore`：`Mutex<HashMap>` + Default + `init(app)`）。
- **验证**：编译通过；`LocalPtyProvider::spawn` 在临时目录起 shell 不崩。

### ⬜ 任务 2.2 — PTY 命令 + 输出通道 + 退出回收
- **文件**：`src-tauri/src/pty/mod.rs`（命令）+ `src-tauri/src/lib.rs`（注册命令到 `:42` 后、`.typ::<T>()` 注册纯 payload 类型、`RunEvent::Exit:160` 加 `pty::shutdown_all`）+ `events.rs`/`events.ts`（若走 emit）
- **当前**：仅 `pty_stop_for_worktree` 注册；输出通道无（项目零 Channel、纯 emit）。
- **目标**：`pty_spawn/write/resize/shutdown/list`（`pty_stop_for_worktree` 桩暂留，2.5 变真）；输出通道 **先 spike `Channel<PtyData>` 验证 tauri-specta**，不行回退 `emit(EVENT_PTY_DATA/EXIT)`（双份维护）；specta 约束（计数 u32、时间戳 i64 标 Number、cols/rows u16）。
- **验证**：临时目录 spawn shell，打字有回显、resize 生效；`gen:bindings` 无 panic、bindings.ts 含新命令。

### ⬜ 任务 2.3 — ring buffer + reattach（刷新重载 scrollback）
- **文件**：`src-tauri/src/pty/session.rs`（环形缓冲）+ `mod.rs`（`pty_exists`/`pty_reattach` 命令 + 注册）
- **当前**：session 无 ring；无 exists/reattach 命令。
- **目标**：PTY 输出始终先入有界 ring 再推 listener；reattach 把 ring 一次性 replay 再切实时流；session 不存在返回 false。
- **验证**：spawn → 刷新前端 → `pty_reattach` 重载 scrollback 后接实时流；边界（有界、TUI 错位可接受）。

### ⬜ 任务 2.4 — xterm 终端组件 + 双向流
- **文件**：`package.json`（加 `@xterm/xterm`/`addon-fit`/`addon-webgl`）+ `DevWorkbenchPage/components/StepContent/EmbeddedTerminal/{TerminalView,usePtySession}.tsx`（新增，PascalCase 目录）
- **当前**：DevelopingStep 是占位 Box；无 xterm 依赖；项目零 Channel（前端 7 处全 `listen`，无 `new Channel`）。
- **目标**：xterm 封装（`onData`→`pty_write`、`onResize`→`pty_resize`、Channel/emit→`write`）；挂载优先 `pty_exists`+`pty_reattach`，不存在才 `pty_spawn`；卸载 `pty_shutdown`；import xterm.css；密集 UI 去 Tooltip、报错硬编码中文、按钮标签走 i18n。
- **验证**：开终端、双向流正常、刷新后 scrollback 重载、关闭无泄漏。

### ⬜ 任务 2.5 — D2 接入 + pty_stop_for_worktree 真实现
- **文件**：`DevelopingStep.tsx:54-65`（占位框 → `<EmbeddedTerminal worktreeId dir />`，与外部终端按钮并列）+ `src-tauri/src/pty/mod.rs`（`pty_stop_for_worktree` 从恒返 0 改为按 worktreeId 批量 kill 绑定 PTY）
- **当前**：D2 占位框；`pty_stop_for_worktree` 恒返 0；模块 1 D4 的 `useCleanupAndAdvance` 调用点已就位。
- **目标**：嵌入式终端接入 D2；`pty_stop_for_worktree` 变真，联动模块 1 任务 1.6 的 D4 编排（前端调用点无需改）。
- **验证**：D2 进嵌入式终端开发；D4「清理并完成」先停 PTY 再删 worktree，干净清理无残留进程/文件锁。

---

## 4. 设计支撑

### 4.1 `PtyProvider` 接口（为远程终端预留）
```rust
trait PtyProvider: Send + Sync {
    fn spawn(&self, opts: SpawnOpts) -> Result<SessionId>;
    fn write(&self, id: &SessionId, data: &[u8]) -> Result<()>;
    fn resize(&self, id: &SessionId, cols: u16, rows: u16) -> Result<()>;
    fn shutdown(&self, id: &SessionId) -> Result<()>;
    fn list(&self) -> Vec<SessionInfo>;
    fn stop_for_worktree(&self, worktree_id: &str) -> usize; // 删 worktree 前调用（模块 1 D4）
    // 输出/退出通过 Channel/emit 回传（§4.4）
}
```
本期只实现 `LocalPtyProvider`，远程后端将来扩展（命令层与前端组件基本不变）。

### 4.2 worktreeId 绑定（引用模块 1 §4.1）
PTY session 绑定 `worktreeId`（= `${localRepositoryId}::${absWorktreePath}`），用于定位会话、删除 worktree 前批量停 PTY。

### 4.3 Rust PTY 模块结构 + 命令
```
src-tauri/src/pty/
  mod.rs            // 命令注册 + dispatch（P1 已有 stop_for_worktree 桩）
  provider.rs       // PtyProvider trait
  local_provider.rs // LocalPtyProvider（portable-pty）
  session.rs        // PtySession（id/worktreeId/cwd/handle + ring buffer）
  state.rs          // PtySessionStore: Mutex<HashMap<SessionId, PtySession>>
```
命令（注册到 `lib.rs:42` 后）：`pty_spawn(opts)->SessionId` / `pty_write(id,data)` / `pty_resize(id,cols,rows)` / `pty_shutdown(id)` / `pty_list_sessions()->Vec<SessionInfo>` / `pty_stop_for_worktree(worktreeId)->usize` / `pty_exists(id)->bool` / `pty_reattach(id,channel)->bool`。

`SpawnOpts = { worktreeId, cwd(=worktree 绝对路径), cols, rows, command? }`；带 command 时需 shell-ready marker（§5.3）。

### 4.4 输出通道：Channel（首选）或 emit
- **首选 `Channel<PtyData>`**：`pty_spawn` 时前端传 Channel，Rust `channel.send()`，定向流式高效。⚠️ 项目零 Channel、纯 emit，tauri-specta 对 `Channel<T>` 类型导出需实测——任务 2.2 先 spike，不行回退 emit。
- **备选 emit**：追加 `EVENT_PTY_DATA`/`EVENT_PTY_EXIT` 到 events.rs/events.ts（双份），前端按 sessionId 过滤。

### 4.5 会话持久化：刷新重载 scrollback（轻量「常驻」）
PTY 在 Rust `tauri::State` 中，前端 webview 重载时 Rust 进程不死 → PTY 天然存活。唯一丢失是 xterm 那屏 scrollback，靠 ring buffer + `pty_reattach` 补回（任务 2.3）。前端挂载优先重挂：`pty_exists`+`pty_reattach`，不存在才 `pty_spawn`。

### 4.6 数据流
```
[键盘] xterm.onData → pty_write → PtySessionStore → portable_pty handle.write
[输出] portable_pty on_data → 入 ring → Channel.send/emit → usePtySession → xterm.write
[尺寸] xterm.onResize → pty_resize
[退出] portable_pty on_exit → emit(EVENT_PTY_EXIT) → 前端清理
```

---

## 5. 工程约束

### 5.1 删 worktree 前必须先停 PTY（本模块变真）
文件锁：PTY cwd 指向 worktree 时目录无法删除。本模块让 `pty_stop_for_worktree` 从 no-op 桩变真（按 worktreeId 批量 kill）；模块 1 D4 编排顺序已就位，无需改前端。

### 5.2 输出 backpressure
高频输出（`find /`）撑爆前端：session 内有界队列，溢出暂停读 master fd；前端 xterm webgl addon + 限频渲染。

### 5.3 启动命令的 shell-ready marker
`pty_spawn` 带 command（如 `claude`）时不能直接注入（被 zsh rc 噪声吞掉）：spawn 发唯一 marker，扫描输出匹配后再 `write(command\n)`。本期默认空（仅开 shell）可跳过；带命令必做。

### 5.4 刷新续接边界
scrollback 有界（最近 N KB/行）；**整 app 退出仍断**（Rust 进程退=PTY 死）；全屏 TUI（vim/less/claude TUI）原始字节 replay 可能轻微错位，TUI 刷新自重绘。

### 5.5 app 退出回收
`lib.rs:160` `RunEvent::Exit` 分支须加 `pty::shutdown_all`，否则 app 退出留孤儿 shell（仿 http_server shutdown）。

### 5.6 tauri-specta 类型约束（项目实证）
计数/长度用 `u32`/`i32`（禁 usize/u64/isize/i64）；时间戳 i64 字段加 `#[specta(type = Number)]`；cols/rows 用 u16；入参 struct 需 `#[derive(Clone,Serialize,Deserialize,Type)]`+`#[serde(rename_all="camelCase")]`；仅作事件 payload、不出现在命令签名的类型须在 `lib.rs:46-51` 用 `.typ::<T>()` 显式注册。

### 5.7 orca → 本项目对照（附录）
| orca | 本项目 |
|---|---|
| `node-pty` pty.spawn | Rust `portable-pty` |
| `ipcMain.handle`+`webContents.send` | Tauri `#[command]`+`Channel`/`emit` |
| xterm.js（renderer） | xterm.js（Web） |
| `IPtyProvider`（TS） | Rust `PtyProvider` trait |
| daemon 常驻+`@xterm/headless` | Rust state 抗刷新+ring buffer（**不做 daemon**） |
| 删 worktree 前停 PTY | 模块 1 §5.1 两阶段编排 |

**远程终端扩展预留**：`PtyProvider` trait + `SpawnOpts` 已抽象；将来加 `RemotePtyProvider`（SSH）时，命令层与前端组件基本不变，只需新增 provider 实现 + 远程连接配置 UI。
