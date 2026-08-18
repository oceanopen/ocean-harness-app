# 终端自动执行 claude（shell-ready 注入）

> **本模块定位**：嵌入式终端（已完成，见 git 历史 `fc8559b^` `docs/embedded_terminal.md`）的后续模块。选中 issue 打开终端后，shell 就绪时自动执行 `claude` 命令，免去每次手敲。参考项目 orca 的 `StartupCommandDelivery: 'shell-ready'` 机制移植（Electron → Tauri/Rust）。
>
> **参考实现**（orca，绝对路径根 `/Users/gaopan/MyFiles/Project/orca/`）：
> - `src/main/daemon/session-shell-ready-barrier.ts` — barrier 完整实现（marker 扫描、stdin 排队、flush gate、超时兜底）
> - `src/shared/startup-command-submission.ts` — 注入字节构造（bracketed paste + submit），66 行
> - `src/main/shell-templates.ts` + `src/main/daemon/shell-ready.ts` — zsh/bash/fish/PowerShell 四套 shell 包装生成
>
> **范围（本期）**：仅 macOS（zsh/bash）；仅注入 `claude`（无 prompt 参数——orca 的 argv 引号转义/多 agent 配置矩阵本期不需要）；fish 不支持（`$SHELL` 为 fish 时跳过包装走 fast 回退，不阻塞）。
>
> **非目标**：`claude --resume` 恢复、`--session-id` pin、prompt 注入（argv 模式）、hooks 状态集成、native chat 双视图——均为后续独立模块。

---

## 1. 模块概览

**要解决什么**：用户选中 issue → 终端打开 → 还要手动敲一次 `claude`。期望开箱即用：shell 提示符就绪瞬间自动注入 `claude\r`，且注入不能被 rc 文件噪声吞掉、不能 ECHO 双显。

**接入点**：
- Rust：`src-tauri/src/pty/local_provider.rs` `spawn_fresh`（spawn 链路）+ `src-tauri/src/pty/session.rs`（reader 线程剥 marker）+ 新增 `src-tauri/src/pty/shell_ready.rs`
- Web：`EmbeddedTerminal.tsx`（spawn opts 传递）+ `usePtySession.ts`
- 配置：`appConfig.ts` 新 key + 设置页新分区

**为什么不用 fast 模式**（spawn 后首个输出 chunk + 30ms 即写入）：用户 rc 文件输出慢于 30ms 时（nvm/pyenv 等重 rc），`claude` 会落在 rc 输出中间被 shell 丢弃或双显。shell-ready 用 marker 精确锚定提示符就绪点，一次做对。

---

## 2. 现状基线

### 2.1 已有
| 能力 | 位置 |
|---|---|
| PTY 七命令（spawn/write/resize/shutdown/exists/reattach/list） | `src-tauri/src/pty/mod.rs` |
| ring buffer + reattach 无缝续流 | `session.rs` `SessionIo`（256KB 有界） |
| 幂等 spawn 三分支 + 并发败者让位 | `local_provider.rs:155-207` |
| shell 解析（`$SHELL -i` 回退 `/bin/zsh`） | `local_provider.rs:148-151` `resolve_shell()` |
| reader 线程（UTF-8 切分 → ring + listener） | `session.rs:205-224` `spawn_reader_thread` |
| 前端编排（exists→reattach→spawn） | `usePtySession.ts` `attach()` |
| issue 删除联动 pty_shutdown | `src/state/tracker/queries.ts` `useDeleteProjectIssue` |

### 2.2 缺口
| 缺口 | 端 |
|---|---|
| shell 包装（ZDOTDIR 换装 / --rcfile）+ marker 发射 | Rust |
| marker 扫描 + stdin 排队 barrier + 超时兜底 | Rust |
| `SpawnOpts.startup_command` 字段 + 注入字节构造 | Rust |
| `pty_shutdown_issue`（前缀扫描关整个 issue 的全部 pane） | Rust |
| 前端 spawn 传 startup_command + 配置开关 | Web |
| appConfig `terminal_auto_run_claude` + 设置分区 | Web |

---

## 3. 设计

### 3.1 底层库能力结论（选型依据，2026-08-18 实证）

`portable-pty 0.9.0` `cmdbuilder.rs:272-342` 提供 `arg()` / `env()` / `env_remove()` / `cwd()` 公开 API。shell-ready 机制**全部在应用层**：
- shell 包装 = 我们生成的 shell 文件 + `env("ZDOTDIR", ...)` / `arg("--rcfile")` —— 标准用法
- marker 发射 = 包装文件内的 shell 代码 —— 我们的文件
- barrier = reader 线程 + PtySession 内 Rust 模块 —— 我们的代码

**不侵入 portable-pty 任何内部，库升级零影响**（orca 用未改造的 node-pty 同理）。维护成本仅限自有的 ~60 行 shell 模板 + ~150 行 Rust barrier。

### 3.2 shell 包装与 marker

**marker 序列**：`OSC 777 ; we-term-shell-ready`（ESC `]777;we-term-shell-ready` BEL）——与 orca 同协议但换应用前缀。字节：`\x1b]777;we-term-shell-ready\x07`。

**zsh**（macOS 主路径）：
```
app_data_dir/shell-ready/zsh/
  .zshrc    # 先 source 用户 ~/.zshrc（存在时），再注册 widget：
             #   WE_TERM_PREV_ZLE_WIDGET 保存原 zle-line-init
             #   zle-line-init() { emit marker; 还原调用原 widget }
             #   zle -N zle-line-init
spawn: env ZDOTDIR=<app_data_dir>/shell-ready/zsh  zsh -i
```
要点（orca 实证教训）：不用 `add-zle-hook-widget`（用户 widget 非零退出时 marker 被吞），自持 widget 并链式调用前一个。`.zshenv`/`.zprofile`/`.zlogin` 若存在则透传 source 用户同名文件（保持登录 shell 语义完整）。

**bash**：
```
app_data_dir/shell-ready/bash/bashrc-wrapper
  # 先 source ~/.bashrc（存在时）；PROMPT_COMMAND 前置 emit marker（保留用户已有 PROMPT_COMMAND）
spawn: bash --rcfile <wrapper>
```

**fish / 其他**：不包装，`startup_command` 直接走 fast 注入（spawn 后首个输出 + 30ms），行为降级不阻塞。Windows（远程 provider 预留位）不在本期。

**包装文件生成**：首次 `pty_spawn`（带 startup_command）时按需生成到 `app_data_dir`（`app.path().app_data_dir()`），幂等（已存在不覆写——用户可自查这些文件）。

### 3.3 barrier（marker 扫描 + stdin 排队）

新增 `src-tauri/src/pty/shell_ready.rs`：

```rust
pub struct ShellReadyBarrier {
    /// marker 前置.stdin 排队（Mutex<Option<Vec<u8>>>，take 语义）
    pending: Mutex<Option<Vec<u8>>>,
    /// 已见过 marker（AtomicBool）
    ready: AtomicBool,
    /// flush 定时（marker 后 30ms 再放行——orca PostReadyFlushGate：等提示符绘制完成防 ECHO 双显）
    ...
}
```

数据流：
```
[写路径] pty_write → 会话带 barrier 且未 ready？
           是 → 字节入 pending 队列（不写 PTY）
           否 → 直接 write_all
[读路径] reader 线程 → ShellReadyScanner 扫描输出块：
           命中 marker → 从输出中剥除（不推前端/ring）→ 置 ready
                        → 30ms 后 flush pending（一次性 write_all + 清空）
           未命中    → 正常推流
[超时]  spawn 起 5s 未见 marker → 强制 ready + flush（rc 挂了也不能永久吞输入）
[退出]  shell 退出时未 ready → 丢弃 pending（无意义了）
```

**marker 剥除**：reader 的 UTF-8 切分在 marker 扫描**之前**——marker 全 ASCII 无跨块问题，但在 `Utf8Tail` 之后加扫描器时需处理 marker 恰被 8KB 读块边界切断的情况：扫描器持自己的尾部缓冲（保留可能的前缀字节，最多 32 字节）。

**为什么 stdin 排队**：marker 出现前用户敲键盘，字节若直达 shell 会被半初始化的 readline 丢弃或错序；排队到 marker 后统一放行，保证注入命令与用户输入都落在完整提示符之后。

### 3.4 注入字节构造

移植 orca `startup-command-submission.ts`（66 行）为 Rust `shell_ready.rs` 内函数：

```rust
/// 构造注入字节。claude 单行命令：`claude` + `\r`。
/// 多行未来扩展：包 ESC[200~..ESC[201~（bracketed paste）再补 submit。
pub fn build_startup_submission(command: &str) -> String
```

本期 `claude` 恒单行，bracketed paste 分支预留结构但暂无触发路径（写成防御式：含 `\n` 即包裹，zsh/bash bracketed-paste-safe 恒真）。

### 3.5 spawn 链路扩展

```rust
pub struct SpawnOpts {
    pub issue_id: String,          // 模块 2 起可为 `issueId` 或 `issueId::paneUuid`
    pub cwd: String,
    pub cols: u16,
    pub rows: u16,
    pub startup_command: Option<String>,  // 新增：Some("claude") 即走包装 spawn
}
```

- `spawn_fresh`：`startup_command` 为 Some 且 shell 是 zsh/bash → 包装 spawn（ZDOTDIR/rcfile）+ 会话挂 barrier；否则现状裸 spawn。
- **注入仅 fresh spawn 发生**：reattach/复用分支不重注入（shell 活着 claude 就活着；shell 已退出走重起 = 新 fresh）。注入实现 = flush pending 队列前先把 `claude\r` 入队（marker 后与用户排队输入一起放行）。
- 复用分支（fresh=false）返回不变—— StrictMode 双挂载安全性与现状一致。

### 3.6 pty_shutdown_issue（为 split 铺路）

```rust
#[tauri::command]
pub fn pty_shutdown_issue(issue_id: String) -> Result<(), String>
```
前缀扫描 store：key == `issueId` 或 key 以 `issueId::` 开头全部 shutdown + 移除。前端 `useDeleteProjectIssue` 从 `ptyShutdown(id)` 换成 `ptyShutdownIssue(id)`（模块 2 接入后一个 issue 可能有 N 个 pane 会话）。

### 3.7 配置与前端

- appConfig 新 key：`terminal_auto_run_claude`（`'Y' | 'N'`，默认 `'Y'`，双端镜像）。
- 设置页新分区「终端」（settings 菜单 + i18n key），首项即此开关；字体大小等模块 3 再入此分区。
- `EmbeddedTerminal.tsx`：`useConfigValue(TERMINAL_AUTO_RUN_CLAUDE_KEY, ...)` 读开关 → `usePtySession` 参数增 `autoRunClaude: boolean` → attach 的 spawn opts 按开关填 `startup_command`。
- `usePtySession` 编排不变（exists→reattach→spawn）；哑会话守卫（cwd null）不变——未配置根目录时既不 spawn 也不注入。

### 3.8 诊断日志（沿用 §3.9 第 6 条规范）

- Rust：`[pty] shell-ready wrapped spawn issue_id=`、`[pty] shell-ready marker seen issue_id=`、`[pty] shell-ready timeout force-flush issue_id=` 各一行；超时走 `log::warn`。
- 前端：无新增（注入全在后端，前端无感知）。

---

## 4. 任务清单

> 按序执行；每个任务独立实现 + 验证。
>
> 状态图例：✅ 已完成 · 🔄 进行中 · ⬜ 待办

### ⬜ 任务 1 — shell 包装文件生成 + marker（zsh/bash）
- **文件**：`src-tauri/src/pty/shell_ready.rs`（新增：包装文件生成 + marker 常量 + ShellReadyScanner 雏形）
- **目标**：按需生成 ZDOTDIR 包装（.zshrc widget 链式 + .zshenv/.zprofile/.zlogin 透传）与 bash rcfile 包装到 app_data_dir；marker 字节常量 `WE_TERM_SHELL_READY_MARKER`。
- **验证**：单测——临时 ZDOTDIR 下 `zsh -i` 手动跑包装文件，stdout 含 marker 字节；bash 同理。cargo test 通过。

### ⬜ 任务 2 — barrier + spawn 链路接入
- **文件**：`shell_ready.rs`（ShellReadyBarrier 完整实现）+ `session.rs`（PtySession 挂 barrier、reader 扫描剥 marker、write_input 排队）+ `local_provider.rs`（SpawnOpts.startup_command 分支：包装 spawn + barrier + 注入字节入 pending）+ `provider.rs`（SpawnOpts 字段）
- **目标**：§3.3/§3.4/§3.5 全链路。marker 前 stdin 排队；marker + 30ms flush；5s 超时强制放行；退出丢 pending。
- **验证**：单测——spawn 带 `startup_command: Some("echo MARKER_OK")`，ring 中出现 MARKER_OK 且**不含 marker 字节**；marker 前并发 write 被排队（顺序不断裂）；5s 超时路径单测（mock 不发 marker 的包装）；现有 smoke 测试不回归。

### ⬜ 任务 3 — pty_shutdown_issue 命令
- **文件**：`pty/mod.rs`（新命令）+ `lib.rs`（collect_commands! 注册）+ `src/state/tracker/queries.ts`（删除联动换用新命令）
- **目标**：前缀扫描关整个 issue 全部 pane 会话。
- **验证**：`pnpm gen:bindings` 出新命令；单测——store 里放 `a`、`a::p1`、`a::p2`、`b`，shutdown_issue("a") 后仅剩 `b`；cargo test + tsc 通过。

### ⬜ 任务 4 — 配置开关 + 设置分区 + 前端接线
- **文件**：`appConfig.ts`（新 key）+ settings 菜单/i18n/新分区页 + `EmbeddedTerminal.tsx` + `usePtySession.ts`
- **目标**：§3.7。开关默认开；关时不传 startup_command（行为与现状完全一致）。
- **验证**：tsc/eslint/web:build；真机——开关开：选中 issue 终端自动进 claude；F5 刷新不重注入（reattach）；关：普通 shell；开关切换即时生效（下次 spawn）。

### ⬜ 任务 5 — fish 回退 + 边界打磨
- **文件**：`shell_ready.rs`（shell 类型判定与 fast 回退）+ `local_provider.rs`
- **目标**：`$SHELL` 非 zsh/bash 时 startup_command 走 fast 注入（spawn 后首输出 + 30ms），日志标注降级。
- **验证**：临时 `SHELL=/usr/bin/fish cargo test` 相关单测；真机 fish 用户路径（如有）不卡死、命令最终执行。

### ⬜ 任务 6 — 真机验证 + 坑位归档
- **目标**：真机全场景过一遍（首开自动进 claude / claude 内 exit 回 shell / 重开按钮再进 claude / F5 / 切 issue 回切 / 删除 issue 无孤儿 / 开关关闭路径 / 慢 rc 用户场景）。踩坑按「现象→根因→修复」归档进本文档新 §（沿用 embedded_terminal.md §5.7 范式）。
- **验证**：全绿 + 文档更新落地记录。

---

## 5. 工程约束

### 5.1 注入语义边界
- 注入只在 **fresh spawn + startup_command 开启** 时发生，每会话至多一次；reattach/复用/重开均按现有语义（重开 = fresh，会再次注入——符合「重开终端重新进 claude」直觉）。
- 用户在 marker 前的键盘输入被排队不丢弃（顺序保持：注入命令 → 用户输入）。
- claude 不存在（未安装）：shell 报 `command not found` 自然暴露在终端里，不做预检（与「目录不存在不自动创建」同一哲学——环境问题交给用户，app 不越权）。

### 5.2 tauri-specta 类型约束（沿用）
`SpawnOpts` 增字段需保持 `#[derive(Clone,Serialize,Deserialize,Type)]` + `#[serde(rename_all="camelCase")]`；`Option<String>` 直接支持；新命令注册 collect_commands! 后 `pnpm gen:bindings`。

### 5.3 与 sessions 监听域的关系
app 的 sessions 监听（`src-tauri/src/sessions/`）扫描 `~/.claude/sessions/<pid>.json` 自动发现终端里的 claude。**已知盲点**：`enrich.rs` 按父进程链宿主识别（iTerm2/Terminal/idea），本 app PTY 内的 claude 会被判 Unknown 滤出列表。修复（识别自家宿主）在模块 3 `terminal_03_toolbar_extras.md` 任务中处理，本模块不动 sessions 域。

### 5.4 orca → 本项目对照
| orca | 本项目 |
|---|---|
| `SessionShellReadyBarrier`（daemon 内，DA1 应答权等） | `shell_ready.rs` 简化版（无 DA1 应答、无 headless 镜像——本项目 renderer xterm 直连） |
| 四 shell 包装 + PowerShell | zsh + bash；fish 走 fast 回退 |
| `buildAgentStartupPlan` 多 agent 配置矩阵 | 恒 `claude`，无参数 |
| `planClaudeSessionPin --session-id` | 本期不做（hooks 集成时再说） |
| 15s 超时 / codex 300ms | 5s（本项目 shell 场景简单，快失败快放行） |

---

## 6. 后续模块（不在本文档范围）
- hooks 状态集成（orca agent-hook server 模式）：claude 工作状态进 app 结构化展示。
- `claude --resume` / 会话延续。
- prompt 注入（argv 引号转义矩阵）。
