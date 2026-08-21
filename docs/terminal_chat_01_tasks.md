# 嵌入式终端对话模式——模块任务清单

> 基于 [技术方案总览](terminal_chat_00_overview.md) 拆解的模块级任务清单。任务粒度为模块+功能+技术方案，不涉及具体代码文件。
>
> **状态标记**：⬜ 待开始 | 🔲 进行中 | ✅ 已完成

---

## 阶段 1：基础设施（P0）

### T0.0 嵌入式终端设置拆分 + 模式切换开关

**状态**：✅

> **第一项处理**：配置项改造，是 chat 功能的前置闸门，独立于后续所有任务。

**功能**：设置页「嵌入式终端」聚合配置拆为「启动设置」「样式设置」两个模块；「启动终端时自动运行」改名「启动主终端时自动运行」；新增「是否支持 Terminal/Chat 模式切换」开关（条件可用）。

**技术方案**：
- 改名：`settings:terminal.row.startupCodeCli` 文案「启动终端时自动运行」→「启动主终端时自动运行」。**行为不变**——`EmbeddedTerminal.tsx` 现有 `startupCodeCli: isMain ? startupCodeCli : 'none'` 本就只作用于主 pane，分屏恒裸 shell，改名仅澄清语义。
- 新增配置项：`TERMINAL_CHAT_MODE_SWITCH_KEY = 'terminal_chat_mode_switch'`，YesNo，默认 `NO`。纯前端偏好，走 `appConfig.ts` 现有 YesNo 范式（参照 `TERMINAL_CURSOR_BLINK_KEY`）。
- 条件可用：`startupCodeCli === 'none'` 时 modeSwitch 强制 `NO` 且置灰不可点；非 `none` 时可切换。
- 模块拆分：`TerminalConfigPage.tsx` 现有 embeddedTerminal SectionCard 拆两个：
  - 「启动设置」：启动主终端时自动运行 + 是否支持 Terminal/Chat 模式切换
  - 「样式设置」：字号 + scrollback 行数 + 主题 + 光标样式 + 光标闪烁 + 行高

**行为矩阵**：

| startupCodeCli | chatModeSwitch | 行为 |
|---|---|---|
| none（默认） | N（强制） | 传统：主 pane 裸 shell，手动跑 claude，无切换 icon |
| claude | N | 传统：主 pane 自动跑 claude TUI，无切换 icon（回退路径） |
| claude | Y | 自动跑 claude + 顶部 Terminal/Chat 切换 icon |

**依赖**：无

**参考**：`TerminalConfigPage.tsx` 现有 embeddedTerminal SectionCard、`appConfig.ts` 的 cursorBlink（YesNo 范式）、`settingOption.ts` 的 `terminalStartupCodeCliOptions`

---

### T0.1 会话锚点统一 `issueId::<paneId>`

**状态**：✅

> **独立任务**：与对话模式（chat）解耦，是既有会话锚点约定的重构，可单独提交、不阻塞后续任务。

**功能**：把会话锚点从「main = 裸 issueId / split = `issueId::paneId`」统一为「一律 `issueId::<paneId>`」，移除「裸 issueId == 主 pane」特例。

**技术方案**：
- 前端：`EmbeddedTerminal.tsx` 派生 `sessionId` 收敛为无分支的 `` `${issueId}::${paneId}` ``（`paneId` 默认 `'main'`，天然得 `issueId::main`）。
- 后端：`pty_shutdown_issue` 命中判定由 `key == issue_id || key.starts_with("issueId::")` 简化为 `key.starts_with("issueId::")`。
- 无迁移：PTY store 为内存态、布局持久化存 (issueId, paneId) 不存 session_id；「是否主 pane」用 `paneId === 'main'`（`types.ts` 的 `MAIN_PANE_ID`），不随锚点变化。

**依赖**：无

**参考**：`EmbeddedTerminal.tsx` 现有 sessionId 派生、`pty/mod.rs` 的 `pty_shutdown_issue`

---

### T1.1 主 pane 自动运行 + chat 闸门派生

**状态**：⬜

**功能**：主 pane 自动运行由 `terminal_startup_code_cli` 配置驱动（现有逻辑，非恒定注入）；`EmbeddedTerminal` 读 `terminal_chat_switch` 派生 `chatEnabled` gate，决定是否具备 Terminal/Chat 切换能力。

**技术方案**：
- 主 pane 自动运行：现有 `startupCodeCli: isMain ? startupCodeCli : 'none'` 已实现（配置驱动、仅主 pane），T0.0 改名后语义不变，本任务仅确认无需后端改动。
- 派生 gate：`EmbeddedTerminal` 新增订阅 `terminal_chat_switch`（YesNo），计算 `chatEnabled = isMain && startupCodeCli !== 'none' && chatSwitch === 'Y'`。
- `chatEnabled` 传给 `TerminalView`，作为「是否渲染切换 icon / 是否启用 viewMode」的总闸（T2.1 消费）。
- 分屏 pane 恒 `chatEnabled = false`（分屏不自动运行 claude，无 chat）。

**依赖**：T0.0（chatSwitch 配置已存在）

**参考**：`EmbeddedTerminal.tsx` 现有 `startupCodeCli` 派生、`appConfig.ts` 的 YesNo 解析（`parseYesNo`）

---

### T1.2 PTY ↔ claude sessionId 关联

**状态**：⬜

**功能**：给定主 pane 的 session_id，定位「跑在该 PTY shell 下的 claude」的 sessionId + transcript 路径。

**技术方案**：
- 从 `claude_state.rs` 抽出「沿 claude pid 父链匹配本会话 shell pid」逻辑，返回 `Option<u32>`（claude pid），而非 bool。
- 用 claude pid 读 `~/.claude/sessions/<pid>.json`（复用 `sessions::raw::RawSessionFile` 的 `session_id` + `cwd` 字段）。
- 推导 transcript 路径：`~/.claude/projects/<cwd.replace('/', '-')>/<session_id>.jsonl`。
- 新增 Tauri command `pty_claude_session(session_id) → Option<ClaudeSessionRef>`，返回 `{ claude_pid, session_id, cwd, transcript_path, status }`。
- cwd 空/异常 → 返回错误态（transcript 路径不可信）。

**依赖**：无（可独立开发；运行时需 claude 在跑才能返回 Some）

**参考**：`claude_state.rs`（父链匹配已实现）、`sessions/raw.rs`（RawSessionFile 已含 session_id/cwd）

---

### T1.3 transcript JSONL 解析层（Rust 新域）

**状态**：⬜

**功能**：新增 `src-tauri/src/transcript/` 域，解析 `~/.claude/projects/**/*.jsonl`，产出结构化消息列表，支持全量读 + 增量 follow。

**技术方案**：
- 子模块拆分（对标 `sessions/` 域）：
  - `raw.rs`：反序列化单行 JSONL（容错 schema 漂移，未知字段忽略）
  - `decode.rs`：`RawLine → TranscriptMessage`（过滤 type=user/assistant；content 分支 string/array；block 映射 text/thinking/tool_use/tool_result/image）
  - `reader.rs`：全量读 + 快照
  - `tail.rs`：增量 follow（notify + poll 双轨，对标 `sessions/watch.rs` + `poll.rs`）
  - `store.rs`：会话级快照缓存 + emit `transcript:changed` 事件
- 消息模型：`TranscriptMessage { id, role, blocks, timestamp }`，specta 导出。
- **容错（KTD）**：未知 type / 未知 block / 非法 JSON → skip 不 panic；`sessionId`/`session_id` 双写兼容（但定位不依赖行内 sessionId）。
- P1 只消费「已完成 turn」；流式合并留 P3。

**依赖**：无（纯文件解析，可独立开发 + 单测）

**参考**：orca `transcript-line-decoders-claude.ts` / `transcript-reader.ts` / `transcript-watch.ts`；本 app `sessions/` 域结构

---

## 阶段 2：chat 模式（P1）

### T2.1 主 pane viewMode 状态 + 切换按钮 + overlay 骨架

**状态**：⬜

**功能**：主 pane 加 `viewMode: 'terminal' | 'chat'` 局部状态，工具条顶部加 Terminal/Chat 切换 icon（仅 `chatEnabled` 为 true 时渲染）；chat 模式在 xterm 同容器 overlay 聊天组件（xterm 不卸载）。

**技术方案**：
- `useState<'terminal' | 'chat'>`（主 pane 组件局部，不建全局 store）。
- 切换 icon 渲染受 `chatEnabled`（T1.1 派生）gate——`false` 时完全不渲染，无回退风险。
- 工具条（`TerminalView`）顶部加切换 icon（对标「启动 claude」按钮位）。
- chat overlay：`absolute inset-0 z-[≥1000] bg-background`（xterm 容器不建堆叠上下文，浮层须 ≥1000 压过内部 z-5/10，参考 xterm 浮层 z-index 经验）。
- 切回 terminal 仅摘 overlay，PTY/xterm 全程存活。

**依赖**：T1.1（chatEnabled gate）

**参考**：orca `TerminalPane.tsx` 的 `createPortal` overlay；本 app xterm 浮层 z-index 经验

---

### T2.2 chat 只读视图（渲染历史对话）

**状态**：⬜

**功能**：chat 视图读 transcript 事件流，渲染 `TranscriptMessage` 气泡列表，含 loading/empty/ready/error 状态。

**技术方案**：
- 前端新组件（`EmbeddedTerminal/` 同级）：`NativeChatView` + `NativeChatMessageList` + 空态/错误态。
- 数据链路：首次 `pty_claude_session` 拿 transcript 路径 → 订阅 `transcript:changed` 事件渲染。
- 消息渲染：role 区分气泡（user 右 / assistant 左）；block 类型分段（text 渲染 markdown，tool-call 折叠，tool-result 折叠，thinking 折叠）。
- composer 本期隐藏/置灰，提示「切回终端发送」。

**依赖**：T1.2 + T1.3（有 transcript 数据可读）

**参考**：orca `NativeChatView.tsx` / `NativeChatMessageList` / `NativeChatEmptyState`

---

### T2.3 边界：分屏/关闭/刷新重连/切换 issue

**状态**：⬜

**功能**：主 pane 的 chat 在分屏、关闭、webview 刷新、切换 issue 时行为正确。

**技术方案**：
- 分屏：chat 只存在于主 pane；分屏不展示切换按钮。
- 关闭/重开：chat 视图随 viewMode 摘挂；PTY 重开（reopen）后重新注入 claude + 重新定位 transcript。
- 刷新重连：webview 刷新后 reattach 恢复 PTY；chat 视图重新走 `pty_claude_session` + 全量读。
- 切换 issue：切走断订阅（现有 `usePtySession` 已保证），切回重新编排 + 重新定位 transcript。

**依赖**：T2.1 + T2.2

**参考**：现有 `usePtySession` reattach/复用语义

---

## 阶段 3：完整对话（P2）

### T3.1 chat 发消息 + 停止

**状态**：⬜

**功能**：chat composer 可发送消息（回写 PTY），可停止（ESC 中断）。

**技术方案**：
- 发消息：composer 提交 → 清空 TUI 未提交输入（Ctrl+U）→ `session.write(text + '\r')`（复用现有 `write`）。
- gated on metadata `status === 'idle'`（响应中/交互态禁止写）。
- 停止：`session.write('\x1b')`（ESC = claude TUI 中断键）。
- 发送后乐观回显（消息先入气泡，等 transcript 落盘后去重，对标 orca pending-send 缓存）。

**依赖**：T2.2

**参考**：orca `native-chat-runtime-send.ts`（clearThenWrite + 正文 + 回车）

---

### T3.2 流式预览（进行中 turn 实时气泡）

**状态**：⬜

**功能**：chat 实时显示 claude 正在生成的回复（非等 turn 落盘）。

**技术方案**：
- 增量 follow：`tail.rs` 逐行 decode 追加，识别「进行中 assistant turn」（尚未以新 user 行为界的 assistant 行序列）。
- 渲染：进行中 turn 显示为「打字中」气泡，完成后固化为普通气泡。
- 与「已完成 turn」合并去重（按 `uuid`）。

**依赖**：T1.3（tail 增量）、T3.1

**参考**：orca `transcript-watch.ts` + `native-chat-streaming.ts`

---

### T3.3 交互式 prompt 处理

**状态**：⬜

**功能**：claude 的权限确认 / yes-no / tool 审批在 chat 模式下可应答或引导切回终端。

**技术方案**：
- 检测：metadata `status === 'waiting'` → chat 顶部提示「claude 等待输入」。
- 应答：识别交互类型后，简单 yes/no 提供按钮（回写 `y`/`n` + 回车）；复杂 prompt 提示切回终端。
- 或降级：`waiting` 态一律提示「切回终端回答」。

**依赖**：T3.1

**参考**：orca `NativeChatInteractiveCard`（question/approval 卡片）

---

## 依赖关系图

```
T0.0 设置拆分 + 模式切换开关（第一项，无前置）
  └─→ T1.1 主 pane 自动运行 + chat 闸门派生
        └─→ T2.1 viewMode 切换骨架（chatEnabled gate）

T0.1 会话锚点统一（无前置，独立）
  └─→ T1.2 PTY↔sessionId 关联
        └─→ T2.2 chat 只读视图

T1.3 transcript 解析层（无前置，可并行）
  └─→ T2.2 chat 只读视图
        └─→ T3.1 发消息/停止
              ├─→ T3.2 流式预览
              └─→ T3.3 交互式 prompt

T2.3 边界（依赖 T2.1 + T2.2，可与 T2.2 并行）
```
