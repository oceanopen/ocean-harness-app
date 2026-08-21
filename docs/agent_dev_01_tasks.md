# Agent 驱动开发流程——模块任务清单

> 基于 [技术方案总览](agent_dev_00_overview.md) 拆解的模块级任务清单。任务粒度为模块+功能+技术方案，不涉及具体代码文件。
>
> **状态标记**：⬜ 待开始 | 🔲 进行中 | ✅ 已完成

---

## 阶段 1：基础设施（P0）

> **关于子任务**：不新建子任务表。现有 `t_project_issues` 已有 `parent_id` 字段支持两级 issue——第一级为父 issue（开发工作台左侧展示），第二级为子 issue（即子任务，右侧工具条展示）。子任务的 CRUD 复用现有 `projectIssue` API，无需新增数据模型。

### T1.1 工作空间初始化 Go Service

**状态**：⬜

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

**状态**：⬜

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

**状态**：⬜

**功能**：在工作空间根目录生成 `.mcp.json`，指向 Go 后端的 MCP Server StreamableHTTP 端点

**技术方案**：
- 生成内容：`{"mcpServers": {"we-terminal": {"type": "streamableHttp", "url": "http://localhost:13173/mcp/streamableHttp/weTerminal"}}}`
- 端口从 Go 后端配置读取，不硬编码
- Claude CLI 启动时自动发现工作空间根目录的 `.mcp.json`

**依赖**：T1.1（嵌入初始化流程中，作为步骤 3）、T2.1（MCP Server 需要先存在）

---

### T1.4 工作空间 Git Clone 与分支创建

**状态**：⬜

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

**状态**：⬜

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

**状态**：⬜

**功能**：Go 后端嵌入 MCP Server，提供 issue/子任务/工作空间查询与操作工具，供 Claude CLI 通过 MCP 协议调用

**技术方案**：
- SDK：`github.com/modelcontextprotocol/go-sdk` v0.2.0
- Transport：StreamableHTTP（`mcp.NewStreamableHTTPHandler`）
- 路由：Gin 注册 `POST /mcp/streamableHttp/weTerminal`
- 架构：三层分离（参考 pros-admin-server）
  - Server 定义 + Tool 注册：`mcp_servers/mcp_we_terminal.go`
  - Tool Handler 实现：`mcp_servers/mcp_we_terminal_tools.go`
  - DTO 类型定义：`mcp_servers/mcp_dto/we_terminal_tools.go`
- 复用现有 `apis.McpTool` 基类（MakeContext/MakeOrm/Validate/MakeService）

**首期工具清单**：

| 工具名 | 功能 |
|--------|------|
| `issue_get_info` | 获取 issue 详情（标题、描述、状态、关联仓库） |
| `issue_update` | 更新 issue（状态、描述） |
| `issue_child_list` | 获取子任务列表（查 parent_id = issueId 的子 issue） |
| `issue_child_create` | 创建子任务（创建 parent_id 指向父 issue 的子 issue） |
| `issue_child_update` | 更新子任务状态（更新子 issue 的 state_code） |
| `workspace_status` | 获取工作空间状态 |

> **注**：子任务复用现有 issue 父子关系（`parent_id` 字段），MCP 工具内部调现有 `projectIssue` service，无需新表。

**依赖**：无（子任务复用现有 issue 父子关系，无需新表）

**参考**：pros-admin-server 的 `mcp_servers/mcp_demo.go` + `services/apis/mcp_tool.go`

---

### T2.2 新增 Skill：`/ocean-code:refine-issue`

**状态**：⬜

**功能**：AI 需求润色与子任务拆分，基于源码上下文澄清需求，首次生成 AGENT.md/CLAUDE.md

**技术方案**：
- 在 ocean-claude-plugins 项目中新增 `commands/refine-issue.md`
- allowed-tools：Agent, AskUserQuestion, Read, Glob, Grep, Bash, MCP
- 流程：
  1. 通过 MCP 工具获取 issue 原始描述
  2. 读取仓库源码理解代码库结构
  3. AI 分析需求、澄清歧义
  4. AskUserQuestion 与用户交互确认
  5. 按需拆分子任务
  6. 首次生成 AGENT.md / CLAUDE.md
  7. 通过 MCP 回写子任务到 DB、更新 issue 描述

**依赖**：T2.1（MCP 工具）、T1.4（仓库已 clone 才能读源码）

**参考**：ocean-claude-plugins 的 `commands/feature-dev.md`（AskUserQuestion 交互模式）

---

### T2.3 AGENT.md / CLAUDE.md 生成与动态更新

**状态**：⬜

**功能**：refine-issue 首次生成 AGENT.md/CLAUDE.md；agent-dev 执行时通过 MCP 更新 CLAUDE.md 进度

**技术方案**：
- AGENT.md（AI 生成，首次）：项目上下文、编码规范、架构概览
- CLAUDE.md（AI 生成，首次+持续更新）：润色后的需求描述、子任务列表、当前进度
- 更新机制：
  - refine-issue Skill 写入首次内容
  - agent-dev Skill 每完成一个子任务，通过 MCP `issue_child_update` 更新状态后，同步更新 CLAUDE.md 进度段落
  - 更新方式：Skill 直接 Edit/Write 文件（Claude 天然具备文件操作能力）

**依赖**：T2.2（refine-issue 生成）、T2.4（agent-dev 更新）

---

### T2.4 新增 Skill：`/ocean-code:agent-dev`

**状态**：⬜

**功能**：Agent 自动执行开发任务，按 issueId 读取子任务并逐项完成

**技术方案**：
- 在 ocean-claude-plugins 项目中新增 `commands/agent-dev.md`
- allowed-tools：Agent, AskUserQuestion, Read, Glob, Grep, Edit, Write, Bash, MCP
- 入参：issueId（必传）
- 流程：
  1. MCP `issue_get_info` + `issue_child_list` 获取任务上下文
  2. 读 CLAUDE.md 获取已澄清的需求和进度
  3. 无子任务 → 执行整个 issue；有子任务 → 逐项执行
  4. 执行子任务：探索代码 → 实施 → MCP `issue_child_update` 更新状态 → 更新 CLAUDE.md
  5. 全部完成 → MCP `issue_update` 更新 issue 状态
- 与 feature-dev 关系：独立 Skill，内部逻辑参考 feature-dev 的探索→实施→审查模式，但跳过澄清/确认阶段

**依赖**：T2.1（MCP 工具）、T2.2（CLAUDE.md 已存在）

---

## 阶段 3：UI 增强（P1）

### T3.1 子任务列表面板（右侧工具条）

**状态**：⬜

**功能**：DevWorkbenchPage 右侧可折叠面板，展示 issue 的子任务列表及各自状态

**技术方案**：
- 前端新增 `IssueSubTaskPanel` 组件
- 位置：DevWorkbenchPage 右侧可折叠区域
- 数据：React Query 订阅 `issueSubTask/getList` API
- 展示：序号 + 标题 + 状态图标（☐ PENDING / ▶ IN_PROGRESS / ☑ DONE / ⊘ SKIPPED）
- 交互：点击子任务 → 终端切换到对应 Claude 会话
- 实时同步：MCP 更新 DB → React Query 自动刷新

**依赖**：无（复用现有 projectIssue API，按 parentId 过滤）

---

### T3.2 任务归档/取消按钮与流程

**状态**：⬜

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

**依赖**：T1.1

---

### T3.3 Issue 详情页「AI 润色」触发按钮

**状态**：⬜

**功能**：Issue 详情页（ProjectIssueDrawer）新增「AI 润色」按钮，点击后在终端中执行 `/ocean-code:refine-issue`

**技术方案**：
- 前端在 ProjectIssueDrawer 组件中新增「AI 润色」按钮
- 点击后：
  1. 检查工作空间是否已初始化（调 `workspace/status`）
  2. 未初始化 → 先触发 T1.5 初始化流程
  3. 已初始化 → 跳转到 DevWorkbenchPage，并在终端中注入 `/ocean-code:refine-issue` 命令
- 终端命令注入：复用现有 `startup_command` 机制（shell-ready 后自动注入）

**依赖**：T2.2（Skill 已存在）、T1.5（工作空间初始化）

---

## 阶段 4：开发闭环（P1）

### T4.1 MCP Server 外部服务工具（GitHub）

**状态**：⬜

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

---

### T4.2 新增 Skill：`/ocean-code:create-pr`

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
- 全部子任务完成后，提示用户是否执行 `/ocean-code:create-pr`

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

**状态**：⬜

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
