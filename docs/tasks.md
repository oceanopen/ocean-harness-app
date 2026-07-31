# 工作空间 / 项目 / Issue 管理 — 开发任务清单

> 控制台新功能：实现个人工作场景下的「工作空间 → 项目 → Issue」三级管理。
> 参考项目：`~/Project/plane`（仅借鉴交互、表结构、逻辑；**砍掉成员/角色/权限/邀请/计费/license**）。
> 后端落在 `src-server/`（go-server），前端新建独立窗口调接口实现 CRUD。

---

## 一、背景与目标

- **个人场景**：单用户，无成员管理、无权限校验。
- **三级模型**：workspace（顶层容器）→ project（项目）→ issue（核心工作项）。
- **体验对齐 plane**：项目卡片网格、issue 列表分组/排序/筛选、侧滑详情编辑。MVP 先打通主链路，高级特性（看板/拖拽/富筛选/富文本/命令面板/gantt）列入后续迭代。
- **技术栈**：后端 gin 分层 + gorm/gen（DO 自动生成）+ goose（迁移）+ sqlite；前端 React + MUI v9（专项库按需引入）+ i18next。

---

## 二、技术方案（架构概要）

### 2.1 数据流

独立 `tracker` 窗口 → **fetch 直连 go-server**（地址从 `commands.httpServerStatus()` 取，不硬编码端口）→ gin 分层 `router → controller → service → gormgen(DO) → sqlite`。
迁移 goose 建表（跨机器自动），`gencode` 脚本基于当前 sqlite 生成 DO（手动触发），两层职责分离、互不冲突。

### 2.2 表结构（SQLite · 自增整数主键 · 软删除，MVP 6 表）

> 命名约定：业务表统一 `t_` 前缀；索引唯一用 `udx_`、普通用 `idx_`（详见 2.3）。

| 表 | 关键字段 | 说明 |
|---|---|---|
| `t_workspaces` | id, name, slug(全局唯一 udx), description, 时间戳, deleted_at | 顶层容器（顶级，表名保持） |
| `t_workspace_projects` | id, workspace_id, name, description, emoji, default_state_id, 时间戳, deleted_at | 项目，所属 workspace；允许重名（无短码、无业务唯一键），issue 用全局自增 id 标识 |
| `t_project_states` | id, project_id, workspace_id, name, color, slug, state_group, sort_order, is_default, is_triage, 时间戳, deleted_at | 状态，所属 project；state_group ∈ backlog/unstarted/started/completed/cancelled |
| `t_project_issues` | id, project_id, workspace_id, name, description, state_id, priority, sort_order, parent_id, start_date, target_date, completed_at, is_draft, 时间戳, deleted_at | issue，所属 project；issue 用全局自增 `id` 标识（无 issue key、无独立 sequence_id）；priority ∈ urgent/high/medium/low/none |
| `t_workspace_labels` | id, workspace_id, name, color, description, sort_order, 时间戳, deleted_at | 标签，所属 workspace；所有项目共享一套通用标签（无 project 级归属） |
| `t_issue_labels` | id, issue_id, label_id, created_at, updated_at, deleted_at | 关联表，所属 issue；udx(issue_id, label_id) 全局唯一；软删除与其他表统一 |

- 软删除：保留 `deleted_at`，`gencode` 配置映射 `gorm.DeletedAt`，查询自动过滤。
- 唯一索引：**全局唯一**（不带 `WHERE deleted_at IS NULL`），即已删除记录仍占用唯一键——配合「恢复式创建」语义（见下）；仅 `workspace(slug)` 与 `t_issue_labels(issue_id,label_id)` 两处有，**project 无业务唯一键（允许重名）**。
- issue 标识：直接用全局自增 `id`（如 `42`），**无 issue key、无独立 sequence_id**——个人场景项目不多，省去短码/计数逻辑。
- 枚举字段：`state_group`/`priority` 为 typed 枚举（`enums.StateGroup`/`enums.Priority`），`is_default`/`is_triage`/`is_draft` 为 `enums.YesNo`（"Y"/"N"，替代 bool）；DB 均为 TEXT、**无默认值**（`sort_order` 同样无默认），一律由代码/前端显式赋值，漏传由 `Value()` 硬错拦下（见 `internal/dal/enums/`）。
- 关联查询：**无 DB 外键约束**，跨表关联一律走 SQL JOIN / 应用层组装；数据级联清理由 service 层手动。

#### 创建逻辑（恢复式 upsert）

针对带业务唯一索引的实体（**workspace by slug**、`t_issue_labels` toggle），create 不直接 INSERT，而是先按唯一键查（**含已软删除记录**，即 `Unscoped` 查询）：

- **存在未删除同键记录** → 返回报错「记录重复」；
- **存在已删除同键记录** → 恢复并重置该行：`deleted_at = NULL` + `updated_at = now` + 业务字段全部用新入参覆盖，**固定列保留**（`id` + `created_at` 不变）；
- **不存在** → 正常 INSERT。

> 适用范围：workspace（slug）、`t_issue_labels`（toggle 语义：未删→软删取消、已删→恢复、无→插入）。**project / t_project_states / t_workspace_labels / t_project_issues 无业务唯一索引，正常创建（project 允许重名）。**

### 2.3 命名规范（全程一致）

- **实体术语**：`workspace / project / projectIssue / state / workspaceLabel` —— types/service/controller/router/前端/DB 全程同一词。
- **公共字段**：`id, name, description, createdAt, updatedAt, deletedAt`。
- **业务字段**：`workspaceId, projectId, slug, stateId, stateGroup, priority, sortOrder, parentId, startDate, targetDate, completedAt, isDraft, isDefault, isTriage, emoji, color`。JSON 全 camelCase。
- **枚举**：`priority = urgent|high|medium|low|none`；`stateGroup = backlog|unstarted|started|completed|cancelled`。
- **表名**：业务表统一 `t_` 前缀 + 所属关系（顶级 `t_workspaces` 保持；子表以直接父单数作前缀：`t_workspace_projects` / `t_project_states` / `t_project_issues` / `t_workspace_labels` / `t_issue_labels`），与系统表（`goose_db_version` 等）区分。
- **索引名**：唯一索引 `udx_{表名去t_}_{列名}`（如 `udx_workspaces_slug`、`udx_issue_labels_issue_id_label_id`），全局唯一；普通索引本期暂不建（数据量小，后续按查询热点以 `idx_{表名去t_}_{列名}` 追加）。
- **各层后缀**（沿用 README）：`XxxRequest`/`XxxResponseData`（types）、service/controller 同名、`XxxDO`（gen 生成的数据库实体 = PO 层）。

### 2.4 gorm/gen 自动生成

- 独立程序 `src-server/cmd/gormgen/`（`main.go` + `init_gen.go` + `gen_model_tracker.go`）：复用服务 initialize 序列（config → zap → sqlite → goose 迁移）确保表就绪 → `gen.NewGenerator` → 对 6 表 `GenerateModelAs`（单数无 `t_` 前缀）→ 配置字段类型映射（`deleted_at→gorm.DeletedAt`、`is_*→bool`、sqlite `INTEGER→int`/`REAL→float64`、JSON tag 小驼峰；不开 `FieldNullable` 以免 PK 被指针化）→ query 层输出到 `src-server/internal/gormgen/`（含 `gen.go` 的 `Use`/`WithContext`），PO 结构体输出到 `src-server/internal/gormgen/model/`。
- 运行：`pnpm server:gorm:gen`（等价 `cd src-server && go run ./cmd/gormgen -config config/settings.dev.yaml`，迁移 + 生成一气呵成）；首次引入依赖需 `GOPROXY=https://goproxy.cn,direct go -C src-server mod tidy`（默认 goproxy.weoa.com 为白名单代理）。
- service 层用 `gormgen.Use(global.SqliteDB).Xxx.WithContext(ctx).Create/First/Find/Save/Delete` 做 CRUD（不生成全局 Q/SetDefault，每次调用 `Use`）。
- 流程：改迁移 → 跑 `pnpm server:gorm:gen`（goose 自动建表 + 重新生成 DO）。
- 依赖锁定：`gorm v1.25.12` + `gen v0.3.28` + `dbresolver v1.5.3`（gen 生态不支持 gorm v1.31；v0.3.28 修复 sqlite 下 `ScanType` 为空导致生成期 panic）。

### 2.5 接口（action 风格 `/api/<module>/<action>`，GET 查 / POST 写）

- `workspace`: list / get / create / update / delete
- `project`: list?workspaceId / get / create(**事务内种 5 默认 state + 回填 default_state_id**) / update / delete(**级联清 state/issue**)
- `state`: list?projectId / create / update / delete / reorder
- `projectIssue`: list?projectId(orderBy/筛选，扁平列表前端分组) / get / create(**默认 state 取 project.default_state_id、sort_order 自算**) / update(**stateId 变化触发 completed_at 流转**) / delete(**级联清 issue 关联**)
- `workspaceLabel`: list?workspaceId / get / create(**sort_order 自算**) / update / delete(**级联清 issue 关联**) / toggleIssue(**恢复式 upsert，返回 issue 的 label 列表**)

### 2.6 前端 tracker 窗口（新建独立窗口）

- `src/windows/tracker/`：`main.tsx` + `TrackerApp.tsx`(三级导航根) + 页面 + `components/`。
- `src/windows/tracker/api.ts`：fetch 封装 + `ApiResponse<T>` + 地址取自 `httpServerStatus`。
- i18n：`src/shared/i18n/locales/{zh-CN,en}/tracker.json` + 注册到 `index.ts`。
- 窗口注册：`tracker.html` + `vite.config.ts` 多入口 + Rust 开窗 command（`showTrackerWindow`，`WebviewWindowBuilder` 动态创建，**不进 `tauri.conf.json`**，`app.windows:[]`）+ `gen:bindings` + panel 顶栏入口按钮。

---

## 三、任务清单

> 执行约定：**每次执行一个任务，用户手动提交后执行下一个**。完成的任务将下方 `[ ]` 改为 `[x]`。

### 阶段 A：后端基座

- [x] **任务 1：[后端·迁移] 建 MVP 6 张业务表**
  - 文件：`src-server/internal/migrations/migrations/20260730001_init_tracker.sql`（新增）
  - 当前：仅有 `20260728001_init.sql`（空迁移验证 goose 机制）
  - 目标：新增 `-- +goose Up` 迁移，建带层级前缀的 6 表（t_workspaces / t_workspace_projects / t_project_states / t_project_issues / t_workspace_labels / t_issue_labels，自增主键 + 软删除 deleted_at + 时间戳）；唯一索引 `udx_` 前缀且**全局唯一**（udx_workspaces_slug、udx_issue_labels_issue_id_label_id，**不带 WHERE deleted_at IS NULL**）；普通索引本期暂不建（数据量小，按需追加）；**不建任何 DB 外键**（跨表关联走 JOIN）；t_project_states 默认 state_group='backlog'、t_project_issues 默认 priority='none'/sort_order=65535。

- [x] **任务 2：[后端·ORM] 引入 gorm/gen + 自动生成脚本 + 生成 DO**
  - 文件：`src-server/go.mod`（修改，加 `gorm.io/gen` 生成期依赖）、`src-server/cmd/gencode/main.go`（新增）、`src-server/internal/model/gen/`（新增，生成产物）、`src-server/README.md`（修改，命名表补 `DO` 后缀 + gencode 说明）
  - 当前：未引入 gorm/gen，无 model 层；go.mod 仅 gorm + glebarez/sqlite + goose
  - 目标：新增 gencode 程序，连当前 sqlite（db 路径取自 config/环境变量），用 gen 对 6 表生成 DO（配置 deleted_at→gorm.DeletedAt、is_*→bool、JSON tag camelCase、表名映射），输出到 `internal/model/gen/`；附运行说明（GOPROXY=goproxy.cn）。先跑任务1迁移建表，再跑 gencode 生成 DO。

### 阶段 B：后端业务模块

- [x] **任务 3：[后端·workspace] 工作空间模块**
  - 文件：`src-server/internal/types/workspace.go`、`service/workspace.go`、`controller/workspace.go`（新增）、`router/router.go`（修改）
  - 当前：无 workspace 模块
  - 目标：实现 workspace CRUD（list/get/create/update/delete）；create 采用**恢复式 upsert**（按 slug 含软删除记录 `Unscoped` 查询：未删除同 slug→报错「记录重复」；已删除同 slug→恢复 `deleted_at=NULL`+`updated_at=now`+业务字段覆盖、保留 `id`+`created_at`；不存在→插入）；update 校验 slug 未删除唯一；软删除；types 用 `WorkspaceCreateRequest`/`WorkspaceResponseData` 等后缀；service 用 `gormgen.Use(global.SqliteDB).Workspace.WithContext(ctx)`；controller 用 `response.OK/Fail`；router 注册 `/api/workspace/*`。

- [x] **任务 4：[后端·state] 状态模块（含默认状态种子）**
  - 文件：`src-server/internal/types/state.go`、`service/state.go`、`controller/state.go`（新增）、`router/router.go`（修改）
  - 当前：无 state 模块
  - 目标：state CRUD（按 projectId 查）+ reorder（批量调 sort_order）；定义 `DefaultStates` 常量（Backlog/Todo/In Progress/Done/Cancelled 五个，含 state_group + color + sort_order + is_default），供 project 创建时调用种子函数 `SeedDefaultStates(tx, projectID, workspaceID)`。

- [x] **任务 5：[后端·project] 项目模块（create 种默认状态）**
  - 文件：`src-server/internal/dal/types/project.go`、`service/project.go`、`controller/project.go`（新增）、`router/router.go`（修改）；回改迁移 `migrations/migrations/20260730001_init_tracker.sql`（去 identifier 列与唯一索引）+ 重跑 `gorm:gen` 重生成 DO
  - 当前：无 project 模块
  - 目标：project CRUD（按 workspaceId 查）；**去 identifier/issue_key 简化**——project 允许重名、无业务唯一键，create 为普通插入（无 upsert），事务内：插入 project → `SeedDefaultStates` 种 5 默认状态 → 回填 `default_state_id`（取 is_default 那条）；update 仅改 name/description/emoji；软删除事务内级联清理其下 state/issue（issue_labels 留给 label/issue 模块）。
  - 设计变更：原计划 identifier 短码 + issue key=`{identifier}-{id}`，经讨论改为去 identifier、issue 用全局自增 id 标识（个人场景简化）。

- [x] **任务 6：[后端·workspaceLabel] 标签模块（含 issue 关联）**
  - 文件：`src-server/internal/dal/types/workspace_label.go`、`service/workspace_label.go`、`controller/workspace_label.go`（新增）、`router/router.go`（修改）；回改迁移 `migrations/migrations/20260730001_init_tracker.sql`（去 `t_workspace_labels.project_id`）+ 重跑 `gorm:gen` 重生成 DO
  - 当前：无 label 模块
  - 目标：workspaceLabel CRUD（按 workspaceId 查，**去 project_id、所有项目共享一套标签**）；create `sort_order` 后端自算（同 workspace MAX+10000）；delete 事务内级联软删 `t_issue_labels` 里该 label 的关联；`toggleIssue` 恢复式 upsert（未删→软删取消、已删→恢复、无→插入）→ 返回该 issue 的 label 列表。命名全程 `workspaceLabel`（对齐 DO 名 `WorkspaceLabel`/表 `t_workspace_labels`，与 projectState 模块惯例一致）。
  - 设计变更：原计划 label 两级归属（workspace 级 + project 级），经讨论改为去 project_id、label 只挂 workspace 共享一套通用标签（个人场景简化）。

- [x] **任务 7：[后端·projectIssue] Issue 核心模块**
  - 文件：`src-server/internal/dal/types/project_issue.go`、`service/project_issue.go`、`controller/project_issue.go`（新增）、`router/router.go`（修改）；gencode 配 `completed_at→*time.Time`（`cmd/gormgen/gen_model_tracker.go`）+ 重跑 `gorm:gen`
  - 当前：无 issue 模块
  - 目标：projectIssue CRUD；create 默认 state 取 `project.default_state_id`、`sort_order` 后端自算（同 project MAX+10000）；`priority`/`is_draft` typed 枚举（create 空值规范为 none/N，update 空值保留原值）；update 检测 stateId 变化触发 completed_at 流转（completed 组→写 now、否则清 NULL）；list 返回扁平列表（groupBy 由前端分组）+ orderBy（默认 sort_order）+ 筛选（stateId/priority/keyword + labelId 两步查）；get/list 返回含 label 列表（应用层组装，3 次查询避 N+1）；delete 级联软删 t_issue_labels。命名全程 `projectIssue`（对齐 DO `ProjectIssue`/表 `t_project_issues`）。
  - 设计变更：completed_at 用 `*time.Time` 指针（gencode `gen.FieldType` 配置），Save 统一处理可空语义；sort_order 改后端自算（tasks.md 原说「前端传、无则 DB 默认 0」，但 DB 无默认值且自算更一致）；list 返回扁平列表而非分组结构（groupBy 前端做）。

### 阶段 C：前端窗口骨架

- [x] **任务 8：[前端·骨架] tracker 独立窗口 + Tauri 注册 + API 封装**
  - 文件：`tracker.html`、`src/windows/tracker/{main.tsx,TrackerApp.tsx,index.css,api.ts}`（新增）、`src/shared/i18n/locales/{zh-CN,en}/tracker.json` + `src/shared/i18n/index.ts`（注册命名空间）、`vite.config.ts`（多入口）、`src-tauri/src/windows/tracker.rs` + `windows/mod.rs` + `lib.rs`（`showTrackerWindow` command + 注册）、`src/windows/panel/PanelApp.tsx` + `panel.json`（入口按钮 + 文案）
  - 当前：无 tracker 窗口
  - 目标：新建 tracker 独立窗口（`AppThemeProvider`/`AppI18nProvider` 包裹 + i18n），TrackerApp **三栏布局壳**（顶 workspace 选择器位 + 左 project 列表 + 右 issue 列表，占位）；panel 顶栏加 IconButton 拉起；`api.ts` 建立项目首个 HTTP 封装（`ApiResponse<T>` + `apiGet`/`apiPost` + 地址取自 `httpServerStatus`）；`pnpm gen:bindings` 重生成 `showTrackerWindow`。
  - 设计变更：窗口**全屏按 maximized 实现**（占满工作区、保留标题栏、`skip_taskbar(false)` 进任务栏、关闭=隐藏复用实例）；**不改 `tauri.conf.json`**（项目所有窗口由 Rust `WebviewWindowBuilder` 动态创建，`app.windows:[]`）；三级导航用**三栏布局**（顶+左+右，非层层进入）；骨架占位，业务在任务 9-12。

### 阶段 D：前端业务页面

- [x] **任务 9：[前端] 工作空间页**
  - 文件：`src/windows/tracker/WorkspacesPage.tsx` + `components/WorkspaceDialog.tsx`（新增）、`src/windows/tracker/TrackerApp.tsx`（修改，接入 selectedWorkspace 状态 + 视图切换）、`src/shared/i18n/locales/{zh-CN,en}/tracker.json`（补 workspace/toast/time 文案）
  - 当前：无
  - 目标：工作空间卡片/列表 + 选择进入项目列表；创建/编辑/删除（slug 由 name 自动生成或手填）；三态机（loading/ready/error）+ toast（照搬 RepositoriesPage 范式）。
  - 设计变更：导航采用**全屏管理视图**——TrackerApp 持 `selectedWorkspace` 状态，为空时全屏渲染 WorkspacesPage（卡片网格 + CRUD + 客户端搜索 + id DESC 排序），选中卡片后切换到三栏工作壳（顶栏显示当前工作空间名 + 「切换工作空间」IconButton 回到网格）；Workspace 类型定义在 WorkspacesPage 并由 WorkspaceDialog `import type` 单向复用（无运行时循环）；slug 由 name 自动派生（`slugify`，手动改过或编辑模式即停止派生）；删除走标准确认文案（后端 delete 不级联，孤儿数据后续按需处理）。

- [ ] **任务 10：[前端] 项目列表页**
  - 文件：`src/windows/tracker/ProjectListPage.tsx` + `components/ProjectDialog.tsx`（新增）
  - 当前：无
  - 目标：项目卡片网格（emoji + 名称 + 描述 + issue 计数）；创建对话框（name/emoji/描述）、编辑、删除（确认）；点击进入 issue 列表。

- [ ] **任务 11：[前端] Issue 列表页**
  - 文件：`src/windows/tracker/IssueListPage.tsx` + `components/IssueCreateDialog.tsx`（新增）
  - 当前：无
  - 目标：list 视图，按 state_group 或 priority 分组（MUI Collapse 可折叠）+ 排序 + 基础筛选（状态/优先级/关键字搜索）；每行显示 id、状态色块、优先级图标、名称；顶部快速创建 issue；点击行打开侧滑详情。

- [ ] **任务 12：[前端] Issue 侧滑详情 + Label 管理**
  - 文件：`src/windows/tracker/IssueDetailDrawer.tsx` + `components/StateSelect.tsx`、`PrioritySelect.tsx`、`LabelSelect.tsx`、`LabelManagerDialog.tsx`（新增）
  - 当前：无
  - 目标：MUI Drawer 侧滑详情，编辑标题/描述(markdown Textarea)/状态/优先级/标签/起止日期（属性变更即时保存）；状态切换由后端维护 completed_at；LabelSelect 多选 + LabelManagerDialog 管理 label 增删改；删除 issue（确认）。

### 收尾

- [ ] **任务 13：[文档/自测] README 同步 + 整体联调**
  - 文件：`src-server/README.md`（修改）、`docs/tasks.md`（更新）
  - 当前：—
  - 目标：README 补充 tracker 模块说明、DO 命名、gencode 用法、新接口列表；端到端联调（建 workspace→project→issue 全链路、状态流转、标签）；勾选全部任务。

---

## 四、后续迭代（本期不做）

- 看板视图 + 拖拽排序（引入 `@hello-pangea/dnd`）
- 富筛选表达式（property-operator-value 构建器，保存为视图）
- 富文本描述编辑器（TipTap）
- 命令面板（`cmdk`）
- calendar / gantt 视图
- cycle（迭代）/ module（功能模块）/ 子任务 / 评论 / 活动流
