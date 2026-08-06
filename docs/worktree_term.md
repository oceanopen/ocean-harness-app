# Issue 开发流程（Worktree + 本地终端）技术方案

> 参考实现：`~/MyFiles/Project/orca`（Electron + TypeScript 的 AI 编排器，多 agent 各占一个 git worktree 并行开发）。
> 本文档将其核心抽象（`IPtyProvider`、路径派生 `worktreeId`、中央元数据、xterm 双向流、删 worktree 前先停 PTY）移植到本项目的 **Tauri(Rust) + Go + Web** 三端架构。
> **范围**：issue → 创建 worktree → 打开本地终端 → 开发 → 清理。仅本地终端，远程终端（SSH relay）预留接口、不在本期实现。
>
> 配套文档：
> - [`docs/issue.md`](issue.md) — issue 状态体系三层模型。**开发流程的阶段(init/dev/pr/cleanup)不存本表**，而是 issue 的 `stateId` 在 `started` 组的开发步骤子 state（「进行中」之外）上推进（completed/cancelled 组即归档）。
> - [`docs/_dev_workflow_ux.md`](_dev_workflow_ux.md) — 「开发工作台」交互方案。本文档负责 worktree + 嵌入式终端的后端 plumbing，UX 层（步骤条、各步骤内容、左任务树）见该文。

---

## 1. 背景与目标

### 1.1 要解决什么
当前 issue 已能关联「仓库 + 分支」（`t_project_issues.local_repository_id + repository_branch`），但缺少真正「在隔离工作区里动手开发」的能力：
- 切分支要 stash 现有改动、污染主工作区；
- 多个 issue 并行开发时互相干扰；
- 无法在应用内直接进入终端、把 issue 与一个独立工作环境绑定。

引入 **git worktree**：每个 issue 拥有独立工作目录（各自分支、互不干扰），并在应用内打开嵌入式终端直达该目录。

### 1.2 目标流程
```
Issue 详情页 ──[开始开发]──▶ 创建 worktree（新分支 + 独立目录）
                              │
                              ▼
                        打开嵌入式终端（cwd = worktree 目录）
                              │
                              ▼
                   开发 / 提交（可选自动注入启动命令）
                              │
                              ▼
        [停止开发] ──▶ 关闭终端 ──▶ 删除 worktree ──▶ 回写 issue 状态
```
> 「回写 issue 状态」= 把 issue 的 `stateId` 推进到 `completed` 组（清理完成）或 `cancelled` 组（取消），见 [`issue.md`](issue.md)。开发流程的阶段位置完全由 `stateId` 表达，本表只存 worktree 元数据。

### 1.3 非目标（本期不做）
- 远程终端 / SSH relay（仅预留 `PtyProvider` 抽象）。
- PR / MR 创建、issue 状态自动回写到外部平台（Linear/GitHub/Jira）。
- 多 AI agent 编排、pane 无限 split、看板视图。
- Windows ConPTY 特殊处理（本期以 macOS/POSIX 为主，Windows 做基本兼容）。

---

## 2. 现状与缺口

### 2.1 已有能力（可复用）
| 能力 | 位置 | 说明 |
|---|---|---|
| 本地仓库注册表 | Go `t_local_repositories` + `service/local_repository.go` | CRUD + git 信息刷新 + 分支列表 |
| 项目↔仓库多对多 | `t_project_local_repositories` | 随项目 create/update 全量保存 |
| issue↔分支关联 | `t_project_issues.local_repository_id/repository_branch` | issue 关联某仓库的某分支 |
| issue 分支选择器 UI | `IssueBranchField.tsx` | 仓库下拉 + 分支 freeSolo Autocomplete |
| git 只读封装 | `gitutil/gitutil.go` | `IsRepo / ParseInfo / LocalBranches`（`os/exec` 调系统 git） |
| 打开宿主终端 | Rust `open_in_terminal` | AppleScript 打开 iTerm2/Terminal.app（**外部终端**） |
| 打开目录后追加命令 | `appConfig.TERMINAL_POST_OPEN_COMMAND_KEY` | `cd {dir} && {cmd}` |
| 事件驱动状态同步 | Rust `tauri::State` + `emit` | 前端纯订阅、不轮询 |

### 2.2 缺口（本期要补）
| 缺口 | 归属端 |
|---|---|
| `git worktree add/list/remove` 写操作 | Go（`gitutil` 扩展） |
| worktree 元数据持久化（issue↔worktree） | Go（新表 + service/controller） |
| 嵌入式终端（PTY spawn + xterm 渲染 + 双向流） | Rust（PTY）+ Web（xterm） |
| 开发流程编排（start/stop dev） | Web（协调 Rust 与 Go 两端） |
| worktree↔PTY 生命周期联动（删前停 PTY） | Web 编排 + `worktreeId` 共享键 |

---

## 3. 核心设计：三端职责划分

### 3.1 划分原则（沿用项目既有约定）
- **OS 动作留 Rust**：进程 spawn、PTY、文件系统、窗口 —— 长生命周期、有状态、需事件推送。
- **业务数据 + git 留 Go**：仓库/项目/issue、worktree 元数据、`git worktree` 命令 —— 与既有 `gitutil.go`、SQLite 一致。
- **Rust 是唯一状态源 + emit payload，前端纯订阅**：PTY 会话状态放 Rust `tauri::State`，输出走事件/Channel 推送。
- **前端是两后端的协调者**：前端已通过「Rust 拿 Go 地址 → fetch Go」协调两端；开发流程同样由前端分阶段编排。

### 3.2 职责矩阵
| 关注点 | 归属 | 理由 |
|---|---|---|
| PTY 生命周期（spawn/write/resize/kill） | **Rust** | OS 动作；有状态长连接资源 |
| 终端会话状态（在跑的 PTY 列表） | **Rust** | `tauri::State`，单一状态源 + emit |
| PTY 输出流式推送 | **Rust → Web** | Tauri `Channel`/`emit` |
| 终端渲染（xterm.js） | **Web** | UI 层 |
| `git worktree` 创建/列举/删除 | **Go** | 与 `gitutil.go` 一致，git 操作集中 |
| worktree 元数据持久化 | **Go/SQLite** | 业务数据 |
| issue/项目/仓库数据 | **Go** | 已有 |
| 开发流程编排（start/stop） | **Web** | 已是两端协调者 |
| PTY↔worktree 生命周期协调 | **Web 编排** + `worktreeId` 共享键 | 两后端解耦 |

### 3.3 关键决策：worktree 在 Go，PTY 在 Rust
worktree 与 PTY 生命周期强耦合（**删 worktree 前必须先停 PTY**，否则文件锁 / 残留进程）。两者分属不同后端，靠两点解耦：

1. **`worktreeId` 共享键**（见 §4）：Go 与 Rust 都用同一个字符串键引用 worktree，前端穿针引线。
2. **前端两阶段编排**：`stop dev` = 先调 Rust 停 PTY → 再调 Go 删 worktree；失败靠 reconcile 兜底（见 §10.4）。

> 备选方案「worktree 也放 Rust，让 Rust 独占整个开发环境」被否：会破坏「git 操作集中在 Go」的既有约定，且 Rust 侧重复实现仓库注册表查询。

---

## 4. 关键抽象

### 4.1 `worktreeId` —— 路径派生的共享键（移植自 orca）
```
worktreeId = `${localRepositoryId}::${absWorktreePath}`
```
- **git 是唯一真相源**：worktree 是否存在以 `git worktree list` 为准，DB 只存关联元数据。
- **路径派生、跨进程一致**：Go（创建/删除/持久化）与 Rust（绑定 PTY、定位会话）无需互相通信即可对齐。
- **代价**：worktree 目录改名 → id 变。本期不支持改名；若将来支持，照 orca 加 `prior_worktree_ids` + `instance_id` 兼容（见 §10.5）。

### 4.2 `PtyProvider` 接口 —— 为远程终端预留（移植自 orca `IPtyProvider`）
Rust 侧定义 trait，本期只实现 `LocalPtyProvider`，远程后端将来扩展：
```rust
trait PtyProvider: Send + Sync {
    fn spawn(&self, opts: SpawnOpts) -> Result<SessionId>;
    fn write(&self, id: &SessionId, data: &[u8]) -> Result<()>;
    fn resize(&self, id: &SessionId, cols: u16, rows: u16) -> Result<()>;
    fn shutdown(&self, id: &SessionId) -> Result<()>;
    fn list(&self) -> Vec<SessionInfo>;
    fn stop_for_worktree(&self, worktree_id: &str) -> usize; // 删除 worktree 前调用
    // 输出/退出通过 callback 或 channel 回传（见 §7.2）
}
```

### 4.3 中央元数据（用 SQLite，不用 git config）
worktree 的用户态元数据（关联 issue、显示名、状态、活跃时间）存 SQLite，不污染仓库。对应 orca 的 `orca-data.json` + `WorktreeMeta`，但用结构化表（见 §5）。

---

## 5. 数据模型（Go / SQLite）

### 5.1 新增迁移 `20260806001_create_issue_worktrees.sql`
```sql
CREATE TABLE t_issue_worktrees (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    worktree_id         TEXT    NOT NULL UNIQUE,        -- ${repoId}::${absPath}，跨端共享键
    issue_id            INTEGER NOT NULL,               -- t_project_issues.id
    local_repository_id INTEGER NOT NULL,               -- t_local_repositories.id
    worktree_path       TEXT    NOT NULL,               -- 绝对路径
    branch              TEXT    NOT NULL,               -- worktree 所在分支
    base_ref            TEXT,                            -- 创建时的基准（如 origin/main）
    status              TEXT    NOT NULL DEFAULT 'active', -- active | stale | removed
    last_active_at      DATETIME,                       -- PTY 有输出时由 Rust 经事件回写（可选）
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at          DATETIME                        -- 软删除，与 tracker 域一致
);
CREATE INDEX idx_issue_worktrees_issue ON t_issue_worktrees(issue_id);
CREATE INDEX idx_issue_worktrees_repo  ON t_issue_worktrees(local_repository_id);
-- 无 DB 外键（项目约定），issue/仓库合法性由 service 层校验
```
> 与 issue 既有 `repository_branch` 的关系：`repository_branch` 表示「issue 关心的分支」（既有语义不变）；`t_issue_worktrees` 表示「为开发而创建的隔离工作区」。一个 issue 可有 0..N 个 worktree（本期 UI 侧重 1:1，模型支持 1:N）。
>
> **开发流程阶段不在本表**：worktree 初始化/开发中/待合并PR/待清理 这些"阶段"是 issue 的 `stateId` 在 `started` 组开发步骤子 state（「进行中」之外）上的位置（见 [`issue.md`](issue.md) §2.2）。本表的 `status`(active/stale/removed) 只描述 **worktree 自身的物理生命周期**，与开发阶段正交。前端编排时：阶段推进 = 改 issue.stateId；worktree 物理增删 = 改本表 + 调 `git worktree`。

### 5.2 gormgen / DO
按项目 `tracker 枚举范式`（见记忆）：
- `internal/dal/enums/issue_worktree_status.go`：`IssueWorktreeStatus` typed enum + `Value()`。
- gencode 生成 `model/issue_worktree.gen.go`（`status` 字段 typed 化）与 `query/issue_worktree.gen.go`。
- 枚举包须先于 gencode 存在。

### 5.3 落盘路径约定（worktree 实际放哪）
移植 orca 的嵌套模式，默认根目录可配置（先放应用配置，后续可进 `app_config`）：
```
<worktreeRoot>/<repoName>/<sanitizedName>
  worktreeRoot 默认: ~/Library/Application Support/<App>/worktrees  （或用户自定义）
  repoName:          t_local_repositories 里的仓库名 / 目录名
  sanitizedName:     issue 标题 + issue id 清洗（保留字母数字/中文，其余 collapse 成 -）
```
- 分支命名：`<branchPrefix?>/<issueKey>-<slug>`，`branchPrefix` 可选（默认空）。
- 名称清洗：拒绝 `..`/`.`，emoji 与不安全字符 collapse 成 `-`（移植 orca `sanitizeWorktreeName`）。

---

## 6. Go 端：Worktree 管理

### 6.1 `gitutil/worktree.go`（扩展 gitutil，纯 `os/exec` 调系统 git）
```go
// 与既有 IsRepo/ParseInfo/LocalBranches 同风格
func WorktreeList(repoDir string) ([]WorktreeInfo, error)      // git worktree list --porcelain
func WorktreeAdd(repoDir, branch, path, baseRef string) error   // git worktree add --no-track -b <branch> <path> <baseRef>
func WorktreeRemove(repoDir, path string, force bool) error     // git worktree remove [--force] <path> + prune
func WorktreeBranchExists(repoDir, branch string) (bool, error)
```
- 沿用 `gitExecFileAsync` 风格（超时、合并 stderr）。
- `--no-track`：避免误报 behind（orca 教训）。
- 可选 capability 探测（macOS git 版本通常较新，本期先不做 `-z` fallback，记 TODO）。

### 6.2 service / controller / router（沿用项目 Api/Service 范式）
```
internal/service/issue_worktree.go        // StartDev / StopDev / ListByIssue / Reconcile
internal/controller/issue_worktree.go     // 嵌入 apis.Api，链式装配
internal/dal/types/issue_worktree.go      // Request/Response DTO
```
路由（全 POST，前缀 `/api`，加在 `router/router.go`）：
| 路由 | 说明 |
|---|---|
| `/api/tracker/issueWorktree/startDev` | 校验 issue/仓库 → `WorktreeAdd` → 写 `t_issue_worktrees` → 返回 `{worktreeId, worktreePath, branch}` |
| `/api/tracker/issueWorktree/stopDev` | 软删 `t_issue_worktrees`（status=removed）→ **不在此删 worktree**（删由前端先停 PTY 后调 `removeWorktree`） |
| `/api/tracker/issueWorktree/removeWorktree` | `WorktreeRemove`（前置：前端已停 PTY）→ 物理清理 |
| `/api/tracker/issueWorktree/getList` | 列某 issue 的 worktree（带 status） |
| `/api/tracker/issueWorktree/getInfo` | 单个 worktree 详情（含 `git status` 摘要） |
| `/api/tracker/issueWorktree/reconcile` | 对比 `WorktreeList` 与 DB，标记 stale（路径消失）行 |

`startDev` 校验（移植既有 `validateIssueRepo` 思路）：仓库须属于项目关联仓库；分支不冲突；worktree 路径不重复。

---

## 7. Rust 端：嵌入式终端（PTY）

### 7.1 依赖
`Cargo.toml` 增加：
```toml
portable-pty = "0.8"   # wezterm 出品，POSIX/Windows，语义贴近 orca 的 node-pty
```
> 不引入 `tauri-plugin-pty` 社区插件（成熟度不足）；`portable-pty` 是终端类应用的事实标准。

### 7.2 模块结构（与既有 `src-tauri/src/terminal/` 外部终端模块区分）
```
src-tauri/src/pty/               # 嵌入式 PTY（新）
  mod.rs                         // 对外命令注册 + dispatch
  provider.rs                    // PtyProvider trait
  local_provider.rs              // LocalPtyProvider（portable-pty 实现）
  session.rs                     // PtySession（id/worktreeId/cwd/handle）
  state.rs                       // PtySessionStore: Mutex<HashMap<SessionId, PtySession>>
src-tauri/src/shared/events.rs   // 追加 EVENT_PTY_DATA / EVENT_PTY_EXIT（与前端 events.ts 双份维护）
```

### 7.3 Tauri 命令（`#[tauri::command]`，加入 `lib.rs` 的 `collect_commands!`）
| 命令 | 方向 | 说明 |
|---|---|---|
| `pty_spawn(opts) -> SessionId` | invoke | opts = `{ worktreeId, cwd, cols, rows, command? }`；启动 PTY，登记 session，订阅输出 |
| `pty_write(id, data)` | invoke | 转发键盘输入（高频，fire-and-forget 语义） |
| `pty_resize(id, cols, rows)` | invoke | resize（高频，fire-and-forget） |
| `pty_shutdown(id)` | invoke | 停止单个 PTY |
| `pty_list_sessions() -> Vec<SessionInfo>` | invoke | 当前在跑的会话（含 worktreeId/cwd） |
| `pty_stop_for_worktree(worktreeId) -> usize` | invoke | **删 worktree 前置**：停掉该 worktree 全部 PTY |
| `pty_exists(id) -> bool` | invoke | session 是否仍存活（前端刷新后判断能否重挂，见 §7.6） |
| `pty_reattach(id, channel) -> bool` | invoke | 把该 session 的 scrollback 一次性 replay 给新 xterm，再切实时流（见 §7.6）；session 不存在返回 false |

### 7.4 输出推送：Tauri 2 `Channel`（首选）或 `emit`
- **首选 `Channel<PtyData>`**：在 `pty_spawn` 时由前端传入 `Channel`，Rust 把 PTY 输出 `channel.send()`，定向、流式、高效，避免 `emit` 广播到所有窗口。
- **备选 `emit`**：与项目现有 `EVENT_*` 模式一致（Claude sessions / http server 都用 emit）。若 tauri-specta 对 `Channel` 的类型导出有问题，回退到 `emit(EVENT_PTY_DATA, {sessionId, data})`，前端按 `sessionId` 过滤。
- 决策点：实现时先验证 tauri-specta + Channel，否则用 emit（见 §11 阶段 2 任务）。

### 7.5 `SpawnOpts` 里的 worktree 绑定（移植 orca 多维绑定）
```rust
struct SpawnOpts {
    worktree_id: String,   // 绑定 worktree（定位会话 / 删除前批量停）
    cwd: String,           // = worktree 绝对路径
    cols: u16, rows: u16,
    command: Option<String>, // 可选启动命令（如 claude / npm run dev）
}
```
- 本期不做 per-worktree shell history 隔离（orca 用 `HISTFILE` 注入），记 TODO。
- 启动命令注入需 shell-ready marker（见 §10.3）。

### 7.6 会话持久化：刷新重载 scrollback（轻量「常驻」效果）
> 目标：前端刷新 / 切走再回来，shell 仍在跑、能看到之前的输出历史。**不做** orca 的 daemon 层（「关 app 也不断」），只做「抗前端刷新」。

**架构优势**：PTY 在 Rust `tauri::State` 中，前端 webview 重载时 Rust 进程不死 → **PTY 天然存活，agent 常驻近乎白送**。唯一丢失的是 xterm 那一屏 scrollback（webview 重载 = xterm 实例销毁）。

补回 scrollback 的 3 个小改动（用原版 xterm，无需 headless addon）：

1. **Rust 每个 session 挂有界环形缓冲**：PTY 输出始终先入 ring（无论前端是否在线），再推当前 listener。这同时解决「刷新那一瞬正在输出的字节会丢」的窗口期。
```rust
struct PtySession {
    // ...既有字段...
    ring: RingBuffer<u8>,   // 最近 N KB / N 行，可配置
}
```

2. **新增命令**（见 §7.3 表）：`pty_exists(id)` 判断 session 是否仍存活；`pty_reattach(id, channel)` 把 ring 内容一次性 replay 给新 xterm，再切实时流。

3. **前端挂载时优先重挂**：
```ts
const sid = restoreSessionIdFor(worktreeId);   // 从 sessionStorage / Rust 查
if (sid && await commands.pty_exists(sid)) {
    await commands.pty_reattach(sid, channel);   // 先吃 scrollback，再接实时流
} else {
    await commands.pty_spawn({ /* 新建 */ });     // 回退
}
```
xterm 侧原版无感：`terminal.write(replayedBytes)` 后接实时流，对它就是连续字节流。

**vs orca**：跳过 daemon 子进程、`@xterm/headless` 权威 buffer、stable-pane-id / 跨进程 reattach —— 那些是为「app 关掉也能续命」。我们靠 Rust state 抗刷新 + 一个 ring buffer 即可。

**边界**：① scrollback 有界（最近 N KB/行）；② **整 app 退出仍断**（Rust 进程退 = PTY 死，要续需上 daemon 层，本期非目标）；③ 全屏 TUI（vim/less/claude TUI）的原始字节 replay 可能轻微错位，普通命令+输出无影响，TUI 刷新一次自重绘。

---

## 8. Web 端：xterm + 开发流程 UI

### 8.1 依赖
```json
"@xterm/xterm": "^6",
"@xterm/addon-fit": "^1",
"@xterm/addon-webgl": "^1"   // 可选，大输出时启用
```

### 8.2 终端组件（新）
```
src/components/Terminal/TerminalView.tsx   // xterm 封装：onData→pty_write、onResize→pty_resize、Channel/emit→write
src/components/Terminal/usePtySession.ts   // hook：spawn / 清理 / 数据流接线
```

### 8.3 状态层（沿用 `state/<domain>/{keys,queries}.ts` 范式）
```
src/state/issueWorktree/keys.ts
src/state/issueWorktree/queries.ts   // useIssueWorktrees / startDev / stopDev / removeWorktree mutations
```

### 8.4 开发流程 UI（挂在 `ProjectIssueDrawer` 或 issue 卡片）
- issue 详情抽屉新增 **「开始开发」** 按钮 / 区块：
  - 预填：仓库（issue 的 `localRepositoryId`，可选项目关联仓库）、基准分支、新分支名（默认 `<prefix>/<issueKey>-<slug>`）。
  - 可选「启动命令」（默认取 `appConfig.TERMINAL_POST_OPEN_COMMAND_KEY`）。
- 点击 → `startDev`(Go) 拿 `worktreePath` → `pty_spawn`(Rust, cwd=worktreePath) → 打开 `TerminalView`。
- 终端区显示：分支、worktree 路径、关联 issue；操作：打开宿主终端 / 编辑器（复用既有 Rust 命令）、停止开发。
- 多 worktree：Tab 或列表切换（本期单终端优先）。

---

## 9. 数据流

### 9.1 双向流（嵌入式终端）
```
[键盘输入]
  xterm.onData(data)
    → commands.pty_write(sessionId, data)      // invoke
      → Rust PtySessionStore
        → portable_pty handle.write(data)

[子进程输出]
  portable_pty handle.on_data(buf)
    → Rust session 回调
      → Channel.send({ sessionId, data: buf })  // 或 emit(EVENT_PTY_DATA, …)
        → 前端 usePtySession 回调
          → xterm.write(data)

[尺寸变化]  xterm.onResize → pty_resize (invoke)
[进程退出]  portable_pty.on_exit → emit(EVENT_PTY_EXIT,{sessionId,code}) → 前端清理 + 标记
```

### 9.2 端到端「开始开发」
```
Web                         Rust(PTY)                    Go(worktree/数据)
 │                                                          
 ├─ startDev(payload) ──────────────────────────────────▶ 校验 → WorktreeAdd → 写 t_issue_worktrees
 │ ◀──────────────────── { worktreeId, worktreePath, branch } ──────────────────────────
 ├─ pty_spawn({worktreeId, cwd:worktreePath, …}) ─▶ 登记 session，启动 PTY
 │ ◀──────── SessionId ───────────────────────────
 ├─ TerminalView 挂载，数据流接通（§9.1）                                         
```

### 9.3 端到端「停止开发 / 清理」（生命周期顺序约束，§10.1）
```
Web                         Rust(PTY)                    Go(worktree/数据)
 ├─ pty_stop_for_worktree(worktreeId) ─▶ kill 全部绑定 PTY
 │ ◀──────── stoppedCount ────────────
 ├─ removeWorktree(worktreeId) ─────────────────────────▶ WorktreeRemove → 软删/物理删 t_issue_worktrees
 │      （前置满足：PTY 已停）                                
```

---

## 10. 关键工程问题与对策（移植 orca 踩坑经验）

### 10.1 删 worktree 前必须先停 PTY（强约束）
- 文件锁：PTY 进程的 cwd 指向 worktree 时，目录无法删除（POSIX 也存在，Windows 更严重）。
- 落地：`stop dev` 走 §9.3 两阶段；前端 `await pty_stop_for_worktree` 成功后再 `removeWorktree`。

### 10.2 输出 backpressure
- 高频输出（`find /`）会撑爆前端缓冲。portable-pty 提供读取暂停能力：在 session 内做有界队列，溢出时暂停读取 master fd（移植 orca `pauseProducer/resumeProducer`）。
- 前端侧：xterm webgl addon + 限频渲染。

### 10.3 启动命令的 shell-ready marker
- 若 `pty_spawn` 带 `command`（如 `claude`），不能直接注入：会被 zsh rc 噪声吞掉。
- 移植 orca 方案：spawn 时发一个唯一 marker（如 `printf 'READY:<token>'`），扫描输出直到匹配再 `write(command\n)`。
- 本期若「启动命令」为空（默认仅开 shell），可跳过；带命令时必做。

### 10.4 两阶段编排的失败 reconcile
- PTY 停了但 worktree 删失败：前端提示重试，Go 的 `reconcile` 定期对比 `git worktree list` 清理孤儿。
- worktree 删了但 PTY 残留：Rust 的 PTY 监听进程退出，孤儿会随 `on_exit` 自然回收；`pty_stop_for_worktree` 兜底强制 kill。

### 10.5 路径派生 ID 的改名问题（本期规避）
- 本期不支持 worktree 改名/移动，避免 id 失联。将来支持时加 `prior_worktree_ids` + `instance_id`（移植 orca）。

### 10.6 时区（项目既有坑）
- `t_issue_worktrees` 的时间字段遵循记忆 [[reference_sqlite_timezone_glebarez]] 的结论：`CURRENT_TIMESTAMP` 走 UTC，展示层转本地，避免少 8 小时。

### 10.7 Go 私有代理白名单
- 不引入外部 Go 库（worktree 用 `os/exec`，无新依赖），规避 [[project_goproxy_whitelist]]。

### 10.8 前端刷新的会话续接
PTY 在 Rust `tauri::State` 中天然抗前端刷新（agent 常驻近乎白送），scrollback 靠 §7.6 的 ring buffer + `pty_reattach` 补回。边界：scrollback 有界、整 app 退出仍断（非 daemon）、全屏 TUI replay 可能轻微错位 —— 详见 §7.6。

---

## 11. 实施阶段

> 每阶段可独立验证、独立合入。姊妹项目 `we-health-tick-app` 可作平行移植参考（[[project_sibling_health_tick_app]]）。

**阶段 1：Go worktree 运维（无 UI、无终端）**
- `gitutil/worktree.go`（List/Add/Remove）+ 单测。
- 迁移 `t_issue_worktrees` + enums + gencode。
- `service/controller/router` 的 `startDev/stopDev/getList/reconcile`。
- 验证：curl 调通创建/列举/删除 worktree，DB 落盘正确。

**阶段 2：Rust 嵌入式终端（最小可用）**
- `portable-pty` 接入；`PtyProvider` trait + `LocalPtyProvider`。
- 命令 `pty_spawn/write/resize/shutdown/list_sessions/stop_for_worktree` + 每会话 ring buffer、`pty_exists/pty_reattach`（§7.6）。
- 先定输出通道：tauri-specta + `Channel` 验证，不行回退 `emit`。
- 验证：在一个临时目录 spawn shell，前端用 xterm 打字有回显、resize 生效；刷新后 `pty_reattach` 能重载 scrollback。

**阶段 3：Web 终端组件 + 数据流**
- `TerminalView` + `usePtySession` + xterm 依赖；挂载时优先 `pty_reattach`，不存在才 `pty_spawn`（§7.6）。
- `state/issueWorktree/`。
- 验证：能开终端、双向流正常、**刷新后 scrollback 重载**、关闭清理无泄漏。

**阶段 4：开发流程编排（串联）**
- issue 详情「开始开发」/「停止开发」UI。
- §9.2/§9.3 端到端流程；§10.1 生命周期顺序；§10.3 shell-ready（若带启动命令）。
- 验证：从某 issue 一键进入 worktree 终端开发、停止后 worktree 干净清理。

**阶段 5：健壮性与体验（可后续迭代）**
- backpressure（§10.2）、reconcile 兜底（§10.4）、`reconcile` 定时任务、worktree 状态摘要（`git status`）。
- per-worktree shell history、capability fallback、Windows ConPTY 兼容。

---

## 12. orca → 本项目 对照表与扩展预留

| orca（Electron/TS） | 本项目对应 | 备注 |
|---|---|---|
| `node-pty` `pty.spawn` | Rust `portable-pty` | OS 动作留 Rust |
| `ipcMain.handle/on` + `webContents.send` | Tauri `#[command]` + `Channel`/`emit` | Rust 单一状态源 + emit |
| xterm.js（renderer） | xterm.js（Web） | 不变 |
| `IPtyProvider`（TS interface） | Rust `PtyProvider` trait | 本期 `LocalPtyProvider` |
| `gitExecFileAsync` | Go `os/exec` 调 git（`gitutil`） | git 操作集中 Go |
| `orca-data.json` + `WorktreeMeta` | SQLite `t_issue_worktrees` | 结构化、多 worktree 查询友好 |
| `worktreeId = repoId::path` | `${repoId}::${absPath}` | 直接照搬 |
| `WorkspaceLinkedItem`（多平台 issue） | issue_id 外键（本平台 issue） | 本期仅本地 issue |
| 删 worktree 前停 PTY | §9.3 两阶段编排 | 前端协调 |

**远程终端扩展预留**：`PtyProvider` trait + `SpawnOpts` 已抽象；将来加 `RemotePtyProvider`（SSH，参照 orca `src/relay/`）时，命令层与前端组件基本不变，只需新增 provider 实现 + 远程连接配置 UI。
