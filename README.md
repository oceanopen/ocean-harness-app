# We Claude Terminal

监听 本地 Claude Code 终端运行状态的桌面应用。

## macOS 安装提示

从 Release 下载安装后，若打开时提示 **“We Claude Terminal” 已损坏，无法打开，你应该将它移到废纸篓**，这是因为应用未经 Apple 代码签名（macOS Gatekeeper 拦截）。在终端执行以下命令移除隔离属性后即可正常打开：

```bash
xattr -cr "/Applications/We Claude Terminal.app"
```

> 若未安装到默认路径，请将路径替换为实际的 `.app` 路径。
