# We Claude Terminal

监听 本地 Claude Code 终端运行状态的桌面应用。

## macOS 安装提示

从 Release 下载安装后，若打开时提示 **“We Claude Terminal” 已损坏，无法打开，你应该将它移到废纸篓**，这是因为应用未经 Apple 代码签名（macOS Gatekeeper 拦截）。在终端执行以下命令移除隔离属性后即可正常打开：

```bash
xattr -cr "/Applications/We Claude Terminal.app"
```

> 若未安装到默认路径，请将路径替换为实际的 `.app` 路径。

## 查看应用 SQLite 数据库

可以用 [DBeaver](https://dbeaver.io/) 查看 sqlite 数据库（文件名固定为 `app.db`，位于 Tauri 的 `app_data_dir` 下）：

| 平台    | 环境    | 路径                                                              |
| ------- | ------- | ----------------------------------------------------------------- |
| macOS   | Release | `~/Library/Application Support/com.we.claude.terminal/app.db`     |
| macOS   | Dev     | `~/Library/Application Support/com.we.claude.terminal.dev/app.db` |
| Windows | Release | `%APPDATA%\com.we.claude.terminal\app.db`                         |
| Windows | Dev     | `%APPDATA%\com.we.claude.terminal.dev\app.db`                     |
| Linux   | Release | `~/.local/share/com.we.claude.terminal/app.db`                    |
| Linux   | Dev     | `~/.local/share/com.we.claude.terminal.dev/app.db`                |

> Dev 与 Release 使用不同 identifier，数据自动隔离。`~` 为用户主目录；Windows `%APPDATA%` 对应 `C:\Users\<用户名>\AppData\Roaming`；Linux 遵循 XDG 规范，若设置了 `XDG_DATA_HOME` 则以其替代 `~/.local/share`。

## 查看服务 SQLite 数据库

应用内置的 Go 旁路服务（HTTP sidecar）持有独立的业务数据库，文件名固定为 `server.db`，位于 `app_data_dir` 下的 `app-server/db/` 子目录，与本地配置库 `app.db` 相互隔离。同样可用 [DBeaver](https://dbeaver.io/) 查看：

| 平台    | 环境    | 路径                                                                               |
| ------- | ------- | ---------------------------------------------------------------------------------- |
| macOS   | Release | `~/Library/Application Support/com.we.claude.terminal/app-server/db/server.db`     |
| macOS   | Dev     | `~/Library/Application Support/com.we.claude.terminal.dev/app-server/db/server.db` |
| Windows | Release | `%APPDATA%\com.we.claude.terminal\app-server\db\server.db`                         |
| Windows | Dev     | `%APPDATA%\com.we.claude.terminal.dev\app-server\db\server.db`                     |
| Linux   | Release | `~/.local/share/com.we.claude.terminal/app-server/db/server.db`                    |
| Linux   | Dev     | `~/.local/share/com.we.claude.terminal.dev/app-server/db/server.db`                |

## 设计

- https://mui.com/material-ui/getting-started/
- https://mui.com/x/introduction/
- https://mui.com/components/
