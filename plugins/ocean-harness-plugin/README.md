# ocean-harness-plugin

issue 驱动的 agent 开发流程插件（ocean-harness-app 配套，随本仓库以独立 marketplace 分发：
`claude plugin marketplace add oceanopen/ocean-harness-app`）。

## 调用链路（skill → CLI → MCP）

所有命令的后端读写一律经 **ocean-harness CLI**（release 构建 `ocean-harness-cli` / dev 构建 `ocean-harness-dev-cli`）完成，不捆绑 MCP server、不直连 MCP 工具——CLI 是可终端调试、可追溯的中间层，直连 ocean-harness Go 后端的 MCP 端点。端口经 `OCEAN_HARNESS_PORT` 环境变量自动获取（ocean-harness 嵌入式终端 spawn 时注入），无需显式配置。

**skill 的定位**：把一系列 CLI 原子操作编排聚合——每个动作（查 issue / 建子任务 / 更新状态 / 建 PR）都是一条独立可追溯的 CLI 调用。**透明执行铁律**：每条 CLI 调用的完整命令行（含参数）与输出都必须回显在会话中，禁止静默调用（完整契约见 `skills/cli-usage`）。

## 命令命名规范

`域-动作` 前缀聚合（对齐 MCP 工具命名风格）：issue 域 `issue-*`、GitHub 域 `github-*`。后续新命令按域扩展前缀，便于识别、维护和管理。

## commands/

- `issue-refine`：AI 需求润色与子任务拆分（T2.2 已落地）——在 issue 工作空间终端执行，基于源码上下文澄清需求，生成 AGENT.md/CLAUDE.md（需求上下文快照），子任务与润色稿经 CLI 回写
- `issue-dev`：按子任务清单逐项自动执行开发（T2.4 已落地）——子任务清单与状态以数据库为唯一真相源（CLI `issue_child_list`），逐项「探索→实施→自检→状态回写」；有子任务时不流转父状态（父→子级联会打回 DONE/复活 CANCELLED，父由后端全完成联动），无子任务时整体执行
- `github-create-pr`：基于 agent 分支变更生成 PR 标题与描述并创建 GitHub PR（T4.2 已落地）——仅处理 github.com 仓库（其他 git 平台后续各自独立命令），多仓库按变更逐个建 PR，创建前经用户确认，不流转 issue 状态

各命令独立手动调用（`/ocean-harness:xxx`），互不串联；跨命令流程编排后续由单独命令承载。

## skills/

- `issue-context`：AGENT.md/CLAUDE.md 结构契约与子任务拆分规范——CLAUDE.md 为纯需求上下文快照（原始需求存档 / 润色快照 / 注意事项），不记录子任务状态（状态唯一真相源为数据库）；issue-refine 遵守生成、issue-dev 只读消费
- `cli-usage`：CLI 调用契约——命令名探测（release/dev 二态）、透明执行铁律、退出码 0/1/2 与输出解析；所有命令引用

## 更新生效

marketplace 安装的插件是复制进缓存的：改动后 bump `plugin.json` 版本 →
`claude plugin update ocean-harness@ocean-harness-app` → 会话内 `/reload-plugins`。
