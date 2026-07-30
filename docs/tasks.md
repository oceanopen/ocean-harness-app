# 工作空间 / 项目 / Issue 管理 — 开发任务清单

> 控制台新功能：实现个人工作场景下的「工作空间 → 项目 → Issue」三级管理。
> 参考项目：`~/Project/plane`（仅借鉴交互、表结构、逻辑；**砍掉成员/角色/权限/邀请/计费/license**）。
> 后端落在 `src-server/`（go-server），前端新建独立窗口调接口实现 CRUD。

---

## 一、背景与目标

- **个人场景**：单用户，无成员管理、无权限校验。
- **三级模型**：workspace（顶层容器）→ project（项目，带 identifier 短码）→ issue（核心工作项）。
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
| `t_workspace_projects` | id, workspace_id, name, identifier(同 workspace 全局唯一 udx), description, emoji, default_state_id, 时间戳, deleted_at | 项目，所属 workspace；identifier 大写短码（PLN），用于 issue key |
| `t_project_states` | id, project_id, workspace_id, name, color, slug, state_group, sort_order, is_default, is_triage, 时间戳, deleted_at | 状态，所属 project；state_group ∈ backlog/unstarted/started/completed/cancelled |
| `t_project_issues` | id, project_id, workspace_id, name, description, state_id, priority, sort_order, parent_id, start_date, target_date, completed_at, is_draft, 时间戳, deleted_at | issue，所属 project；issue key = `{identifier}-{id}`（直接用全局自增 id）；priority ∈ urgent/high/medium/low/none |
| `t_workspace_labels` | id, workspace_id, project_id(可空), name, color, description, sort_order, 时间戳, deleted_at | 标签，所属 workspace（可挂 workspace 或 project 级） |
| `t_issue_labels` | id, issue_id, label_id, created_at, updated_at, deleted_at | 关联表，所属 issue；udx(issue_id, label_id) 全局唯一；软删除与其他表统一 |

- 软删除：保留 `deleted_at`，`gencode` 配置映射 `gorm.DeletedAt`，查询自动过滤。
- 唯一索引：**全局唯一**（不带 `WHERE deleted_at IS NULL`），即已删除记录仍占用唯一键——配合「恢复式创建」语义（见下）。
- issue key = `{identifier}-{id}`（如 `PLN-1`）：直接用全局自增 `id` 组 key，**无独立 sequence_id**（项目间不连号可接受，省一张计数逻辑）。
- 枚举字段：`state_group`/`priority` 为 typed 枚举（`enums.StateGroup`/`enums.Priority`），`is_default`/`is_triage`/`is_draft` 为 `enums.YesNo`（"Y"/"N"，替代 bool）；DB 均为 TEXT、**无默认值**（`sort_order` 同样无默认），一律由代码/前端显式赋值，漏传由 `Value()` 硬错拦下（见 `internal/dal/enums/`）。
- 关联查询：**无 DB 外键约束**，跨表关联一律走 SQL JOIN / 应用层组装；数据级联清理由 service 层手动。

#### 创建逻辑（恢复式 upsert）

针对带业务唯一索引的实体（**workspace by slug**、**project by (workspace_id, identifier)**），create 不直接 INSERT，而是先按唯一键查（**含已软删除记录**，即 `Unscoped` 查询）：

- **存在未删除同键记录** → 返回报错「记录重复」；
- **存在已删除同键记录** → 恢复并重置该行：`deleted_at = NULL` + `updated_at = now` + 业务字段全部用新入参覆盖，**固定列保留**（`id` + `created_at` 不变）；
- **不存在** → 正常 INSERT。

> 适用范围：workspace、project（创建语义：未删同键→报错、已删→恢复重置、无→插入），以及 `t_issue_labels`（toggle 语义：未删→软删取消、已删→恢复、无→插入）。`t_project_states / t_workspace_labels / t_project_issues` 无业务唯一索引，正常创建。

### 2.3 命名规范（全程一致）

- **实体术语**：`workspace / project / issue / state / label` —— types/service/controller/router/前端/DB 全程同一词。
- **公共字段**：`id, name, description, createdAt, updatedAt, deletedAt`。
- **业务字段**：`workspaceId, projectId, identifier, slug, stateId, stateGroup, priority, sequenceId, sortOrder, parentId, startDate, targetDate, completedAt, isDraft, isDefault, isTriage, emoji, color`。JSON 全 camelCase。
- **枚举**：`priority = urgent|high|medium|low|none`；`stateGroup = backlog|unstarted|started|completed|cancelled`。
- **表名**：业务表统一 `t_` 前缀 + 所属关系（顶级 `t_workspaces` 保持；子表以直接父单数作前缀：`t_workspace_projects` / `t_project_states` / `t_project_issues` / `t_workspace_labels` / `t_issue_labels`），与系统表（`goose_db_version` 等）区分。
- **索引名**：唯一索引 `udx_{表名去t_}_{列名}`（如 `udx_workspaces_slug`、`udx_workspace_projects_workspace_id_identifier`），全局唯一；普通索引本期暂不建（数据量小，后续按查询热点以 `idx_{表名去t_}_{列名}` 追加）。
- **各层后缀**（沿用 README）：`XxxRequest`/`XxxResponseData`（types）、service/controller 同名、`XxxDO`（gen 生成的数据库实体 = PO 层）。

### 2.4 gorm/gen 自动生成

- 独立程序 `src-server/cmd/gormgen/`（`main.go` + `init_gen.go` + `gen_model_tracker.go`）：复用服务 initialize 序列（config → zap → sqlite → goose 迁移）确保表就绪 → `gen.NewGenerator` → 对 6 表 `GenerateModelAs`（单数无 `t_` 前缀）→ 配置字段类型映射（`deleted_at→gorm.DeletedAt`、`is_*→bool`、sqlite `INTEGER→int`/`REAL→float64`、JSON tag 小驼峰；不开 `FieldNullable` 以免 PK 被指针化）→ query 层输出到 `src-server/internal/gormgen/`（含 `gen.go` 的 `Use`/`WithContext`），PO 结构体输出到 `src-server/internal/gormgen/model/`。
- 运行：`pnpm server:gorm:gen`（等价 `cd src-server && go run ./cmd/gormgen -config config/settings.dev.yaml`，迁移 + 生成一气呵成）；首次引入依赖需 `GOPROXY=https://goproxy.cn,direct go -C src-server mod tidy`（默认 goproxy.weoa.com 为白名单代理）。
- service 层用 `gormgen.Use(global.SqliteDB).Xxx.WithContext(ctx).Create/First/Find/Save/Delete` 做 CRUD（不生成全局 Q/SetDefault，每次调用 `Use`）。
- 流程：改迁移 → 跑 `pnpm server:gorm:gen`（goose 自动建表 + 重新生成 DO）。
- 依赖锁定：`gorm v1.25.12` + `gen v0.3.28` + `dbresolver v1.5.3`（gen 生态不支持 gorm v1.31；v0.3.28 修复 sqlite 下 `ScanType` 为空导致生成期 panic）。

### 2.5 接口（action 风格 `/api/<module>/<action>`，GET 查 / POST 写）

- `workspace`: list / get / create / update / delete
- `project`: list?workspaceId / get / create(**自动种 6 默认 state**) / update / delete
- `state`: list?projectId / create / update / delete / reorder
- `issue`: list?projectId(groupBy/orderBy/筛选) / get / create(**自动 sequenceId+sortOrder**) / update / delete
- `label`: list / create / update / delete / toggleIssue

### 2.6 前端 tracker 窗口（新建独立窗口）

- `src/windows/tracker/`：`main.tsx` + `TrackerApp.tsx`(三级导航根) + 页面 + `components/`。
- `src/windows/tracker/api.ts`：fetch 封装 + `ApiResponse<T>` + 地址取自 `httpServerStatus`。
- i18n：`src/shared/i18n/locales/{zh-CN,en}/tracker.json` + 注册到 `index.ts`。
- 窗口注册：`tracker.html` + `vite.config.ts` 多入口 + `tauri.conf.json` 窗口配置 + Rust 开窗 command + `gen:bindings` + panel 加入口按钮。

---

## 三、任务清单

> 执行约定：**每次执行一个任务，用户手动提交后执行下一个**。完成的任务将下方 `[ ]` 改为 `[x]`。

### 阶段 A：后端基座

- [x] **任务 1：[后端·迁移] 建 MVP 6 张业务表**
  - 文件：`src-server/internal/migrations/migrations/20260730001_init_tracker.sql`（新增）
  - 当前：仅有 `20260728001_init.sql`（空迁移验证 goose 机制）
  - 目标：新增 `-- +goose Up` 迁移，建带层级前缀的 6 表（t_workspaces / t_workspace_projects / t_project_states / t_project_issues / t_workspace_labels / t_issue_labels，自增主键 + 软删除 deleted_at + 时间戳）；唯一索引 `udx_` 前缀且**全局唯一**（udx_workspaces_slug、udx_workspace_projects_workspace_id_identifier、udx_issue_labels_issue_id_label_id，**不带 WHERE deleted_at IS NULL**）；普通索引本期暂不建（数据量小，按需追加）；**不建任何 DB 外键**（跨表关联走 JOIN）；t_project_states 默认 state_group='backlog'、t_project_issues 默认 priority='none'/sequence_id=1/sort_order=65535。

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

- [ ] **任务 5：[后端·project] 项目模块（create 种默认状态）**
  - 文件：`src-server/internal/types/project.go`、`service/project.go`、`controller/project.go`（新增）、`router/router.go`（修改）
  - 当前：无 project 模块
  - 目标：project CRUD（按 workspaceId 查）；create 在**同一事务**内：identifier 大写化 + 按 (workspace_id, identifier) **恢复式 upsert**（含软删除记录查询：未删除同键→报错；已删除→恢复重置、保留 `id`+`created_at`；不存在→插入）→ 若为新插入或恢复后该项目下无未删除 state，则调 `SeedDefaultStates` 种 5 默认状态 → 回填 `default_state_id`；软删除（**无 DB 外键**，级联清理其下 state/issue 由 service 手动）。

- [ ] **任务 6：[后端·label] 标签模块（含 issue 关联）**
  - 文件：`src-server/internal/types/label.go`、`service/label.go`、`controller/label.go`（新增）、`router/router.go`（修改）
  - 当前：无 label 模块
  - 目标：label CRUD（按 workspaceId/projectId 查，project_id 可空表 workspace 级）；`toggleIssue` 关联/取消关联 t_issue_labels（**恢复式 upsert**：含软删记录查询，未删→软删取消、已删→恢复、无→插入）。

- [ ] **任务 7：[后端·issue] Issue 核心模块**
  - 文件：`src-server/internal/types/issue.go`、`service/issue.go`、`controller/issue.go`（新增）、`router/router.go`（修改）
  - 当前：无 issue 模块
  - 目标：issue CRUD；create 默认 state 取 project.default_state_id，`priority`/`is_draft` 用 typed 枚举（前端传）、`sort_order` 前端传（无则 DB 默认 0）、issue key 由 `{identifier}-{id}` 组装（id 取插入后自增主键）；update 检测 state_id 变化，新 state 的 state_group=completed 则写 completed_at、否则清空；list 支持 `groupBy`(state/priority)、`orderBy`(id/sort_order/priority/created_at)、基础筛选（stateId/priority/labelId/keyword 搜 name）；get/list 返回含 label 列表。

### 阶段 C：前端窗口骨架

- [ ] **任务 8：[前端·骨架] tracker 独立窗口 + Tauri 注册 + API 封装**
  - 文件：`tracker.html`（新增）、`vite.config.ts`（修改，多入口）、`src/windows/tracker/main.tsx` + `TrackerApp.tsx`（新增，三级导航壳）、`src/shared/i18n/locales/{zh-CN,en}/tracker.json` + `src/shared/i18n/index.ts`（修改，注册命名空间）、`src/windows/tracker/api.ts`（新增，fetch + ApiResponse + 地址取自 httpServerStatus）、`src-tauri/tauri.conf.json` + `tauri.dev.conf.json`（修改，窗口配置）、`src-tauri/src/`（新增 `showTrackerWindow` command）、`src/windows/panel/PanelApp.tsx`（修改，加入口按钮）
  - 当前：无 tracker 窗口
  - 目标：新建 tracker 独立窗口（复用 AppThemeProvider/AppI18nProvider 暗黑亮色 + i18n），TrackerApp 三级导航壳（工作空间选择 → 项目列表 → issue 列表占位）；panel 顶栏加入口按钮拉起；前端 API 封装统一 fetch（`ApiResponse<T>`、code≠0 抛错）；`pnpm gen:bindings` 重新生成开窗 command 类型。

### 阶段 D：前端业务页面

- [ ] **任务 9：[前端] 工作空间页**
  - 文件：`src/windows/tracker/WorkspacesPage.tsx` + `components/WorkspaceDialog.tsx`（新增）
  - 当前：无
  - 目标：工作空间卡片/列表 + 选择进入项目列表；创建/编辑/删除（slug 由 name 自动生成或手填）；三态机（loading/ready/error）+ toast（照搬 RepositoriesPage 范式）。

- [ ] **任务 10：[前端] 项目列表页**
  - 文件：`src/windows/tracker/ProjectListPage.tsx` + `components/ProjectDialog.tsx`（新增）
  - 当前：无
  - 目标：项目卡片网格（identifier 徽章 + emoji + 描述 + issue 计数）；创建对话框（name/identifier 自动大写/emoji/描述）、编辑、删除（确认）；点击进入 issue 列表。

- [ ] **任务 11：[前端] Issue 列表页**
  - 文件：`src/windows/tracker/IssueListPage.tsx` + `components/IssueCreateDialog.tsx`（新增）
  - 当前：无
  - 目标：list 视图，按 state_group 或 priority 分组（MUI Collapse 可折叠）+ 排序 + 基础筛选（状态/优先级/关键字搜索）；每行显示 key（identifier-sequenceId）、状态色块、优先级图标、名称；顶部快速创建 issue；点击行打开侧滑详情。

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
