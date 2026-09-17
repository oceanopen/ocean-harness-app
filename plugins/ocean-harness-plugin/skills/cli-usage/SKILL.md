---
name: cli-usage
description: ocean-harness CLI 调用契约：命令名探测、透明执行铁律（每条调用的命令行与输出必须回显）、退出码与输出解析规则，所有 ocean-harness 命令的后端读写一律经 CLI 原子操作完成
---

# ocean-harness CLI 调用契约

所有 ocean-harness 命令对后端的读写一律经 **ocean-harness CLI**（skill → CLI → MCP），不直接调用 MCP 工具。skill 的角色是把一系列 CLI 原子操作编排聚合：该查 issue 查 issue、该建子任务建子任务、该更新状态更新状态——每个动作都是一条独立、可追溯的 CLI 调用。

## 命令名探测

CLI 注册名随 ocean-harness 应用构建模式二选一：

- release 构建：`ocean-harness-cli`
- dev 构建：`ocean-harness-dev-cli`

首次调用前用 `command -v` 逐一探测（探测命令本身也须回显）：先试 `ocean-harness-cli`，再试 `ocean-harness-dev-cli`，取第一个存在者作为本次会话的调用名；均不存在 → 按各命令错误处理表终止提示。

## 调用形态

```bash
<CLI 命令名> mcp call <工具名> --data '<入参 JSON 对象>'
```

- `--data` 三种来源：内联 JSON 对象 `'{"key":...}'`、`@path/file.json` 读文件、`-` 读 stdin；省略视为 `{}`
- 工具清单与入参 schema 可随时经 `mcp tools` / `mcp schema <工具名>` 自查（同样须回显）
- 端口自动取 `OCEAN_HARNESS_PORT` 环境变量（ocean-harness 嵌入式终端 spawn 时已注入），外部终端回落编译期默认（release=9100 / dev=9000），无需显式传端口

## 透明执行铁律

**每一条 CLI 调用，用户必须看得见。** 禁止任何形式的静默调用：

1. **调用前**：以正文回显完整命令行（含工具名与全部 `--data` 参数内容）
2. **调用后**：展示该调用的输出——stdout JSON（可摘取关键结构，但不得省略到无从核对）与 stderr 文案
3. 多条原子操作按执行顺序**逐条**回显，不得合并笼统表述为「已查询 issue 信息」

问题排查与执行追溯完全依赖这些回显，宁可冗长不可省略（后续再按情况精简）。

## 退出码与输出契约

| 退出码 | 含义 | 处理 |
|--------|------|------|
| 0 | 成功 | 解析 stdout JSON 继续流程 |
| 1 | 工具业务错误 | stderr 为服务端中文错误文案，中止当前动作并原文展示 |
| 2 | 用法或连接错误 | stderr 为 CLI 中文提示，按各命令错误处理表处置 |

- stdout 为换行 + 2 空格缩进的格式化 JSON，须解析后取用字段，不得当纯文本截取
- 退出码 1 不必然终止整个命令：单仓库 / 单子任务失败时按各命令错误处理表决定（重试 / 跳过 / 终止），并在收尾摘要中如实报告
