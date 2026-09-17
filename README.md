# Ocean Harness

监听 本地 Claude Code 终端运行状态的桌面应用。

## macOS 安装提示

从 Release 下载安装后，若打开时提示 **“Ocean Harness” 已损坏，无法打开，你应该将它移到废纸篓**，这是因为应用未经 Apple 代码签名（macOS Gatekeeper 拦截）。在终端执行以下命令移除隔离属性后即可正常打开：

```bash
xattr -cr "/Applications/Ocean Harness.app"
```

> 若未安装到默认路径，请将路径替换为实际的 `.app` 路径。

## 查看应用 SQLite 数据库

可以用 [DBeaver](https://dbeaver.io/) 查看 sqlite 数据库（文件名固定为 `app.db`，位于 Tauri 的 `app_data_dir` 下）：

| 平台    | 环境    | 路径                                                         |
| ------- | ------- | ------------------------------------------------------------ |
| macOS   | Release | `~/Library/Application Support/com.ocean.harness/app.db`     |
| macOS   | Dev     | `~/Library/Application Support/com.ocean.harness.dev/app.db` |
| Windows | Release | `%APPDATA%\com.ocean.harness\app.db`                         |
| Windows | Dev     | `%APPDATA%\com.ocean.harness.dev\app.db`                     |
| Linux   | Release | `~/.local/share/com.ocean.harness/app.db`                    |
| Linux   | Dev     | `~/.local/share/com.ocean.harness.dev/app.db`                |

> Dev 与 Release 使用不同 identifier，数据自动隔离。`~` 为用户主目录；Windows `%APPDATA%` 对应 `C:\Users\<用户名>\AppData\Roaming`；Linux 遵循 XDG 规范，若设置了 `XDG_DATA_HOME` 则以其替代 `~/.local/share`。

## 查看服务 SQLite 数据库

应用内置的 Go 旁路服务（HTTP sidecar）持有独立的业务数据库，文件名固定为 `server.db`，位于 `app_data_dir` 下的 `app-server/db/` 子目录，与本地配置库 `app.db` 相互隔离。同样可用 [DBeaver](https://dbeaver.io/) 查看：

| 平台    | 环境    | 路径                                                                          |
| ------- | ------- | ----------------------------------------------------------------------------- |
| macOS   | Release | `~/Library/Application Support/com.ocean.harness/app-server/db/server.db`     |
| macOS   | Dev     | `~/Library/Application Support/com.ocean.harness.dev/app-server/db/server.db` |
| Windows | Release | `%APPDATA%\com.ocean.harness\app-server\db\server.db`                         |
| Windows | Dev     | `%APPDATA%\com.ocean.harness.dev\app-server\db\server.db`                     |
| Linux   | Release | `~/.local/share/com.ocean.harness/app-server/db/server.db`                    |
| Linux   | Dev     | `~/.local/share/com.ocean.harness.dev/app-server/db/server.db`                |

## Claude Code 插件安装（ocean-harness）

本仓库自带 Claude Code 插件 marketplace（`.claude-plugin/marketplace.json`），提供 issue 驱动的 agent 开发流程插件 **ocean-harness**。安装：

```bash
claude plugin marketplace add oceanopen/ocean-harness-app
claude plugin install ocean-harness@ocean-harness-app
```

安装后会话内 `/reload-plugins` 生效，命令以 `/ocean-harness:xxx` 调用。
更新：仓库内插件改动发布后执行 `claude plugin update ocean-harness@ocean-harness-app`（更新检测以 `plugin.json` 版本为准，随本仓库 `pnpm release` 同步 bump）。

## ocean-harness CLI

app 启动时自动将随包 CLI 命令注册到用户级 `~/.local/bin`（release 构建注册 `ocean-harness-cli`，dev 构建注册 `ocean-harness-dev-cli`；首次会向 `~/.zshrc` 幂等注入 PATH 行，fish 等其他 shell 需手动将 `~/.local/bin` 加入 PATH）。CLI 内置 MCP 客户端直连本机服务的 MCP 端点，是插件 skill ↔ MCP 之间的可调试中间层——插件所有命令的后端读写均经它完成，也可在终端直接手工调用：

```bash
ocean-harness-cli --version                          # 版本与构建模式
ocean-harness-cli mcp tools                          # 列出全部 MCP 工具（name + description；--full 看完整定义）
ocean-harness-cli mcp schema <tool>                  # 查看单个工具的入参/出参 schema
ocean-harness-cli mcp call <tool> --data '<json>'    # 调用工具（省略 --data 视为 {}）
ocean-harness-cli mcp call <tool> --data @payload.json   # 复杂/多行入参走文件；--data - 读 stdin
ocean-harness-cli completion zsh                     # shell 补全脚本（bash/zsh/fish/powershell）
```

端口自动取 `OCEAN_HARNESS_PORT` 环境变量（app 内嵌终端 spawn 时注入），外部终端回落编译期默认（release=9100 / dev=9000）。退出码约定：`0` 成功（stdout 输出格式化 JSON）；`1` 工具业务错误（stderr 为服务端中文文案）；`2` 用法或连接错误。

## 设计

- https://mui.com/material-ui/getting-started/
- https://mui.com/x/introduction/
- https://mui.com/components/
- https://fonts.google.com/icons?icon.set=Material+Icons
