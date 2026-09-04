# Agent 驱动开发流程——模块任务清单

> 基于 [技术方案总览](agent_dev_00_overview.md) 拆解的模块级任务清单。任务粒度为模块+功能+技术方案，不涉及具体代码文件。
>
> **状态标记**：⬜ 待开始 | 🔲 进行中 | ✅ 已完成
>
> **状态回写规则（内置，无需人工提醒）**：任务实现完成后，执行方（开发 Agent）在总结阶段
> 直接把对应任务状态改为 ✅ 并补方案变更记录（实施定稿段落，标注日期），同步提交——
> 不等待用户单独指示。

---

## 阶段 1：基础设施（P0）

> **关于子任务**：不新建子任务表。现有 `t_project_issues` 已有 `parent_id` 字段支持两级 issue——第一级为父 issue（开发工作台左侧展示），第二级为子 issue（即子任务，右侧工具条展示）。子任务的 CRUD 复用现有 `projectIssue` API，无需新增数据模型。

### T1.1 工作空间初始化 Go Service

**状态**：✅

**功能**：选中 issue 时自动检测并初始化工作空间目录

**技术方案**：
- Go 后端新增 `workspace_init` service
- 初始化流程：
  1. 创建 `workspace_base_dir/{issueId}/` 目录结构
  2. 生成 `.ssh/config`（按需）
  3. 生成 `.mcp.json`
  4. git clone 各关联仓库到 `repo/` 子目录
  5. 在每个仓库创建 `agent_{issueId}` 分支（从基准分支拉出）
  6. 写 `.workspace-ready` marker 文件
- 增量更新：关联仓库新增时 clone + 创建分支；基准分支变更时重建分支
- 幂等性：`.workspace-ready` 存在且 hostname 集合未变则跳过

**API 设计**：
- `POST /api/workspace/init`：入参 issueId，触发完整初始化
- `POST /api/workspace/status`：入参 issueId，返回初始化状态（未初始化/进行中/已完成）

**依赖**：无

---

### T1.2 工作空间 SSH Config 按需生成

**状态**：✅

**功能**：根据 issue 关联仓库的 SSH URL，从全局 `~/.ssh/config` 提取匹配 Host 段，生成 workspace 级 `.ssh/config`

**技术方案**：
- 依赖库：`github.com/kevinburke/ssh_config` v1.6.0
- SSH URL 解析：正则提取 hostname（支持 `git@host:path` 和 `ssh://git@host:port/path` 两种格式）
- Host 段匹配：精确匹配 + 通配符匹配（`*.weoa.com`）
- 生成 `.ssh/config`：仅写入匹配的 Host 段，IdentityFile 保持全局密钥原路径，不复制私钥
- 降级处理：`~/.ssh/config` 不存在或无匹配 → 跳过生成，git clone 走默认 SSH
- git clone 时通过 `GIT_SSH_COMMAND="ssh -F <workspace>/.ssh/config"` 环境变量指定 config

**依赖**：T1.1（嵌入初始化流程中，作为步骤 2）

**参考**：code-agent-scheduler 的 `git-env.ts:buildGitEnv()`

---

### T1.3 工作空间 MCP Config 生成

**状态**：✅（方案变更：workspace 级生成取消，改由 ocean-harness 插件捆绑承载）

**功能**：让工作空间内的 Claude CLI 自动接入 Go 后端 MCP Server

**技术方案（2026-09-01 变更后）**：
- MCP 以 plugin 方式驱动，workspace 级单独配置收益不大；`.mcp.json` 放 ocean-claude-plugins
  的 `plugins/ocean-harness-plugin/` 根目录（issue 流程专用插件，插件自动发现、随插件安装注册，
  免逐项审批；后续 T2.2/T2.4 的 issue 相关 skill 亦落此插件）
- 生成内容：`{"mcpServers": {"ocean-harness": {"type": "http", "url": "http://127.0.0.1:${OCEAN_HARNESS_PORT:-9100}/mcp/streamableHttp/oceanHarness"}}}`
  （type 用 Claude CLI 合法值 `http` 即 Streamable HTTP；插件 .mcp.json 支持 `${VAR:-default}` 展开）
- 端口注入：Rust `pty_spawn` spawn PTY 时注入 `OCEAN_HARNESS_PORT=<HttpServerState 端口>`
  （默认 dev=9000/build=9100，用户可配）；外部终端无此 env 时回落默认 9100
- 工作空间初始化的 mcpConfig 步骤已随方案变更整体移除（步骤骨架与前端常量一并清理，
  初始化仅剩 createDirs → sshConfig → cloneRepos），未来需要 workspace 级单独支持时再加回骨架
- 插件更新生效方式：bump plugin.json 版本 → `claude plugin update` → `/reload-plugins`

**依赖**：T1.1（嵌入初始化流程中，作为步骤 3）、T2.1（MCP Server 需要先存在）

---

### T1.4 工作空间 Git Clone 与分支创建

**状态**：✅

**功能**：根据 issue 关联的仓库+分支列表，clone 仓库到 `repo/` 子目录并创建 `agent_{issueId}` 分支

**技术方案**：
- Go 后端扩展 `gitutil` 包：新增 `Clone`、`CreateBranch`、`CheckoutBranch` 函数
- Clone 策略枚举 `CloneStrategy`（`clone` / `worktree`），本期固定 `clone`，预留 worktree 口子
- 分支命名：统一 `agent_{issueId}`，所有关联仓库同名
- Clone 方式：`git clone --branch <基准分支> <url> <target_dir>`，如基准分支不存在则 clone 后 `git checkout -b agent_{issueId}`
- 环境变量：clone 时设置 `GIT_SSH_COMMAND` 指向 workspace `.ssh/config`

**依赖**：T1.2（需要 .ssh/config）、T1.1（目录结构）

---

### T1.5 前端工作空间初始化触发

**状态**：✅

**功能**：DevWorkbenchPage 选中 issue 时自动检测工作空间状态，未初始化则触发初始化

**技术方案**：
- 前端在 DevWorkbenchPage 选中 issue 后，调 `workspace/status` API 检查状态
- 未初始化 → 弹确认框 → 调 `workspace/init` API 触发初始化
- 初始化中 → 显示进度（clone 进度等）
- 初始化完成 → 正常进入终端

**依赖**：T1.1

---

## 阶段 2：AI 流程（P0）

### T2.1 MCP Server（Go 版，项目管理工具集）

**状态**：✅

**功能**：Go 后端嵌入 MCP Server，提供 issue/子任务/工作空间查询与操作工具，供 Claude CLI 通过 MCP 协议调用

**技术方案**：
- SDK：`github.com/modelcontextprotocol/go-sdk` v0.2.0
- Transport：StreamableHTTP（`mcp.NewStreamableHTTPHandler`）
- 路由：Gin 注册 `POST /mcp/streamableHttp/oceanHarness`
- 架构：三层分离（参考 pros-admin-server）
  - Server 定义 + Tool 注册：`mcp_servers/mcp_ocean_harness.go`
  - Tool Handler 实现：`mcp_servers/mcp_ocean_harness_tools.go`
  - DTO 类型定义：`mcp_servers/mcp_dto/ocean_harness_tools.go`

> **注（2026-09-03 随 T4.1 实施后用户定稿变更）**：原「多 server 并存、路由按 server
> 扩展」的预留框架取消——全部工具（含 T4.1 的 github_*）归口单一 ocean_harness server，
> 详见 T4.1 实施定稿的「单 server 归口」变更段。
- 复用现有 `apis.McpTool` 基类（MakeContext/MakeOrm/Validate/MakeService）

**首期工具清单**：

| 工具名 | 功能 |
|--------|------|
| `issue_get_info` | 获取 issue 详情（标题、描述、状态、关联仓库） |
| `issue_update` | 更新 issue（状态、描述） |
| `issue_child_list` | 获取子任务列表（查 parent_id = issueId 的子 issue） |
| `issue_child_create` | 创建子任务（创建 parent_id 指向父 issue 的子 issue） |
| `issue_child_update` | 更新子任务状态（更新子 issue 的 state_code） |
| `issue_workspace_status` | 获取工作空间状态 |

> **注**：子任务复用现有 issue 父子关系（`parent_id` 字段），MCP 工具内部调现有 `projectIssue` service，无需新表。

**依赖**：无（子任务复用现有 issue 父子关系，无需新表）

**参考**：pros-admin-server 的 `mcp_servers/mcp_demo.go` + `services/apis/mcp_tool.go`

---

### T2.2 新增 Skill：`/ocean-harness:refine-issue`

**状态**：✅

**功能**：AI 需求润色与子任务拆分，基于源码上下文澄清需求，首次生成 AGENT.md/CLAUDE.md

**技术方案**：
- 在 ocean-claude-plugins 项目新增 `plugins/ocean-harness-plugin/commands/refine-issue.md`（issue 流程 skill 统一落 ocean-harness 插件）
- allowed-tools：Agent, AskUserQuestion, Read, Glob, Grep, Bash, Write, Edit, mcp__plugin_ocean-harness_ocean-harness
- 流程：
  1. 通过 MCP 工具获取 issue 原始描述
  2. 读取仓库源码理解代码库结构
  3. AI 分析需求、澄清歧义
  4. AskUserQuestion 与用户交互确认
  5. 按需拆分子任务
  6. 首次生成 AGENT.md / CLAUDE.md
  7. 通过 MCP 回写子任务到 DB、更新 issue 描述

**实施补充（2026-09-01 定稿）**：
- 落地为 command + skill 契约分离：`commands/refine-issue.md`（四阶段流程编排）+
  `skills/issue-context/SKILL.md`（AGENT.md/CLAUDE.md 模板契约、子任务拆分规范、进度段
  更新规范）——模板单一真相源，T2.4 agent-dev 经 `skills: issue-context` 引用同一契约
- 四阶段：定位与采集（cwd basename 推导 issueId + MCP 取上下文 + 增量检测 + Agent 探索
  源码）→ 分析与澄清 → 成稿与确认（循环确认「确认回写」）→ 回写落盘
- 回写顺序：AGENT.md → 子任务（create 记录返回 DB ID）→ CLAUDE.md（子任务表含 DB ID，
  agent-dev 凭此更新状态）→ issue_update（description=结构化润色稿；仅当前为 BACKLOG
  时流转 stateCode=TODO，父状态变化级联子任务）
- 重复执行为增量模式：已有子任务默认保留，成稿时标注 [新增]/[保留]/[建议作废] 差异，
  用户确认后回写；润色稿为结构化重写（背景/目标/需求明细/边界/验收标准），原始描述在
  CLAUDE.md 原文存档
- AGENT.md 首次生成为深入分析（Agent 并行探索代码库），已存在时仅增量补充不重写
- 随附 plugin.json bump 1.1.0、插件 README 更新

> **注（2026-09-02 随 T2.3 方案变更修订）**：本段为 2026-09-01 定稿时的实施记录，其中
> 「进度段更新规范」「CLAUDE.md 子任务表含 DB ID，agent-dev 凭此更新状态」「仅 BACKLOG 时
> 流转 TODO（后改为双条件：BACKLOG 且无 IN_PROGRESS/DONE 子任务）」已被 T2.3 的 CLAUDE.md
> 去状态化变更推翻——以 T2.3/T2.4 段与 issue-context 技能现行契约为准。

**依赖**：T2.1（MCP 工具）、T1.4（仓库已 clone 才能读源码）

**参考**：ocean-claude-plugins 的 `commands/feature-dev.md`（AskUserQuestion 交互模式）

---

### T2.3 AGENT.md / CLAUDE.md 生成与动态更新

**状态**：✅（方案变更：CLAUDE.md 去状态化，随 T2.4 收口）

**功能**：refine-issue 首次生成 AGENT.md/CLAUDE.md

**技术方案（2026-09-02 变更后）**：
- AGENT.md（AI 生成，首次）：项目上下文、编码规范、架构概览（不变）
- CLAUDE.md（AI 生成，首次）：原始需求存档 + 润色后需求快照 + 注意事项——纯需求上下文，
  不记录任何状态
- 更新机制：refine-issue 写入首次内容，增量重跑按 issue-context「增量重跑规则」修订
- **变更原因**：原设计的「CLAUDE.md 记录子任务列表/状态/进度 + agent-dev 每完成一个
  子任务同步更新进度段」与 DB 形成同一数据两份副本，双写漂移且无必要——agent-dev 本就
  经 MCP 读写 DB。子任务状态唯一真相源为数据库（MCP `issue_child_list` / 看板），
  agent-dev 执行期不修改上下文文件，「进度段更新规范」整体取消

**依赖**：T2.2（refine-issue 生成）

---

### T2.4 新增 Skill：`/ocean-harness:agent-dev`

**状态**：✅

**功能**：Agent 自动执行开发任务，按子任务清单逐项完成并经 MCP 回写状态

**技术方案**：
- 在 ocean-claude-plugins 项目新增 `plugins/ocean-harness-plugin/commands/agent-dev.md`
- allowed-tools：Agent, AskUserQuestion, Read, Glob, Grep, Skill, Bash, Write, Edit,
  TaskCreate, TaskUpdate, mcp__plugin_ocean-harness_ocean-harness
- 流程：
  1. MCP `issue_get_info` + `issue_child_list` 获取任务上下文（description 即澄清后需求）
  2. 读 AGENT.md/CLAUDE.md 作只读上下文（issue-context 契约）
  3. 无子任务 → issue_update IN_PROGRESS → 整体执行 → DONE；有子任务 → 逐项执行
  4. 执行子任务：置 IN_PROGRESS → 探索 → 实施 → 对照完成标准自检 → `issue_child_update` 置 DONE
  5. 全部完成 → 后端联动父 issue 自动 DONE（不显式流转）
- 与 feature-dev 关系：独立 Skill，内部逻辑参考 feature-dev 的探索→实施→审查模式，但跳过澄清/确认阶段

**实施定稿（2026-09-02）**：
- issueId 改为 cwd basename 推导（uuid 校验），不显式传参，与 refine-issue 一致
- 不依赖 CLAUDE.md 存在：issue 描述即澄清后需求（refine-issue 回写），未经润色、描述
  足够清晰的 issue 可直接执行
- 状态安全规则：有子任务时绝不流转父 issue 状态（父→子级联会把 DONE 打回、CANCELLED
  复活）；逐个流转子任务，父由「全部子任务 DONE」后端联动自动完成；CANCELLED 子任务
  会阻断父自动完成，此时不强制流转，提示用户在 tracker 手工处理
- 续跑语义：IN_PROGRESS 子任务视为上次中断，重新执行；DONE/CANCELLED 跳过；实施受阻
  AskUserQuestion（重试/跳过/终止），跳过不置 DONE
- 执行前分支检查：各仓库须在 agent_{issueId} 分支，不符时 AskUserQuestion 切回/终止
- 随附 issue-context 契约修订（CLAUDE.md 去状态化，见 T2.3）、plugin.json bump 1.2.0、
  插件 README 更新

**依赖**：T2.1（MCP 工具）、T2.2（需求上下文软依赖——未润色 issue 可直接执行）

---

## 阶段 3：UI 增强（P1）

### T3.1 子任务列表面板（右侧工具条）

**状态**：✅（方案变更：手动刷新替代自动刷新、纯展示替代会话切换）

**功能**：DevWorkbenchPage 右侧可折叠面板，展示 issue 的子任务列表及各自状态

**技术方案**：
- 前端新增 `IssueSubTaskPanel` 组件
- 位置：DevWorkbenchPage 右侧可折叠区域
- 数据：React Query 订阅 `issueSubTask/getList` API
- 展示：序号 + 标题 + 状态图标（☐ PENDING / ▶ IN_PROGRESS / ☑ DONE / ⊘ SKIPPED）
- 交互：点击子任务 → 终端切换到对应 Claude 会话
- 实时同步：MCP 更新 DB → React Query 自动刷新

**实施定稿（2026-09-02）**：
- 数据零新增：`issueSubTask/getList` API 不存在也无需新建——面板订阅现有 `useProjectIssues`
  （同 query key 与左树/顶栏共享缓存，零新增请求），子任务由 `filterIssueSubTasks`
  （devWorkbench 域 derive.ts）按 parentId 过滤 + sortOrder 升序前端派生
- 状态枚举按实际映射：PENDING→TODO（BACKLOG 同灰圈）、SKIPPED→CANCELLED（灰杠+删除线）；
  IN_PROGRESS 转圈、DONE 绿勾（对齐 WorkspaceInitGate StepStatusIcon 风格）
- 交互变更：子任务与终端会话无映射（agent-dev 在单会话内逐项执行），「点击切换对应
  Claude 会话」不可行，本期纯展示（行不可点）
- 实时同步变更：后端 MCP 写库无事件推送，自动刷新整体后移——本期面板头部手动刷新按钮
  （invalidate `trackerKeys.projectIssues`，左树/顶栏状态徽章同步受益），自动刷新待单独
  方案（`tracker:changed` Tauri 事件方向，state/README.md 已预留）
- 布局：DevWorkbenchPage 根 flex 行新增第三栏 280px，折叠范式照抄左栏（外层 width 过渡 +
  内层固定宽防重排），折叠态走 appConfig 新 key `panel_dev_subtask_collapsed`（跨重启/
  多窗口同步）；未选中 issue 时收 0 不占位，选中但无子任务显示空态引导（提示运行
  /ocean-harness:refine-issue）
- 文件：`components/IssueSubTaskPanel/IssueSubTaskPanel.tsx` 单文件（状态图标用内部函数
  不导出，未拆 subtaskMeta.ts，规避图标组件引用类型体操）

**依赖**：无（复用现有 projectIssue API，按 parentId 过滤）

---

### T3.2 任务归档/取消按钮与流程

**状态**：✅（方案变更：父子状态级联解耦 + 两段式契约 + ⋯ 菜单形态，见实施定稿）

**功能**：DevWorkbenchPage 右上角增加归档/取消 icon，点击后删除工作空间目录并更新 issue 状态

**技术方案**：
- 前端：DevWorkbenchPage 顶部操作栏新增归档/取消 icon 按钮
- 点击后弹出确认框（含安全检查提示：未提交/未推送的变更）
- 确认后调 Go 后端 API
- Go 后端新增 `POST /api/workspace/archive`：
  1. 安全检查：遍历工作空间仓库，`git status --porcelain` 检查未提交变更，`git log origin/agent_{issueId}..HEAD` 检查未推送提交
  2. 有未提交/未推送 → 返回警告，前端二次确认
  3. 删除 `workspace_base_dir/{issueId}/` 目录
  4. 更新 issue 状态（归档 → DONE，取消 → CANCELLED）
- 归档和取消都是工程化操作，不依赖 AI

**实施定稿（2026-09-02）**：
- API 落地 `POST /api/issueWorkspace/archive`（对齐现有命名空间，非原文的 /api/workspace/）
- **两段式契约**：force=false 仅安全检查不执行（干净返回空 warnings，前端内部续发执行段）；
  force=true 跳过检查直接执行。检查永不删目录——「删目录前必杀该 issue 全部终端会话」
  （ptyShutdownIssue）固定在前端执行段前置，用户在警告态取消零副作用
- **安全检查**：仓库清单读状态文件 steps[].repos（.workspace-ready marker 从未实现，状态
  文件即唯一真相）；未推送对照本地 origin/agent_{issueId} ref（不联网 fetch，ref 不存在 =
  从未推送警告）；检查失败的仓库记警告不中止（宁可多警告不可漏警告）
- **父子状态级联解耦（用户定稿，影响全链路）**：删除 project_issue.go 的
  maybeSyncChildrenState 及 Update/Move 两处调用——父状态流转不再级联子任务（含看板
  拖拽父卡）；保留唯一单向联动「全部子任务完成 → 父自动 DONE」（maybeAutoCompleteParent）；
  新建子任务默认 BACKLOG（STATE_CODE_DEFAULT 原有行为）。顺手移除 applyStateTransition
  从未使用的 orm 参数
- 防护：issueWorkspaceActive 拒绝（init 进行中）；issueWorkspaceValidIssueID 路径穿越校验；
  os.RemoveAll 幂等（未初始化/重复归档安全）；事务流转照 Move 范式（completed_at 口径 +
  父自动完成联动）
- UI（用户定稿）：单个 ⋯（MoreHoriz）icon + Menu 两项「归档任务…/取消任务…」（非两个
  独立 icon）；确认 Dialog 首确认 → 警告态（Alert 列表 + error 色强确认按钮）→ force 执行；
  成功 toast + selectIssue(null) + 清 URL ?pid&iid 双清（防 URL→store 同步恢复选中）+ 失效
  projectIssues/issueWorkspace.status 双缓存（左树自动移出、状态回 NOT_INITIALIZED）
- 文件：gitutil/status.go（新增）、issue_workspace_archive.go（新增）、types/issue_workspace.go、
  controller/issue_workspace.go、router.go、project_issue.go（Go 侧）；
  IssueWorkspaceService.ts、services/index.ts、state/issueWorkspace/queries.ts+index.ts、
  DevWorkbenchPage.tsx（前端）

**依赖**：T1.1

---

### T3.3 Issue 详情页「AI 润色」触发按钮

**状态**：✅

**功能**：Issue 详情页（ProjectIssueDrawer）新增「AI 润色」按钮，点击后在终端中执行 `/ocean-harness:refine-issue`

**技术方案**：
- 前端在 ProjectIssueDrawer 组件中新增「AI 润色」按钮
- 点击后：
  1. 检查工作空间是否已初始化（调 `workspace/status`）
  2. 未初始化 → 先触发 T1.5 初始化流程
  3. 已初始化 → 跳转到 DevWorkbenchPage，并在终端中注入 `/ocean-harness:refine-issue` 命令
- 终端命令注入：复用现有 `startup_command` 机制（shell-ready 后自动注入）

**依赖**：T2.2（Skill 已存在）、T1.5（工作空间初始化）

**实施定稿（2026-09-03）**：
- **方案变更（startup_command 已退役）**：原方案的「startup_command 机制（shell-ready
  后自动注入）」已随 chat 模式退役删除，现行 directCommand 仅能整条直启 CLI（token
  白名单不含空格/冒号），承载不了 slash 命令注入。落地为「意图标志 + 终端侧编排」：
  devWorkbench store 新增 `pendingRefine {issueId, requestedAt}`，抽屉按钮写入 +
  跳转（与「进入开发」同款 URL），EmbeddedTerminal（main pane）useRefineInjection
  hook 消费编排；工作空间未初始化由 WorkspaceInitGate 现有面板引导手动初始化，
  闸门放行后编排自然续上（零新增 UI）
- **注入编排时序**（全链事件驱动，无延时猜测）：会话 active → await 进程探测
  （ptyClaudeRunning，不复用 useClaudeRunning——其回填存在「先见 false」竞态，
  自动注入不可接受）→ claude 已运行 toast 提示手动执行 / 未运行则裸 shell
  （plain/reattach）补发 claude\r（直启 direct 不补发，防 REPL 当 prompt 文本）
  → 等 EVENT_CLAUDE_SESSIONS_CHANGED 重 probe 转 true → 写入命令 → 清标志。
  usePtySession 新暴露 spawnKind（direct/plain/reattach，attach 落点真值）供直启判定
- **双超时职责分离**：意图过期 10 分钟（编排启动前搁置——闸门卡住/用户离开场景防
  「数小时后误注入」）+ 就绪超时 30s（编排启动后 claude 未起来，静默清不提示——
  用户定稿「尽力而为」）；finish 幂等 latch 保证「一次注入」为本地不变量
- **左树过滤放宽**（用户定稿）：isDevIssue 从「IN_PROGRESS 顶级」放宽为「非终态
  （BACKLOG/TODO/IN_PROGRESS）顶级」——工作台覆盖润色→开发全生命周期，「AI 润色」
  与「进入开发」共用 URL 选中链路；DONE/CANCELLED 不展示（终态已收尾且工作空间多
  已归档清理）
- **按钮门槛**（用户定稿）：仅终态禁用（DONE/CANCELLED，title 提示「终态任务无需
  润色」）+ dirty 禁用（未保存修改时禁用，防静默丢弃编辑且润色旧版——按钮语义
  「润色我刚写的」放大误用概率，故比「进入开发」更严）
- **无需环境变量**：refine-issue 由 cwd basename 推导 issueId（T2.2 定稿），
  OCEAN_HARNESS_PORT 已由 pty_spawn 注入，链路零新增环境变量
- **随附**：useToast 新增 info severity；TerminalView 工具栏禁用态半透明修复
  （显式 sx color 压过 MUI .Mui-disabled 灰导致置灰不可见，用户反馈）
- **遗留**（审查记录，低收益重构待后续）：终态判定在 derive.ts 与抽屉双写（A-2）、
  注入 hook 内锚点重派生 issueId::main（D-1）、Rust 直启回落裸 shell 时 spawnKind
  误报 direct（alias 安装 claude 场景自动润色静默失效，已留 debug 日志）

---

## 阶段 4：开发闭环（P1）

### T4.1 MCP Server 外部服务工具（GitHub）

**状态**：✅

**功能**：MCP Server 新增 GitHub 外部服务工具，支持创建 PR、列出 PR、获取 CI 状态

**技术方案**：
- Go 后端新增 `mcp_servers/mcp_github.go` + `mcp_github_tools.go`
- GitHub API 调用：通过 `golang.org/x/oauth2` + GitHub REST API v3
- 认证：GitHub Personal Access Token，从 workspace 配置或环境变量读取
- 新增工具：

| 工具名 | 功能 |
|--------|------|
| `github_create_pr` | 创建 Pull Request（标题、描述、head/base 分支） |
| `github_list_prs` | 列出仓库的 PR 列表 |
| `github_ci_status` | 获取 PR 的 CI 检查状态 |

**依赖**：T2.1（MCP Server 框架已搭建）

**实施定稿（2026-09-03）**：
- **落地结构（按 T2.1 预留扩展框架）**：`mcpservers/mcp_github.go`（Server 单例 + 3 工具
  注册 + Handler 工厂）+ `mcp_tool/mcp_github_tools.go`（handler）+ `mcp_dto/github_tools.go`
  （三 tag DTO）+ router 一行挂 `/mcp/streamableHttp/github`；`McpTool` 基类与
  McpOK/McpFail 共用零改动
- **变更（2026-09-03 用户定稿：单 server 归口）**：github 独立 server 取消——`mcp_github.go`
  删除、3 个 `github_*` 工具注册并入 `mcp_ocean_harness.go` 的 `init()`（工具名前缀分组，
  对 AI 调用方透明），router 删 `/mcp/streamableHttp/github`，插件 `.mcp.json` 回归单
  `ocean-harness` 条目；T2.1 预留的「多 server 按路径扩展」框架整体取消（多端点对调用方
  有割裂感），代码组织改为「单 server + 工具前缀分组 + handler 按业务域文件」
- **方案变更（oauth2 未引入）**：PAT 为静态令牌，oauth2 全套流程用不上——改
  `internal/githubapi/` 纯 net/http 客户端（Authorization: Bearer + vnd.github+json），
  API base 固定 api.github.com，少一个依赖（全库首个出站 HTTP 保持最小面）
- **平台适配（用户定稿「各 host 单独适配」）**：`gitutil.ParseRemoteURL`（新写，scp/
  https/ssh:// 三形态 → host/owner/repo + 单测，企业 host 用例覆盖）→ host 判定集中
  handler 层 `githubTarget`：github.com → githubapi，其他 host 报「暂仅支持 github.com，
  gitee/gitlab 待后续」——后续平台加 api 包 + 判定分支即可，不动现有结构
- **PAT 链路（用户定稿 app_config KV）**：设置窗口新增 **userProfile（个人中心）分区**
  （用户定稿命名，凭据卡片组织——后续第三方账号/gitee 私有部署/apikey 各自成卡片）；
  GitHub 卡片密码型录入（不回显明文、已配置状态 Chip、空输入保持不变、清除按钮）；
  Rust `GITHUB_PAT_KEY` 常量经 specta 导出（gen:bindings），Go `mcputil.ReadGithubPAT()`
  短连接只读 app.db（与 ReadWorkspaceBaseDir 同范式，顺带重构出共用 helper）
- **工具入参（用户定稿 localRepositoryId）**：三工具均以 localRepositoryId 定位仓库
  （service.GithubTool.ResolveRepo 查 remote_url + issue 关联基准分支）；create_pr 的
  head 缺省 `agent_{issueId}`、base 缺省「issue 关联基准分支 → 仓库默认分支」（两级
  回退，均空则要求显式传 base）；list 缺省 open（≤50 条）；ci_status 为 combined
  status + check runs 归并
- **插件侧同步（用户定稿纳入本期）**：ocean-claude-plugins 的 `.mcp.json` 加 github
  server 条目（同款 `${OCEAN_HARNESS_PORT:-9100}` 展开）+ plugin.json bump 1.4.0 +
  README 补 github 工具说明（变更在插件仓库工作树，待随插件仓库提交）

---

### T4.2 新增 Skill：`/ocean-harness:create-pr`

**状态**：⬜

**功能**：基于当前分支变更自动生成 PR 标题和描述，通过 MCP 创建 PR

**技术方案**：
- 在 ocean-claude-plugins 项目中新增 `commands/create-pr.md`
- allowed-tools：AskUserQuestion, Bash(git*), Read, MCP
- 流程：
  1. 获取当前分支名和基准分支
  2. `git diff base...head` 获取变更
  3. AI 生成 PR 标题和描述（变更摘要 + 测试计划）
  4. 通过 MCP `github_create_pr` 创建 PR
  5. 通过 MCP `issue_update` 更新 issue 状态

**依赖**：T4.1（GitHub MCP 工具）

---

### T4.3 agent-dev 完成后自动提交代码

**状态**：⬜

**功能**：agent-dev 执行完子任务后，自动调用 `/ocean-code:git-auto-commit-push` 提交并推送

**技术方案**：
- 在 agent-dev Skill 的子任务执行完成逻辑中，增加步骤：调用 `/ocean-code:git-auto-commit-push`
- 复用现有 git-auto-commit-push Skill，无需新建
- 提交后继续下一个子任务（如有）
- 全部子任务完成后，提示用户是否执行 `/ocean-harness:create-pr`

**依赖**：T2.4（agent-dev）

---

### T4.4 完整流程端到端验证

**状态**：⬜

**功能**：验证从初始化 → 润色 → 执行 → 提交 → PR → 归档的完整闭环

**验证场景**：
1. 新建 issue → 选中 → 工作空间自动初始化
2. Issue 详情页点击「AI 润色」→ 终端执行 refine-issue → AGENT.md/CLAUDE.md 生成 → 子任务创建
3. 终端执行 agent-dev → 逐项执行子任务 → 状态实时更新
4. 执行完成 → git-auto-commit-push → create-pr
5. 归档 → 工作空间删除 → issue 状态更新

**依赖**：T1.x ~ T4.x 全部完成

---

## 阶段 5：增值功能（P2）

### T5.1 工作空间文件浏览器与 Diff 查看

**状态**：⬜（列表 + 预览已落地，diff 未做——见实施定稿）

**功能**：右侧工具条展示工作空间文件列表，查看 diff 和文件内容

**技术方案**：
- Go 后端新增 API：
  - `POST /api/workspace/fileList`：列出 `repo/` 下的文件树（`git ls-files`）
  - `POST /api/workspace/fileDiff`：获取指定文件的 `git diff`（对比基准分支）
  - `POST /api/workspace/fileContent`：获取文件内容（文本直接展示，图片预览，二进制显示信息）
- 前端新增 `WorkspaceFilePanel` 组件，参考 hello-halo 的 `FileChangesList` + `DiffContent`
- 布局：右侧工具条切换「子任务」/「文件」标签页
- Diff 展示：侧边栏模式（非弹窗），适配 DevWorkbenchPage

**依赖**：T1.1（工作空间已初始化）

**参考**：hello-halo 的 `src/renderer/components/diff/`

**实施定稿（2026-09-04，列表 + 预览部分落地）**：

- 本期完成：工具条「文件」工具（toolRegistry 注册，FolderOutlined/exclusive）；工具面板区
  展示工作空间一次性全目录树（自绘递归树 + 默认展开 `repo/` + 手动刷新）；点击文件在
  **终端内容区上方浮层**预览（absolute inset 0 + zIndex mobileStepper，不挤压布局零
  SIGWINCH），浮层内**多 tab**（tab id = 文件相对路径，按 issue localStorage 持久化，
  Escape 关激活 tab）。预览查看器：代码 = CodeMirror 6 只读态（`readOnly + editable(false)`，
  虚拟滚动）、markdown = 复用 `@uiw/react-md-editor` 的 `MDEditor.Markdown`、图片 = base64
  data URL `<img>`、二进制/超大 = 信息面板。
- 后端落地：`/api/issueWorkspace/getFileTree`（WalkDir 扁平节点表 + 前端 `buildFileTree` 纯函数
  组树）与 `/api/issueWorkspace/getFileContent`（kind = text/image/binary/tooLarge，后端定夺
  传输类型；文本 2MB / 图片 8MB / 节点 2 万上限）。
- 方案偏离 ①：fileList 由 `git ls-files` 改为**全目录树 + 后端忽略清单**（.git/
  node_modules/.workspace-init-state.json/.DS_Store + \_\_pycache\_\_/target/dist/build/out/
  .next/.venv/venv 共 12 项——.ssh 按用户决策移出名单、树中可见，2026-09-04）——产物浏览
  需看见 agent 产出的未跟踪新文件；忽略名单同时约束两接口（列表看不见的文件点名也读不到）。
- 方案偏离 ②：路由由规划的 `/api/workspace/*` 改挂 **`/api/issueWorkspace/*`**——与 tracker
  的 workspace（任务管理容器）区分，文件浏览器操作对象即 issue 运行工作空间本体，且复用该域
  baseDir/issueId 防穿越校验范式。
- 方案偏离 ③：代码高亮选 **CodeMirror 6 只读态**（原方案未定组件）——为下期编辑铺路，
  摘掉两行只读配置即得编辑器；配套语言包 js/ts/go/py/rs/json/css/html（未命中纯文本展示，
  刻意不引 legacy-modes 近似映射）。
- 前端状态：新域 `src/state/workspaceFiles/`（keys/queries + zustand store：预览 tabs 持久化、
  树展开态会话级）；`useInitIssueWorkspace` 成功后整域失效文件缓存。
- 剩余未做（下期接续，契约已预留）：`fileDiff`（git 变更标记 + diff 视图，gitutil 基建在位）、
  `fileSave`（编辑保存，复用同一套路径安全链/文本判定，前端 CodeViewer 摘 readOnly 即编辑器）。

**实施定稿补记（2026-09-04，预览层升级为 hello-halo 同款栈）**：

- **CM6 代码查看观感对齐 halo**：移植其 `codemirror-theme.ts`（SF Mono 字体栈/行高 1.6/
  gutter 样式/活动行/折叠槽/搜索面板全套样式 → 新增 `fileViewer/codeViewerTheme.ts`，
  shadcn CSS 变量改接 MUI palette 双模式）+ reader-first 扩展集（foldGutter ▸/▾、Cmd+F
  搜索、选词高亮、括号匹配、scrollPastEnd）+ 官方语言包扩充（yaml/xml/sql/cpp/java/php/
  vue/markdown；仍不引 legacy-modes 近似映射）。
- **Markdown 换 Streamdown**（halo 同款）：static 模式 + `@streamdown/code` Shiki 双主题
  （github-dark 在前——内联色取首主题的已知约定）+ KaTeX 公式（remark-math/rehype-katex，
  katex css 随插件懒加载——Streamdown 声明不注入的必要补充）+ 预览/源码切换 + 复制按钮。
  配色排版由容器 sx 承载（streamdown styles.css 只管动画布局；halo 靠 tailwind prose，
  本项目用 sx 复刻关键排版参数，不引入 tailwind——避免 MUI 双体系/preflight 冲突）。
  md 内嵌相对路径图片解析（./ ../ / 裸相对，halo resolveImageSrc 简化版）→ fileRaw URL。
- **图片直连**：新增 `GET /api/issueWorkspace/fileRaw`（query 传 issueId/baseDir/path；校验链
  与 getFileContent 完全一致 + 图片扩展名白名单；原始字节 + Content-Type，类静态资源，
  no-store）。`getFileContent` image 分支改 stat-only（kind/mimeType/size，不再 base64 整读，
  图片不再受大小上限约束）。前端 `<img src>` 直指（零 base64 转码），`?v=dataUpdatedAt`
  缓存刷新令牌。curl 自测 9 项全过（含 symlink 逃逸/穿越/忽略名单回归）。
- **.ssh 移出忽略名单**（用户决策，2026-09-04）：工作空间内 ssh 配置在树中可见、可点击
  预览；其余过滤项不变，忽略名单对两接口的双约束不变，`../` 穿越与 EvalSymlinks root
  包含断言等路径安全全部保留。
- **图片交互换 react-zoom-pan-pinch**（替代 halo ~150 行手写缩放平移数学）：滚轮/双击/pinch
  缩放、拖拽平移内建，fitOnInit 加载即 contain，适应窗口按钮重算（先测量容器后使用），
  棋盘格透明底（halo 同款四渐变，随主题深浅两套）+ 尺寸/百分比显示。
- 依赖净增：streamdown/@streamdown/code、@codemirror/{search,commands,lang-yaml,lang-xml,
  lang-sql,lang-cpp,lang-java,lang-php,lang-vue,lang-markdown}、@lezer/highlight、
  react-zoom-pan-pinch、remark-math/rehype-katex/katex。

**实施定稿补记二（2026-09-04，预览细节打磨）**：

- **tab 栏交互**：右缘新增「关闭全部」按钮（store 增 `closeAllPreviewTabs` → 置共享空常量，
  落盘链自动清 key）；长文件名省略号截断（label Typography `flex + minWidth:0`、关闭钮
  `flexShrink:0`——flex 默认 `min-width:auto` 不收缩是截断失效根源）。
- **CM6 光标与活动行**：去掉 `editable.of(false)`（与 readOnly 双挂会把光标/焦点一起禁掉；
  readOnly 单独即可防编辑且保留闪烁光标与选中，halo 同款）；活动行/活动行号/内联码底色
  修正 `alpha(action.hover, 0.6)` 误用（alpha() 会整体覆盖 MUI token 自带透明度 → 深色下
  60% 白刺眼）——直接用 `action.hover` 原生微灰。
- **md 代码块整块自绘**（弃用 streamdown 2.6 内置 CodeBlock）：其 token 配色/块样式只通过
  tailwind 任意值类（`text-[var(--sdm-c)]` 等）生效，无 tailwind 全失效；且 context 缺省
  `shikiTheme` 时其 highlight 同步崩溃（读 `undefined[0]`，实测复现）——「代码块展示不出
  来」的双重根因。改走 `plugins.renderers`（精确语言匹配，覆盖常见 fence 语言清单）自绘：
  CM6 CodeViewer 承载配色/明暗/折叠/搜索（与源码模式同观感），头部语言标签 + 复制 + 全屏
  （Dialog 铺满），**无下载按钮**（halo 同款为已知 bug，刻意不跟）。`@streamdown/code` 依赖
  移除；`shikiTheme` 传合法元组兜底罕见语言不崩溃；`controls` 全关（表格等内置控件同为
  tailwind 依赖）。版本保持最新 streamdown@2.6.0（用户决策，不随 halo 降 2.2.0）。

**实施定稿补记三（2026-09-04，md 切 tab 卡顿根治与 tab 观感修正）**：

- **md 代码块弃用「每块一个 CM6 实例」改 shiki 静态高亮**（halo 同款高亮内核，直连 shiki
  而非 @streamdown/code 插件——保留自绘块头部，块体引擎与 halo 同源）：卡顿根因为切 tab
  整层重挂载（`PreviewContent` 按 path 作 key）时 N 个代码块各建一个完整 EditorView 同步
  压在主线程（实测观感 4-5s，仅 md 触发；请求层排除——服务地址缓存 + 内容 SWR 缓存命中
  同步返回）。新链路：块挂载即纯文本 pre 底，`highlightCodeBlock` 异步回填双主题静态 HTML
  （`github-light/dark` + `defaultColor:false` → token 颜色走 `--shiki-light/dark` CSS var，
  明暗切换零重高亮）；highlighter 单例懒加载 + 语言按需 `loadLanguage`（shiki 内部幂等守卫
  去重，无需额外簿记；单例/加载失败置空缓存可重试），模式同 katex 插件；未知语言/失败回落
  纯文本。新增依赖 shiki@4.4.3（动态 import 按需 chunk 不进
  首屏）。折叠/Cmd+F 保留在全屏 Dialog 的按需 CM6 实例（单实例打开才建）。
- **MuiTab 全局关大写**（`AppThemeProvider` styleOverrides，MuiButton 同款先例）：MUI Tab 根
  默认 `text-transform: uppercase` 把预览 tab 文件名整体大写，与磁盘实际大小写不一致。
- **右缘「关闭全部」icon** 由 `CloseFullscreenOutlined` 改 `Close`（用户决策，与单 tab 关闭
  视觉统一，tooltip 区分语义）。
- **DEV 计时日志**：`workspaceFiles` content query 记录 fetch 耗时；`PreviewContent` 记录
  「挂载 → 内容提交」耗时（`import.meta.env.DEV` 门控），供切 tab 性能自查。

---

### T5.2 Skill/MCP/Plugin 可视化配置

**状态**：⬜

**功能**：Settings 页新增 Skill/MCP/Plugin 管理入口，可视化查看和配置已安装项

**技术方案**：
- Go 后端新增 `t_plugin_registry` 表：记录已安装的 plugin、skill、MCP server
- 前端 Settings 页新增「插件管理」分区
- 发现机制：文件系统扫描 `.claude-plugin/` 和 `.mcp.json`
- 初期仅展示，后续支持安装/卸载操作
- 本期优先级低，前期手动安装即可

**依赖**：T2.1（MCP Server 存在后才需要管理）

---

### T5.3 Worktree 模式支持

**状态**：⬜

**功能**：工作空间初始化支持 git worktree 代替 git clone

**技术方案**：
- Go 后端 `CloneStrategy` 枚举已有 `worktree` 值，本期固定 `clone`
- 实现时：新增 worktree 分支处理逻辑——先 clone 主仓库到共享位置，再 `git worktree add` 创建工作目录
- 配置项：`WorkspaceConfig.CloneStrategy` 可选 `clone` / `worktree`
- 无需改上层接口，仅扩展 `gitutil` 包

**依赖**：T1.4（git clone 已实现）

---

## 依赖关系图

```
T2.1 MCP Server（无前置依赖，子任务复用现有 issue 父子关系）
  ├─→ T2.2 refine-issue Skill
  │    └─→ T2.3 AGENT.md/CLAUDE.md 生成
  │    └─→ T2.4 agent-dev Skill
  │         └─→ T4.3 自动提交代码
  └─→ T4.1 GitHub MCP 工具
       └─→ T4.2 create-pr Skill

T1.1 工作空间初始化 Service
  ├─→ T1.2 SSH Config 生成
  ├─→ T1.3 MCP Config 生成
  ├─→ T1.4 Git Clone + 分支
  ├─→ T1.5 前端触发初始化
  ├─→ T3.2 归档/取消
  └─→ T5.1 文件浏览器

→ T3.1 子任务列表面板（复用现有 projectIssue API，按 parentId 过滤）
T2.2 + T1.5 → T3.3 AI 润色触发按钮
T2.1 → T5.2 插件可视化配置
T1.4 → T5.3 Worktree 支持
```
