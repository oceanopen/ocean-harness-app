# 终端工具条与增强功能

> **本模块定位**：嵌入式终端 + 自动 claude + split 窗格之上的工具条完善与散点增强——基础操作（清屏/复制/粘贴/搜索）、claude 工作流（手动启动/重开重启）、终端设置（字体大小持久化）、链接点击、sessions 监听联动。
>
> **前置依赖**：模块 2 `docs/terminal_02_split_panes.md`（pane 结构与 TerminalPaneRoot 工具条挂载点）。
>
> **范围（本期）**：上列五组。**非目标**：主题自定义（跟随 app 明暗已够用）、scrollback 行数设置、右键菜单（工具条按钮覆盖）、快捷键体系。

---

## 1. 模块概览

**要解决什么**：现工具条仅「关闭终端」一个按钮（`TerminalView.tsx` 28px 栏）；终端基础操作（复制/粘贴/清屏/搜索）缺位；claude 会话管理（手动起 claude / 退出后重启）要靠手敲；终端字号只能用默认；终端里的链接不可点。

**接入点**：`TerminalView.tsx` 工具条（单 pane 内基础操作）+ `TerminalPaneRoot.tsx` 工具条（pane 级操作：分割/关闭/启动 claude）+ `appConfig.ts` + 设置「终端」分区（模块 1 已建，本期加字体大小项）。

---

## 2. 现状基线

### 2.1 已有
| 能力 | 位置 |
|---|---|
| 28px 工具条 + 关闭按钮 + exited 覆盖条 | `TerminalView.tsx` |
| xterm 实例封装（fit/webgl/dispose 全套） | `TerminalView.tsx` mount effect |
| appConfig + 设置分区「终端」（模块 1 建，含 auto_run_claude 开关） | `appConfig.ts` / settings |
| sessions 监听域（发现 claude 进程） | `src-tauri/src/sessions/` |
| MUI IconButton 范式（size small + aria-label，密集 UI 不挂 Tooltip） | 全局 |

### 2.2 缺口
| 缺口 | 端 |
|---|---|
| 清屏/复制/粘贴/搜索按钮 | Web |
| `@xterm/addon-search` 依赖（**私有代理白名单风险**） | Web |
| 手动「启动 claude」/「重开并重启 claude」 | Web |
| 字体大小运行时调节 + 持久化 | Web |
| 链接点击（URL/文件路径） | Web |
| enrich 识别本 app 宿主（app 内 claude 进监控列表） | Rust |

---

## 3. 设计

### 3.1 基础操作组（TerminalView 工具条扩展）

按钮：清屏（`terminal.clear()` + `\x0c` 写 PTY——双管：xterm 视口清 + shell 重绘提示符）、复制（`terminal.getSelection()` → `navigator.clipboard`，空选禁用态）、粘贴（`navigator.clipboard.readText()` → `session.write`，注意 `onData` 与剪贴板写入路径一致——经 bracketed paste 包裹与否跟随 xterm 内部行为，直接 `terminal.paste(text)` 优先，其内部处理 paste 模式）、搜索（见下）。

**搜索**：`@xterm/addon-search` 是官方 addon，但 Go 私有代理 npm 白名单可能拉不动（memory：仅已在依赖图中的模块）。**任务 1 先 spike**：
- `pnpm add @xterm/addon-search` 能装 → 直接用（SearchAddon API：`findNext/findPrevious/clearDecorations`），搜索条 overlay（输入框 + 上/下/关闭按钮 + 大小写切换）。
- 拉不动 → 降级方案：`terminal.buffer` 遍历行自实现 find + `scrollToLine`（功能子集：无高亮装饰，仅跳转），零依赖。

**工具条布局**：28px 栏左侧按钮组（清屏 | 复制 | 粘贴 | 搜索），右侧现有关闭。图标 `@mui/icons-material` Outlined 变体，全部 aria-label（密集 UI 不挂 Tooltip 约定）。

### 3.2 claude 工作流组（TerminalPaneRoot 工具条）

- **「启动 claude」按钮**：对活跃 pane 已 ready 的 shell 注入 `claude\r`（复用模块 1 `shell_ready.rs` 的字节构造——经 pty_write 直写，会话已过 barrier 直接落 shell）。何时可见：auto_run_claude 关闭时（用户手动模式）；开启时隐藏（已自动跑，重复入口徒增困惑）。附加 pane 恒可见（§3.6 附加 pane 不自动跑）。
- **「重开并重启 claude」**：exited 态的增强重开——现有「重开」按钮语义不变（裸 shell 重开），工具条下拉或长按扩展「重开并进 claude」= `reopen()` 后 spawn opts 强制带 startup_command（一次性覆盖 autoRun 配置）。首版可简化为：exited 覆盖条加第二按钮「重开并启动 claude」。

### 3.3 终端设置组

- appConfig 新 key：`terminal_font_size`（数字字符串，默认 13，范围 10-20，存 '13' 形式——appConfig 表是 k-v 字符串）。
- 设置「终端」分区加字号项（下拉：10/11/12/13/14/16/18/20）。
- 生效路径：`EmbeddedTerminal` 经 `useConfigValue` 读字号 → TerminalView props 增 `fontSize`；**xterm fontSize 改动需 `terminal.options.fontSize = n` + 重新 fit**（运行时生效，不重建实例）。切 issue 重挂载自然读取。

### 3.4 链接点击

`TerminalView` mount effect 注册 `terminal.registerLinkProvider({...})`（xterm 官方 API，零新依赖）：
- 匹配：URL（`https?://\S+`）与文件路径（`/` 开头 + 常见源码扩展名 or 含 `:` 行号模式）；用 `BufferRange` + hover underline 光标反馈，click 打开（URL → `window.open`；文件 → shell 打开所在目录暂不做编辑器集成，直接系统默认 `open`——Rust 新命令 `open_path` 或复用现有外部终端命令族）。
- 首版匹配纯 `activate`（单击即开），不做 orca 的存在性探测/跨行重组（量力裁剪，落地记录注明）。

### 3.5 sessions 监听联动（Rust）

`src-tauri/src/sessions/enrich.rs` `classify_terminal` 增识别本 app：父进程链爬到本 app 进程名（we-claude-terminal-app / 进程名常量）时归类新 `TerminalApp::WeTerm`（枚举已有 TerminalApp 类型，扩展一个变体）。效果：PTY 内 claude 进 `~/.claude/sessions` 扫描后不再被 Unknown 过滤，进 app 监控列表（cwd 匹配 issue 目录）。
- 事件/前端零改动（走既有 EVENT_CLAUDE_SESSIONS_CHANGED 链路）。
- 注意 i18n：宿主展示名若需加词条，进 pet/panel 相关 namespace。

### 3.6 分组落地顺序与依赖

```
任务 1（搜索 spike）→ 任务 2（基础操作组，含搜索条）
任务 3（claude 工作流组，依赖模块 1 的注入构造）
任务 4（字体大小）
任务 5（链接点击）
任务 6（enrich 联动，独立可并行）
```

---

## 4. 任务清单

> 按序执行；每个任务独立实现 + 验证。
>
> 状态图例：✅ 已完成 · 🔄 进行中 · ⬜ 待办

### ⬜ 任务 1 — @xterm/addon-search 依赖 spike
- **文件**：`package.json`（试探性 add）
- **目标**：验证私有代理可拉 `@xterm/addon-search`；能 → 安装并记录版本；不能 → 移除，任务 2 走自研降级方案。
- **验证**：`pnpm install` 成功 + web:build 通过；或确认 502 并记录降级决策。

### ⬜ 任务 2 — 基础操作组（清屏/复制/粘贴/搜索）
- **文件**：`TerminalView.tsx`（工具条按钮 + 命令接线）+ `TerminalSearch.tsx`（新增：搜索条 overlay，addon 或自研）
- **目标**：§3.1。复制空选禁用；粘贴走 `terminal.paste`；搜索条 Enter/Shift+Enter 导航。
- **验证**：tsc/eslint/web:build；真机——四按钮各自生效，搜索可跳转可关闭，Docker 式滚动输出中搜索不卡顿。

### ⬜ 任务 3 — claude 工作流组
- **文件**：`TerminalPaneRoot.tsx`（「启动 claude」按钮）+ `EmbeddedTerminal.tsx` / `usePtySession.ts`（exited 覆盖条「重开并启动 claude」：reopen + 强制 startup_command）
- **目标**：§3.2。
- **验证**：真机——关闭自动执行配置后手动按钮进 claude；exit claude 后「重开并启动 claude」一键回到 claude；附加 pane 按钮可用。

### ⬜ 任务 4 — 字体大小设置
- **文件**：`appConfig.ts`（`terminal_font_size`）+ 设置终端分区 + `EmbeddedTerminal.tsx` + `TerminalView.tsx`（options.fontSize 运行时更新 + refit）
- **目标**：§3.3。
- **验证**：设置改字号即时生效于所有 pane；重开 app 保持；越界值回落 13。

### ⬜ 任务 5 — 链接点击
- **文件**：`TerminalView.tsx`（registerLinkProvider）+ Rust `open_path` 命令（若文件路径要系统打开）+ `lib.rs` 注册
- **目标**：§3.4。
- **验证**：真机——终端里 `ls` 输出的文件路径、`git remote -v` 的 URL 可点击打开；hover 有下划线反馈。

### ⬜ 任务 6 — sessions 监听联动（enrich 识别本 app 宿主）
- **文件**：`src-tauri/src/sessions/enrich.rs`（classify_terminal 增变体）+ 相关类型/展示
- **目标**：§3.5。app PTY 内 claude 进监控列表。
- **验证**：真机——终端跑 claude 后 app 监控列表（pet/panel 消费方）出现该会话且宿主显示本 app；iTerm2 内 claude 照常识别（不回归）。

---

## 5. 工程约束

### 5.1 依赖纪律（私有代理白名单）
新增 npm 依赖前必 spike（任务 1 范式）；Rust 侧本模块零新增 crate。拉不动一律走零依赖自研降级，不硬闯。

### 5.2 工具条层级
- **TerminalView（pane 内）**：作用于本终端实例——清屏/复制/粘贴/搜索。
- **TerminalPaneRoot（pane 级）**：作用于活跃 pane / 整 issue——分割/关闭/启动 claude。
按钮不跨层复用（职责不同层不同壳），但 IconButton 样式范式统一。

### 5.3 §3.9 前端范式延续
搜索条 overlay 状态用局部 state（用户交互型数据流 → state）；链接 hover/active 回调用 xterm 原生回调链，不建 ref 转发层。

### 5.4 orca → 本项目裁剪对照
| orca | 本项目 |
|---|---|
| 右键菜单全量（Quick Commands/Fork/Copy ID…） | 工具条按钮子集 |
| 文件链接存在性探测 + 跨行重组 + WSL 路径映射 | 单行正则匹配 + 系统打开 |
| OSC 52 / kitty 协议 / IME 深度支持 | 不做（xterm 默认行为够用） |
| bell/未读/agent 完成通知 | 不做（hooks 集成模块再议） |

---

## 6. 后续模块（不在本文档范围）
- hooks 状态集成 + agent 完成通知（依赖 claude hooks 配置体系）。
- 右键菜单、Quick Commands。
- 终端主题/scrollback 行数等进阶设置。
