# Issue 状态体系重构 — 三层模型

> 配套文档:
> - [`docs/_dev_workflow_ux.md`](_dev_workflow_ux.md) — 基于本状态体系的「开发工作台」交互方案
> - [`docs/worktree_term.md`](worktree_term.md) — worktree + 嵌入式终端后端技术方案
>
> 本文档定义 issue 的**状态(state)体系**:`stateGroup` 常量、子 state 目录、`projectState` 数据。
> **数据层一律不国际化** —— 状态名/组名/颜色由 Go 目录直出中文,前端原样展示;接口报错也由后端原样返回。
> i18n 仅用于页面静态提示文案。

---

## 1. 背景与目标

### 1.1 现状
issue 的状态 = `issue.stateId` → `ProjectState` → `stateGroup`(5 值枚举:`backlog/unstarted/started/completed/cancelled`)。
状态按项目一份、用户可改,但每个 stateGroup 默认只种一个 state(`Backlog / Todo / In Progress / Done / Cancelled`),由后端 `SeedDefaultStates` 在建项目时自动种入。

现状核实(改造基线):

| 视图/逻辑 | 按 stateGroup 还是 state | 位置 |
|---|---|---|
| 列表视图分组 | **stateGroup**(5 组,组内扁平) | `ProjectIssueList.tsx:43,134,173` |
| 看板视图分列 | **state**(每 `ProjectState` 一列,列头显 state 名) | `useKanbanColumns.ts:16-35` / `KanbanColumn.tsx:44` |
| IssueCard | 仅用 stateGroup 判断是否显示子任务进度 | `IssueCard.tsx:72-76` |
| 状态选择/筛选 | state 级(`ProjectStateSelect` + 列表筛选) | `ProjectIssueList.tsx:159,310` |
| completedAt 副作用 | stateGroup 级(completed 组触发) | `useKanbanDnd.ts:72-73` |
| 状态名中文化 | name→中文映射(仅命中 5 个默认名) | `stateDisplayName.ts` |

> 结论:列表按 group,但**看板早已按 state**(每个 ProjectState 一列)。给 `started` 组加多个子 state 后,看板会自然多列,核心分组/拖拽逻辑无需改动。

### 1.2 目标
让 `started` 组能承载**开发流程的子状态**(`worktree初始化 / 开发中 / 待合并PR / 待清理`),从而:
- 不同项目按需启用不同开发步骤(全开 / 只开部分 / 全关);
- **开发流程的位置 = issue 在 started 组里的子 state**,不再另设 `devPhase` 字段;
- 看板天然多列、开发工作台步骤条由项目配置驱动。

### 1.3 已锁定的决策
1. `projectState` 用**引用模型**:存 `catalog_key`,name/color/icon 由目录解析;
2. **保留** `is_default`(每项目一个,新建 issue 的初始状态);
3. 项目 create 随请求带 `initialStates`,**后端无 seed 逻辑**;保存走**全量替换**(全删全插,不比对 diff);
4. `started` 组 devPhase 项**默认勾选** —— 新项目开箱自带完整开发步骤条,与「进行中」并存;不需要开发流程的项目在配置里手动取消勾选。

---

## 2. 三层模型总览

| 层 | 内容 | 性质 | 位置 |
|---|---|---|---|
| 第 1 层 | **stateGroup 常量** | 固定 5 值 + 展示元数据 | Go `internal/dal/enums` |
| 第 2 层 | **子 state 目录**(每 group 一组固定可选项) | 常量(含 devPhase 标记) | Go `internal/dal/enums/state_catalog.go` |
| 第 3 层 | **projectState** | 数据(每项目一份,引用目录) | SQLite `t_project_states` |

前端通过接口获取第 1、2 层(常量映射)+ 第 3 层(项目数据),自身不维护任何状态常量。

### 2.1 第 1 层:stateGroup 常量(不变 + 展示元数据)
`enums/state_group.go` 的 5 个枚举值不动(`backlog/unstarted/started/completed/cancelled`,`completed` 组触发 `issue.completed_at`)。
新增**组的展示元数据**(Go 直出中文):

| stateGroup | 展示名 | 色 | sortOrder |
|---|---|---|---|
| `backlog` | 待办池 | #94a3b8 | 10000 |
| `unstarted` | 未开始 | #475569 | 20000 |
| `started` | 进行中 | #f59e0b | 30000 |
| `completed` | 已完成 | #16a34a | 40000 |
| `cancelled` | 已取消 | #ef4444 | 50000 |

### 2.2 第 2 层:子 state 目录(固定可选集)
每个 group 有一组**固定的、不可由用户扩展**的子 state,各项目从中勾选(每 group 至少 1 项)。**devPhase 并入 `started` 组的目录**:

```
backlog:    [ 待办池 ]
unstarted:  [ 未开始 ]
started:    [ 进行中(devPhase=nil,默认),
              worktree初始化(devPhase=init),
              开发中(devPhase=developing),
              待合并PR(devPhase=pr),
              待清理(devPhase=cleanup) ]
completed:  [ 已完成 ]
cancelled:  [ 已取消 ]
```

每个目录项字段:`key` / `name`(中文,Go 出)/ `color` / `icon` / `sortOrder` / **`devPhase?`**(仅 started 组部分项有:`init/developing/pr/cleanup`,驱动开发工作台渲染哪一步内容)。

> 目录是**固定可选集**:项目只能勾选已有目录项,不能自造。这满足"子状态各项目可自定义、但固定可选、不能随意选"。普通项目 started 只勾「进行中」;要做开发流程的项目勾上 devPhase 那几项 —— **不同项目开发步骤不一样**由此解决,跳过就是不勾某项。

Go 注册(示意,`internal/dal/enums/state_catalog.go`):
```go
type CatalogEntry struct {
    Key       string
    Name      string  // 中文，Go 直出，不走 i18n
    Color     string
    Icon      string
    SortOrder float64
    DevPhase  string  // 空 = 非开发步骤；init/developing/pr/cleanup
}

var StateCatalog = map[StateGroup][]CatalogEntry{
    STATE_GROUP_BACKLOG:    {{Key: "backlog", Name: "待办池", Color: "#94a3b8", SortOrder: 10000}},
    STATE_GROUP_UNSTARTED:  {{Key: "todo", Name: "未开始", Color: "#475569", SortOrder: 20000}},
    STATE_GROUP_STARTED: {
        {Key: "in_progress", Name: "进行中", Color: "#f59e0b", SortOrder: 30000},
        {Key: "wt_init",     Name: "worktree初始化", Color: "#0ea5e9", SortOrder: 31000, DevPhase: "init"},
        {Key: "developing",  Name: "开发中", Color: "#2563eb", SortOrder: 32000, DevPhase: "developing"},
        {Key: "pr_open",     Name: "待合并PR", Color: "#7c3aed", SortOrder: 33000, DevPhase: "pr"},
        {Key: "cleanup",     Name: "待清理", Color: "#ea580c", SortOrder: 34000, DevPhase: "cleanup"},
    },
    STATE_GROUP_COMPLETED:  {{Key: "done", Name: "已完成", Color: "#16a34a", SortOrder: 40000}},
    STATE_GROUP_CANCELLED:  {{Key: "cancelled", Name: "已取消", Color: "#ef4444", SortOrder: 50000}},
}
```

### 2.3 第 3 层:projectState(数据,引用模型)
`t_project_states` 只存"项目选了哪些目录项",name/color/icon 一律从目录(第 2 层)实时解析,不在数据行里冗余。
- **引用目录**:行里的 `(state_group, catalog_key)` 对应 `StateCatalog[group]` 中某项;
- **无 seed**:后端不再 `SeedDefaultStates`;前端在项目 create 时按"默认勾选"算出 `initialStates` 随请求传入;
- **全量替换保存**:`replaceAll` 事务内硬删该 project 全部行 → 批量插入请求数据,不比对 diff;
- **`is_default` 保留**:每项目仅一个 `Y`,= 新建 issue 的初始状态(默认指向 backlog 组那项)。

---

## 3. 数据模型与迁移

### 3.1 目标表结构(`t_project_states`)
```sql
CREATE TABLE t_project_states (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id   INTEGER  NOT NULL,
    workspace_id INTEGER  NOT NULL,
    state_group  TEXT     NOT NULL,      -- 冗余存，便于分组/查询
    catalog_key  TEXT     NOT NULL,      -- 引用 StateCatalog[(state_group)].Key
    sort_order   REAL     NOT NULL DEFAULT 0,
    is_default   TEXT     NOT NULL,      -- 每项目仅一个 Y（新建 issue 初始状态）
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME
);
-- 唯一约束：同一项目同 group 同目录项仅一条
CREATE UNIQUE INDEX udx_project_states_proj_group_key
    ON t_project_states (project_id, state_group, catalog_key);
-- 相比旧表删除：name / color / slug / is_triage（改由目录提供）
```

> `catalog_key` 存普通 TEXT,**不做 gorm typed enum**。原因:同一 key 理论上可出现在多个 group,用扁平 enum 校验不合适;合法性由 service 层查 `StateCatalog[(group)]` 校验。

### 3.2 迁移脚本 `20260806002_alter_project_states_to_catalog.sql`
```sql
-- 1) 加 catalog_key
ALTER TABLE t_project_states ADD COLUMN catalog_key TEXT NOT NULL DEFAULT '';

-- 2) 按 (state_group, name) 回填 catalog_key（匹配 5 个默认种子名）
UPDATE t_project_states SET catalog_key='backlog'    WHERE state_group='backlog'   AND name='Backlog';
UPDATE t_project_states SET catalog_key='todo'       WHERE state_group='unstarted' AND name='Todo';
UPDATE t_project_states SET catalog_key='in_progress' WHERE state_group='started'   AND name='In Progress';
UPDATE t_project_states SET catalog_key='done'       WHERE state_group='completed' AND name='Done';
UPDATE t_project_states SET catalog_key='cancelled'  WHERE state_group='cancelled' AND name='Cancelled';

-- 3) 用户改名/自建的行回退到该 group 的默认 key（兜底）
UPDATE t_project_states SET catalog_key='backlog'    WHERE state_group='backlog'   AND catalog_key='';
UPDATE t_project_states SET catalog_key='todo'       WHERE state_group='unstarted' AND catalog_key='';
UPDATE t_project_states SET catalog_key='in_progress' WHERE state_group='started'   AND catalog_key='';
UPDATE t_project_states SET catalog_key='done'       WHERE state_group='completed' AND catalog_key='';
UPDATE t_project_states SET catalog_key='cancelled'  WHERE state_group='cancelled' AND catalog_key='';

-- 4) 删除冗余列（需 SQLite ≥ 3.35；旧版本走"建新表+拷贝+ rename"重建）
ALTER TABLE t_project_states DROP COLUMN name;
ALTER TABLE t_project_states DROP COLUMN color;
ALTER TABLE t_project_states DROP COLUMN slug;
ALTER TABLE t_project_states DROP COLUMN is_triage;

-- 5) 唯一索引
CREATE UNIQUE INDEX udx_project_states_proj_group_key
    ON t_project_states (project_id, state_group, catalog_key);
```
迁移后须 **重跑 gormgen**(`cmd/gormgen/gen_model_tracker.go`)重生成 `model/project_states.gen.go`(去掉 name/color/slug/is_triage 字段、加 CatalogKey)。

### 3.3 时区
沿用记忆 [[reference_sqlite_timezone_glebarez]]:`created_at/updated_at` 走 `CURRENT_TIMESTAMP`(UTC),展示层转本地。

---

## 4. 项目编辑「状态管理」模块

挂在项目编辑表单内,从目录(第 2 层)渲染可勾选项,全量替换保存:

```
状态管理
┌ 待办池 (backlog) ───────────── 至少 1 项 ┐
│  ☑ 待办池                                │
├ 未开始 (unstarted) ──────────────────────┤
│  ☑ 未开始                                │
├ 进行中 (started) ────────────────────────┤
│  ☑ 进行中  ← 非开发流程的 issue 用此状态  │
│  ☑ worktree初始化  ☑ 开发中 ☑ 待合并PR ☑ 待清理│
│  勾选的 devPhase 项按下列顺序构成开发步骤条（可拖序）:
│  [worktree初始化]→[开发中]→[待合并PR]→[待清理]│
├ 已完成 (completed) ──────────────────────┤
│  ☑ 已完成                                │
├ 已取消 (cancelled) ──────────────────────┤
│  ☑ 已取消                                │
└──────────────────────────────────────────┘
   [保存] → replaceAll（事务内全删全插）
```

- 每 group 从目录勾选,**至少 1**(删到最后一个时禁用减号);
- started 组的 devPhase 项勾选后进入"步骤顺序"区,可拖动排序(写 `sort_order`);
- 「进行中」与 devPhase 项可共存:项目既有走普通"进行中"的 issue,也有走开发步骤条的 issue;
- `is_default`:默认标在 backlog 组那项(新建 issue 初始状态),配置内可改标。

---

## 5. API 契约(Go 侧)

| 接口 | 说明 |
|---|---|
| `GET /api/tracker/projectState/catalog` | **新增**。返回第 1+2 层常量:`groups[]`(group/name/color/sortOrder)+ 每 group 的 `states[]`(key/name/color/icon/sortOrder/devPhase?)。前端用它渲染配置模块、状态徽章、步骤条、看板列头 |
| `POST /api/tracker/projectState/getList` | 不变。返回某项目的 projectState(行含 `catalog_key`,不再有 name/color) |
| `POST /api/tracker/projectState/replaceAll` | **新增,取代 create/update/delete/reorder**。入参 `{projectId, states:[{stateGroup, catalogKey, sortOrder, isDefault}]}`。事务内:`Unscoped().Where(project_id).Delete`(硬删,避免软删行占用全局唯一键)→ 批量 `Insert`。校验:每行 `(group,key)` 须在目录内;每 group ≥1 项;`is_default=Y` 恰好一个 |
| 项目 `create` | 入参新增 `initialStates`。建 project 后循环插入(不再调 `SeedDefaultStates`);`default_state_id` = `is_default=Y` 那条的 id |
| 项目 `update` | 不变(状态管理独立走 replaceAll,不混入项目 update) |

`initialStates` 默认值(决策 #4,新项目开箱自带完整开发步骤条):
```
[
  {backlog,   backlog,     is_default=Y},
  {unstarted, todo},
  {started,   in_progress},
  {started,   wt_init},
  {started,   developing},
  {started,   pr_open},
  {started,   cleanup},
  {completed, done},
  {cancelled, cancelled}
]
```

---

## 6. i18n 策略(数据层不国际化)

- **删除** `stateDisplayName.ts` —— 状态名/组名一律走 Go 目录,前端原样显示;
- **删除** `tracker.json` 里的 group 标签(`待办池/未开始/进行中/已完成/已取消`)—— 改由目录 group 元数据提供;
- 列表 group 头、看板列头、状态徽章、步骤条标签 = 全部用目录里的中文名;
- 接口报错(如 `errors.New("无法删除默认状态…")`)**原样返回**,前端直接 toast,不再 `t()` 包裹;
- i18n 仅保留:按钮文案、空状态提示、表单字段标签等**静态展示文案**。

---

## 7. 前端改动清单

| 文件 | 改动 |
|---|---|
| `services/ProjectStateService.ts` | 加 `getCatalog()` / `replaceAll()`;`ProjectStateModel` 去掉 name/color/slug/isTriage,加 `catalogKey` |
| `state/tracker/queries.ts` | 加 `useStateCatalog()` / `useReplaceProjectStates(projectId)`(自失效 `projectStates` key) |
| `state/tracker/keys.ts` | 加 `stateCatalog()` |
| `components/stateDisplayName.ts` | **删除** |
| `ProjectIssueList.tsx` | group 头标签改用目录 group 元数据(非 i18n) |
| `KanbanView/KanbanColumn.tsx` | 列头 name 由目录解析(按 state.group+catalogKey join) |
| `ProjectIssueDrawer/ProjectStateSelect.tsx` | 选项 name/color 由目录 join |
| `TrackerPage/components/ProjectStateManage/` | **新增** 状态管理模块组件(供项目编辑表单嵌入) |
| 项目编辑抽屉 | 嵌入 `ProjectStateManage`;create 时附带 `initialStates` |
| `i18n/locales/*/tracker.json` | 删除 group 标签 |

---

## 8. 与开发工作台的关系(见 [`_dev_workflow_ux.md`](_dev_workflow_ux.md))

开发工作台的**步骤条 = 当前项目 started 组里、带 `devPhase` 的子 state**(按 `sort_order`)。issue 的开发位置 = `stateId` 落在 started 组哪个子 state;推进开发 = `stateId` 顺次后移;完成 = 移到 completed 组;取消 = 移到 cancelled 组。**issue 始终只有一个 `stateId`**,看板/列表/步骤条全统一,再无第二套状态字段。
