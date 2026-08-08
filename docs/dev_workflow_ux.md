# Issue 开发工作台 — 交互方案

> 配套文档:
> - [`docs/issue.md`](issue.md) — 状态体系三层模型(本方案的数据基础)
> - [`docs/worktree_term.md`](worktree_term.md) — worktree + 嵌入式终端后端技术方案
>
> 本文档定义「开发工作台」的**交互方案**。开发流程的状态基础见 `issue.md`(开发步骤已并入 `started` 组子 state,无独立 devPhase 字段)。
> 终端能力本期**占位**(写提示 + 复用现有「外部终端/编辑器打开」),真终端实现见 `worktree_term.md`。

---

## 1. 定位:规划面 vs 执行面

把现有的「**项目事项管理**」(规划面:CRUD / 看板 / 分诊)和一个新视图「**开发工作台**」(执行面:初始化 worktree → 开发 → PR → 清理)解耦成两个互补视图,共用同一份 issue 数据,通过「开始开发」动作和「子状态徽章」双向桥接。

> 关键:issue 浏览与开发执行不在同一棵树里。规划不被流程绑架,执行又有清晰步骤条。

---

## 2. 双视图模型

| 视图 | 角色 | 入口 | 改动量 |
|---|---|---|---|
| 项目事项管理(现有) | 规划面:issue CRUD / 看板 / 分诊 | 侧栏 `tracker` | **小**:处理中 issue 加开发步骤子状态徽章 + 「开始开发」入口 |
| 开发工作台(新增) | 执行面:worktree 初始化 / 开发 / PR / 清理 | 侧栏 `devWorkbench`(`MenuKey` 新增) | **全新**:左任务树 + 右步骤条 |

**状态统一**:两个视图共用 issue 的 `stateId`。开发工作台的步骤条 = 项目 `started` 组里勾选的子 state、排除「进行中」(见 `issue.md` §2.2);issue 的开发位置就是它的 `stateId`,不存在第二套开发阶段字段。

---

## 3. 开发工作台布局

复用 tracker 的 3-zone shell 范式(`TrackerPage.tsx:75-103` 的顶栏 + 左固定宽列表 + 右弹性区),新增 `MenuKey = 'devWorkbench'`(`commandPalette/types.ts`)+ 侧栏菜单项 + 命令面板 `nav.devWorkbench`。

```
┌──────┬─────────────────────────────────────────────────────────────┐
│侧栏  │  开发工作台                              [🔄刷新][🧹清理中心]│
│ ...  ├───────────────┬─────────────────────────────────────────────┤
│▸开发 │ ◉ 我的个人空间 │  #42  登录页空白修复        [在事项管理打开]  │
│ 工作台│───────────────│  仓库 we-claude-terminal-app · 分支 fix/login │
│ 事项 │ ▾ we-health    │  ─────────────────────────────────────────── │
│ 管理 │   ▾ we-claude  │  ① worktree初始化 ─② 开发中 ─③ 生成PR ─④ 清理 │
│ 仓库 │    • #42 登录   │      ✓ 完成          ● 进行    ○ 待办  ○ 待办 │
│      │      🔵开发中⦿ │  ─────────────────────────────────────────── │
│      │    • #39 时区   │                                             │
│      │      🟣待合并   │       [当前步骤内容区 — 随当前 state 切换]   │
│      │   ▸ app-x      │       例:developing → 终端占位+[开发完成]    │
│      │ ▾ 团队空间      │                                             │
│      └───────────────┴─────────────────────────────────────────────┘
```

- **顶栏**:工作空间切换条(同 tracker 48px bar)+ 右侧「清理中心」批量入口。
- **左任务树**:workspace → project → **处于开发流程的 issue**(其 `stateId` 落在 `started` 组「进行中」之外的子 state)。默认只显示这些;可切换显示已归档(completed/cancelled)。
- **右侧**:issue 头部(meta + 回链)→ 步骤条 → 当前步骤内容区 → 取消动作。

---

## 4. 左侧任务树行

每行 = issue 标题 + **子状态徽章**(由当前 state 的目录项 name/color 渲染)+ 可选**实时活动指示**:

```
▾ we-claude-terminal-app
   #42 登录页空白修复        🔵 开发中      ⦿ 终端在跑
   #39 修复时区计算          🟣 待合并PR
   #55 升级 TS               🟠 待清理
```

- **徽章**:直接用 issue 当前 state 在目录里的 `name`+`color`(见 `issue.md` §2.2),无需额外映射;
- **实时活动指示**(建议 P2):小脉动点表示"终端有活跃进程 / worktree 有未提交改动",让左树一眼就"活";
- 点击行 → 选中 → 右侧渲染该 issue 的步骤条 + 当前步骤内容。

---

## 5. 步骤条(由项目配置驱动,非固定向导)

**步骤条 = 当前项目 `started` 组里、「进行中」之外的子 state**,按 `sort_order` 排序。
- 项目没勾「进行中」之外的项 → 没有步骤条(纯状态管理,该 issue 不进开发工作台);
- 不同项目勾不同项 → 不同步骤序列;**跳过** = 配置里不勾某项;
- 步骤状态:`✓已完成`(stateId 已越过) / `●进行中`(= 当前 stateId) / `○待办`;
- 点某步骤可回看(已完成步骤只读展示当时信息,进行中步骤可操作)。

步骤项的 `state_code` 决定右侧渲染哪种内容。

---

## 6. 各步骤内容(按 state_code)

### Step · worktree 初始化(`state_code=wt_init`)
- **未初始化**(无 worktree 记录)→ 表单卡(复用 `IssueBranchField` 的仓库+分支逻辑):
  - 仓库(默认取 `issue.localRepositoryId`,可改选项目关联仓库)
  - 基准分支 baseRef(默认仓库默认分支)
  - 开发分支名(自动建议 `<prefix>/<issueKey>-<slug>`,可编辑)
  - worktree 路径预览(只读,按 `worktree_term.md` §5.3 派生)
  - 启动命令(可选,默认取 `appConfig` post-open 命令)
  - `[创建 worktree 并开始]` → 调 Go `startDev`,创建过程**就地内联展示**(占在终端将来要占的同一帧,不用模态 spinner)
- **已初始化** → 同表单进入"已就绪"只读态,回填当前 worktree 信息,带 `[调整/重新初始化]` 次按钮。
- **推进**:创建成功 → 自动把 issue `stateId` 后移到下一个 started 子 state(通常是「开发中」)。

### Step · 开发中(`state_code=developing`,终端本期占位)
- 终端区(xterm 占位):带边框空终端框 + 提示条:
  > 📌 嵌入式终端即将支持。当前可点「在外部终端打开」用原生终端开发;开发完成后点下方「开发完成」进入下一步。
- 快捷操作行(**复用** `bindings.ts` 现有 `openInEditor/openInTerminal/openInFileManager`,范式照搬 `RepositoryCard.tsx:117-127`):`VSCode 打开 / iTerm2 打开 / 访达打开`;
- `[开发完成]` 主按钮 → `stateId` 推进到下一个 started 子 state(待合并PR);
- 顶部常显 worktree 路径 + 分支 chip。

### Step · 生成 PR(`state_code=pr_open`)
- PR 配置卡:源分支(`devBranch`,只读)、目标分支(`baseBranch`,可改)、PR 标题(默认 issue 名)、描述(默认 issue.description);
- `[生成 PR / 打开创建页]` → 构造 compare URL `https://<host>/compare/<base>...<head>` 并打开,记录 `prUrl`;
  > 后端 PR 创建在 `worktree_term.md` 明确是本期非目标。这步先用"引导式 compare URL 生成"顶住,后续加 token 可无缝升级为应用内建 PR,UX 不变。
- `prUrl` 已存在 → 显示 PR 链接 + `[打开 PR]` + `[合并完成]` → `stateId` 推进到「待清理」。

### Step · 待清理(`state_code=cleanup`)
- 清理确认卡:列出将删除项(worktree 路径、分支);若 `git status` 有未提交改动则警告;
- `[清理并完成]` → 按 `worktree_term.md` §9.3 两阶段编排(先 `pty_stop_for_worktree` 停 PTY,再 `removeWorktree`)→ `stateId` 移到 **completed 组**(自动归档);
- `[仅停止开发,保留 worktree]` 次按钮 → 取消流程但留工作区(`stateId` → cancelled 组)。

---

## 7. 生命周期状态机(全部经 `stateId`)

```
未进入开发(backlog/unstarted)
   │ [开始开发]
   ▼
started 组:worktree初始化 ─▶ 开发中 ─▶ 待合并PR ─▶ 待清理
   │              │           │           │            │
   │           [取消]      [取消]      [取消]      [清理并完成]
   │              ▼           ▼           ▼            ▼
   │          cancelled ◀────┴───────────┘       completed(自动归档)
   └─→ 任意进行中步骤可取消(脏改动时二次确认)
```

- **前进靠显式按钮**(无静默推进),仅最终 cleanup→completed 是用户描述的自动归档;
- **取消**任意进行中步骤 → cancelled(若有未提交改动二次确认);
- done/cancelled 后从左树移除(归档),但仍能在「项目事项管理」的已完成/已取消列看到。

---

## 8. 与「项目事项管理」的桥接(改动极小)

1. **`ProjectIssueDrawer` 加「开始开发」按钮**(footer 区,或 `IssueBranchField` 附近):issue 处于 backlog/unstarted/started 且 `localRepositoryId≠0`、且项目 started 组勾了「进行中」之外的项时显示。点击 → 切到「开发工作台」并定位该 issue(或就地创建 worktree、把 `stateId` 推到首个开发步骤子 state 再跳转)。
2. **`IssueCard` 显示开发步骤子状态徽章**:started 组的 issue 按当前 state 显目录里的 name+color。点徽章 → 跳开发工作台该 issue。

---

## 9. 延伸建议(基于 orca 借鉴)

1. **双状态轴**:子状态徽章(目录 name/color,固定)+ 派生活动指示(终端在跑/有改动,脉动点)。让左树"活",成本很低(PTY 状态 + git status 摘要)。
2. **PR 步骤先做"引导式 compare URL"**(见 §6),真 PR API 后续无缝升级 —— 这步现在就有用,不依赖后端能力。
3. **清理中心**:批量清理 stale/done worktree(对应 orca `WorkspaceCleanupDialog`),放开发工作台顶栏。单 issue 清理保留,批量入口等 issue 多了会刚需。
4. **左树过滤**:默认只显处理中 dev 任务,可切显已归档 —— 防 history 增长后 clutter。
5. **创建过程就地内联**:worktree 创建进度占在终端将来要占的同一帧,不用模态 spinner,handoff 更顺。

---

## 10. 分阶段落地

- **P1(本期,终端占位)**:开发工作台骨架(左树 + 步骤条)→ worktree初始化表单调 Go `startDev` → 开发中终端占位 + 外部终端/编辑器打开 → 生成PR compare URL → 待清理调 stop+remove。`stateId` 推进串联全流程;子状态徽章双视图回显;tracker 加「开始开发」桥接。**后端用桩接口即可跑通完整可点框架。**
- **P2**:开发中占位换成真 xterm(对应 `worktree_term.md` 阶段 2–4)+ 左树活动指示点。
- **P3**:真 PR 创建 + 清理中心批量入口 + 配置跳过开关。

---

## 11. 关键复用点(代码库已有)

| 能力 | 位置 |
|---|---|
| 打开编辑器/终端/访达 | `shared/bindings.ts` `openInEditor/openInTerminal/openInFileManager` |
| 打开按钮行范式 | `RepositoriesPage/components/RepositoryCard.tsx:117-127` |
| 仓库+分支选择器 | `ProjectIssueDrawer/IssueBranchField.tsx` |
| 3-zone master-detail shell | `TrackerPage.tsx:75-103` |
| 顶栏/菜单/命令面板接入 | `PanelApp.tsx:127-132` / `commandPalette/{types,commands}.tsx` |
| MUI `Stepper`(代码库未用,可直接用) | MUI v9 自带 |
| worktree/PTY 后端 | 见 `worktree_term.md` |

---

## 12. 任务拆分(P1 细化 + P2/P3 概要)

> 本章节基于 §11 复用点核对**当前代码**后拆分,作为实施清单。状态体系已落地于 `state_catalog.go`(`issue.md` 文档已移除,以代码为准)。分 **模块 A–G**,每模块含原子任务,标注文件落点与复用点。行号以当前代码为准,实施时若已漂移按符号定位。

> **实施方式**:后续按 §12.9 实施顺序**逐个任务推进**,每个原子任务以 `- [ ]` 标记;每完成一个任务,将对应条目改为 `- [x]` 同步状态,全程跟踪进度。P2/P3 为阶段概要,落地时再细化。

### 12.0 架构决策点(实施前确认)

| 决策点 | 推荐 | 理由 |
|---|---|---|
| 页面保活 | 复刻 tracker 保活(`devWorkbenchMounted` + `display:none`) | 左树选中态 + 步骤进度需跨视图保留 |
| 工作空间 store | 共享 tracker store(`useTrackerStore`) | issue 数据挂 workspace→project,共享 `selectedWorkspace` 最省事,命令面板跳转零改动 |
| state 层 | 新建 `src/state/devWorkbench/`(照 state 层架构模板),queries 复用 tracker issue/state 数据 + 自有左树选中态 | 域隔离干净 |
| 后端桩语言 | Go(§6 提示 `startDev`),依 `worktree_term.md` | worktree/PTY 真实现见该文档;P1 返回桩数据跑通 UI |

### 12.1 模块 A:工作台骨架与路由接入

- [ ] **A1** MenuKey 加 `'devWorkbench'` — `commandPalette/types.ts:6`(SSOT 一处改,TS 报错驱动补全;后端 `navigate_to` 是 `Option<String>` 无类型约束,**不改 Rust**)
- [ ] **A2** 侧栏菜单项 — `PanelApp.tsx` `menuItems`(:127) 追加 + icon import;条件渲染区(:308) 挂载 `<DevWorkbenchPage/>`;保活则加 `devWorkbenchMounted` state + 在 `navigate` 回调/侧栏 onClick/`EVENT_PANEL_NAVIGATE` 监听三处补 `setDevWorkbenchMounted(true)`,用 `display:none` 包裹(照 tracker :312-317)
- [ ] **A3** 命令面板 `nav.devWorkbench` — `commands.tsx` navigation 组(:18-54) 追加 + icon import
- [ ] **A4** i18n 文案 — `panel.json`(zh-CN/en) 加 `menu.devWorkbench` + `commandPalette.nav.devWorkbench`
- [ ] **A5** 新建 `DevWorkbenchPage/DevWorkbenchPage.tsx` — 复刻 `TrackerPage.tsx:36-105` 的 3-zone shell(顶栏 48px + 左固定宽 + 右弹性),顶栏右侧加「刷新」「清理中心」占位;顶栏切换按钮用 `aria-label`,不挂 Tooltip

### 12.2 模块 B:左侧任务树

- [ ] **B1** 新建 `src/state/devWorkbench/queries.ts` — 查询处于开发流程的 issue(`stateId` 落在 started 组、`stateCode !== 'in_progress'` 的子 state),复用 tracker issue/state 数据
- [ ] **B2** 左树组件 — workspace→project→issue 三级树,行 = issue 标题 + 子状态徽章
- [ ] **B3** 行内徽章 — 直接用 `ProjectStateView`(`queries.ts:71-80`) 的 name+color,数据已就绪无需映射
- [ ] **B4** 选中态 + 右侧联动(点行 → 右侧渲染该 issue 步骤条+内容)
- [ ] **B5** 归档过滤 — 默认只显处理中,可切显已归档(completed/cancelled)

### 12.3 模块 C:步骤条

- [ ] **C1** 取当前项目 started 组「进行中」之外子 state(`wt_init/developing/pr_open/cleanup`),按 `sortOrder` 排序
- [ ] **C2** 步骤状态计算 — `✓已完成`(stateId 已越过) / `●进行中`(= 当前 stateId) / `○待办`
- [ ] **C3** MUI `Stepper` 渲染
- [ ] **C4** 点步骤回看(已完成只读、进行中可操作)

### 12.4 模块 D:步骤内容区(按 `stateCode` switch)

- [ ] **D1 `wt_init`** — worktree 初始化表单:复用 `IssueBranchField` 的 `useLocalRepositories`+`useLocalBranches`,扩展 baseRef/devBranch/worktree 路径预览;`[创建并开始]` 调 `startDev` 桩 + 创建成功推进 stateId
- [ ] **D2 `developing`** — 终端占位框 + 提示条;快捷操作行复用 `RepositoryCard` `openTarget`(:119) + `bindings` 三函数 `openInEditor/openInTerminal/openInFileManager`(VSCode/iTerm2/访达);`[开发完成]`推进
- [ ] **D3 `pr_open`** — PR 配置卡(源分支 devBranch 只读 / 目标分支 baseBranch 可改 / 标题默认 issue 名 / 描述默认 issue.description) + 构造 compare URL 打开并记录 prUrl;`[合并完成]`推进
- [ ] **D4 `cleanup`** — 清理确认卡(列删除项 worktree 路径+分支、未提交改动警告);`[清理并完成]`调 `pty_stop_for_worktree`+`removeWorktree` 桩 → completed;`[仅停止,保留 worktree]`→cancelled

### 12.5 模块 E:状态机推进(stateId 流转)

- [ ] **E1** 推进 hook — 照搬 `useKanbanDnd.ts` 乐观更新范式(预估下一 started 子 state 的 stateId → `ProjectIssueService.move`(:103) → 权威校正/失败整表回滚)
- [ ] **E2** 取消流程 — 任意进行中步骤→cancelled,脏改动二次确认
- [ ] **E3** cleanup→completed 自动归档 — 后端 `applyStateTransition`(`project_issue.go:262`) 已支持 completed 组写 `completed_at`,前端仅推进 stateId

### 12.6 模块 F:与事项管理桥接

- [ ] **F1** `ProjectIssueDrawer.tsx` — footer(:313) 或 IssueBranchField 后(:296) 加「开始开发」按钮(条件:issue 处 backlog/unstarted/started + `localRepositoryId≠0` + 项目 started 组有非 in_progress 子 state) → 跳工作台并定位 issue(或就地推进 stateId 到首个开发步骤再跳)
- [ ] **F2** `IssueCard.tsx` `stateBadge`(:216) — 加 onClick 跳工作台(条件 `stateGroupCode==='started' && stateCode!=='in_progress'`);徽章渲染零改动(已用 state name+color)

### 12.7 模块 G:后端桩接口(P1)

- [ ] **G1** `startDev` 桩(Go,§6 提示;接收 issue+仓库+分支,返回假 worktree 记录)
- [ ] **G2** `removeWorktree` + `pty_stop_for_worktree` 桩(Go,§9.3 两阶段编排:先停 PTY 再删 worktree)
- [ ] **G3** 前端 service 封装(照 `ProjectIssueService` 风格) + 调用;P1 桩数据跑通 UI,真实现等 `worktree_term.md` 落地

### 12.8 P2 / P3 概要

- **P2**:D2 占位换真 xterm(`worktree_term.md` 阶段 2–4) + 左树活动指示点(PTY 状态 + git status 摘要脉动,§9.1)
- **P3**:D3 真 PR 创建(token,§9.2) + 清理中心批量入口(顶栏,§9.3) + 项目 started 组勾选配置开关(§5 跳过)

### 12.9 实施顺序建议(依赖)

`A`(骨架/路由) → `B`(左树,需 A 的页面) → `C`(步骤条,需 B 的选中 issue) → `D`(步骤内容,需 C 的当前步骤) → `E`(推进,串联 D 各步骤);`G`(后端桩,D1/D4 依赖) 可与 D 并行;`F`(桥接,需工作台可跳转) 最后接。
