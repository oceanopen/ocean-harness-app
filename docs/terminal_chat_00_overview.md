# 嵌入式终端对话模式（terminal ⇄ chat）——技术方案

> **本模块定位**：在现有嵌入式终端（`src-tauri/src/pty` + `src/windows/panel/.../EmbeddedTerminal`）基础上，为**主 pane**（paneId == 'main'，session_id == `issueId::main`）增加「对话模式」：claude 常驻跑在底层 PTY，terminal 模式渲染其 TUI 原始字节，chat 模式读 transcript JSONL 渲染成原生聊天气泡 + 回写 PTY。分屏 pane 及 panel/pet 其他窗口保持现状（裸 shell + 用户自选跑不跑 claude）。
>
> **核心原则**：**PTY 是底层唯一事实源**，claude 常驻跑在 PTY 的 shell 里；terminal 与 chat 都是盖在它上面的「上层 UI」，读/写的是同一个 claude 会话。切换只是换 UI 层，PTY 从不卸载（现有 `reattach` + ring 已保证）。

---

## 1. 需求全景

### 1.1 功能清单

| # | 功能 | 优先级 | 类型 |
|---|------|--------|------|
| F1 | 主 pane 自动运行 + Terminal/Chat 模式切换开关（配置驱动，含设置页拆「启动/样式」两模块） | P0 | 前端配置 |
| F2 | PTY ↔ claude sessionId 关联（定位本 pane 的 transcript 文件） | P0 | 工程化 |
| F3 | transcript JSONL 解析层（读 + follow，容错 schema 漂移） | P0 | 工程化 |
| F4 | 主 pane `viewMode: 'terminal' | 'chat'` 状态 + 切换按钮 | P0 | 前端 |
| F5 | chat 只读视图（transcript 渲染气泡，空态/loading/error） | P1 | 前端 |
| F6 | chat 发消息（回写 PTY）+ 停止（ESC） | P1 | 前端+PTY |
| F7 | 流式预览（进行中 turn 的实时气泡） | P2 | 前端 |
| F8 | 交互式 prompt 处理（waiting 态 / 权限确认） | P2 | 前端 |
| F9 | 边界：分屏、关闭、刷新重连、切换 issue | P1 | 全端 |

### 1.2 已确认的设计决策

| 决策项 | 结论 |
|--------|------|
| 作用范围 | 仅**主 pane**（paneId == 'main'）；分屏 pane 与 panel/pet 窗口保持现状 |
| 会话锚点 schema | 统一 `issueId::<paneId>`（main → `issueId::main`，split → `issueId::<uuid>`），移除「裸 issueId == 主 pane」特例（见 §3.2 独立任务） |
| claude 生命周期 | 主 pane 自动运行由 `terminal_startup_code_cli` 配置驱动（默认 `none` = 不自动、裸 shell）；分屏恒不自动（现有 `isMain ? startupCodeCli : 'none'`） |
| 模式切换闸门 | 新增 `terminal_chat_switch`（YesNo，默认 NO）；仅 `startupCodeCli != 'none' && chatSwitch == 'Y'` 才显示切换 icon；`none` 时强制 NO 且置灰 |
| 数据源关系 | terminal 模式 = PTY 原始字节（现状，xterm 渲染 TUI）；chat 模式 = transcript JSONL 解析 + PTY 回写。两者共享同一 sessionId |
| PTY 卸载策略 | 切换 viewMode 时 PTY 不卸载、xterm 不卸载（chat 是 overlay portal） |
| transcript 定位 | claude pid → `~/.claude/sessions/<pid>.json`（sessionId + cwd）→ `~/.claude/projects/<cwd 的 `/`→`-`>/<sessionId>.jsonl` |
| 解析容错 | 顶层 `sessionId` / `session_id` 双写、未知 `type` / 未知 block 一律 skip 不 panic（同 `raw.rs` 现有 schema 漂移意识） |
| chat 发消息门槛 | gated on metadata `status === 'idle'`（避免在 TUI 响应中/权限弹窗中误写） |
| 交互式 prompt | P2 之前降级：chat 检测到 `status === 'waiting'` 时提示「切回终端回答」 |

---

## 2. 参考项目分析

### 2.1 orca（`~/Project/orca`）

**定位**：多 agent 终端（Claude Code / Codex / Grok），terminal ⇄ chat 切换是它的成熟能力。

**关键机制**：

| 机制 | orca 实现 | 与本方案关系 |
|------|-----------|-------------|
| 切换状态 | tab 级 `viewMode: 'terminal' | 'chat'`，`toggleTabViewMode` | **直接借鉴**：本方案在主 pane 加同名字段 |
| 渲染切换 | chat 是 `createPortal` 盖在 xterm 上的 `absolute inset-0 z-10` 不透明层，terminal 不卸载 | **直接借鉴**：xterm 常驻，chat overlay |
| 读消息 | `transcript-reader` / `transcript-watch` 读 `~/.claude/projects/**/*.jsonl`，逐行 decode → `NativeChatMessage` | **直接借鉴**：本方案 Rust 侧照此拆分 raw/reader/tail |
| 发消息 | `sendNativeChatMessage` → 写 PTY（清空未提交输入 + 正文 + 回车）；Stop = 写 ESC | **直接借鉴**：本方案复用现有 `session.write()` |
| 流式预览 | agent-status hook（`lastAssistantMessage`）+ transcript 边界合并 | **P2 才做**，P1 只显示已完成 turn |
| 交互式 prompt | `NativeChatInteractiveCard`（question/approval 专用卡片） | **P2 才做**，P1 降级提示切回终端 |
| leaf 路由 | `native-chat-leaf-routing.ts` 处理分屏/关闭/重连时 chat 落哪个 leaf | 本方案无分屏 chat（仅主 pane），该复杂度**不引入** |

**关键启发**：
- chat 模式不另起非终端进程——**claude 仍在 PTY 里跑**，chat 只是「皮肤」，输入输出在 PTY 层桥接。这恰好复用本 app 已有的 PTY 栈。
- orca 为「流式预览 + 交互式 prompt + 分屏路由」写了大量代码；本方案只做主 pane，第一版可跳过这些，先跑通「同一份底层、两种 UI」。

---

## 3. 技术方案

### 3.1 架构总览

```
┌───────────────────────────────────────────────────────────────┐
│        DevWorkbench 主 pane（paneId='main'，session_id=issueId::main）        │
│                                                                 │
│   viewMode: 'terminal' | 'chat'（切换按钮）                        │
│   ┌──────────────────────────┐    ┌───────────────────────────┐ │
│   │ terminal 模式            │    │ chat 模式（overlay 盖 xterm）│ │
│   │  xterm 渲染 TUI 原始字节  │    │  NativeChatView + Composer │ │
│   └──────────────────────────┘    └───────────────────────────┘ │
└───────────────────────────────────────────────────────────────┘
              ▲  PTY 字节（现状）        ▲ transcript 事件（读）  │
              │                          │                       │  session.write（发消息/停止）
              │                          │                       ▼
        ┌─────┴──────────────────────────┴──────────────────────────┐
        │  PTY（shell 常驻，claude 作为子进程跑在 shell 里）           │
        │  = 底层唯一事实源（terminal 与 chat 共享同一会话）           │
        └──────────────────────────────────────────────────────────┘
                                ▲
                                │ claude 自己写
              ┌─────────────────┴──────────────────────────┐
              │  ~/.claude/sessions/<pid>.json（元数据）      │
              │  ~/.claude/projects/<dir>/<sessionId>.jsonl │
              └────────────────────────────────────────────┘
```

### 3.2 独立任务：会话锚点统一 `issueId::<paneId>`

> **独立于 chat 功能**：这是既有会话锚点约定（「main = 裸 issueId / split = `issueId::paneId`」→「一律 `issueId::<paneId>`」）的重构，与对话模式解耦，可单独提交、不阻塞 chat 任务。

- 前端：`EmbeddedTerminal.tsx` 派生 `sessionId` 的表达式由 `paneId === 'main' ? issueId : `${issueId}::${paneId}`` 收敛为无分支的 `` `${issueId}::${paneId}` ``（`paneId` 默认 `'main'`，天然得 `issueId::main`）。
- 后端：`pty_shutdown_issue` 命中判定由 `key == issue_id || key.starts_with("issueId::")` 简化为 `key.starts_with("issueId::")`（`issueId::main` 已被前缀覆盖）。
- 无持久化迁移：PTY store 为内存态、布局持久化存 (issueId, paneId) 不存 session_id；「是否主 pane」本就用 `paneId === 'main'`（`types.ts` 的 `MAIN_PANE_ID`），不随锚点变化。

### 3.3 F1：主 pane 自动运行 + 模式切换开关（配置驱动）

**前置**：依赖 §3.2 会话锚点统一（独立任务，先落地）；配置改造先行（任务 T0.0）。

**配置模型**（两个纯前端偏好，`appConfig.ts`）：
- `terminal_startup_code_cli`（label「启动主终端时自动运行」）：`'none'`（默认）| `'claude'`
- `terminal_chat_switch`（label「是否支持 Terminal/Chat 模式切换」）：YesNo，默认 `NO`；`startupCodeCli === 'none'` 时强制 NO 且置灰

**行为矩阵**：

| startupCodeCli | chatSwitch | 行为 |
|---|---|---|
| none（默认） | N（强制） | 主 pane 裸 shell，手动跑 claude，无切换 icon |
| claude | N | 主 pane 自动跑 claude TUI，无切换 icon（回退路径） |
| claude | Y | 自动跑 claude + 顶部 Terminal/Chat 切换 icon |

**实现**：
- 自动运行：现有 `startupCodeCli: isMain ? startupCodeCli : 'none'` 已实现（配置驱动、仅主 pane），改名后语义不变，后端零改动。
- gate 派生：`EmbeddedTerminal` 读 `terminal_chat_switch`，计算 `chatEnabled = isMain && startupCodeCli !== 'none' && chatSwitch === 'Y'`，传给 `TerminalView` 决定是否渲染切换 icon。
- 「启动 claude」按钮语义不变（claude 常驻时置灰，分屏保留）。

**影响面**：`TerminalConfigPage.tsx`（设置拆分 + 新开关）、`EmbeddedTerminal.tsx`（读 chatSwitch 派生 gate）；后端零改动。

### 3.4 F2：PTY ↔ sessionId 关联

**目标**：给定主 pane 的 `session_id`（= `issueId::main`），定位「跑在这个 PTY shell 下的 claude」的 transcript 文件路径。

**现有能力**：`src-tauri/src/pty/claude_state.rs` 已实现「沿 claude pid 父链向上匹配本会话 shell pid」——当前只返回 bool（`claude_running`）。

**改造**：
1. 从 `claude_state.rs` 抽出「找到本 shell 下的 claude pid」——返回 `Option<u32>`（claude pid），而非 bool。
2. 用 claude pid 读 `~/.claude/sessions/<pid>.json`（`sessions::raw::RawSessionFile` 已含 `session_id` + `cwd`）。
3. 推导 transcript 路径：
   ```
   dir = "~/.claude/projects/" + cwd.replace('/', '-') + "/"
   path = dir + session_id + ".jsonl"
   ```
4. 新增 Tauri command（如 `pty_claude_session`）返回 `{ session_id, transcript_path, status }`，前端 chat 视图据此订阅。

**注意事项**：
- 现有 `raw.rs` 只声明了 6 个字段，`session_id` 与 `cwd` 已包含——够用，无需扩 schema。
- cwd 为空/异常（如根路径）时 transcript 路径不可信，返回错误态让 chat 视图显示空态。
- 多 claude 同壳罕见（正常一个 shell 只跑一个 claude 交互会话）；若多个，取父链最近的。

### 3.5 F3：transcript JSONL 解析层（Rust 新域）

**新域**：`src-tauri/src/transcript/`，对标现有 `sessions/` 域的单一关注点拆分：

| 子模块 | 职责 | 对标 |
|--------|------|------|
| `raw.rs` | 反序列化单行 JSONL（容错 schema 漂移），输出 `RawLine` | `sessions/raw.rs` |
| `decode.rs` | `RawLine → TranscriptMessage`（type 过滤 + content block 映射） | orca `transcript-line-decoders-*.ts` |
| `reader.rs` | 全量读文件 + 已解析快照 | orca `transcript-reader.ts` |
| `tail.rs` | 增量 follow（notify 文件监听 + poll 兜底，对标 `sessions/watch.rs` + `poll.rs`） | orca `transcript-watch.ts` |
| `store.rs` | 会话级快照缓存 + emit `transcript:changed` 事件 | `sessions/store.rs` |

**JSONL 行 schema（本机实测，仅字段名）**：

```
顶层 type：mode / permission-mode / file-history-snapshot / user / assistant /
          attachment / ai-title / last-prompt / ...（只关心 user / assistant）
user 行顶层：cwd, entrypoint, gitBranch, isSidechain, message, origin, parentUuid,
             permissionMode, promptId, promptSource, sessionId, timestamp, type,
             userType, uuid, version
assistant 行顶层：cwd, effort, entrypoint, gitBranch, isSidechain, message, parentUuid,
                  sessionId, session_id, timestamp, type, userType, uuid, version
message.content（array 或 string）：
  user      → string 或 [ tool_result ]
  assistant → [ text | thinking | tool_use | image ]
```

**关键容错点（KTD）**：
- 顶层同时出现 `sessionId`（早期行）与 `session_id`（后期行）——**解析层不依赖 transcript 行里的 sessionId**（文件名即 sessionId），故不影响定位；但 decode 时若需 uuid 须兼容两种 key。
- 未知 `type` / 未知 content block / 非法 JSON 行 → **skip 不 panic**（同 `raw.rs` 处理损坏 session json 的方式）。
- content 可能是 `string` 或 `array`（实测 user 两种都有），decode 须两分支。

**消息模型（Rust 侧，specta 导出给前端）**：

```rust
struct TranscriptMessage {
    id: String,                    // record.uuid
    role: TranscriptRole,          // user / assistant / tool / system
    blocks: Vec<TranscriptBlock>,  // text / thinking / tool-call / tool-result / image
    timestamp: Option<i64>,
}
```

**read/follow 策略**：
- 首次进入 chat：`reader` 全量读，产出已完成 turn 列表。
- 会话进行中：`tail` 用 notify 监听文件 append（claude 逐行写）+ 轮询兜底（对标 `sessions` 域的 watch/poll 双轨），增量行经 `decode` 后追加。
- P1 只消费「已完成 turn」（assistant turn 以 `user` 行或空闲为界）；P3 才做「进行中 turn」的流式合并。

### 3.6 F4/F5/F6：viewMode 状态 + chat UI

**状态**：主 pane 组件内加 `viewMode: 'terminal' | 'chat'`（`useState`），切换按钮放终端工具条（`TerminalView` 现有工具栏）。**不需要**全局 store——chat 只属于主 pane，局部状态足够；若后续要持久化再迁。

**渲染**：`TerminalView` 保持 xterm 常驻；`viewMode === 'chat'` 时，在 xterm 同一容器上 overlay 一个 `NativeChatView`（`absolute inset-0 z-[≥1000] bg-background`，参考 xterm 浮层 z-index 经验：容器不建堆叠上下文，浮层须 ≥1000 级压过 xterm 内部 z-5/10）。切换回 terminal 只是摘掉 overlay。

**chat 视图**（前端新组件，放 `EmbeddedTerminal/` 同级或子目录）：
- 数据：订阅 `transcript:changed` 事件 + 首次 `pty_claude_session` 拿 transcript 路径 → 渲染 `TranscriptMessage` 列表。
- 状态机：`loading`（transcript 未就绪）/ `empty`（无消息）/ `ready`（有消息）/ `error`（transcript 不可读）。
- **发消息（P1 后置，P2 做）**：composer 输入 → `session.write(text + '\r')`（复用现有 `write`）。gated on metadata `status === 'idle'`。
- **停止（P2）**：`session.write('\x1b')`（ESC = claude TUI 中断键）。

**只读降级（P1）**：chat 视图只显示已完成 turn，composer 隐藏或置灰并提示「切回终端发送」。先验证「同一份底层、两种 UI」的切换与数据链路。

---

## 4. 实施顺序

| 阶段 | 内容 | 交付 | 预计 |
|------|------|------|------|
| P0 | F1 + F2 + F3（配置改造 + 模式切换开关 + sessionId 关联 + 解析层） | 配置就绪 + 后端能定位并吐出 transcript | 2-3 天 |
| P1 | F4 + F5 + F9（切换 + 只读 chat + 边界） | 主 pane 可切 chat 看历史对话 | 2-3 天 |
| P2 | F6 + F7 + F8（发消息 + 流式 + 交互） | chat 可完整对话 | 3-4 天 |

---

## 5. 风险与待确认项

### 5.1 待确认

| # | 问题 | 影响 | 建议默认 |
|---|------|------|----------|
| Q1 | chat 发消息是否要「清空 TUI 未提交输入」再写（orca 的 `clearThenWrite`）？ | 发送可靠性 | 是，P2 发消息先 Ctrl+U 清空再写正文+回车 |
| Q2 | 主 pane 是否保留「启动 claude」按钮？ | UI | 主 pane 隐藏（claude 常驻）；分屏保留 |
| Q3 | viewMode 是否需随 issue 切换 / 刷新持久化？ | 状态管理 | 本期局部 state，不持久化 |
| Q4 | 用户手动 exit claude 后，chat 视图如何表现？ | 边界 | 检测到 transcript 不再 append + shell 仍活 → chat 显示「claude 已退出」提示可重开 |

### 5.2 已知风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| transcript schema 随 claude 版本漂移（`sessionId`/`session_id` 双写已见） | 解析错乱/崩溃 | decode 全分支容错，未知字段 skip；文件名定位不依赖行内 sessionId |
| claude TUI 交互态（权限弹窗/yes-no）在 chat 覆盖下不可见 | 对话卡住 | metadata `status` 驱动：`waiting` 时 chat 顶部提示切回终端（P2 前）；P3 做专用卡片 |
| 流式 follow 的开销 | 大 transcript 频繁监听 | 复用 `sessions` 域 watch/poll 双轨 + 去抖；P1 仅全量读 |
| 发送时机不当（claude 响应中写 PTY） | 输入乱序 | gated on `status === 'idle'` + 发送前清空未提交输入 |

---

## 6. 参考项目映射

| 本方案功能 | 参考项目 | 参考文件/模块 | 借鉴点 |
|-----------|---------|-------------|--------|
| viewMode 切换 | orca | `tabs.ts` `toggleTabViewMode` / `TerminalPane.tsx` overlay portal | 切换状态 + overlay 渲染（xterm 不卸载） |
| transcript 解析 | orca | `transcript-line-decoders-claude.ts` / `transcript-reader.ts` / `transcript-watch.ts` | 逐行 decode + 全量/增量双路 + schema 容错 |
| 发消息/停止 | orca | `native-chat-runtime-send.ts` | 写 PTY（清空 + 正文 + 回车）/ ESC 停止 |
| 文件监听/轮询 | 本 app | `sessions/watch.rs` + `poll.rs` | 复用 watch/poll 双轨模式，无需新依赖 |
| shell 就绪注入 | 本 app | `pty/shell_ready.rs` | 复用 marker + barrier 注入 `claude` |
