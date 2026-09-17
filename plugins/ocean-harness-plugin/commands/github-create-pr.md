---
allowed-tools: AskUserQuestion, Read, Glob, Grep, Bash, TaskCreate, TaskUpdate
argument-hint: 可选的补充说明（如指定仓库、标题侧重）
description: 基于 agent 分支变更生成 PR 标题与描述，经 CLI 创建 GitHub PR
skills: cli-usage
---

你是一位资深软件开发专家，正在为 issue 的开发成果创建 Pull Request。你基于 agent 分支相对基准分支的实际变更生成 PR 标题与描述，经 ocean-harness CLI 创建 GitHub PR。

# /ocean-harness:github-create-pr

基于当前 issue 工作空间的 agent 分支变更，自动生成 PR 标题与描述并创建 GitHub Pull Request。在 issue 运行工作空间的终端中手动执行：逐仓库分析变更、生成 PR 内容、确认后经 CLI 创建。仅支持 github.com 仓库（其他 git 平台后续由各自独立的命令处理）；issue 关联多个仓库时，只对**有变更的 github.com 仓库**逐个创建。

## 使用方法

```bash
# 在 issue 工作空间终端中执行（cwd 即工作空间根目录，issueId 从目录名推导）
/ocean-harness:github-create-pr

# 可附带补充说明（如指定仓库、PR 标题侧重）
/ocean-harness:github-create-pr 只处理 web 前端仓库，标题侧重性能优化
```

## 核心原则

- **skill 是 CLI 原子操作的编排聚合**：该查 issue 调 `issue_get_info`、该校验工作空间调 `issue_workspace_status`、该建 PR 调 `github_create_pr`，每个动作都是一条独立可追溯的 CLI 调用（契约见 cli-usage 技能）
- **仅处理 github.com 仓库**：remote 非 github.com 的仓库跳过并在摘要说明（其他平台后续单独命令承载）；仓库按 localRepositoryId 定位（`issue_get_info` 返回的 repositoryBranchList）
- **按实际变更生成内容**：PR 标题与描述必须基于 `git diff base...head` 的真实变更分析得出，禁止凭 issue 描述臆造；描述含变更摘要与测试计划
- **统一使用 AskUserQuestion 获取用户反馈**：所有确认环节必须通过 `AskUserQuestion` 提供选项按钮点选，禁止纯文本提问迫使用户手动输入"yes/确认"
- **每项询问保留 Other 自定义输入入口**：工具每题自带的「Other」自由输入天然满足，不占选项名额，不输入则忽略
- **正文末尾禁追加过渡文字**：呈现完正文后，下一个动作只能是调用 `AskUserQuestion`，正文末尾不得出现"请确认：…？"、"是否…？"等纯文本问句
- **禁止接受纯文本回复作为确认信号**：用户纯文本回"确认"时，再次调用 `AskUserQuestion` 让其点选
- **长内容正文先行**：PR 标题与描述必须先以正文完整呈现，再用 `AskUserQuestion` 问短问题
- **创建前必须获得「确认创建」点选**：循环呈现与调整，用户点选「确认创建」后才调用 `github_create_pr`
- **不流转 issue 状态**：PR 创建 ≠ 任务收尾（issue 状态流转与工作空间归档由各自独立流程处理），本命令只建 PR 并输出链接
- **使用 TaskCreate 跟踪进度**：全程跟踪各阶段进展

## CLI 原子操作（ocean-harness CLI）

以下操作均经 `<CLI 命令名> mcp call <工具名> --data '<入参 JSON>'` 调用（命令名探测、透明执行、退出码契约见 cli-usage 技能）：

| 原子操作 | 用途 |
|------|------|
| `issue_get_info` | 获取 issue 详情与 repositoryBranchList[]（localRepositoryId 定位仓库、repositoryBranch 即基准分支） |
| `issue_workspace_status` | 工作空间初始化状态；steps[cloneRepos].repos[].name 是 repo/ 子目录名的来源 |
| `github_create_pr` | 创建 PR（head 留空默认 `agent_{issueId}`，base 留空默认 issue 关联基准分支 → 仓库默认分支两级回退，通常均无需传） |

调用注意：成功（退出码 0）的 stdout 为格式化 JSON（出参含 number / htmlUrl / headRef / baseRef），解析后取用；退出码 1 为工具业务错误（stderr 中文文案，如 PAT 未配置、非 github.com 仓库），中止当前动作并原文展示。

## 执行流程

### 阶段 1：定位与采集（自动执行，无交互）

1. **推导 issueId**：取当前工作目录 basename 作为 issueId（uuid 格式）。非 uuid 格式 → 按错误处理表终止
2. **探测 CLI 命令名**：按 cli-usage 契约 `command -v` 探测（`ocean-harness-cli` → `ocean-harness-dev-cli`）
3. **获取 issue**：CLI `issue_get_info` → repositoryBranchList[]（localRepositoryId / repositoryBranch 基准分支）、issue 标题与描述（PR 内容的语境）
4. **校验工作空间**：CLI `issue_workspace_status` → serverStatus 必须 `SUCCESS`，否则按错误处理表终止；记录 steps[cloneRepos].repos[].name（repo/ 目录映射）
5. **逐仓库检查**（对 repositoryBranchList 涉及的每个 repo/{name}）：
   - **分支**：`git branch --show-current` 须等于 `agent_{issueId}`，不一致按错误处理表处理
   - **平台**：`git remote get-url origin` 解析 host，非 github.com → 标记跳过（摘要说明，后续其他平台命令处理）
   - **变更**：`git log --oneline {基准分支}..HEAD`（基准分支空则用 origin/HEAD 或仓库默认分支）——无提交 → 标记跳过（无变更无 PR）
   - **未提交/未推送**：`git status --porcelain` 有未提交变更，或 `git log origin/{agent 分支}..HEAD` 有未推送提交 → `AskUserQuestion`（自动 push / 终止）：选自动 push 则先处理未提交（`git add -A` + `git commit` 需用户经 Other 给出或确认提交信息）再 `git push -u origin {agent 分支}`；选终止则提示手动处理后重跑本命令
6. **建执行清单**：TaskCreate 为每个待建 PR 仓库建 task；用户提供了 `$ARGUMENTS` 补充说明时，将其作为仓库筛选 / 标题侧重融入

### 阶段 2：变更分析与成稿（自动执行，无交互）

对每个待建 PR 仓库：

1. **变更分析**：`git diff {基准分支}...HEAD --stat` + 关键文件 diff 精读，结合 issue 需求理解变更意图
2. **生成 PR 内容**：
   - **标题**：`<type>(<scope>): <主题>` 规范（对齐仓库 commit 风格），一句话概括变更意图
   - **描述**（Markdown）：变更摘要（分点列出关键改动）+ 测试计划（如何验证）

### 阶段 3：呈现与确认（确认点，创建闸门）

正文逐仓库完整呈现：仓库名、head → base 分支、PR 标题、PR 描述全文。`AskUserQuestion` 循环确认（选项：「确认创建 (推荐)」/「需要调整」；支持 Other 输入调整意见）：选「需要调整」→ 吸收意见更新对应仓库成稿 → 再次呈现并询问，**直至点选「确认创建」**。

### 阶段 4：创建 PR（确认后自动执行，不再确认）

逐仓库 CLI `github_create_pr`：`--data '{"localRepositoryId": <id>, "issueId": "<issueId>", "title": "<标题>", "body": "<描述>"}'`（head / base 留空走缺省推导）。单仓库失败 → `AskUserQuestion`（重试 / 跳过该仓库 / 终止），不中断其他仓库。

### 阶段 5：收尾摘要（正文）

1. **创建结果**：逐仓库列出 PR 编号与 htmlUrl、head → base
2. **跳过说明**：未建 PR 的仓库及原因（非 github.com / 无变更 / 用户跳过 / 创建失败）
3. **后续建议**：PR 合并后再走状态流转与工作空间归档（各自独立流程）；可用 CLI `github_ci_status` 观察 CI（`pullNumber` 入参）

## 错误处理

| 场景 | 处理 |
|------|------|
| cwd basename 非 uuid 格式 | 终止：提示本命令须在 issue 工作空间终端（cwd 为 `{baseDir}/{issueId}`）中执行 |
| CLI 命令未注册（command -v 均无） | 终止：提示需先启动 ocean-harness 应用完成 CLI 注册（`~/.local/bin`），新开终端或重开当前终端使 PATH 生效 |
| CLI 调用退出码 2（连接错误） | 终止：提示检查 ocean-harness 应用是否运行（OCEAN_HARNESS_PORT 端口可达） |
| `issue_get_info` 返回 "issue 不存在" | 终止：提示工作空间目录与 issue 不匹配 |
| `issue_workspace_status` 非 SUCCESS | 终止：展示 serverStatus 与失败原因，提示先在工作台完成工作空间初始化 |
| issue 未关联任何仓库 | 终止：无仓库可建 PR，提示在 tracker 为 issue 关联仓库 |
| 全部仓库均跳过（非 github.com / 无变更） | 终止：逐仓库说明跳过原因；非 github.com 提示后续由对应平台命令处理 |
| 仓库不在 agent_{issueId} 分支 | `AskUserQuestion`（切回分支后继续 / 终止）：切回即 `git checkout agent_{issueId}`；工作区有未提交变更时先向用户说明再操作 |
| 未提交 / 未推送变更 | `AskUserQuestion`（自动 push / 终止）：详见阶段 1 第 5 步 |
| PAT 未配置（stderr 提示设置 → 个人中心 → GitHub） | 终止：原文展示服务端提示，引导录入 PAT 后重跑本命令 |
| `github_create_pr` 业务失败（base 推导失败 / 422 等） | `AskUserQuestion`（重试 / 跳过该仓库 / 终止）：原文展示 stderr 中文文案；跳过不阻断其他仓库，摘要如实报告 |
