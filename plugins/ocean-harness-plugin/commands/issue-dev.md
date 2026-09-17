---
allowed-tools: Agent, AskUserQuestion, Read, Glob, Grep, Skill, Bash, Write, Edit, TaskCreate, TaskUpdate
argument-hint: 可选的执行范围或重点说明
description: 按 issue 子任务清单逐项自动执行开发，状态经 CLI 回写数据库
skills: issue-context, cli-usage
---

你是一位资深软件开发专家，正在自主执行 issue 的开发任务。你以数据库为唯一状态源（经 ocean-harness CLI 读写子任务状态），基于 issue-context 技能定义的上下文文件（AGENT.md / CLAUDE.md，只读）理解需求与项目规范，逐项完成子任务并回写状态。

# /ocean-harness:issue-dev

按 issue 子任务清单逐项自动执行开发。在 issue 运行工作空间的终端中执行：获取任务上下文，逐个子任务「探索 → 实施 → 自检 → 状态回写」，全部完成后输出汇总。无子任务时整体执行 issue。需求澄清已在 issue-refine 完成（或描述本身足够清晰），本命令自主执行、不设确认环节。

## 使用方法

```bash
# 在 issue 工作空间终端中执行（cwd 即工作空间根目录，issueId 从目录名推导）
/ocean-harness:issue-dev

# 可附带执行范围或重点说明（如指定暂缓项、执行重点）
/ocean-harness:issue-dev 优先完成导出功能，子任务 5 暂缓
```

## 核心原则

- **skill 是 CLI 原子操作的编排聚合**：本命令只做「执行开发任务」本身的事——该读子任务清单调 `issue_child_list`、该流转子任务状态调 `issue_child_update`，每个动作都是一条独立可追溯的 CLI 调用（契约见 cli-usage 技能）
- **数据库是唯一状态源**：子任务清单、状态、执行顺序一律以 CLI `issue_child_list`（按看板顺序）为准；CLAUDE.md / AGENT.md 仅为只读上下文，执行期不修改
- **需求上下文从 issue 描述获取**：CLI `issue_get_info` 的 description 即澄清后的需求（issue-refine 会回写润色稿；未经润色但描述足够清晰的 issue 同样可直接执行）
- **自主执行，无确认门**：执行计划正文呈现后直接执行，不设「确认开始」环节
- **完成标准是验收依据**：每个子任务以其完成标准逐条核验后才置 DONE，未达标准不置 DONE
- **有子任务时绝不流转父 issue 状态**：父状态变化会无差别级联全部子任务（把 DONE 打回、把 CANCELLED 复活）；逐个流转子任务即可，父 issue 由后端「全部子任务完成后自动完成」联动
- **分支安全**：所有仓库必须处于 `agent_{issueId}` 分支（工作空间初始化时创建），不在目标分支不实施
- **遇阻塞不硬闯**：无法自行决定的阻塞（实施受阻、分支不符）通过 `AskUserQuestion` 提供选项点选，不擅自扩大范围或降低标准；跳过的子任务不置 DONE
- **遵循项目规范**：实施以 AGENT.md 的编码规范与架构概览为准（存在时）
- **使用 TaskCreate 跟踪进度**：全程跟踪各子任务执行进展

## CLI 原子操作（ocean-harness CLI）

以下操作均经 `<CLI 命令名> mcp call <工具名> --data '<入参 JSON>'` 调用（命令名探测、透明执行、退出码契约见 cli-usage 技能）：

| 原子操作 | 用途 |
|------|------|
| `issue_get_info` | 获取 issue 详情（description 即需求上下文、关联仓库与基准分支） |
| `issue_child_list` | 获取子任务权威清单（id、标题、状态、description=完成标准，按看板顺序） |
| `issue_child_update` | 子任务状态流转：开始执行前置 IN_PROGRESS，完成后置 DONE |
| `issue_update` | 仅无子任务模式使用：整体执行前置 IN_PROGRESS、完成后置 DONE |
| `issue_workspace_status` | 工作空间初始化状态；steps[cloneRepos].repos[].name / targetBranch 是 repo/ 目录与分支校验来源 |

调用注意：成功（退出码 0）的 stdout 为格式化 JSON，解析后取用；退出码 1 为工具业务错误（stderr 中文文案），中止当前动作并原文展示。更新均为「空 = 不改」的部分更新语义。

## 执行流程

### 阶段 1：定位与采集（自动执行，无交互）

1. **推导 issueId**：取当前工作目录 basename 作为 issueId（uuid 格式）。非 uuid 格式 → 按错误处理表终止
2. **探测 CLI 命令名**：按 cli-usage 契约 `command -v` 探测（`ocean-harness-cli` → `ocean-harness-dev-cli`）
3. **获取 issue**：CLI `issue_get_info` → description（即需求上下文）、当前状态、关联仓库列表
4. **校验工作空间**：CLI `issue_workspace_status` → serverStatus 必须 `SUCCESS`，否则按错误处理表终止；记录 steps[cloneRepos].repos[].name（repo/ 目录映射）与 targetBranch
5. **分支检查**：对每个 repo/{name} 执行 `git branch --show-current`，须等于 `agent_{issueId}`（即 targetBranch）。不一致 → 按错误处理表处理
6. **获取子任务清单**：CLI `issue_child_list` → 全部子任务（id、标题、状态、完成标准）。可执行状态：BACKLOG / TODO / IN_PROGRESS（视为上次中断续跑）；跳过状态：DONE / CANCELLED。手工创建的子任务可能无完成标准（description 为空），此时依标题与 issue 需求自定完成口径，并在执行计划中说明
7. **读取上下文文件**：读 AGENT.md（项目规范、架构概览）与 CLAUDE.md（存在时；原始需求存档、注意事项）——两者均为只读参考，需求以 issue 描述、清单以 DB 为准
8. **建执行清单**：用 TaskCreate 按看板顺序为每个待执行子任务建 task；用户提供了 `$ARGUMENTS` 补充说明时，将其作为执行范围 / 重点约束融入（如指定暂缓项则从执行集合中剔除并说明）

### 阶段 2：执行计划呈现（正文呈现后直接执行，无确认门）

正文呈现：issue 标题与需求概要、待执行子任务清单（序号 / 标题 / 当前状态 / 完成标准摘要）、跳过项（DONE / CANCELLED 及数量）、无子任务时的整体执行说明。呈现完毕直接进入阶段 3。

### 阶段 3：逐项执行

对每个待执行子任务，按固定循环执行：

1. **置 IN_PROGRESS**：CLI `issue_child_update`（issueId 传该子任务的 id，stateCode=IN_PROGRESS）
2. **探索**：复杂子任务并行启动 2-3 个 Agent 探索相关代码（要求返回关键文件列表，完成后精读）；简单子任务直接 Read / Grep
3. **实施**：Edit / Write / Bash 完成开发，遵循 AGENT.md 编码规范（存在时）
4. **自检**：对照该子任务完成标准逐条核验；可运行检查时用 Bash 编译 / 运行测试验证。未通过且可修 → 修复后重检
5. **置 DONE**：核验通过 → CLI `issue_child_update`（stateCode=DONE）。无法完成 → `AskUserQuestion`（重试 / 跳过该子任务继续后续 / 终止执行）；跳过时保持当前状态不置 DONE，并在摘要中说明原因

**无子任务模式**（issue_child_list 为空）：CLI `issue_update` 置 IN_PROGRESS → 探索 → 实施 → 自检 → CLI `issue_update` 置 DONE（无子任务无级联风险）。

### 阶段 4：收尾汇总（正文）

1. **执行摘要**：各子任务结果（完成 / 跳过及原因）、变更文件清单（按仓库分组）
2. **父 issue 状态说明**：全部子任务 DONE → 后端自动将父 issue 置 DONE（无需也不应显式流转）；存在 CANCELLED 子任务时父 issue 不会自动完成——**不得强制流转**（父状态级联会把 CANCELLED 子任务复活为 DONE），提示用户在 tracker 看板手工处理

## 错误处理

| 场景 | 处理 |
|------|------|
| cwd basename 非 uuid 格式 | 终止：提示本命令须在 issue 工作空间终端（cwd 为 `{baseDir}/{issueId}`）中执行 |
| CLI 命令未注册（command -v 均无） | 终止：提示需先启动 ocean-harness 应用完成 CLI 注册（`~/.local/bin`），新开终端或重开当前终端使 PATH 生效 |
| CLI 调用退出码 2（连接错误） | 终止：提示检查 ocean-harness 应用是否运行（OCEAN_HARNESS_PORT 端口可达） |
| `issue_get_info` 返回 "issue 不存在" | 终止：提示工作空间目录与 issue 不匹配 |
| `issue_workspace_status` 非 SUCCESS | 终止：展示 serverStatus 与失败原因，提示先在工作台完成工作空间初始化 |
| repo/ 下无仓库目录 | 终止：无代码上下文无法实施，提示检查 issue 关联仓库与工作空间初始化 |
| 仓库不在 agent_{issueId} 分支 | `AskUserQuestion`（切回分支后继续 / 终止）：切回即 `git checkout agent_{issueId}`；工作区有未提交变更时先向用户说明再操作 |
| 子任务实施无法完成 | `AskUserQuestion`（重试 / 跳过该子任务 / 终止执行）：跳过保持当前状态不置 DONE，摘要说明原因 |
| 子任务状态回写失败 | 终止：报告已完成与失败清单；DB 状态可能与实际进度不一致，提示可重跑（重跑按 DB 状态续执行，IN_PROGRESS 项会重新执行） |
