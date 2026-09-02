# Agent 驱动开发流程——技术方案与实施规划

> **本模块定位**：在现有 we-claude-terminal-app（Tauri + React + Go 后端）的 issue/terminal/workspace 基础上，构建完整的 Agent 驱动开发闭环：工作空间初始化 → AI 需求润色/子任务拆分 → Skill 自动执行 → 代码提交/PR → 任务归档。
>
> **核心原则**：工程化确定性操作 + AI 理解性操作。git clone、分支创建、.ssh/config 生成、文件生成等确定性操作走工程化代码（Rust/Go）；需求澄清、子任务拆分、代码生成等理解性操作走 AI + Skill。

---

## 1. 需求全景

### 1.1 功能清单

| # | 功能 | 优先级 | 类型 |
|---|------|--------|------|
| F1 | 工作空间初始化（.ssh/config 生成、.mcp.json、git clone、分支创建） | P0 | 工程化 |
| F2 | AI 需求润色 & 子任务拆分（终端 Skill 执行，同时生成 AGENT.md/CLAUDE.md） | P0 | AI+Skill |
| F3 | Skill 自动执行开发任务（读 issue 上下文 → 调 feature-dev） | P0 | AI+Skill |
| F4 | 子任务列表 & 状态展示（右侧工具条） | P1 | 前端 |
| F5 | 工作空间文件浏览器 & diff 查看（右侧工具条） | P2 | 前端+后端 |
| F6 | 代码提交 & PR 生成（Skill + MCP） | P1 | Skill+MCP |
| F7 | 任务归档/取消（删除工作空间 + 更新 issue 状态） | P1 | 工程化+Skill |
| F8 | MCP Server（项目管理 + 外部服务） | P1 | 后端 |
| F9 | Skill/MCP/Plugin 可视化配置 | P2 | 前端+后端 |

### 1.2 已确认的设计决策

| 决策项 | 结论 |
|--------|------|
| 初始化方式 | 工程化确定性 + AI 理解性 |
| AI 润色上下文 | 需要源码上下文（先 clone 再润色） |
| 润色触发时机 | Issue 详情页手动触发 |
| 工作空间布局 | 按 issue 隔离：`workspace_base_dir/issueId/{.ssh/config, AGENT.md, CLAUDE.md, repo/{仓库目录...}}`，`.ssh/config` 按需生成 |
| 分支命名 | 统一 `agent_{issueId}`，所有关联仓库同名分支 |
| MCP 范围 | 本项目管理 + 外部服务（GitHub API 等），目前无 MCP Server，需从零规划 |
| MCP 技术栈 | 直接 Go 实现（`modelcontextprotocol/go-sdk`），参考 pros-admin-server |
| 自动执行 Skill | 新建独立 Skill（`agent-dev`），不改造 feature-dev；按任务 ID 读子任务信息后执行 |
| 多仓库处理 | 仓库统一 clone 到 `repo/` 子目录，agent-dev 在 issue 根目录执行，Claude 天然可访问所有仓库 |
| SSH 策略 | 按需生成 workspace `.ssh/config`（只含关联仓库的 Host 段），IdentityFile 指向全局密钥原路径，不复制私钥 |

---

## 2. 参考项目分析

### 2.1 Ocean Claude Plugins（`~/MyFiles/Project/ocean-claude-plugins`）

**定位**：自建 Claude Plugin 项目（marketplace：ocean-claude-plugins），包含 ocean-code-plugin
（通用研发技能）与 ocean-harness-plugin（issue 流程专用：refine-issue/agent-dev 等 skill +
we-terminal MCP 捆绑）。

**现有 Skill**：

| Skill | 功能 | 与本方案关系 |
|-------|------|-------------|
| `/ocean-code:feature-dev` | 引导式功能开发（9 阶段：需求→探索→澄清→架构→任务确认→实施→审查→规范审查→总结） | **核心复用**：F3 自动执行时直接调用，但需改造为接收 issue 上下文 |
| `/ocean-code:git-commit` | 人工确认式 Git 提交 | F6 复用 |
| `/ocean-code:git-auto-commit-push` | 全自动 Git 提交推送 | F6 复用 |
| code-reviewer agent | 代码审查 | feature-dev 内部调用 |
| code-explorer agent | 代码探索 | feature-dev 内部调用 |
| code-architect agent | 架构设计 | feature-dev 内部调用 |

**关键启发**：
- feature-dev 已有成熟的9阶段流程，F3 需要的是**将 issue 上下文注入**而非重新开发
- git-commit/git-auto-commit-push 可直接复用
- **需要新增**：`/ocean-harness:create-pr`（生成 PR）、`/ocean-harness:refine-issue`（AI 需求润色/子任务拆分）

---

## 3. 技术方案

### 3.1 架构总览

```
┌─────────────────────────────────────────────────────────┐
│                    we-claude-terminal-app                │
│                                                         │
│  ┌─────────────┐  ┌─────────────┐  ┌──────────────┐   │
│  │ TrackerPage  │  │ DevWorkbench│  │  Settings    │   │
│  │ (issue 列表) │  │  (终端+工具条)│  │  (MCP/Skill  │   │
│  │             │  │             │  │   可视化配置) │   │
│  └──────┬──────┘  └──────┬──────┘  └──────────────┘   │
│         │                │                              │
│  ┌──────┴────────────────┴──────────────────────┐      │
│  │              Go 后端 (Gin + GORM)              │      │
│  │  ┌──────────┐ ┌──────────┐ ┌───────────────┐ │      │
│  │  │ issue    │ │ workspace│ │  MCP Server   │ │      │
│  │  │ service  │ │ service  │ │  (项目管理    │ │      │
│  │  │ (子任务) │ │ (初始化) │ │   +外部服务)  │ │      │
│  │  └──────────┘ └──────────┘ └───────────────┘ │      │
│  └───────────────────────────────────────────────┘      │
│                                                         │
│  ┌───────────────────────────────────────────────┐      │
│  │           Rust (Tauri) - PTY + 文件系统       │      │
│  │  ┌──────────┐ ┌──────────┐ ┌───────────────┐ │      │
│  │  │ PTY 会话 │ │ 文件监听 │ │  Shell Ready  │ │      │
│  │  │ 管理     │ │ + Diff   │ │  + Startup    │ │      │
│  │  └──────────┘ └──────────┘ └───────────────┘ │      │
│  └───────────────────────────────────────────────┘      │
└─────────────────────────────────────────────────────────┘
                         │
                    ┌────┴────┐
                    │ Claude  │ ← Skill 执行环境
                    │   CLI   │ ← MCP Server 连接
                    └─────────┘
```

### 3.2 F1：工作空间初始化

#### 3.2.1 触发时机

- 用户选中 issue → 进入 DevWorkbenchPage → 检测工作空间未初始化 → 自动触发
- issue 关联仓库配置变更时 → 增量更新

#### 3.2.2 初始化流程

```
选中 issue → 检查 workspace_base_dir/{issueId}/ 是否存在
  │
  ├─ 不存在 → 执行完整初始化
  │   1. 创建目录结构
  │   2. 生成 .ssh/config（按需：遍历仓库 URL → 解析 hostname → 匹配 ~/.ssh/config → 生成 workspace 级 config）
  │   3. 生成 .mcp.json（MCP Server 配置，指向 Go 后端 StreamableHTTP 端点）
  │   4. git clone 各关联仓库到 repo/ 子目录（根据 issue 关联的仓库+分支列表）
  │   5. 在每个仓库创建 agent_{issueId} 分支（从基准分支拉出）
  │   6. 写 .workspace-ready marker 文件
  │
  └─ 已存在 → 检查是否需要增量更新
      ├─ 关联仓库新增 → clone 新仓库 + 创建分支
      ├─ 基准分支变更 → 重建分支
      └─ 无变化 → 跳过
```

#### 3.2.3 目录结构

```
workspace_base_dir/{issueId}/
├── .ssh/
│   └── config              # 按需生成的 SSH config（仅含关联仓库的 Host 段，IdentityFile 指向全局密钥）
├── .workspace-ready        # marker 文件（标识初始化完成）
├── .mcp.json               # MCP Server 配置（指向本应用 Go 后端的 StreamableHTTP 端点）
├── repo/                    # 仓库目录
│   ├── {repo1}/            # 克隆的仓库目录（仓库名）
│   │   └── .git/           # agent_{issueId} 分支已检出
│   ├── {repo2}/
│   │   └── .git/
│   └── ...
├── AGENT.md                # [AI 生成] 静态上下文（项目信息、编码规范、架构概览）
└── CLAUDE.md               # [AI 生成] 需求上下文快照（原始需求存档、润色后需求、注意事项）
```

> **AGENT.md / CLAUDE.md 由 AI 生成，不由工程化初始化生成**。原因：这两个文件需要 AI 理解代码库结构和需求内容后才能写出有价值的内容，工程化只能生成空壳。它们在 AI 需求润色阶段（F2）由 `/ocean-harness:refine-issue` Skill 首次生成，增量重跑时按契约修订（CLAUDE.md 不随任务进展更新——状态以 DB 为唯一真相源，见 §3.2.6 方案变更）。

> **SSH 策略**：按需生成 workspace 级 `.ssh/config`。遍历 issue 关联仓库的 SSH URL，提取 hostname，从全局 `~/.ssh/config` 匹配对应 Host 段，只将匹配的段写入 workspace `.ssh/config`。**不复制私钥**——`IdentityFile` 指向全局密钥原路径（如 `~/.ssh/id_rsa_weoa`）。git clone 时通过 `GIT_SSH_COMMAND="ssh -F <workspace>/.ssh/config"` 指定 config，实现按 issue 隔离且不失灵活性。

#### 3.2.4 实现方案

**方案 A：Go 后端工程化实现（推荐）**

| 维度 | 说明 |
|------|------|
| 实现 | 在 Go 后端新增 `workspace_init` service，封装 `git clone`/`git checkout -b`/`os.MkdirAll`/文件生成 |
| 优点 | 确定性操作不依赖 AI；Go 已有 `gitutil` 包可扩展；错误处理可控；仓库放 `repo/` 子目录 |
| 缺点 | 需新增 Go API + Rust Tauri command 桥接 |
| 规范 | 按 `repo/<repoId>/` 子目录隔离 |

**方案 B：Rust 端工程化实现**

| 维度 | 说明 |
|------|------|
| 实现 | 在 Rust 端新增 workspace init command |
| 优点 | 与 PTY 管理同端，可直接操作文件系统 |
| 缺点 | Rust 代码量大，已有 gitutil 在 Go 端，双端维护 |
| 参考 | orca 的 `setup-agent-sequencing.ts` |

**方案 C：混合——Go 做重逻辑 + Rust 做文件系统操作**

| 维度 | 说明 |
|------|------|
| 实现 | Go 后端负责 git clone/branch 操作；Rust 端负责目录创建/文件生成 |
| 优点 | 各端发挥所长 |
| 缺点 | 两端协调复杂度增加 |

**建议**：采用**方案 A**。理由：Go 端已有 `gitutil` 和 `local_repository` service，扩展成本最低；workspace 初始化本质是 git 操作 + 文件 I/O，Go 的 `os/exec` 调 `git` 和 `os.MkdirAll` 足够；Rust 端通过 Tauri command 调 Go HTTP API 即可。

#### 3.2.5 SSH 策略：按需生成 workspace `.ssh/config`

**核心思路**：遍历 issue 关联仓库的 SSH URL → 提取 hostname → 从全局 `~/.ssh/config` 匹配 Host 段 → 只将匹配段写入 workspace `.ssh/config`。**不复制私钥**——IdentityFile 指向全局密钥原路径。

**依赖库**：[`github.com/kevinburke/ssh_config`](https://github.com/kevinburke/ssh_config) v1.6.0（Tailscale 赞助，471 stars，稳定可用）

**生成流程**：

```
1. 遍历 issue 关联仓库列表，收集所有 SSH URL
   git@code.weoa.com:mst/rcs-km.git       → hostname: code.weoa.com
   git@github.com:myorg/myrepo.git         → hostname: github.com

2. 解析全局 ~/.ssh/config（kevinburke/ssh_config.Decode）

3. 为每个 hostname 匹配 Host 段
   - 精确匹配：Host code.weoa.com
   - 通配符匹配：Host *.weoa.com
   - 无匹配：跳过（走默认 SSH 行为）

4. 生成 workspace .ssh/config（仅含匹配的 Host 段）
   # Auto-generated for issue workspace
   Host code.weoa.com
     Port 36000
     IdentityFile ~/.ssh/id_rsa_weoa

   Host github.com
     HostName ssh.github.com
     Port 443
     IdentityFile ~/.ssh/id_ed25519_github_ssh

5. git clone 时指定 config：
   GIT_SSH_COMMAND="ssh -F <workspace>/.ssh/config" git clone ...
```

**关键设计**：
- **不复制私钥**：`IdentityFile` 保持 `~/.ssh/id_rsa_weoa` 等原路径，私钥不动
- **降级处理**：`~/.ssh/config` 不存在或无匹配 → 跳过生成，git clone 走默认 SSH
- **幂等性**：重复初始化时检测已有 `.ssh/config`，hostname 集合未变则跳过
- **局限性**：`kevinburke/ssh_config` 不支持 `Match` 指令（实际罕见，不影响）

**实现预估**：约 60-80 行 Go 代码（URL 解析 + config 匹配 + 文件生成），已验证跑通。

#### 3.2.6 AGENT.md / CLAUDE.md：AI 生成，非工程化

**由 AI 需求润色阶段（F2）生成，不由工作空间初始化（F1）生成。** 原因：这两个文件需要 AI 理解代码库结构和需求内容后才能写出有价值的内容，工程化只能生成空壳无意义。

**AGENT.md**（静态上下文，AI 润色时首次生成，后续可补充）：
- 项目名称、描述
- 关联仓库列表及基准分支
- 编码规范（AI 从代码库推断）
- 架构概览（AI 从代码库分析）

**CLAUDE.md**（需求上下文快照，AI 润色时首次生成，增量重跑时修订）：
- Issue 标题 + 原始需求存档（润色会覆盖 issue 描述，原文唯一留档于此）
- 润色后需求快照（issue 描述为权威版本）
- 注意事项/约束
- **不记录子任务清单、状态、进度**（见下方方案变更）

生成时机：`/ocean-harness:refine-issue` Skill 执行时首次生成。
更新时机：仅 refine-issue 增量重跑（「原始需求（存档）」段永不修改）；agent-dev 只读消费，执行期不修改。

> **方案变更（2026-09-02）：CLAUDE.md 去状态化**。原设计 CLAUDE.md 含子任务列表（DB ID +
> 状态列）与当前进度段，agent-dev 每完成一个子任务同步更新进度段——这与 DB 形成同一数据
> 两份副本，双写漂移且无必要（agent-dev 本就经 MCP 读写 DB）。变更为：子任务清单、状态、
> 进度以数据库为唯一真相源（MCP `issue_child_list` / tracker 看板），agent-dev 凭 MCP 返回
> 的子任务 id 回写状态，不经 CLAUDE.md。

#### 3.2.7 Git Clone 策略（留 worktree 口子）

本期采用 `git clone` 方式，但设计上预留 worktree 支持：

```go
// CloneStrategy 克隆策略枚举
type CloneStrategy string

const (
    CloneStrategyGitClone  CloneStrategy = "clone"    // git clone（本期默认）
    CloneStrategyWorktree  CloneStrategy = "worktree" // git worktree（后续支持）
)

// WorkspaceConfig 工作空间配置（持久化到 DB）
type WorkspaceConfig struct {
    BaseDir       string        // workspace_base_dir
    CloneStrategy CloneStrategy // clone 策略
    // 后续可扩展：branchPrefix template、ssh strategy 等
}
```

本期 `CloneStrategy` 固定为 `clone`，Go 后端 `git clone` 时 `--branch agent_{issueId}` 或 `git checkout -b agent_{issueId}`。后续增加 worktree 支持时，只需新增 worktree 分支处理逻辑，无需改上层接口。

---

### 3.3 F2：AI 需求润色 & 子任务拆分

#### 3.3.1 触发流程

```
Issue 详情页 → 点击「AI 润色」按钮
  │
  ├─ 检查工作空间是否已初始化 → 否则先触发 F1
  │
  └─ 在终端中执行 /ocean-harness:refine-issue
      │
      ├─ Skill 读取仓库源码上下文（理解代码库结构）
      ├─ AI 分析需求：澄清歧义、补充边界条件
      ├─ 通过 AskUserQuestion 与用户交互确认
      ├─ 如果需求较大 → 生成子任务列表
      ├─ 首次生成 AGENT.md（项目上下文、编码规范、架构概览）
      ├─ 首次生成 CLAUDE.md（润色后的需求描述、子任务列表）
      ├─ 子任务列表通过 MCP 回写到 DB
      └─ 润色后的 issue 描述通过 MCP 更新 issue
```

#### 3.3.2 新增 Skill：`/ocean-harness:refine-issue`

```markdown
---
allowed-tools: Agent, AskUserQuestion, Read, Glob, Grep, Bash, MCP
argument-hint: 可选的补充说明
description: AI 需求润色与子任务拆分，基于源码上下文澄清需求
---

你是一位资深产品经理兼技术专家，正在帮助开发者梳理和澄清任务需求。

# /ocean-harness:refine-issue

## 流程

1. **读取上下文**：通过 MCP 工具获取 issue 原始描述；从仓库源码理解代码库结构
2. **需求分析**：识别歧义、边界条件、技术可行性
3. **需求澄清**：通过 AskUserQuestion 逐项确认
4. **润色需求**：将澄清后的内容整理为结构化的需求描述
5. **子任务拆分**（按需）：如果需求较大，拆分为可验证的子任务
6. **生成上下文文件**：
   - 首次生成 AGENT.md（项目上下文、编码规范、架构概览）
   - 首次生成/更新 CLAUDE.md（润色后的需求描述、子任务列表）
7. **回写结果**：
   - 子任务列表 → 通过 MCP 工具回写到 DB
   - 润色后的 issue 描述 → 通过 MCP 工具更新 issue

## 核心原则

- 必须基于源码上下文理解需求
- 澄清的问题要具体、可操作
- 子任务必须可验证（有明确完成标准）
- 润色后保留原始意图，只增补不篡改
- AGENT.md / CLAUDE.md 在本步骤首次生成（非工程化空壳），后续随任务进展动态更新
- 润色后保留原始意图，只增补不篡改
```

#### 3.3.3 子任务数据模型：复用现有 issue 父子关系

**不新建子任务表**。现有的 `t_project_issues` 已有 `parent_id` 字段，支持两级 issue：

- **第一级**：`parent_id` 为空的 issue = 主任务（开发工作台左侧展示）
- **第二级**：`parent_id` 指向父 issue 的子 issue = 子任务（右侧工具条展示）

现有模型已具备的能力：
- `ProjectIssueCreateRequest.ParentID`：创建时指定父 issue（仅一层）
- `ProjectIssueUpdateRequest.StateCode`：更新状态
- `ProjectIssueGetListRequest`：按 project 查询列表（前端过滤 parent_id）

子任务 = 子 issue，状态流转复用现有 StateCode（BACKLOG/TODO/IN_PROGRESS/DONE/CANCELLED）。无需新增枚举、无需新表、无需新 service。

#### 3.3.4 方案对比：AI 需求润色的执行位置

| 维度 | 方案 A：终端 Skill 执行（推荐） | 方案 B：独立 AI Agent 服务 |
|------|------|------|
| 实现成本 | 低——复用现有 Claude Plugin 体系 + 终端 | 高——需要独立 AI 服务 + API 调用 + 上下文管理 |
| 源码上下文 | ✅ 天然有——终端已 `cd` 到工作空间，Claude 可直接读源码 | ❌ 需额外传递——要打包源码传给 API，token 开销大 |
| 交互性 | ✅ AskUserQuestion 原生支持 | ❌ 需自建 WebSocket 交互 |
| 维护性 | ✅ 跟随 Claude 升级 | ❌ 独立维护 AI 服务 |
| 可靠性 | 依赖 Claude CLI 可用 | 可自控但复杂度高 |
| 后续自动化 | ✅ 可从手动触发演进到自动触发 | 同 |

**建议**：采用**方案 A**。核心论点：既然终端执行 agent 也有理解源码的需求，且 Claude CLI 天然具备源码读取能力，将润色放到终端是最自然的选择。避免"搭独立 AI Agent 要做的事情和环境也很多"的问题。

---

### 3.4 F3：Skill 自动执行开发任务

#### 3.4.1 执行流程

```
终端中执行 /ocean-harness:agent-dev（issueId 从 cwd 目录名推导，不传参）
  │
  ├─ Skill 通过 MCP 工具获取 issue 信息（标题、description=需求上下文）
  │
  ├─ Skill 通过 MCP issue_child_list 获取子任务权威清单（按看板顺序）
  │
  ├─ 读取 AGENT.md / CLAUDE.md 作只读上下文（编码规范、原始需求存档等）
  │
  ├─ 检查子任务列表
  │   ├─ 无子任务 → issue_update 置 IN_PROGRESS → 直接执行 issue 级任务 → 置 DONE
  │   └─ 有子任务 → 按顺序逐项执行
  │       ├─ 取下一个 BACKLOG/TODO/IN_PROGRESS 的子任务（DONE/CANCELLED 跳过）
  │       ├─ 置 IN_PROGRESS → 独立执行（内部走 feature-dev 精简流程）→ 对照完成标准自检
  │       ├─ 执行完成后通过 MCP 更新子任务状态为 DONE
  │       └─ 继续下一个子任务
  │
  └─ 全部完成 → 父 issue 由后端「全部子任务 DONE」联动自动完成
      （有子任务时绝不显式流转父状态：父状态变化会无差别级联全部子任务）
```

#### 3.4.2 新增 Skill：`/ocean-harness:agent-dev`

**设计决策**：新建独立 Skill，不改造 feature-dev。理由：
- feature-dev 是"从零开始"的9阶段交互式流程，没有"根据任务 ID 拿任务描述"的概念
- agent-dev 需要通过 MCP 按任务 ID 获取子任务信息，这是 feature-dev 不具备的能力
- 独立 Skill 职责更清晰：agent-dev 负责"编排"（读任务、更新状态），内部可复用 feature-dev 的部分阶段逻辑

```markdown
---
allowed-tools: Agent, AskUserQuestion, Read, Glob, Grep, Skill, Bash, Write, Edit, TaskCreate, TaskUpdate, mcp__plugin_ocean-harness_we-terminal
argument-hint: 可选的执行范围或重点说明
description: 按 issue 子任务清单逐项自动执行开发，状态经 MCP 回写数据库
skills: issue-context
---

你是一位自动化开发代理，负责根据已澄清的 issue 需求执行开发任务。

# /ocean-harness:agent-dev

## 流程

1. **获取任务上下文**：
   - 取当前工作目录 basename 作为 issueId（uuid 格式，非 uuid 终止）
   - 通过 MCP 工具 `issue_get_info` 获取 issue 详情（description 即需求上下文）
   - 通过 MCP 工具 `issue_child_list` 获取子任务权威清单（含 id/状态/完成标准）
   - 读取 AGENT.md / CLAUDE.md 作只读上下文（不依赖 CLAUDE.md 存在）

2. **判断任务类型**：
   - 无子任务 → `issue_update` 置 IN_PROGRESS，执行整个 issue，完成后置 DONE
   - 有子任务 → 按看板顺序逐项执行

3. **执行子任务**：
   - 取下一个 BACKLOG/TODO/IN_PROGRESS 的子任务（DONE/CANCELLED 跳过）
   - 置 IN_PROGRESS，理解子任务的标题、描述和完成标准
   - 按需探索代码库（理解相关代码）
   - 实施代码修改，对照完成标准自检
   - 完成后，通过 MCP 工具 `issue_child_update` 置 DONE（issueId 传子任务自身 id）
   - 继续下一个

4. **全部完成**：输出执行摘要；父 issue 由后端「全部子任务 DONE」联动自动完成，
   不显式流转父状态（父状态级联会把 DONE 打回、CANCELLED 复活）

## 核心原则

- 数据库是唯一状态源：子任务清单、状态、顺序以 MCP `issue_child_list` 为准，
  CLAUDE.md / AGENT.md 仅为只读上下文，执行期不修改
- 每个子任务独立执行，前一个完成才进入下一个
- 子任务失败时 AskUserQuestion（重试/跳过/终止），跳过不置 DONE
- 子任务状态实时同步到 DB（通过 MCP），不经任何本地状态文件
```

#### 3.4.3 与现有 feature-dev 的关系

agent-dev 是**独立 Skill**，不复用 feature-dev 的调用链。但内部逻辑参考 feature-dev 的阶段模式：

| feature-dev 阶段 | agent-dev 对应 | 说明 |
|------------------|----------------|------|
| 1. 需求理解 | MCP 获取 | 通过 `issue_get_info` + `issue_child_list` 获取，无需澄清 |
| 2. 代码库探索 | 保留 | 每个子任务可能涉及不同代码区域，需按需探索 |
| 3. 澄清 | 跳过 | 已在 refine-issue 阶段完成 |
| 4. 架构设计 | 按需 | 简单子任务跳过，复杂子任务保留 |
| 5. 任务确认 | 跳过 | 已在 refine-issue 阶段确认 |
| 6. 实施 | 保留 | 核心执行阶段 |
| 7. 质量审查 | 按需 | 复杂子任务保留，简单子任务跳过 |
| 8. 规范审查 | 跳过 | 非自动化场景关注点 |
| 9. 总结 | 简化 | 通过 MCP 更新状态即可 |

---

### 3.5 F4：子任务列表 & 状态展示（右侧工具条）

#### 3.5.1 UI 设计

在 DevWorkbenchPage 右侧新增可折叠的工具条面板：

```
┌──────────────────────────────────────────────────┐
│ [任务树] │              终端区域              │工具条│
│          │                                    │      │
│ ▼ WS1    │                                    │子任务│
│   ▼ Proj │         Claude Terminal            │ ☐ 1  │
│     ▪ Iss│                                    │ ☑ 2  │
│          │                                    │ ▶ 3  │
│          │                                    │ ☐ 4  │
│          │                                    │      │
│          │                                    │文件  │
│          │                                    │ ...  │
└──────────────────────────────────────────────────┘
```

- 子任务列表：序号 + 标题 + 状态图标（☐ PENDING / ▶ IN_PROGRESS / ☑ DONE / ⊘ SKIPPED）
- 点击子任务 → 终端切换到该子任务对应的 Claude 会话（如已启动）
- 状态实时同步：MCP 更新 DB → 前端 React Query 自动刷新

#### 3.5.2 实现

- 复用现有 `projectIssue/getList` API，按 `parentId = 当前 issue ID` 过滤查询子 issue
- 复用现有 `projectIssue/update` API 更新子任务状态
- 前端新增 `IssueSubTaskPanel` 组件（放于 DevWorkbenchPage 右侧）
- 数据模型：复用 `t_project_issues` 表的 `parent_id` 字段，无需新表

---

### 3.6 F5：工作空间文件浏览器 & Diff 查看（右侧工具条）

#### 3.6.1 方案对比

| 维度 | 方案 A：Rust 端文件监听 + Diff 计算（推荐） | 方案 B：Go 后端 git diff |
|------|------|------|
| 实时性 | ✅ 文件系统事件实时触发 | ❌ 需轮询或手动刷新 |
| Diff 质量 | ✅ 可做行级 diff | ✅ git diff 原生输出 |
| 性能 | ⚠️ 大仓库需注意监听开销 | ✅ git diff 按需计算 |
| 复杂度 | 较高（Rust 端新增 fs watcher） | 较低（Go 调 git diff） |
| 参考 | orca 的 diff-comment 体系 | hello-halo 的 FileChangesList |

**建议**：本期采用**方案 B**（Go 后端 git diff），降低复杂度。后续可引入 Rust 端文件监听做实时性优化。

#### 3.6.2 实现

- Go 后端新增 API：
  - `POST /api/workspace/fileList`：列出工作空间文件树
  - `POST /api/workspace/fileDiff`：获取指定文件的 git diff
  - `POST /api/workspace/fileContent`：获取文件内容（支持文本和二进制）
- 前端参考 hello-halo 的 `FileChangesList` + `DiffContent`，改为侧边栏布局
- 文件类型判断：文本文件直接展示，图片（png/jpg）展示预览，二进制显示信息

---

### 3.7 F6：代码提交 & PR 生成

#### 3.7.1 代码提交

直接复用 `/ocean-code:git-auto-commit-push`。在 `agent-dev` 执行完子任务后，可自动调用此 Skill。

#### 3.7.2 PR 生成（新增 Skill）

新增 `/ocean-harness:create-pr`：

```markdown
---
allowed-tools: AskUserQuestion, Bash(git*), Read, MCP
description: 生成 Pull Request，基于当前分支变更自动生成 PR 标题和描述
---

流程：
1. 获取当前分支名和基准分支
2. 获取 diff（git diff base...head）
3. AI 生成 PR 标题和描述（包含变更摘要、测试计划）
4. 通过 MCP 调 GitHub API 创建 PR
5. 通过 MCP 更新 issue 状态
```

**MCP 需求**：需要 MCP 工具封装 GitHub API（创建 PR、获取 CI 状态等）。

---

### 3.8 F7：任务归档/取消

#### 3.8.1 归档流程

```
用户点击归档按钮
  │
  ├─ 确认弹窗：是否归档该 issue？归档将删除工作空间目录。
  │
  ├─ 确认后：
  │   1. Go 后端删除 workspace_base_dir/{issueId}/ 目录
  │   2. Go 后端更新 issue 状态为 DONE
  │   3. 前端刷新
  │
  └─ 取消后：无操作
```

#### 3.8.2 取消流程

```
用户点击取消按钮
  │
  ├─ 确认弹窗：是否取消该 issue？取消将删除工作空间目录。
  │
  ├─ 确认后：
  │   1. Go 后端删除 workspace_base_dir/{issueId}/ 目录
  │   2. Go 后端更新 issue 状态为 CANCELLED
  │   3. 前端刷新
  │
  └─ 取消后：无操作
```

#### 3.8.3 方案对比

| 维度 | 方案 A：工程化实现（推荐） | 方案 B：Skill 实现 |
|------|------|------|
| 可靠性 | ✅ 确定性操作，不依赖 AI | ❌ Skill 执行可能中断 |
| 速度 | ✅ 毫秒级 | ⚠️ AI 调用开销 |
| 适用性 | 归档/取消是确定性操作 | 适合需要 AI 判断的场景 |

**建议**：采用**方案 A**。归档和取消是确定性操作，工程化更可靠。

---

### 3.9 F8：MCP Server

#### 3.9.1 MCP Server 架构

```
Claude CLI ←── MCP Protocol ──→ Go MCP Server（嵌入 Go 后端进程）
                                    │
                                    ├── 项目管理工具
                                    │   ├── issue_get_info     # 获取 issue 详情
                                    │   ├── issue_update       # 更新 issue（状态/描述）
                                    │   ├── issue_child_list       # 获取子任务列表（查 parent_id 的子 issue）
                                    │   ├── issue_child_create     # 创建子任务（创建子 issue，parent_id 指向父 issue）
                                    │   ├── issue_child_update     # 更新子任务状态（更新子 issue 的 state_code）
                                    │   └── workspace_status   # 获取工作空间状态
                                    │
                                    └── 外部服务工具
                                        ├── github_create_pr    # 创建 PR
                                        ├── github_list_prs     # 列出 PR
                                        └── github_ci_status    # 获取 CI 状态
```

#### 3.9.2 实现方案

**方案：Go MCP Server，嵌入现有 Go 后端（推荐）**

| 维度 | 说明 |
|------|------|
| SDK | `github.com/modelcontextprotocol/go-sdk` v0.2.0 |
| Transport | StreamableHTTP（主力）+ SSE（备用），参考 pros-admin-server |
| 路由 | 在 Gin Router 中注册 `/mcp/streamableHttp/weTerminal` 路径 |
| 认证 | 本期无认证（单机场景，MCP 与 Go 后端同进程） |
| 数据层 | 直接复用现有 service 层（issue_service、workspace_service 等），无需额外 HTTP 跳转 |
| 参考 | pros-admin-server 的 `mcp_servers/` 目录结构 + `apis.McpTool` 基类 |

**关键优势**（vs Node.js 方案）：
- 与 Go 后端共享 DB 连接和 service 层，零序列化开销
- 技术栈统一，不引入 Node.js 依赖
- pros-admin-server 已有成熟的实现模式可直接移植

**实现结构**（参考 pros-admin-server 的三层分离）：

```
src-server/internal/
├── mcp_servers/
│   ├── mcp_we_terminal.go           # Server 定义 + Tool 注册
│   ├── mcp_we_terminal_tools.go     # Tool Handler 实现
│   ├── mcp_github.go                # GitHub 外部服务 Server
│   ├── mcp_github_tools.go          # GitHub Tool Handler
│   └── mcp_dto/                     # DTO 类型定义
│       ├── we_terminal_tools.go
│       └── github_tools.go
```

**代码范式**（参考 pros-admin-server）：

```go
// mcp_we_terminal.go
var mcpServerWeTerminal *mcp.Server

func init() {
    mcpServerWeTerminal = mcp.NewServer(&mcp.Implementation{
        Name:    "we_terminal",
        Version: "v1.0.0",
        Title:   "we-claude-terminal 项目管理工具，操作 issue、子任务、工作空间等",
    }, nil)

    mcp.AddTool(mcpServerWeTerminal, &mcp.Tool{
        Name:        "issue_get_info",
        Description: "获取 issue 详情（标题、描述、状态、关联仓库等）",
    }, McpWeTerminalTool{}.IssueGetInfo)

    mcp.AddTool(mcpServerWeTerminal, &mcp.Tool{
        Name:        "issue_child_list",
        Description: "获取 issue 的子任务列表",
    }, McpWeTerminalTool{}.SubtaskList)

    mcp.AddTool(mcpServerWeTerminal, &mcp.Tool{
        Name:        "issue_child_update",
        Description: "更新子任务状态",
    }, McpWeTerminalTool{}.SubtaskUpdate)

    // ... 更多工具
}

func McpWeTerminalStreamableHTTPHandler() *mcp.StreamableHTTPHandler {
    return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
        return mcpServerWeTerminal
    }, nil)
}
```

```go
// mcp_we_terminal_tools.go
type McpWeTerminalTool struct {
    apis.McpTool  // 复用现有基类（MakeContext/MakeOrm/Validate/MakeService）
}

func (mt McpWeTerminalTool) IssueGetInfo(
    ctx context.Context,
    cc *mcp.ServerSession,
    params *mcp.CallToolParamsFor[mcpdto.IssueGetInfoArgs],
) (*mcp.CallToolResultFor[any], error) {
    args := params.Arguments
    err := mt.MakeContext(&ctx).MakeOrm().Validate(&args).Errors
    if err != nil {
        return nil, err
    }

    svc := service.ProjectIssue{}
    mt.MakeService(&svc.Service)
    data, err := svc.GetInfo(&types.ProjectIssueGetInfoRequest{ID: args.IssueID})
    if err != nil {
        return nil, err
    }

    return &mcp.CallToolResultFor[any]{
        Content: []mcp.Content{
            &mcp.TextContent{Text: fmt.Sprintf("Issue: %s\n状态: %s\n描述: %s",
                data.Name, data.StateCode, data.Description)},
        },
    }, nil
}
```

#### 3.9.3 MCP 配置（方案变更 2026-09-01：插件捆绑承载）

MCP 以 plugin 方式驱动，不在工作空间初始化时生成 `.mcp.json`；配置放 ocean-claude-plugins
的 `plugins/ocean-harness-plugin/`（issue 流程专用插件，后续 refine-issue/agent-dev 等
skill 亦落此插件）根目录（插件安装即注册，免逐项审批；工具名带
`mcp__plugin_ocean-harness_we-terminal__*` 前缀）：

```json
{
  "mcpServers": {
    "we-terminal": {
      "type": "http",
      "url": "http://127.0.0.1:${WE_TERMINAL_PORT:-9100}/mcp/streamableHttp/weTerminal"
    }
  }
}
```

- `type` 取 Claude CLI 合法值 `http`（即 Streamable HTTP；`streamable-http` 为 kebab-case
  别名，驼峰 `streamableHttp` 不是合法值）。插件 `.mcp.json` 支持 `${VAR:-default}` 展开
  （与手工配置的 server 同一套环境变量语义）。
- 端口注入：Rust `pty_spawn` spawn PTY 时注入 `WE_TERMINAL_PORT`（HttpServerState 的端口：
  默认 dev=9000 / build=9100 / 用户配置覆盖）；外部终端无此 env 时回落默认 9100。
- 插件更新生效：bump plugin.json 版本 → `claude plugin update` → `/reload-plugins`。
- 工作空间初始化的 mcpConfig 步骤保留 SKIPPED 占位，未来需要 workspace 级单独支持时恢复。

> **注**：pros-admin-server 使用 StreamableHTTP（`mcp.NewStreamableHTTPHandler`），Claude Code 原生支持 streamable HTTP 类型的 MCP 配置。本期不需要 stdio transport。

---

### 3.10 F9：Skill/MCP/Plugin 可视化配置

本期不实现，但预留口子：

- Go 后端新增 `t_plugin_registry` 表：记录已安装的 plugin、skill、MCP server
- 前端预留 Settings 页的 Plugin/Skill/MCP 管理入口
- 通过文件系统扫描 `.claude-plugin/` 和 `.mcp.json` 发现已安装项

---

## 4. 实施顺序

### 阶段 1：基础设施（P0，预计 2-3 天）

| 序号 | 任务 | 依赖 | 涉及端 |
|------|------|------|--------|
| 1.1 | 工作空间初始化 Go service（目录创建、.ssh/config 按需生成、.mcp.json 生成） | 无 | Go |
| 1.2 | 工作空间 git clone + 分支创建 | 1.1 | Go |
| 1.3 | 前端触发初始化（DevWorkbenchPage 选中 issue 时自动检测+触发） | 1.1 | React+Rust |

### 阶段 2：AI 流程（P0，预计 3-4 天）

| 序号 | 任务 | 依赖 | 涉及端 |
|------|------|------|--------|
| 2.1 | MCP Server（Go 版，嵌入 Go 后端，项目管理工具集；子任务复用 issue 父子关系） | 无 | Go（参考 pros-admin-server） |
| 2.2 | 新增 `/ocean-harness:refine-issue` Skill | 1.2, 2.1 | Skill |
| 2.3 | AGENT.md/CLAUDE.md 生成（refine-issue 首次生成；2026-09-02 变更：CLAUDE.md 去状态化，状态唯一真相源为 DB） | 2.1, 2.2 | Skill |
| 2.4 | 新增 `/ocean-harness:agent-dev` Skill | 2.2, 2.3 | Skill |

### 阶段 3：UI 增强（P1，预计 2-3 天）

| 序号 | 任务 | 依赖 | 涉及端 |
|------|------|------|--------|
| 3.1 | 子任务列表面板（右侧工具条） | 1.1 | React |
| 3.2 | 任务归档/取消按钮 + 流程 | 1.2 | React+Go |
| 3.3 | Issue 详情页「AI 润色」触发按钮 | 2.2 | React |

### 阶段 4：开发闭环（P1，预计 2-3 天）

| 序号 | 任务 | 依赖 | 涉及端 |
|------|------|------|--------|
| 4.1 | 新增 `/ocean-harness:create-pr` Skill + GitHub MCP 工具 | 2.1 | Go MCP+Skill |
| 4.2 | agent-dev 完成后自动调用 git-auto-commit-push | 2.4 | Skill |
| 4.3 | 完整流程端到端验证 | 全部 | 全端 |

### 阶段 5：增值功能（P2，预计 2-3 天）

| 序号 | 任务 | 依赖 | 涉及端 |
|------|------|------|--------|
| 5.1 | 工作空间文件浏览器 + Diff 查看 | 1.2 | React+Go |
| 5.2 | Skill/MCP/Plugin 可视化配置 | 2.1 | React+Go |
| 5.3 | worktree 模式支持 | 1.3 | Go |

---

## 5. 风险与待确认项

### 5.1 待确认

| # | 问题 | 影响 | 建议默认 |
|---|------|------|----------|
| Q1 | 子任务是否支持嵌套（子任务的子任务）？ | 数据模型设计 | 本期不支持，仅一层 |
| Q2 | 工作空间目录删除是否需要确认 git 状态（未提交/未推送）？ | 归档安全性 | 是，归档前检查并提醒 |
| Q3 | MCP Server 是否需要认证？本期单机场景无认证是否安全？ | 安全性 | 本期无认证（单机），后续可加 Bearer Token |
| Q4 | agent-dev 执行子任务时，如果 AI 需要用户交互（AskUserQuestion），是暂停还是自动决策？ | 自动化程度 | 暂停等待人工介入，不自动决策 |

### 5.2 已知风险

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| MCP Server 开发周期 | 阻塞 Skill 自动化 | 参考 pros-admin-server 的成熟模式，移植成本低；前期 Skill 可用 Bash 调 Go API 替代 |
| Claude CLI MCP 连接稳定性 | Skill 执行中断 | StreamableHTTP 比 stdio 更稳定；增加 .mcp.json 健康检查 |
| 大仓库 clone 耗时 | 初始化体验差 | 浅克隆 `--depth=1` + 后台执行 + 进度展示 |
| 子任务拆分质量 | AI 可能拆分不合理 | 润色阶段用户确认，支持手动调整 |

---

## 6. 参考项目映射

| 本方案功能 | 参考项目 | 参考文件/模块 | 借鉴点 |
|-----------|---------|-------------|--------|
| F2 AI 润色 | ocean-claude-plugins | `plugins/ocean-code-plugin/commands/feature-dev.md` | 9 阶段流程、AskUserQuestion 模式 |
| F3 自动执行 | ocean-claude-plugins | `commands/feature-dev.md` + `commands/git-auto-commit-push.md` | Skill 调 Skill 模式 |
