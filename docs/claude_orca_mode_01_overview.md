# Claude Orca 模式：后台执行 + 统一数据源

> 可行性分析与技术方案（基于 orca 最新代码重新梳理），2026-08-24

## 背景与目标

当前 chat 模式切换体验"隔离、不丝滑"。目标：

- **不支持 chat 模式切换**（`chat_mode_switch=N`）：保持手动触发 claude 方式**完全不变**，零风险。
- **支持 chat 模式切换**（`chat_mode_switch=Y`）：改成 orca 那种丝滑方式——终端与 chat 共享同一数据源（PTY + transcript + hook 状态推送），交互对齐 orca。

先澄清一个关键事实：**orca 的 chat 发消息同样是回写 PTY 字节**（`native-chat-send.ts`：Ctrl+U 清行 + bracketed paste + 延迟独立 Enter，与本仓 `chatSend.ts` 同源）。发送通道不是差距所在，**状态与数据的驱动方式**才是。

---

## 一、与 orca 的真实差距（体验"隔离感"来源）

| 环节 | 现状 | orca | 差距 |
|------|------|------|------|
| 状态感知 | 轮询 `~/.claude/sessions/*.json` + 爬进程树，秒级延迟 | **Claude hooks 主动推送**（Assistant/Stop/Notification/SessionStart） | canSend/Busy 滞后、切换生硬 |
| 流式预览 | transcript tail 500ms 轮询，仅完整消息 | hook `lastAssistantMessage` 实时合成 streaming 气泡，transcript 兜底 | 无"正在打字"活感 |
| 发送反馈 | 消息发出后消失，等 transcript 追上 | **乐观 echo**：立即显示，transcript 落地后 prune 替换 | "消息凭空消失"空窗 |
| waiting 交互 | banner「切回终端回答」强制上下文切换 | **NativeChatQuestionCard / ApprovalCard**：原生卡片（按钮+输入框）直接在 chat 回答（选项字节回写 PTY） | 隔离感最大来源 |
| transcript 定位 | cwd 推导路径 | hook 直接给 `transcript_path`（orca 注释明说：新版 claude 文件名 uuid ≠ session_id，**推导不可靠**） | 路径推导是隐患 |
| 会话归属 | 进程树匹配 | spawn 时 **env 打标**（launchToken），hook 回传 | 精确免轮询 |
| 启动方式 | shell 起来后注入 `claude\r`（有中间层） | PTY 直接 spawn claude；退出后 `claude --resume <id>` 重开 | "直接进入会话" |

### orca 关键实现索引（最新代码确认）

- 视图切换：`TerminalPane.tsx:3138-3183`——chat 用 `createPortal` 渲染进 pane 容器（`absolute inset-0 z-10` overlay），**xterm 全程存活**；`effectiveChatViewMode = nativeChatEnabled && isChatViewMode`。
- 发送：`native-chat-send.ts`（纯字节函数）+ `native-chat-runtime-send.ts`（clearThenWrite：Ctrl+U → 确认清空 → framed body → **独立延迟 Enter**）+ `native-chat-pty-send-queue.ts`（**per-PTY 串行队列**，防二次发送与延迟 Enter 交错）。
- 乐观 echo：`NativeChatView.tsx`——`appendPendingSendCache` 立即回显，`prunePendingSends` 在 transcript 真实 user turn 落地后剪除。
- 流式气泡：`native-chat-streaming.ts`——hook 预览文本 > 最后一条 assistant 文本时显示合成气泡，transcript 追上后自然消失。
- 交互卡片：`NativeChatInteractiveCard`（question 卡片激活时**替换 composer**）+ `use-native-chat-interactive-send.ts`（选项步进字节，`NATIVE_CHAT_QUESTION_STEP_MS` 等常量）。
- Hook 链路：`src/main/agent-hooks/`——`installer-utils.ts` 的 `writeHooksJson`（temp+rename 原子写、滚动 `.bak`、内容相同跳过）+ curl 超时（`--connect-timeout 0.5 --max-time 1.5`，#4633）+ token 校验（`X-Orca-Agent-Hook-Token`，每次启动 `randomUUID()`，错则 403）；`posix-hook-command.ts`（`if [ -f -r -x ]` 守卫）；`hook-stdin-contract.ts`（stdin drain 契约，#8110/#11549）。
- 会话恢复：`agent-session-resume.ts`——hook 提供 `session_id` + `transcript_path`，`getAgentResumeArgv('claude') → ['claude', '--resume', id]`。
- 陈旧事件围栏：`server.ts:1146` —— launchToken hash 围栏，防同 pane 重启 claude 后旧会话迟到事件覆盖新状态。

---

## 二、目标架构

```
┌────────────────────────────────────────────────────────┐
│ Claude Code (PTY 子进程, env 带 WE_TERM_* 标)            │
│  ├─ stdout → SessionIo → xterm        （终端视图，不变）  │
│  ├─ transcript JSONL → tail 增量       （权威消息列表）    │
│  └─ hooks → 脚本 append spool JSONL                     │
│       SessionStart: session_id + transcript_path 绑定    │
│       Assistant:     working + 预览文本                  │
│       User:          working（用户已提交）                │
│       Stop:          idle                              │
│       Notification:  waiting + 权限/提问卡片载荷          │
└────────────────────────────────────────────────────────┘
Rust claude_runtime 域（notify watch spool → 唯一状态源，事件驱动 emit）
  → 前端 useClaudeRuntime 订阅（纯订阅不轮询）
Chat 视图 = 三源合成：transcript 消息 + 乐观 echo + hook 流式气泡
发送 = 现有 PTY 字节 + per-session 发送队列（防交错）
```

三视图共享三数据源（与 orca 一致）：
- **同一 PTY**：终端渲染 stdout；chat 发送回写 stdin。
- **同一 transcript**：chat 权威消息列表（tail 增量）。
- **同一 hook 状态**：Rust store 推送，驱动 composer 门槛/流式气泡/交互卡片/按钮置灰。

---

## 三、关键设计决策

### 1. Hook 作用域：工作区级（我们适配）

orca 写全局 `~/.claude/settings.json`；本仓改为 **`<workspace_base_dir>/<issueId>/.claude/settings.json`**：
- 与 agent_dev 已规划的 `.mcp.json` 工作区初始化同一范式。
- 零全局污染；claude 按目录合并 global+project hooks，天然只对本工作区生效。
- spawn 前幂等确保文件存在（内容相同跳过写）。

### 2. Hook 回传通道：spool 文件 + fs watch（比 orca HTTP 更简洁的本仓适配）

orca 用 Electron main 的 Node HTTP server（它有 SSH 远程 pane / relay 需求）；本仓为 macOS 单用户纯本地 app，且全链路已是 fs-watch 驱动（sessions watch / transcript tail），改用 **spool 文件追加 + notify watch**：

- **通道形态**：hook 脚本 `printf '%s\n' "$payload" >> "$WE_TERM_SPOOL_DIR/<paneId>.jsonl"`；Rust 侧 notify watcher 监听 spool 目录 → 读新增行 → 归一化 → store → emit。
- **零新依赖**：notify 已在依赖树（sessions watch 在用）；无端口、无 token、无慢连接/超时问题。
- **claude 零阻塞**：app 崩溃/未启动时 append 照常成功；无 curl 超时等待。
- **多实例天然隔离**：spool 目录在各自 app_data_dir，dev/release 双开无冲突。
- **单写者保证**：每 pane 单文件；claude hooks 同进程串行执行，无并发写。
- **归因即文件名**：paneId 在文件名里；无标（外部终端的 claude）时 `WE_TERM_SPOOL_DIR` env 不存在 → 脚本 env guard 直接 exit 0（**不读 stdin**，防 orca #11549 式永久挂起）。
- **清理**：SessionStart 事件截断该 pane 的 spool（新会话新起点）+ 目录容量上限（按 mtime 淘汰）。
- **延迟**：notify 触发即读，200ms 去抖合并（同 sessions watch 范式）。

**stdin 契约**（orca #8110 教训，照抄）：脚本用 `{ command -p cat 2>/dev/null || cat; }` 读 payload（防 PATH 被剥、防 repo-local cat 劫持），任何早退路径都已 drain stdin；env guard 在读 stdin **之前**（无标路径不读即退，见上）。

**预留 HTTP 升级位**：`claude_runtime` 域的 ingest 接口（`fn ingest(event) -> ()`）与通道解耦；未来需要 SSE 推流 / 远程 pane 时加 axum（`axum = "0.8"`；依赖树已兼容：tokio 1.x / hyper 1.x / tower 均在树中，实际新增仅 3 个小 crate）另起通道，spool 不动。

> 备选记录：axum HTTP（orca 同款成熟路径，端口/token/超时/慢连接都要处理；SSH 远程与 SSE 场景出现时再上）；tiny_http（全同步但维护完成态）；Go sidecar 复用（hook server 须常驻而 sidecar 用户手动启停，生命周期耦合）。均暂不采用。

### 3. 会话归因：env 打标

spawn PTY 时（shell 注入与直接启动两条路径都覆盖）`CommandBuilder.env` 注入：
- `WE_TERM_PANE=<issueId::paneId>`：归因标（→ spool 文件名）。
- `WE_TERM_SPOOL_DIR=<app_data_dir>/claude-spool`：通道目标。
- `WE_TERM_LAUNCH_TOKEN=<uuid>`：本次 spawn 的代际标（陈旧事件围栏用，见 §三.7）。
- shell → claude → hook 脚本逐层继承 env。
- 无标事件（用户在 iTerm2 里跑的 claude）→ env guard 不写 spool，天然过滤外部会话。
- 兜底：现有 `claude_state.rs` 进程树匹配保留，作为 hook 缺席时的 fallback。

### 4. 模式分流

| 配置 | 启动方式 | 状态来源 | Chat |
|------|---------|---------|------|
| `chat_mode_switch=N`（默认） | 现状：shell + 手敲/注入 claude | 现状轮询 | 无，**一行不改** |
| `chat_mode_switch=Y` | PTY 直接 spawn claude（无 shell 中间层） | hook 推送（轮询兜底） | orca 式三源合成 |

- hook 安装仅在 chat 模式启用时进行；关闭后残留脚本因 env guard 是 no-op（orca 同策略，不卸载）。
- claude 二进制解析：启动时经 login shell `which claude` 探测缓存；失败回落现有 shell 注入路径。

### 5. transcript_path 权威化

hook `SessionStart` 载荷自带 `session_id` + `transcript_path`，作为 chat 视图定位的第一优先级；现有 cwd 推导路径降级为 fallback。**顺带修复**：新版 claude transcript 文件名 uuid 可能 ≠ session_id，推导路径随时会断（orca 已踩坑）。

### 6. 安装器健壮性（照抄 orca 范式）

- `writeHooksJson`：temp+rename 原子写、滚动 `.bak` 单备份、内容相同跳过（防反复 install 滚掉备份）。
- **合并而非覆盖**用户已有 hooks 配置（识别自有条目：command 含本仓脚本路径，升级时替换、卸载语义只删自己的）。
- 脚本守卫：`if [ -f -x <script> ]; then ...; fi`——脚本缺失时静默 no-op，不阻塞 claude。

### 7. 陈旧事件围栏与持久化（orca server.ts:1146 教训）

- **代际围栏**：同 pane 重启 claude 后，旧会话的迟到事件不能覆盖新状态。store 记录当前 `launch_token`（SessionStart 时绑定），后续事件 token 不匹配则丢弃。
- **落盘**：runtime store 为内存态；`session_id`（resume 用）+ 最后状态在变更时追加写 app_data_dir 下的 JSON 快照（容量截断），app 重启后 hydrate——支撑「重开 → `claude --resume`」与监控页冷启动。

---

## 四、实施分期

### P1 Rust claude_runtime 基建（核心）

新增 `src-tauri/src/claude_runtime/` 域：
- `script.rs`：生成 hook 脚本（app_data_dir；env guard + stdin 契约 + spool append，模板含超时不需要——无网络）。
- `installer.rs`：工作区 `.claude/settings.json` 幂等合并（orca writeHooksJson 范式：原子写/备份/相同跳过/只识别自有条目）。
- `watch.rs`：notify 监听 spool 目录，200ms 去抖 → 读新增行 → `ingest`。
- `ingest.rs`：载荷反序列化 + 归一化（launch_token 围栏 + SessionStart 绑定/截断）。
- `store.rs`：`ClaudeRuntimeStore`（pane → {claudeSessionId, transcriptPath, status, previewText, notification, launchToken}）+ JSON 快照落盘/hydrate。
- `types.rs`：hook 载荷 + 前端事件 payload（specta 导出）。
- 事件：`claude-runtime:changed` emit。
- `pty/local_provider.rs`：spawn env 打标（WE_TERM_PANE / WE_TERM_SPOOL_DIR / WE_TERM_LAUNCH_TOKEN）。
- `lib.rs`：init + spool 目录创建 + watcher 启动。

### P2 前端接入

- `shared/events.ts`：`EVENT_CLAUDE_RUNTIME_CHANGED`；`pnpm gen:bindings`。
- 新 hook `useClaudeRuntime(sessionId)`：状态/预览/notification 订阅。
- `useTranscript` 定位段：优先 runtime 绑定（transcriptPath）→ fallback 现有 `ptyClaudeSession` 路径。
- composer 门槛（canSend/isBusy/isWaiting）改由 runtime 驱动。

### P3 发送体验

- `chatSendQueue.ts`：per-session 发送串行队列（对齐 orca native-chat-pty-send-queue：二次发送先取消上一次延迟 Enter）。
- 乐观 echo：发送立即入列显示，transcript 真实 user turn 落地后 prune。
- 流式气泡：runtime previewText 合成 assistant 气泡，transcript 追上自然消失（deriveStreamingText 对齐 orca 规则）。

### P4 交互卡片

- `NativeChatInteractiveCard`：Notification 载荷 → 审批卡（允许/拒绝/总是允许 → 写选项序号 + `\r`）与提问卡（选项按钮 + 自由输入）。
- 卡片激活时替换 composer（orca 同构）；字节序列含步进延迟常量。
- 替换现有「切回终端回答」banner。

### P5 直接启动 + resume

- `SpawnOpts` 增启动模式：chat 模式下 `CommandBuilder::new(claude_bin)` 直接 spawn（env 打标，无 shell_ready barrier）。
- claude 退出 → 现有 exited UI；「重开」改为 `claude --resume <last_session_id>`（session id 来自 runtime 快照）。
- claude 路径解析：login shell `which` 探测 + 缓存 + 失败回落注入路径。

### P6 边界与回退

- spool watch 不可用/hook 未装：全链回落现有轮询（现有代码不动，仅降级为 fallback）。
- 模式热切换：开启即装 hook（运行中 claude 不生效，重启会话后生效——claude 启动时读配置，文档说明）。
- `useClaudeRunning` 按钮置灰：优先 runtime 查询，fallback ps 探测。

---

## 五、验证方案

- **Rust 单测**：spool 解析（合法/半行/损坏行 skip）、归一化（token 围栏/SessionStart 截断/状态机迁移）、安装器合并（已有 hooks 合并不覆盖、备份滚动、相同跳过、只删自有条目）、快照落盘/hydrate 往返。
- **前端单测**：发送队列交错、乐观 echo prune、流式气泡 derive 规则。
- **手动 e2e**（chat 模式）：
  1. 打开 issue → 直接进 claude 会话（无 shell 中间层）
  2. chat 发消息 → 立即见 echo → 流式气泡 → transcript 落地替换
  3. 触发 Bash 权限 → chat 内审批卡 → 按钮允许 → 任务继续
  4. 停止 → 状态即时 idle（无秒级滞后）
  5. claude 退出 → 重开 → resume 恢复上下文
  6. 关 chat 开关 → 行为完全回到现状
- **隔离验证**：iTerm2 里跑 claude（同目录）→ env guard 不写 spool，监控页不受扰。
- **claude 无感验证**：杀掉 app 后 claude 继续跑，hook append 不报错不卡顿。

---

## 六、与旧版分析的差异

1. **统一数据源的核心不是 transcript，而是 hook 状态推送**——transcript 我们已有，缺的是 push 语义。
2. **发送通道无需改造**——orca 也是 PTY 回写；要补的是队列串行与乐观 echo，不是换通道。
3. **hook 通道本仓适配为 spool 文件**——orca 的 HTTP 是为远程 pane 服务；纯本地场景 spool + fs watch 零依赖、零阻塞、天然隔离，且与现有 sessions watch / transcript tail 同构。
4. **坑位清单沉淀**（认证/超时/stdin/env-guard/双实例/陈旧事件）大多来自 orca 已修复的 issue（#4633/#8110/#11549/#1146），spool 通道直接消解了其中网络相关的坑。

自动化任务（`claude -p` 后台 spawn）结论不变，仍为后续独立扩展；本方案 P5 的直接 spawn + resume 是它的地基。
