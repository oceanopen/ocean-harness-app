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

### ✅ 任务 1 — @xterm/addon-search 依赖 spike
- **文件**：`package.json`（试探性 add）
- **目标**：验证私有代理可拉 `@xterm/addon-search`；能 → 安装并记录版本；不能 → 移除，任务 2 走自研降级方案。
- **验证**：`pnpm install` 成功 + web:build 通过；或确认 502 并记录降级决策。
- **结果**：✅ 私有代理可拉取，已安装 `@xterm/addon-search@^0.16.0`；`pnpm install` + `web:build` 均通过。任务 2 走官方 addon 方案。

### ✅ 任务 2 — 基础操作组（清屏/复制/粘贴/搜索）
- **文件**：`TerminalView.tsx`（工具条按钮 + 命令接线）+ `TerminalSearch.tsx`（新增：搜索条 overlay，addon 或自研）
- **目标**：§3.1。复制空选禁用；粘贴走 `terminal.paste`；搜索条 Enter/Shift+Enter 导航。
- **验证**：tsc/eslint/web:build；真机——四按钮各自生效，搜索可跳转可关闭，Docker 式滚动输出中搜索不卡顿。
- **已落地（实现细节与实测坑）**：
  - 四按钮长在 TerminalView 工具条（exited 禁用清屏/粘贴，复制/搜索保留——scrollback 检索仍有价值）；terminal/searchAddon 实例经本地 ref 桥（`terminalRef`/`searchAddonRef`）供 JSX 回调直读，不走 props 命令对象。
  - 清屏仅写 `\x0C`（等价 Ctrl+L，交互程序自处理重绘；双管 `clear()+\x0C` 会两次清屏闪烁——裁剪）；复制 `getSelection()` → `navigator.clipboard.writeText`（空选禁用，`onSelectionChange` → state）；粘贴 `readText()` → `terminal.paste`（bracketed paste 由 xterm 内部处理）；成功静默、仅失败 toast。
  - 搜索条 overlay 右上角（absolute），官方 SearchAddon 方案：输入即 incremental findNext、Enter/Shift+Enter 导航、大小写 toggle（aria-pressed）、计数 n/m（超 highlightLimit 显 `>m`）、Escape 关闭。
  - **坑 1（z-index 竞争）**：xterm 容器是静态定位不建堆叠上下文，其内部层（helpers z-5、accessibility z-10、webgl canvas z-0..2）直接参与外层 z-index 竞争；浮层 `zIndex:2` 被压/平局 DOM 顺序反压 → 点击落 canvas、xterm mousedown 无条件 preventDefault → 输入框无法聚焦、按钮全失效（症状酷似事件拦截）。修复：`theme.zIndex.mobileStepper`（1000）。
  - **坑 2（计数与装饰绑定）**：addon 源码 `fireResultsChanged(!!options?.decorations)` 无 decorations 直接 return——`onDidChangeResults`（计数 n/m）永不触发。修复：搜索 options 统一带 `decorations`（matchBackground/activeMatchBackground 明暗配色；overview ruler 透明色占位满足必填）。
  - **坑 3（装饰前置）**：decorations 分支依赖 `registerDecoration` 等 proposed API，Terminal 构造须 `allowProposedApi: true`，否则带装饰的 findNext/findPrevious 抛错、**搜索全面失效**（jsdom + 同版本依赖实证）。清空词条时 `clearDecorations()` + 计数 state 手动归零（装饰清后 addon 不再发事件）。

### ✅ 任务 3 — claude 工作流组
- **文件**：`EmbeddedTerminal/TerminalView.tsx`（「启动 claude」按钮）+ `EmbeddedTerminal.tsx` / `usePtySession.ts`（exited 覆盖条「重开并启动 claude」：reopen + 强制 startup_command）+ `useClaudeRunning.ts`（新增：运行态探测）+ Rust `pty/claude_state.rs`（新增：pid 父链匹配）+ `pty/mod.rs`（`pty_claude_running` 命令）
- **目标**：§3.2。
- **验证**：真机——关闭自动执行配置后手动按钮进 claude；exit claude 后「重开并启动 claude」一键回到 claude；附加 pane 按钮可用。
- **已落地（重规划终版，推翻两版中间方案）**：
  - **按钮位置（第三版定稿）**：TerminalView 工具条内、搜索 icon 右侧（每 pane 自己的终端工具栏——跟终端走，非标题栏）。演进：文档原定 TerminalPaneRoot（无工具条作废）→ 标题栏 TerminalSplitButtons 旁 + store 传导（claudeRequests/claudeInjected——按钮层拿不到 session 实例的绕路方案，已全部回滚）→ 终版 pane 工具条内直连 `session.write('claude\r')`（barrier 已 Open 直通，字节语义同 Rust `build_startup_submission` 单行分支）。
  - **运行态探测（核心新增）**：按钮禁用 = claude 在跑（实时）|| exited。真值来源 Rust `pty_claude_running(sessionId)`（`pty/claude_state.rs`）：`~/.claude/sessions` 活跃 claude pid 沿 `ps -o ppid=` 爬父链（上限 8 级防环），任一级命中本会话 shell pid（`child.process_id()`）即 true——进程树匹配精确到具体终端（多 pane 同 cwd 可区分），非输出流启发式。此匹配为后续「会话项点击聚焦对应终端」的公共地基。
  - **驱动时机（useClaudeRunning hook）**：会话 active 即探测（自动注入/reattach 场景）+ `EVENT_CLAUDE_SESSIONS_CHANGED` 事件（watch 秒级——claude 启动写 json 落盘即触发，置灰快）+ 5s 轮询兜底**退出恢复**（Dead 会话 json 保留、watch 不触发，进程退出只能轮询感知）。非 active 恒 false 不探测；探测失败静默保持现值。
  - exited 覆盖条第二按钮「重开并启动 claude」恒显示（忽略 autoRun 配置）：`reopen(true)` → `startupOverrideRef` 暂存一次性覆盖（ref 非 state——startupCodeCli 在 attachKey/effect deps，state 版会持久生效与配置变化耦合）→ attach 取用即清。
  - **缺口修复（attach fallthrough，首版保留）**：自然退出会话仍留 store，原 attach 的 `reattached.exited` 直接短路 return 'exited'，**「重开」永远走不到 ptySpawn**（现有有效路径全靠先 ptyShutdown）。改为回放 scrollback 后 fallthrough 到 ptySpawn，Rust 端「已退出移除重起」幂等语义承接——「重开」与「重开并启动 claude」同受益。

### ✅ 任务 4 — 字体大小设置
- **文件**：`appConfig.ts`（`terminal_font_size`）+ 设置终端分区 + `EmbeddedTerminal.tsx` + `TerminalView.tsx`（options.fontSize 运行时更新 + refit）
- **目标**：§3.3。
- **验证**：设置改字号即时生效于所有 pane；重开 app 保持；越界值回落 13。
- **已落地**：
  - `appConfig.ts`：`TERMINAL_FONT_SIZE_OPTIONS = [10..20]` 离散选项集 + `parseTerminalFontSize`（非数字/越界/不在选项集如 15 一律回落 13——枚举校验范式，非 poll_interval 的连续 clamp）；纯前端 key 无后端镜像。选项数组放 appConfig 而非 settingOption.ts：数字 label 无 i18n 需求，且 parse 依赖它做值域校验，同文件即值域 SSOT。
  - `TerminalView`：本仓库首个 options 运行时赋值范式——props `fontSize` 构造取初值 + 独立 `useEffect([fontSize])` 做 `terminal.options.fontSize = n` + `fitAddon.fit()`（不重建实例，webgl 官方支持运行时改、字形 atlas 自动重建）；新增 `fitAddonRef` ref 桥（原 mount effect 局部变量）；容器折叠期宽高 0 跳过 fit（同 ResizeObserver 守卫，展开时 observer 再 fit，options 已生效不丢）。fit 后 cols/rows 经既有 onResize 链路自动同步后端 PTY。
  - `EmbeddedTerminal`：`useConfigValue` 订阅下传 props；多 pane 各自订阅，保存事件天然全量 pane 生效。
  - 设置页「应用嵌入终端」卡片加字号行（Select + FormatSizeOutlined icon），draft/saved/dirty 五配置共管；i18n 两语言 `row.fontSize`/`help.fontSize`。

### ✅ 任务 5 — 链接点击
- **文件**：`TerminalView.tsx`（registerLinkProvider）+ Rust `open_path` 命令（若文件路径要系统打开）+ `lib.rs` 注册
- **目标**：§3.4。
- **验证**：真机——终端里 `ls` 输出的文件路径、`git remote -v` 的 URL 可点击打开；hover 有下划线反馈。
- **已落地**：
  - 匹配（模块级正则 + `buildLinks`，不进 effect 闭包）：URL（`https?://\S+`，行尾标点裁剪）+ 绝对文件路径（扩展名白名单 rs/ts/go/py 等 + 可选 `:行号:列` 后缀——编译器/claude 输出形态）；**不匹配裸目录**（无扩展名误报率高，ls 输出到处是——对文档「ls 输出的文件路径」取文件子集）。无存在性探测/跨行重组（§5.4 裁剪）。
  - `provideLinks` 行号 1-based 绝对值（取行 -1），`ILink.range` 列 0-based index 转 1-based、end 含末字符；hover 反馈走默认 decorations（underline + pointer cursor，零自绘）。
  - **URL 打开改道 plugin-shell**（推翻文档原定 window.open）：Tauri webview 拦截 window.open——仓库已有实证（MarkdownEditor.tsx help 按钮注释）；`@tauri-apps/plugin-shell` 的 `open()` 走系统浏览器，依赖/权限（shell:allow-open 对全部 4 窗口）/先例齐备。
  - 路径打开新增 Rust `open_path` 命令（而非复用 open_in_file_manager）：复用私有 `open_dir` 三平台实现，语义「系统默认应用打开任意路径」与「文件管理器打开目录」分立（explorer 对文件是定位非打开，跨平台行为不一致故不复用）。校验非空绝对路径，lib.rs 注册 + gen:bindings。
  - activate 失败 toast（URL「打开链接失败」/路径「打开文件失败」），成功静默；provider 注册在 mount effect 步骤 5.5（toast 回调依赖组件内 useToast），cleanup 逆序 dispose。
  - **坑 4（OSC 8 链接双报错）**：真机点击报 `dialog.confirm not allowed` + `Opening link blocked as opener could not be cleared`——点的链接是 **OSC 8 转义序列**（claude/zsh 等现代 CLI 输出的超链接格式），走 xterm 内置 OscLinkProvider，**与自建正则 provider 是两条独立路径**；未配 `options.linkHandler` 时其默认 activate 是 `confirm() + window.open()`，Tauri 下双失败（confirm 映射的 dialog.confirm IPC 未授权——dialog:default 不含它；window.open 被 webview 拦截返回 null 落 warn 分支）。修复：构造 options 配 `linkHandler.activate` 转发 `activateLink`（提升为组件体 useCallback，showToast 经 useToast 的 useCallback([]) 稳定引用），OSC 8 与正则两路同分流。
  - **交互定稿（修饰键 + Click）**：默认单击即开改误触多（终端链接混在输出流中，单击常是选区/聚焦意图）——两条路径（正则 provider + OSC 8 linkHandler）的 activate 统一加 `hasOpenModifier` 守卫：**macOS Cmd+Click、Windows/Linux Ctrl+Click**（macOS Ctrl+Click 是右键语义故取 metaKey）；普通点击静默，hover 下划线保留作为暗示。
  - **主题增强（hello-halo 调研移植，收尾追加）**：视觉差距调研结论——halo 体验好大半是完整终端主题（我们原只 3 色硬编码）。移植：`terminalTheme.ts` 新文件（独立于组件文件：fast-refresh 限组件导出）——暗色背景 `#121212`/前景 `#eeeeec`（halo --card 7%/98% 亮度档）、ANSI 16 色 Tango 色板明暗两套（ls/git diff/claude 彩色输出鲜活的关键）、光标亮蓝 + cursorAccent（光标下字符反色）、选区着色；**主题热切换**：TerminalView 第二个运行时 option（options.theme 赋值即时重绘，JSON 串去重防重复赋值），推翻原「theme 仅初值生效、切换靠刷新」取舍。halo 调研另确认其流畅度优势核心是 ack 水位背压 + 独立 worker 进程（背压移植列后续模块候选）。

### ✅ 任务 6 — sessions 监听联动（enrich 识别本 app 宿主）
- **文件**：`src-tauri/src/sessions/enrich.rs`（classify_terminal 增变体）+ 相关类型/展示
- **目标**：§3.5。app PTY 内 claude 进监控列表。
- **验证**：真机——终端跑 claude 后 app 监控列表（pet/panel 消费方）出现该会话且宿主显示本 app；iTerm2 内 claude 照常识别（不回归）。
- **已落地**：
  - `classify_terminal` 增 `"we-claude-terminal"` → `TerminalApp::WeTerm`（dev 二进制同名——tauri.dev.conf.json 只改 identifier/productName 不改二进制名，dev/release 同名识别）；枚举变体 + specta 导出 + 两语言词条（zh「本应用」/en「WeTerm」）。
  - 跳转分发（terminal/mod.rs dispatch/open_directory_dispatch）与前端 UNSUPPORTED_HOST（ClaudeSessionCard/ClaudeSessionItem）同步加 WeTerm——当前禁用跳转，聚焦联动（点击会话项切到对应 issue 终端页）在后续模块接线。
  - 监控列表过滤逻辑零改动（`store.rs` 只滤 Unknown）——WeTerm 天然通过，app 内 claude 进列表。
  - 诊断发现（顺带记录）：`rescan: N session(s)` 日志计数是 Unknown 过滤后的——嵌入终端 claude 起来后计数不含它曾造成「监听不到」的误判，实际是 classify 不识别所致（本任务修复）。

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
- ~~终端主题/scrollback 行数等进阶设置~~ → 拆分落地：scrollback 行数 = `terminal_04_advanced_settings.md`（已落地）；主题选择/光标/行高/预览 = terminal_05（规划见 terminal_04 文档 §4）。
