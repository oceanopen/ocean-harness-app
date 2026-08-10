# 模块 1：Issue Worktree 生命周期（创建 + PR + 清理）

> 配套：[`docs/dev_terminal.md`](dev_terminal.md)（模块 2 嵌入式终端，**依赖本模块先做通**）。
>
> **本模块定位**：issue 隔离工作区从创建到清理的完整生命周期。**开发阶段（D2）本期手动**——外部终端 + 手动扭转 issue 状态；嵌入式终端见模块 2。
> **范围**：worktree 创建 → 手动开发 → PR → 清理。本期仅本地。
> **现状基线**：issue_worktree 的 P1 脚手架已大面积存在（Go 表/桩 + Web DevWorkbench 4 步流程）。本模块任务是「P1 桩 → P2 真实现」。

---

## 1. 模块概览

**4 步状态机**（issue 的 `stateId` 在 `started` 组开发步骤子 state 上推进，阶段不另建表）：

```
wt_init ──[创建并开始]──▶ developing ──[开发完成]──▶ pr_open ──[合并完成]──▶ cleanup ──[清理并完成]──▶ completed（自动归档）
                                                                                                    │
                                                                            cancelled ◀──（由「事项管理」规划面处理，不在开发流程执行面）
```

| 步骤 | 含义 | 本期 |
|---|---|---|
| **wt_init** | worktree 初始化：选仓库/基准/开发分支 → 创建 worktree | P2：真 `git worktree add` + 真路径 |
| **developing** | 开发中 | **手动**（外部终端 + 手动扭转状态，P1 已有）→ 模块 2 补嵌入式终端 |
| **pr_open** | 待合并 PR | P2：平台 REST API 创建 PR（替代「引导式 compare URL」） |
| **cleanup** | 待清理：删 worktree | P2：真 `git worktree remove` + prune |

- `cleanup`（最后一步）→ 推进到 `completed` 组首个 state（**自动归档**）。
- `cancelled` 不在开发流程执行面，由「事项管理」（规划面）处理。
- 状态推进统一走 `ProjectIssueService.move({ id, stateId, sortOrder })`；前端 `getNextDevStepStateId` / `useAdvanceDevStep`（`src/state/devWorkbench/queries.ts`）驱动。

**先后顺序**：本模块（worktree 生命周期）先做通 → 模块 2（嵌入式终端）后补。两者靠 `worktreeId` + `removeWorktree` API 桥接。

**涉及端**：Go（worktree 运维 + 元数据 + PR）、Web（DevWorkbench 4 步编排）、Rust（`pty_stop_for_worktree` 桩，本模块范围内 no-op）。

---

## 2. 现状基线（P1 已有 / P2 缺口）

### 2.1 P1 已有（可复用，无需重建）
| 能力 | 位置 |
|---|---|
| worktree 元数据表 | `t_issue_worktrees`（迁移 `20260809001_create_issue_worktrees.sql`） |
| typed 枚举 | `internal/dal/enums/issue_worktree_status.go`（active/stale/removed + Value） |
| gencode DO/Query | `model/issue_worktrees.gen.go` / `query/issue_worktrees.gen.go` |
| service 桩 | `internal/service/issue_worktree.go`（CreateWorktree 派生**假**路径 / RemoveWorktree 软删 / GetList） |
| controller/router | `internal/controller/issue_worktree.go` / `router/router.go:106-113`（三路由已注册） |
| git 只读封装 | `gitutil/gitutil.go`（IsRepo/ParseInfo/LocalBranches） |
| 仓库合法性校验 | `service/project_issue.go:557 validateIssueRepo` |
| DevWorkbench 4 步 | `DevWorkbenchPage/`（WtInitStep/DevelopingStep/PrOpenStep/CleanupStep） |
| 状态机 | `src/state/devWorkbench/queries.ts` |
| worktree 编排 hook | `src/state/issueWorktree/queries.ts`（useCreateWorktreeAndAdvance / useCleanupAndAdvance） |
| compare URL | `src/shared/gitRemote.ts buildCompareUrl`（D3 现状用） |
| 外部打开 | `commands.openInTerminal/openInEditor/openInFileManager`（D2 手动开发落点） |
| Rust PTY 停止桩 | `src-tauri/src/pty/mod.rs pty_stop_for_worktree`（恒返 0） |

### 2.2 P2 缺口（本模块要补）
| 缺口 | 步骤 | 端 |
|---|---|---|
| `git worktree add/list/remove` 写操作 | D1/D4 | Go |
| 真路径派生（替换 `<worktree-root-placeholder>`） | D1 | Go |
| worktree 创建前的仓库合法性校验 | D1 | Go |
| PR 创建（平台 REST API） | D3 | Go + Web |
| 真 worktree 删除 + prune | D4 | Go |
| reconcile（git worktree list vs DB 标 stale） | 兜底 | Go |

> **D2（developing）本期不改**：保持「外部终端 + 手动扭转状态」。

---

## 3. 任务清单（主线）

> 按序执行；每个任务独立实现 + 验证。任务 1.7 可选，不阻塞模块 2。
>
> 状态图例：✅ 已完成 · 🔄 进行中 · ⬜ 待办

### ✅ 任务 1.1 — gitutil worktree 写操作封装
- **文件**：`src-server/internal/gitutil/worktree.go`（新增）+ 单测
- **当前**：`gitutil.go` 只有只读（IsRepo/ParseInfo/LocalBranches），无任何 worktree 函数。
- **目标**：`WorktreeList/Add/Remove/BranchExists`，照 `gitutil.go` 实态（`gitOutput` 风格、`os/exec`、无超时）；写操作新增返回 `(string,error)` 的 helper（合并 stderr 进 error，因 add/remove 失败原因须回传）；写前用 `IsRepo` 校验；`add` 用 `--no-track`。
- **验证**：单测覆盖 add/list/remove/prune 正常 + 分支冲突/路径占用/脏工作区。

### ✅ 任务 1.2 — worktreeRoot 配置 + 路径派生（per-workspace 改造）
- **文件**：`migrations/20260809002_add_workspace_worktree_root.sql`（t_workspaces 加 worktree_root）+ `service/workspace.go`/`dal/types/workspace.go`（Create/Update 透传）+ `gitutil/naming.go`（RepoNameFromRemoteURL + 单测）+ `service/issue_worktree.go`（CreateWorktree 真路径派生，删 placeholder）+ 前端 `WorkspaceService.ts`/`WorkspaceDrawer.tsx`（目录选择器，参考 AddRepositoryDrawer）/i18n
- **设计变更**（取代原全局 appConfig 方案）：探明 Go sidecar 读不到 appConfig（与 Rust 配置物理隔离，仅 `GO_SERVER_*` 环境变量桥接），改为 **per-workspace**——worktreeRoot 配在 `t_workspaces.worktree_root`，Go CreateWorktree 查 issue→workspace 直接拿（本就要查），无需环境变量、不动 Rust。worktree 跟着工作空间走，不同工作空间可不同存放位置。
- **目标**：派生 `<worktreeRoot>/<repoName>/workspace_{wid}-project_{pid}-issue_{iid}`；repoName 从 remote_url 的 `/xxx.git` 末段解析（空回退 filepath.Base(local_dir)）；worktreeRoot 为空报错要求配置；末段用稳定 id 段（取代 issue 标题清洗——标题含中文/emoji 清洗复杂）。
- **验证**：go build + gitutil 单测（SSH/HTTPS/subgroup）+ tsc 通过。本期只派生路径写记录，不真调 git worktree add（任务 1.3 才建目录）。

### ✅ 任务 1.3 — D1 worktree 真创建
- **文件**：`src-server/internal/service/issue_worktree.go`（改 CreateWorktree）+ `dal/types/issue_worktree.go`（入参按需）
- **当前**：CreateWorktree 派生假路径写记录（不真调 git），P1 幂等逻辑（按 worktreeId UNIQUE 重置 active）已就位。
- **目标**：真路径派生（1.2）→ `validateIssueRepo(orm, projectID, repoID, branch)`（入参 projectID 非 IssueID；CreateWorktree 已查 issue 拿 `issue.ProjectID` 直接传，校验 repo 属于 project 关联仓库集合）→ `WorktreeExists` 防目录已存在（幂等跳过）+ `WorktreeBranchExists` 防分支冲突 → `WorktreeAdd`；保留幂等。git 写盘在事务外，事务失败不回滚磁盘（reconcile 兜底）。
- **验证**：某 issue「创建并开始」→ 磁盘真生成 worktree 目录、DB 记录路径正确、推进到 developing。

### ⬜ 任务 1.4 — D3 PR：githost 平台抽象包
- **文件**：`src-server/internal/githost/`（新增包）+ 单测
- **当前**：无 Go 侧 PR 能力；前端 `gitRemote.ts` 有 host 解析（仅 compare URL）。
- **目标**：`DetectProvider(remoteURL)` 解析 host（github.com/gitlab.com/自建）+ `Provider` 接口（`CreatePullRequest`/`MergePullRequest`）+ `GitHubProvider`/`GitLabProvider`（REST API，`net/http` 标准库）+ owner/repo 解析（参考 gitRemote.ts，含 GitLab subgroup）。
- **验证**：单测覆盖 host 检测、owner/repo 解析；可选集成测试真打 API。

### ⬜ 任务 1.5 — D3 PR：token + 端点 + 前端接线
- **文件**：`appConfig.ts`/`app_config.rs`（加 `github_token`/`gitlab_token`）+ 设置页 token UI + `service/issue_worktree.go`（CreatePullRequest + 端点 `createPr`，可选 `mergePr`）+ `router.go` 注册 + `IssueWorktreeService.ts` + `PrOpenStep.tsx`
- **当前**：PrOpenStep「引导式 compare URL」（buildCompareUrl + plugin-shell 打开），不真创建 PR。
- **目标**：token 经 appConfig（前端读 → 随 body 传 Go，Go 不持久化/不入日志）；有 token 调 `createPr` 真创建并展示返回 `prUrl`，无 token 走 `buildCompareUrl` fallback。
- **验证**：填 base/head/title → 平台真生成 PR、prUrl 可点开；无 token 回退 compare 页。

### ⬜ 任务 1.6 — D4 worktree 真清理
- **文件**：`src-server/internal/service/issue_worktree.go`（改 RemoveWorktree）
- **当前**：RemoveWorktree 仅软删（status=removed），不真删目录；前端 `useCleanupAndAdvance` 编排顺序（先 pty_stop_for_worktree 再 removeWorktree）已就位。
- **目标**：软删 + `WorktreeRemove`（真删目录 + prune）。前置约束由前端编排满足（本期 pty_stop 为 no-op 桩）。
- **验证**：「清理并完成」→ worktree 目录真删除、记录软删、推进 completed 自动归档。

### ⬜ 任务 1.7（可选/健壮性）— reconcile + getInfo
- **文件**：`service/issue_worktree.go`（Reconcile/GetInfo）+ controller/router 注册 `reconcile`/`getInfo`
- **当前**：无 reconcile/getInfo。
- **目标**：Reconcile 对比 `WorktreeList` 与 DB 标 stale（路径消失）；GetInfo 返回 `git status` 摘要。
- **验证**：手动删目录后 reconcile 标 stale；getInfo 返回脏工作区摘要。
- **注**：非模块 2 前置（模块 2 在 1.6 完成后即可启动）。

---

## 4. 设计支撑

### 4.1 `worktreeId` —— 路径派生的共享键
```
worktreeId = `${localRepositoryId}::${absWorktreePath}`
```
- git 是唯一真相源（worktree 是否存在以 `git worktree list` 为准，DB 只存元数据）。
- 路径派生、跨进程一致：Go（创建/删除/持久化）与 Rust（模块 2 绑定 PTY）无需通信即对齐。
- 代价：目录改名 → id 变，本期不支持改名（§5.3）。

### 4.2 数据模型 `t_issue_worktrees`（P1 已建好，结构不变）
```sql
CREATE TABLE t_issue_worktrees (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    worktree_id         TEXT     NOT NULL UNIQUE,        -- ${repoId}::${absPath}
    issue_id            INTEGER  NOT NULL,
    local_repository_id INTEGER  NOT NULL,
    worktree_path       TEXT     NOT NULL,
    worktree_branch     TEXT     NOT NULL,
    base_branch         TEXT     NOT NULL DEFAULT '',
    status              TEXT     NOT NULL,               -- active|stale|removed（typed enum，无默认值）
    created_at          DATETIME NOT NULL,               -- gorm 写 time.Now()（本地时间）
    deleted_at          DATETIME                          -- 软删除
);
```
- 开发流程阶段不在本表：`status` 只描述 worktree 物理生命周期，与开发阶段（stateId）正交。
- 与 issue 既有 `repository_branch` 正交：`repository_branch`=issue 关心的分支；本表=为开发创建的隔离工作区。一个 issue 可 0..N worktree（本期 UI 1:1）。
- gencode 用 `gen.FieldType("status","enums.IssueWorktreeStatus")` 映射 typed 枚举；枚举包须先于 gencode 存在。

### 4.3 落盘路径约定（per-workspace，任务 1.2 落地）
`<workspace.worktreeRoot>/<repoName>/workspace_{wid}-project_{pid}-issue_{iid}`：
- **worktreeRoot**：配在 `t_workspaces.worktree_root`（跟着工作空间走），workspace 编辑表单用目录选择器配置；为空 CreateWorktree 报错要求配置。取代原全局 appConfig 方案（Go 读不到 appConfig，per-workspace DB 字段 Go 直接查，无需环境变量/不动 Rust）。
- **repoName**：从 `t_local_repositories.remote_url` 的 `/xxx.git` 末段解析（`gitutil.RepoNameFromRemoteURL`，覆盖 SSH/HTTPS/subgroup）；remote_url 为空回退 `filepath.Base(local_dir)`。
- **末段**：`workspace_{wid}-project_{pid}-issue_{iid}`（id 段，全局唯一，取代原 issue 标题清洗——标题含中文/emoji 清洗复杂，改用稳定 id 段）。

### 4.4 Go 范式（service/controller/router）
- **gitutil**：`gitOutput(dir,args...)` = `exec.Command("git","-C",dir,args...).Output()`，**无超时、stderr 丢弃**（失败返回 `""`）。写操作需新增 error helper。**注意：原文档「`gitExecFileAsync`（超时、合并 stderr）」在本项目不存在，以代码为准。**
- **service**：嵌 `apis.Service`；`q:=query.Use(svc.Orm)`+`WithContext(svc.Context)`；事务 `svc.Orm.Transaction(func(tx *gorm.DB)error{...})` 内用 `query.Use(tx)`；错误 `gorm.ErrRecordNotFound`→`errors.New("中文")`。
- **controller**：嵌 `apis.Api`，5 步链式装配（MakeContext/Bind/Validate/MakeService/Errors → svc.Xxx → JsonOK/JsonFail）。
- **router**：`router.go:108` issueWorktreeGroup 内追加 POST（全 POST，前缀 `/api/tracker/issueWorktree`）。

### 4.5 PR 平台抽象（D3）
按「git 操作集中 Go」，PR 创建放 Go（避免 webview CORS、避免 token 落前端内存）。`githost` 包做 host 检测 + REST 调用；token 由前端从 appConfig 读后随请求传 Go（Go 不持久化）。owner/repo 从 `t_local_repositories.remote_url` 解析。

### 4.6 数据流（D1/D2/D3/D4 端到端，手动开发）
- **D1**：`createWorktree`（Go：校验→WorktreeAdd→写表）→ `move(developing)` → invalidate。
- **D2**（手动）：`openInTerminal(worktreePath)`（外部终端）→ 用户开发 → `[开发完成]` → `move(pr_open)`。
- **D3**：`createPr`（Go：githost.CreatePullRequest，无 token 走 compare URL）→ `[合并完成]` → `move(cleanup)`。
- **D4**：`pty_stop_for_worktree`（no-op）→ `removeWorktree`（Go：WorktreeRemove+软删）→ `move(completed)`。

---

## 5. 工程约束

### 5.1 删 worktree 前必须先停 PTY（forward-compat）
文件锁：PTY 进程 cwd 指向 worktree 时目录无法删除。本模块无嵌入式 PTY，`pty_stop_for_worktree` 是 no-op；模块 2 接 PTY 后变真（前端编排顺序已就位）。

### 5.2 两阶段编排的失败 reconcile
PTY 停了但 worktree 删失败 / 反之：靠 reconcile 兜底（对比 git worktree list 标 stale，清理孤儿）。

### 5.3 路径派生 ID 的改名问题（本期规避）
不支持 worktree 改名/移动，避免 worktreeId 失联。

### 5.4 时区（以代码为准，修正原文档）
app 业务时间字段走 gorm `time.Now()`（**本地时间**，非 UTC）；SQLite DSN 无 `_loc`。`t_issue_worktrees.created_at` 同。**新代码遵循本地时间，不要引入 UTC 转换**。原文档「CURRENT_TIMESTAMP 走 UTC」与实际不符，已废弃。

### 5.5 Go 私有代理白名单
本模块不引入外部 Go 库（worktree 走 `os/exec`，PR 走 `net/http` 标准库），规避 goproxy 白名单。

### 5.6 名称清洗安全
拒绝 `..`/`.`，不安全字符 collapse `-`（worktree 目录名 + 分支名都需清洗，防路径穿越与非法 ref）。

### 5.7 orca → 本项目对照（附录）
| orca | 本项目 |
|---|---|
| `gitExecFileAsync` | Go `os/exec`（gitutil） |
| `orca-data.json`+WorktreeMeta | SQLite `t_issue_worktrees` |
| `worktreeId=repoId::path` | `${repoId}::${absPath}` |
| 删 worktree 前停 PTY | 本模块 no-op，模块 2 变真 |
| PR 创建 | Go `githost` REST API |
