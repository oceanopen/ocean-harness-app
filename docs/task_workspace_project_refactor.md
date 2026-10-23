# 工作空间/项目任务重构——方案与任务清单

> 状态：实施中
> 范围：工作空间与项目两条独立实体线（工作空间→会话；项目→任务→主/子会话）、会话实体化与 claude session 锚定（终端+ACP 双受控模式）、bot 与归属实体解耦、浏览器实例归属与登录态全局化、「工作空间」「项目任务」双页面交互重构、Claude 会话查看工具
> 前序基线：ACP 统一会话方案（已全部落地：issue 级锚点 / acpsession 域 / bot ACP 驱动 / launch_settings 合并链），本次在其上重构归属模型
> **状态标记**：⬜ 待开始 | 🔲 进行中 | ✅ 已完成
> **状态回写规则（内置，无需人工提醒）**：任务开始执行时置 🔲；任务实现完成后，执行方（开发 Agent）在总结阶段直接把对应任务状态改为 ✅ 并补「实施定稿」段落（只记实施终态与相对方案的偏离，作为当前事实；不写日期、不写「何时补充/修订了什么」的演变叙述——变更历史归 git）——回写状态但不主动 git commit，提交动作必须经用户明确确认。头部「状态」总览行随之同步：首个任务开始时改为**实施中**，全部任务 ✅ 后改为**已完结**。实施中若需偏离方案，先修订本文对应设计段落再动代码。

---

## 1. 背景与动机

### 1.1 现状关键事实

| # | 事实 | 代码位置 |
|---|---|---|
| 1 | 任务必须归属工作空间（`t_workspace_issues.workspace_id NOT NULL`），任务运行目录 = `{workspace.dir}/{issueId}/`；无 DB 外键，完整性靠 service 事务级联 | `server/internal/migrations/migrations/20261019001_init_tracker.sql`、`server/internal/service/workspace_issue.go`、`issue_workspace.go` |
| 2 | 「默认会话执行模式」唯一配置通道：workspace `launch_settings` 基线 + issue 字段级覆盖；mode 四档 `none/terminal-manual/terminal-auto/acp` + `permissionMode` | `packages/web/src/shared/launchSettings.ts`、`DevWorkbenchPage.tsx` |
| 3 | ACP 会话 id ≡ claude session uuid（claude 侧会话文件按此 id 定位）——双模式统一锚的机制依据 | `server/internal/acp/client.go`（LoadSession 注释） |
| 4 | ACP 锚点机制现成：`t_issue_acp_sessions` 一 issue 一活跃锚；`Ensure → Peek → LoadSession` 即 resume，闲置回收/重建场景在用 | `server/internal/acpsession/manager.go`、`binding.go` |
| 5 | 跨重启 resume 被有意禁用：sidecar 启动 `SweepSessionIDs` 全表清零锚值（claude 会话文件本可 resume） | `server/internal/acpsession/binding.go` |
| 6 | bot 会话表粒度 = `(bot_id, conversation_key)` 一行（`single:<userid>`/`group:<chatid>`），非每消息一行；无消息表；已存双锚 `claude_session_id`（headless `--resume` 锚）+ `acp_session_id`（运行时锚）；`bound_issue_id` 任务关联锚 | `server/internal/bot/conversations.go`、`types.go` |
| 7 | bot headless：每回合新 spawn `claude -p` 进程，`--resume <id>` 续同一会话；开新会话三触发（锚空/闲置 2h/resume 失败自愈）——同一 vendored CLI 的 `--resume` 已实证 | `server/internal/bot/driver_claude.go`、`orchestrator.go` |
| 8 | bot ACP：绑定 issue → 与桌面共享同一 agent 进程/会话视图/审批；未绑定 → `conv-<行id>` 锚形独立会话，桌面不可见（「开盲盒」来源） | `server/internal/bot/driver_route.go`、`driver_acp.go`、`acpsession/binding.go` |
| 9 | bot 关联零唯一性校验：多 bot 可关联同一工作空间（面板有补绑入口），多会话可绑定同一 issue | `server/internal/service/im_bot.go` |
| 10 | 终端直启参数透传机制现成：首 token 白名单校验（仅 `claude`），余参原样透传；终端链路全仓零处 `--resume` 使用 | `app/src/pty/local_provider.rs`、`cli_bin.rs` |
| 11 | 终端 sessionId 数据源现成：claude 运行中写 `~/.claude/sessions/<pid>.json`（sessionId/cwd/status）；`claude_state.rs` 已实现 claude 进程↔pane shell 父链精确匹配（当前仅用于「运行中」判定） | `app/src/sessions/raw.rs`、`app/src/pty/claude_state.rs` |
| 12 | 消息历史 SSOT：`~/.claude/projects/<slug>/*.jsonl` 持久存在（进程死后仍在），应用未读取；ACP 视图历史随 sidecar 重启消失 | `app/src/sessions/`（仅活跃元数据）；jsonl 未消费 |
| 13 | 浏览器 profile 命名双端 SSOT：`issue-`/`workspace-` 前缀派生；单任务上限 5、全局上限 10 | `server/internal/browser/paths.go`、`packages/web/src/state/browserSession/derive.ts`、`manager.go` |
| 14 | 浏览器登录态按工作空间隔离：`auth-states/<workspaceId>.json`，注入/60s 周期导出/并集合并 | `server/internal/browser/authstate_sync.go` |
| 15 | 渠道适配器架构：渠道无关核心 + 工厂契约，现有 wecom/weixin 两适配器，飞书为预留缝；claude 引擎在核心层（headless/ACP/路由三驱动） | `server/internal/bot/channel.go`、`driver_*.go` |
| 16 | 存量 bug：工作空间目录变更触发的 bot 热更新分支先赋值后比较恒为 false，永不触发 | `server/internal/service/workspace.go` |

### 1.2 需求诉求（逐条编号）

- O1（初衷）：agent 对话（claude 会话）是第一性单位，工作空间/项目/任务只是组织容器——设计围绕此点。
- O2（原 1）：工作空间 → 对话。
- O3（原 2）：项目 → 任务 → 对话。
- O4（原 3）：工作空间与项目相互独立、无直接关系，走独立路线；场景分工 = 工作目录不变继续对话 vs 每任务不同工作目录。
- O5（原 4）：bot 与两者完全解耦，可自由关联工作空间或项目+任务；本轮做到「关联项目必选、任务可选」，自动建任务不做。
- O6（原 5）：浏览器登录态全局共用一份默认。
- O7（原 7）：拆「工作空间」「项目任务」两个独立页面。
- O8（原 8）：工作空间页交互 8.1–8.5（顶部新增/列表 icon+浮层、平铺树、配置抽屉去仓库项、标题 hover「...」菜单、折叠、会话行时间/运行态/「...」菜单）。
- O9（原 9）：项目任务页交互 9.1–9.6（同构交互、项目抽屉字段、任务抽屉字段）。
- O10（原 10）：bot 加会话类型配置（拍板：两档必填）；项目下任务非必填；问答按需配置而非硬拦截。
- O11（原 11）：bot 会话机制现状确认 + bot 产生的会话在工作空间会话列表可见（消灭开盲盒）。
- O12（原 12）：会话仅受控两模式（终端自动启动 Agent / ACP）；新建指定会话 id、点开历史自动 resume；会话历史自动拿到，应用不存储。
- O13（原 13）：任务 = 主会话 + N 子会话模型；此核心逻辑先确认可行再定其他（已确认，见 §2.2 D4–D6）。
- O14（原 14）：内容区+右侧工具条两页共用；主终端工具栏无 claude icon、无关闭按钮。
- O15（原 15）：浏览器实例改名 `workspace-<id>_session-<id>-NN` / `issue-<id>_session-<id>-NN`（01 起）；单会话上限 5→10；全局上限 10→50。
- O16（原 16）：新增「Claude 会话查看」工具项（卡片列表/实时查看/子会话拦截/关联 bot 展示）。
- O17（原 17）：bot 列表项单独一行展示关联类型+对象链。
- O18（原 18）：bot 会话表存 claude 会话 id、可倒推归属；bot 端新增会话自动 ACP（含三分支判定）。

## 2. 方案总览

### 2.1 目标模型

```
工作空间 workspace ─→ 会话 conversation ×N        （cwd = workspace.dir，工作目录不变）
项目 project ─→ 任务 task ─→ 主会话 + N 子会话     （cwd = {project.dir}/{taskId}/，每任务不同目录）

bot 配置：会话类型 ∈ {工作空间, 项目}（必填两档）
  ├ 工作空间 → workspace_id（必填）
  └ 项目     → project_id（必填）+ task_id（可选；未关联时收到消息 → 提示后拦截）

t_conversations（第一性实体，应用只存元数据）：
  id(uuid) · owner_type(workspace|task) · owner_id · title ·
  mode(terminal|acp) · agent_code（当前恒 claude，为 codex 等预留）·
  claude_session_id（统一锚，双模式同源）· parent_id（''=主会话，子会话预留）· created_at/updated_at
消息内容不落库：实时读 ~/.claude/projects/<slug>/<claude_session_id>.jsonl
```

表结构终态（就地改写基线迁移）：

- `t_projects`：同 `t_workspaces` 字段族（id/name/dir/description/emoji/launch_settings/timestamps）。
- `t_project_tasks`（原 `t_workspace_issues` 改写）：`id / project_id / name / description / type_id / launch_settings / created_at / updated_at`；`state_code、completed_at、start_date、target_date、sort_order` 全部删除（无看板，后续有需要再加列）。
- `t_project_types`（原 `t_workspace_types` 改写）：挂 `project_id`。
- 仓库关联：`t_project_local_repositories` 新建（项目↔仓库多对多）；`t_task_local_repositories`（原 `t_issue_local_repositories` 改写，含 `repository_branch`）；`t_workspace_local_repositories` 删除（工作空间去仓库化）。
- `t_workspaces`：保留，去任务归属与仓库关联；仅工作目录 + 默认会话执行模式 + 基础信息。
- `t_conversations`：如上目标模型。
- `t_im_bots`：加 `conversation_type`（`workspace|project`，保存必填两档）、`project_id`、`task_id`；`workspace_id` 保留（type=workspace 时必填）。
- `t_im_bot_conversations`：`bound_issue_id`、`acp_session_id` 由 `conversation_id` 取代（归属与锚收敛到 `t_conversations`）。
- 删除：`t_issue_acp_sessions`（锚语义并入 `t_conversations.claude_session_id`）、bot 会话表 `seen_message_ids` 之外的冗余不动。

### 2.2 技术决策记录（已定稿）

| # | 决策 | 理由 |
|---|---|---|
| D1 | 统一会话表 `t_conversations`，锚 = `claude_session_id`（终端/ACP 同源）；含 `agent_code` 为多 agent（codex 等）预留 | ACP sessionId ≡ claude session uuid 已实证；用户拍板补充 agent_code |
| D2 | 归属模型：`t_projects` 新表；任务就地改写挂 `project_id`；types 挂项目；工作空间去任务/去仓库，与项目彻底独立 | O1–O4；issueWorkspace 机制整体平移给项目 |
| D3 | 任务表瘦身边界：删 `state_code/completed_at/start_date/target_date/sort_order` 五列，仅留 `id/project_id/name/description/type_id/launch_settings/timestamps` | 用户拍板：无看板皆不需要，后续有需要再加列 |
| D4 | acpsession 锚键 issue uuid → conversation id（复用既有 `conv-<rowId>` 锚形）；删除 `t_issue_acp_sessions`；移除启动清扫，跨重启 resume 打开 | O12「点开历史自动 resume」；claude 会话文件本支持跨重启 resume |
| D5 | 启动档位收敛：`none / terminal-auto / acp` 三配置档（none = 打开时临场两选一面板）；`terminal-manual` 移除；主终端工具栏去 claude 启动按钮与关闭按钮；附加 pane 保留裸 shell 无 claude 按钮 | 用户拍板保留 none 过渡；堵住无锚会话旁路（O14） |
| D6 | 终端模式锚定三段：`claude_state.rs` 父链匹配升级返回 sessionId → fresh spawn 后捕获回写锚 → 仅 fresh spawn 注入 `--resume <id>`（参数透传现成，零 Rust 解析改动）；cwd 由归属实体派生保证 resume 目录一致 | vendored CLI（2.1.300）交互式 `--resume` 与 `--session-id` 已实证（见 T0.1） |
| D7 | 消息内容/最新消息时间：Rust 新增 jsonl 只读命令（cwd→slug 单点映射、宽松解析、增量读）；应用不存消息 | 用户拍板「元数据入库+内容直读」 |
| D8 | bot 配置：`conversation_type` 必填两档；type=workspace → `workspace_id` 必填，type=project → `project_id` 必填、`task_id` 可选；type=project 未关联任务收到消息 → 回复提示后拦截 | 用户拍板（去 0 档、提示后拦截） |
| D9 | bot 驱动收敛 ACP 单驱动（headless 路由下线）；路由三分支：workspace → 取/建该 bot 在该工作空间的 ACP 会话；task 主会话 ACP → 复用；主会话非 ACP → 新建 ACP 子会话；无主会话 → 新建 ACP 主会话 | O18.2；消息同步仅 ACP 可达 |
| D10 | `t_im_bot_conversations` 加 `conversation_id`（取代 bound_issue_id/acp_session_id 双锚）；bot↔会话不设唯一性互斥，展示全部关联 bot | 现状零校验 + O16「展示当前管理的 bot」 |
| D11 | 浏览器：登录态全局一份 `auth-states/default.json`；profile = `workspace-<wsId>_session-<convId>-NN` / `issue-<taskId>_session-<convId>-NN`，槽 01–10（单会话上限 10），全局上限 50 | O6、O15 |
| D12 | 子会话仅「Claude 会话查看」工具可见可实时查看；查看工具内「新增子会话」按钮拦截提示；任务页不加子会话切换器 | 用户拍板 |
| D13 | 迁移就地改写基线 SQL（不新建 goose 迁移、不考虑历史数据）；顺带修 `workspace.go` 目录热更新死分支 | 项目既定惯例；探索发现 |

### 2.3 会话生命周期固定时序

**应用端新建会话**（对齐编码规则 1，逐项定时序）：

1. 新建入口（工作空间标题菜单/任务自动进入）→ `conversation.create`（owner、title 默认「新会话」、mode 取归属实体 `mergeLaunchSettings` 结果、agent_code 取 catalog 默认）。
2. 进入内容区，按 `mode` 分流：`acp` → `Ensure(conversationId)` 拉起，`session.ID()` 回写 `claude_session_id`；`terminal-auto` → 先测量后 spawn，裸启 claude（无锚）。
3. 终端捕获：claude 进程经 sessions 发现 + 父链匹配回读 sessionId → 一次性回写 `conversation.claude_session_id`（pid 匹配防竞态，不覆盖非空锚）。

**点开历史会话**：

1. 读 conversation 行（title/mode/claude_session_id/归属 cwd）。
2. `acp`：`Ensure` Peek 锚 → 非空 `LoadSession`（失败降级 `NewSession` 并显式提示历史不可恢复，见 R2）。
3. `terminal`：锚非空且 fresh spawn → 以实测尺寸 spawn `claude --resume <id>`；锚空 → 裸启并走捕获；活会话/reattach → 仅订阅回放 scrollback，绝不重启。

**bot 消息路由**（D8+D9）：

1. 定位 bot conversation 行 → `conversation_type` 分支。
2. workspace → 取/建该 bot 在该工作空间下的 conversation（恒 ACP）。
3. project：`task_id` 空 → 回复提示后拦截；有 task → 主会话 ACP 则复用 / 主会话非 ACP 则新建 ACP 子会话 / 无主会话则新建 ACP 主会话。
4. 回合经 acpsession `PromptQueued` 跨入口排队互斥（桌面/bot 同会话同审批，机制现成）。

## 3. 风险与开放问题

- R1 已消解：交互式 `--resume <id>` 与 `--session-id <uuid>` 在 vendored claude（CLI 2.1.300）pty 交互链路实证可用，暗号问答判据确认上下文真实恢复；`--session-id` 建会话以指定 id 落盘且可再 resume——终端新建可显式指定 id，捕获段降级为兜底。
- R2 `LoadSession` 失败现静默降级 `NewSession`——本次改为显式提示「历史不可恢复」，避免无感丢历史。
- R3 终端模式 resume 后 scrollback 为空是物理事实（ring buffer 不跨进程，CLI 仅打 resume 横幅）——历史回看一律走 D7 jsonl 直读，终端区只呈现 resume 后新内容。
- R4 jsonl 为 claude CLI 内部格式，版本升级可能漂移——只读 + 宽松解析 + 失败降级空态，不做强依赖。
- R5 裸 shell 手敲 claude / 外部终端旁路会话仍会存在：无锚不进会话列表（可按 cwd 事后认领展示，本期不做，开放）。
- R6 headless 驱动下线为删码动作，回退路径 = git 历史；如需双驱动仅恢复 route 分支。
- R7 MCP `issue_*` 工具的消费方（工作空间内 Skill）随域改名需同步——是否保留旧名兼容一版，实施时定。

## 4. 任务清单（进度 SSOT）

> 任务粒度：模块 + 功能 + 技术方案，一个任务 ≈ 一次可独立验收的交付
> 执行顺序：T0.1 → T0.2 → …（阶段间按依赖排列，阶段内按序）

### 阶段 0：地基与前置验证

#### T0.1 vendored claude 交互式 resume 能力验证
**状态**：✅
**功能**：实证交互式终端模式 `--resume <id>` 在 vendored claude 可用，顺带探测 `--session-id` 可用性。
**技术方案**：经 pty 直启链路以 vendored 二进制手工/脚本验证；结论回写 §3 R1 与 D6。
**依赖**：无
**验收**：结论落文档；若不可用则 D6 修订为「spawn 后捕获」单路径再开工。
**实施定稿**：vendored claude（adapter 0.93.0 / CLI 2.1.300）经 pty 脚本交互链路实证：新会话投递暗号后 `--resume <id>` 重启追问答出暗号原文（上下文真实恢复，非仅启动不报错）；`--session-id <uuid>` 建会话即以该 id 落盘 jsonl 且可再 resume。D6 主路径「spawn 后捕获」维持不变，终端新建显式指定 id 为可选增强路径。环境发现：宿主进程泄漏的 `CLAUDE_CODE_*` 环境变量会令 CLI 关闭 transcript 落盘（子会话标记语义），app 自有 spawn 链无此变量、不受影响；手工复验时需清洗该族变量。

#### T0.2 就地迁移改写：项目/任务/会话/bot 表
**状态**：✅
**功能**：一次迁移完成 §2.1 全部表结构重构。
**技术方案**：改写 `20261019001_init_tracker.sql`——新增 `t_projects`、`t_conversations`；`t_workspace_issues`→`t_project_tasks`（`workspace_id`→`project_id`，删五列见 D3）；`t_workspace_types`→`t_project_types`；仓库关联三表调整见 §2.1；删 `t_issue_acp_sessions`；`t_im_bots` 加三列；`t_im_bot_conversations` 加 `conversation_id` 删双锚列；`pnpm server:gorm:gen` 重生成 DO 层。
**依赖**：无
**验收**：`pnpm server:gorm:gen` 通过；`pnpm server:test` 构建启动正常；DO 层无旧表残留引用。
**实施定稿**：全部 4 个迁移文件合并为单文件基线 `20260728001_init.sql`（10 张终态表，用户拍板「SQL 整理为一个初始化 SQL、直接重置不考虑历史数据」）。编译适配口径（用户拍板）：最小编译适配——被删锚载体（`t_issue_acp_sessions` 与 `acp_session_id` 列）的读写点窄桩化（Peek 恒空/写删 no-op），`SweepSessionIDs` 启动清扫按 D4 移除；Move/bindIssue 端点与五状态枚举直接移除；bot 新三列仅透传持久化（`conversationType` 缺省空值哨兵，D8 校验归 T4.1）；任务仓库归属校验暂松为仓库存在性校验（T3.2 恢复项目关联集合校验）。连带吸收 T0.3 部分 scope：`workspace.go` 的 issue/仓库级联删码与 D13 死分支 bug（重写中自然修复，目录变更热更新已正确）在本次完成，T0.3 余量 = 前端 `WorkspaceDrawer` 去仓库选择。gen 管线注意：gormgen 编译图经 `global` 传导至全部 internal 包，重生成前须先清空 `internal/dal` 生成物且模块不可有半改状态（实操：摘 `global.go` 的 acpsession/bot 单例边跑完再还原）。验收：gorm:gen 幂等、`go vet`/`go test`（19 包）全绿、dev 二进制空库启动 goose 单版本 `20260728001` 一次应用且 10 表建成、接口探活正常。

#### T0.3 工作空间域瘦身
**状态**：✅
**功能**：工作空间去任务归属、去仓库关联，抽屉字段收敛为工作目录+默认会话执行模式+基础信息。
**技术方案**：`server/internal/service/workspace.go` 删 issue/仓库级联逻辑与 `t_workspace_local_repositories` 引用；修 D13 死分支 bug；前端 `WorkspaceDrawer` 去仓库选择。
**依赖**：T0.2
**验收**：工作空间 CRUD 正常；编辑/删除不再触碰任务与仓库；目录变更后关联 bot 正确热更新。
**实施定稿**：后端部分（workspace.go 级联删码、`t_workspace_local_repositories` 引用清零、D13 死分支修复）已随 T0.2 编译适配完成，本任务余量纯前端：`WorkspaceService` 三处 `localRepositoryIds` 字段删除、`WorkspaceDrawer` 去仓库多选块（收敛为基础信息+启动设置）、任务抽屉仓库下拉选项源改全部本地仓库（create 默认不预派生仓库行，归属过滤随 T3.2 项目关联接入收窄）、空态文案改「暂无本地仓库」语义（zh/en）。`workspace.add.repositories*` 三个死 i18n 键留待 T9.1 残留清零。验收：web:lint + web:build 全绿。

### 阶段 1：后端会话域

#### T1.1 conversation CRUD 与前端状态域
**状态**：⬜
**功能**：会话实体增删改查全链路。
**技术方案**：`/api/conversation/{getList,getInfo,create,update,delete}`（tracker 范式，按 owner 过滤、update 支持改标题）；`packages/web/src/services/ConversationService.ts` + `state/conversation/`（store/keys/queries）。
**依赖**：T0.2
**验收**：按工作空间/任务过滤列表、创建、改名、删除（仅删行不动 claude 文件）可用。

#### T1.2 acpsession 锚改造：conversation 键 + 跨重启 resume
**状态**：⬜
**功能**：ACP 会话锚从 issue 迁移到 conversation，跨重启可恢复。
**技术方案**：`acpsession/binding.go` 锚键改 conversation id（复用 conv- 形）；`manager.go` `Ensure` 入参改 conversation 上下文（cwd：workspace 会话=`ws.dir`，task 会话=`{project.dir}/{taskId}/`）；删 `SweepSessionIDs` 清扫；删 `t_issue_acp_sessions` 读写点。
**依赖**：T0.2、T1.1
**验收**：ACP 会话经锚 resume；sidecar 重启后点开历史会话恢复同一 claude 会话；闲置回收/重建链路回归通过。

#### T1.3 会话消息 jsonl 直读命令
**状态**：⬜
**功能**：从 claude 本地会话文件直读消息内容与最新消息时间。
**技术方案**：Rust 新 command：cwd→slug 单点映射 + `~/.projects/<slug>/<sessionId>.jsonl` 宽松解析 + 增量读；失败降级空态（R4）；`pnpm gen:bindings`。
**依赖**：T1.1
**验收**：给定 conversation 可取历史消息与最新消息时间；进程死后的历史会话仍可读。

### 阶段 2：终端模式锚定

#### T2.1 终端 sessionId 捕获
**状态**：⬜
**功能**：终端模式 fresh spawn 的 claude 会话自动回写锚。
**技术方案**：`claude_state.rs` 父链匹配升级为返回 sessionId；新增/扩展 command 供 pane 查询；按 §2.3 时序一次性回写（pid 匹配、不覆盖非空锚）。
**依赖**：T0.1、T1.1
**验收**：terminal-auto 新建会话后 conversation 行持有 claude_session_id。

#### T2.2 终端 resume 注入
**状态**：⬜
**功能**：点开历史终端会话自动 `--resume`。
**技术方案**：会话打开编排（`usePtySession` 扩展）——锚非空且 fresh spawn 时 directCommand 拼 `--resume <id>`；reattach/活会话不注入；cwd 与锚归属一致断言。
**依赖**：T2.1
**验收**：点开历史会话恢复同一 claude 会话（上下文延续）；运行中会话重进仅 reattach 不重启。

#### T2.3 启动档位收敛与工具栏改造
**状态**：⬜
**功能**：受控两档 + none 临场面板；主终端无 claude icon、无关闭按钮。
**技术方案**：launch settings mode 档位调整（D5）；`LaunchModePicker` 两选一；主 pane 工具栏移除「启动 claude」按钮与关闭按钮；附加 pane 保留裸 shell。
**依赖**：T2.2
**验收**：工作台内不存在无锚 claude 启动路径；主终端工具栏符合 O14。

### 阶段 3：项目域后端

#### T3.1 项目 CRUD 与仓库关联
**状态**：⬜
**功能**：项目实体全链路（对齐 workspace 服务范式）。
**技术方案**：`/api/project/*` + `service/project.go`（目录校验、仓库关联全量替换、删除级联）；types 挂项目；前端 `ProjectService.ts` + state 域。
**依赖**：T0.2
**验收**：项目 CRUD/仓库多选关联/类型管理可用。

#### T3.2 任务域平移与 issueWorkspace 项目化
**状态**：⬜
**功能**：任务 CRUD 与运行目录机制整体迁移到项目维度。
**技术方案**：`workspace_issue.go`→`project_task.go`（校验仓库属项目关联集合）；`issue_workspace*` 平移：目录 = `{project.dir}/{taskId}/`，init/archive/runner 解析链改两跳 project；archive 与状态流转解耦（状态列已删）。
**依赖**：T3.1
**验收**：任务初始化/归档/取消全流程在新目录布局下可用；仓库分支关联校验正确。

#### T3.3 MCP 工具改造
**状态**：⬜
**功能**：工作空间内 Skill 可读写项目/任务。
**技术方案**：`mcp_ocean_harness_tools.go` 的 `issue_*`/`workspace_*` 改造为 project/task 语义（旧名兼容与否按 R7 实施时定）。
**依赖**：T3.2
**验收**：MCP 调用 project/task 工具返回正确数据。

### 阶段 4：bot 解耦与会话打通

#### T4.1 bot 配置改造
**状态**：⬜
**功能**：会话类型两档必填 + 条件必填字段 + 抽屉级联。
**技术方案**：`ImBotCreateRequest` 等加 `conversation_type/project_id/task_id` 及保存校验（D8）；`ImBotDrawer` 类型下拉联动工作空间/项目（项目时展示任务列表可关联，任务非必填可保存）。
**依赖**：T0.2、T3.1
**验收**：两档配置/切换/保存校验符合 D8；项目档不选任务可保存。

#### T4.2 bot 驱动收敛 ACP 与路由三分支
**状态**：⬜
**功能**：bot 消息一律 ACP；按 D9 三分支路由。
**技术方案**：`driver_route.go` 判定收敛（launch mode 不再分流 headless）；headless 驱动下线（R6）；未关联任务拦截提示文案（D8）；回合排队沿用 `PromptQueued`。
**依赖**：T1.2、T4.1
**验收**：三分支行为与 §2.3 时序一致；headless 路径无残留调用。

#### T4.3 bot 会话↔conversation 打通
**状态**：⬜
**功能**：bot 产生的会话全部进会话列表，归属可倒推。
**技术方案**：bot conversation 行读写 `conversation_id`（首条消息按 D9 定位/创建）；删 `bound_issue_id/acp_session_id` 读写点；`ImBotPanel` 改为「当前会话关联 bot」展示。
**依赖**：T4.2
**验收**：bot 发消息后，工作空间/项目任务页对应会话可见可查消息；多 bot 关联同一会话互不干扰。

### 阶段 5：页面骨架与工作空间页

#### T5.1 双页路由与内容区组件抽取
**状态**：⬜
**功能**：「工作空间」「项目任务」两个独立页面；内容区+右侧工具条抽为共用组件。
**技术方案**：`routes.ts` 加 `/workspaces`、`/projects`；`DevWorkbenchPage` 中栏+工具条抽为 `WorkbenchContent` 共用组件（含 `WorkspaceInitGate`/`TerminalLaunchFlow` 编排）；菜单项替换。
**依赖**：T1.1、T2.3
**验收**：两页可导航；共用内容区在两页行为一致。

#### T5.2 工作空间页左树
**状态**：⬜
**功能**：O8 全部交互（8.1–8.5）。
**技术方案**：左树组件（顶部新增/列表 icon+浮层复用 `WorkspacesView` 交互、平铺树、折叠、标题 hover「...」菜单=编辑工作空间/新建会话、会话行=标题+时间/运行态+「...」菜单=改标题/删除）；会话时间取 T1.3 最新消息时间，运行态取 claude 活跃状态。
**依赖**：T5.1、T1.1、T1.3
**验收**：交互逐项对照 O8 通过；px 字符串/注释规范达标。

#### T5.3 会话内容区编排接入（两页共用）
**状态**：⬜
**功能**：新建/点开会话按 §2.3 时序进入终端或 ACP 视图。
**技术方案**：`WorkbenchContent` 按 conversation.mode 分流：terminal → spawn/resume 编排（T2.2）；acp → `Ensure`+`AcpSessionView`；历史消息经 T1.3 直读呈现。
**依赖**：T5.2、T2.1、T2.2、T1.2、T1.3
**验收**：新建→锚回写、重开→resume、消息历史可见三条链路全通。

### 阶段 6：项目任务页

#### T6.1 项目任务页左树与抽屉
**状态**：⬜
**功能**：O9 全部交互（9.1–9.6）。
**技术方案**：左树与 T5.2 同构（场景参数定制，参照共享组件场景参数约定）；项目抽屉（id/名称/dir/emoji/描述/关联仓库多选/默认执行模式，字段族与工作空间抽屉复用）；任务抽屉（所属项目/任务id/名称/Markdown 内容/任务类型/默认执行模式/关联仓库+分支，无状态无日期）。
**依赖**：T5.2、T3.1、T3.2
**验收**：交互逐项对照 O9 通过；任务抽屉字段与 D3 一致。

#### T6.2 任务主会话编排
**状态**：⬜
**功能**：点击任务自动进入主会话（无则按合并 launchSettings 默认档创建）。
**技术方案**：进入任务 → 查主会话（owner=task 且主会话）→ 无则 `conversation.create` → 走 T5.3 编排。
**依赖**：T6.1、T5.3
**验收**：点任务稳定进入同一主会话；首次进入自动建会话并锚定。

### 阶段 7：浏览器实例

#### T7.1 实例命名与上限改造
**状态**：⬜
**功能**：profile 命名改会话维度，槽位 10、全局 50。
**技术方案**：`browser/paths.go` + `browserSession/derive.ts` 双端 SSOT 同步改（D11 格式与解析）；`MaxSessionsPerTask`=10（按 conversation 配额归组）、`maxConcurrentProfiles`=50；MCP `resolveProfile` 归属参数改 conversationId。
**依赖**：T1.1
**验收**：新旧命名解析按新规则；超限报错文案准确；面板过滤/开新实例走最低空闲槽。

#### T7.2 登录态全局化
**状态**：⬜
**功能**：登录一次全局共用。
**技术方案**：`auth-states/default.json` 单份；`resolveWorkspaceID` 归属链删除，注入/导出/合并机制不变；面板归属徽标按会话展示。
**依赖**：T7.1
**验收**：任一会话登录后全部实例共享；注入/导出回归通过。

### 阶段 8：可视化与展示

#### T8.1 Claude 会话查看工具
**状态**：⬜
**功能**：O16 全部（卡片列表/实时查看/新增子会话拦截/关联 bot 展示）。
**技术方案**：`toolRegistry` 新增 claudeSessions 工具；卡片=conversation 行 join claude 活跃状态 + 关联 bot（查 `t_im_bot_conversations`）；点开实时查看：acp → `AcpSessionView` 只读订阅，terminal → 终端只读 attach + jsonl 历史；「新增子会话」按钮拦截提示（D12）。
**依赖**：T4.3、T5.3
**验收**：卡片/实时查看/拦截/ bot 展示逐项对照 O16 通过。

#### T8.2 bot 列表关联信息行
**状态**：⬜
**功能**：O17 关联类型+对象链展示。
**技术方案**：`ImBotsPage` 卡片加一行：工作空间档=类型+工作空间+会话；项目档=类型+项目+任务（如有）+会话。
**依赖**：T4.3
**验收**：两档展示正确，未关联任务时明确留空。

### 阶段 9：收尾

#### T9.1 清理与全链路回归
**状态**：⬜
**功能**：旧世界残留清零 + 端到端验证。
**技术方案**：删旧 TrackerPage/DevWorkbenchPage 页面壳与死代码、五状态枚举（`stateMeta.ts`/Go `enums`）残留、`t_issue_acp_sessions` 读写点复查；全链路验证矩阵：工作空间会话新建→跨重启 resume、项目任务主/子会话、bot 两类型三分支路由、浏览器命名/上限/登录态、`pnpm web:lint/web:test/web:build` 全绿。
**依赖**：T8.1、T8.2
**验收**：验证矩阵逐项通过；无旧表/旧页/死枚举残留。
