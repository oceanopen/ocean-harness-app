# Claude Orca 模式——模块任务清单

> 基于 [技术方案总览](claude_orca_mode_01_overview.md) 拆解的模块级任务清单。任务粒度为模块+功能+技术方案，不涉及具体代码文件。
> 任务执行完，要及时更新任务状态。
> **状态标记**：⬜ 待开始 | 🔲 进行中 | ✅ 已完成

---

## 阶段 1：Rust claude_runtime 基建（P1）

> 三源合一的核心：hook 事件经 spool 文件回传，Rust 侧归一化为唯一状态源，事件驱动推送前端。
> spool 通道选型依据见总览 §三.2（对比 axum HTTP：零依赖、零阻塞、天然多实例隔离）。

### T1.1 claude_runtime 域骨架 + 类型 + Store

**状态**：✅

**功能**：建立 `claude_runtime` 域骨架，定义 hook 载荷与前端事件类型，实现运行时状态 Store（含快照落盘）

**技术方案**：
- 新增 `src-tauri/src/claude_runtime/` 域（mod.rs 声明子模块，lib.rs 注册）
- `types.rs`：
  - hook 载荷反序列化（Claude hooks stdin JSON：`hook_event_name` / `session_id` / `transcript_path` / `cwd` / tool 信息（tool_name/tool_input）/ message / `prompt` / `delta`+`index`+`final`（MessageDisplay 流式增量，T1.3 接入）/ `permission_suggestions`（PermissionRequest，T4.1 接入）等，serde 容忍未知字段；T1.3 需按需扩字段）
  - 前端事件 payload `ClaudeRuntimeChangedPayload`（pane / status / previewText / notification / transcriptPath / claudeSessionId / lastUpdatedAt），specta 导出
- `store.rs`：
  - `ClaudeRuntimeStore`（`Mutex<HashMap<pane, ClaudeRuntimeState>>`，tauri State 管理；state 含 launch_token / claude_session_id / transcript_path / status / preview_text / notification / updated_at）
  - JSON 快照落盘（`app_data_dir/claude_runtime_snapshot.json`，store 变更时覆写）+ 启动 hydrate（支撑 app 重启后 resume 与冷启动展示）
- `lib.rs`：mod 注册 + `app.manage(store)` + init 时序（须早于 pty spawn 能力）

**依赖**：无

**参考**：本仓 `shared/state/transcript.rs` 的 TranscriptWatchStore 范式、`shared/types.rs` 的 specta 类型范式

---

### T1.2 hook 脚本生成 + 工作区 settings.json 安装器

**状态**：✅

**功能**：生成 claude hook 脚本（append spool），并提供工作区级 hooks 配置安装器（幂等合并）

**技术方案**：
- `script.rs`——生成 hook 脚本（`app_data_dir/claude-hooks/hook.sh`），模板顺序（对齐 orca 契约）：
  1. **env guard**（在读 stdin 之前，防 orca #11549 挂起）：`[ -z "$WE_TERM_SPOOL_DIR" ] && exit 0`
  2. **stdin 读取**（#8110 drain 契约）：`payload=$({ command -p cat 2>/dev/null || cat; })`，空则 `exit 0`
  3. **append**：`printf '%s\n' "$payload" >> "$WE_TERM_SPOOL_DIR/${WE_TERM_PANE//::/__}.jsonl"`（pane 文件名 sanitize：`::` → `__`）
  4. 全部失败路径静默（claude 无感，不因 hook 报错阻塞工具链）
- `installer.rs`——`ensure_workspace_hooks(workspace_dir)`：
  - 读写 `<workspace>/.claude/settings.json`（不存在则新建；已存在则保留用户内容；损坏从空起步 + `.bak` 兜底）
  - hooks 事件注册（7 事件，**实施时经 orca 最新 hook-settings.ts + claude 2.1.231 官方文档核查修正**——原稿 `User`/`Assistant` 事件不存在，会被 claude 静默忽略）：SessionStart / UserPromptSubmit / MessageDisplay / Stop / StopFailure / Notification（无 matcher）+ PermissionRequest（matcher `*`）
    - `UserPromptSubmit`（原稿 User）：用户提交 prompt → working
    - `MessageDisplay`（原稿 Assistant）：流式文本增量（delta/index/final），T3.1 流式气泡数据源
    - `StopFailure`（新增）：API 错误时 Stop 不触发（orca 踩坑，防 working 永久卡死）
    - `PermissionRequest`（新增）：审批瞬时信号（Notification.permission_prompt 要等 ~6s 且新版 claude 把 AskUserQuestion 也报成 PermissionRequest，orca waiting 态实际驱动源）
  - **只识别自有条目**（command 含 needle `claude-hooks/hook.sh`，文件名级——dev/release 双实例互扫陈旧条目）做替换升级，用户已有 hooks 条目与其他顶层键（permissions 等）不动
  - orca `writeHooksJson` 范式：temp+rename 原子写、滚动 `.bak` 单备份（拒符号链接）、内容相同跳过（防反复 install 滚掉备份）、脚本先于 settings 落盘
  - hook command 用守卫包裹（orca wrapPosixHookCommand）：`if [ -f -r -x '<script>' ]; then /bin/sh '<script>'; else <drain stdin>; fi`——脚本缺失静默 no-op，claude transcript 不报 hook error
- Tauri 命令 `ensure_workspace_hooks(cwd)`（前端 spawn 前调用，幂等；bindings 已导出 `ensureWorkspaceHooks`）

**依赖**：T1.1

**单测**：✅ 全绿（17 个）——合并不覆盖用户条目、陈旧自有条目替换、同 definition 内用户/自有 handler 共存只剥自有、备份滚动、内容相同跳过（.bak 不滚）、损坏 settings 恢复、脚本先落且 755、模板 env guard 顺序（guard 在 stdin 读取之前）、守卫路径单引号转义

**实测**（2026-08-24，claude 2.1.231 真会话）：受控工作区装 hooks + env 标跑 `claude -p` → spool 收到 SessionStart / UserPromptSubmit / MessageDisplay（含 delta/index/final）/ Stop 四事件全链载荷，`::`→`__` sanitize 生效；env 缺席时脚本不读 stdin 即退（无挂起）；守卫缺失脚本路径时 drain stdin exit 0

---

### T1.3 spool watcher + ingest 归一化 + 事件 emit

**状态**：⬜

**功能**：监听 spool 目录，消费新增行，归一化为状态机更新并事件推送前端

**技术方案**：
- `watch.rs`：
  - notify watcher（线程模型对齐 `sessions/watch.rs`，去抖 200ms）监听 `app_data_dir/claude-spool/`
  - 变更文件集 → 读新增行（per-file offset 记忆，`read_range` 范式同 `transcript/tail.rs`；尾部半行留待下轮）
  - 逐行调 `ingest`
- `ingest.rs`——`ingest(app, pane, line)`：
  - 反序列化 hook 载荷（损坏行 skip + warn，不 panic）
  - **launch_token 围栏**：store 内该 pane 的 token 与载荷不符 → 丢弃（防同 pane 重启 claude 后旧会话迟到事件覆盖新状态，orca #1146 教训）
  - 按 `hook_event_name` 归一化状态机（事件名以 T1.2 实测核查为准）：
    - `SessionStart` → 绑定 session_id / transcript_path + **截断该 pane 的 spool 文件**（新会话新起点）+ status=idle + 记录 launch_token
    - `UserPromptSubmit` / `MessageDisplay` → status=working，MessageDisplay 时以 delta 增量拼 preview_text（同一消息按 index 追加）
    - `Stop` / `StopFailure` → status=idle + 清 preview / notification
    - `PermissionRequest` → status=waiting + notification 载荷（tool_name/tool_input/permission_suggestions；AskUserQuestion 提问卡也走此事件）
    - `Notification` → waiting 补充信号 + message 原文（permission_prompt 有 ~6s 延迟，仅兜底展示）
  - 更新 store + 落快照 + `app.emit("claude-runtime:changed", payload)`
- 目录治理：启动时按 mtime 淘汰超限 spool 文件（如保留最近 50 个）
- **hydrate 陈旧态重置**：启动 hydrate 时对每条恢复的 state 重置 `status=idle` + 清空 `launch_token` / `preview_text` / `notification`（app 重启后 claude 实际状态未知，等新 SessionStart 重新绑定 token；避免快照陈旧 working 态误导前端门槛与 T6.1 fallback 判定；`claude_session_id` / `transcript_path` 保留供 resume 与定位）
- `lib.rs`：spool 目录创建 + watcher 线程启动

**依赖**：T1.1（store）

**单测**：状态机迁移、token 围栏、半行容忍

---

### T1.4 PTY spawn env 打标

**状态**：⬜

**功能**：PTY spawn 时注入归因/通道/代际三个 env 标，打通「pane → claude → hook 脚本」的归因链

**技术方案**：
- `pty/local_provider.rs` 的 `spawn_fresh`，`CommandBuilder.env` 注入：
  - `WE_TERM_PANE=<session_id>`（即 `issueId::paneId` 锚点，→ spool 文件名）
  - `WE_TERM_SPOOL_DIR=<app_data_dir>/claude-spool`（通道目标）
  - `WE_TERM_LAUNCH_TOKEN=<每次 spawn 新 uuid>`（代际标，围栏用）
- **无条件注入**（与 chat 模式开关无关）：N 模式下脚本未安装，env 存在但无 hook 消费，无害；模式热开启后已运行会话的下一个 hook 事件即可被归因
- 验证 zsh 包装分支（ZDOTDIR 换装）env 透传不被剥

**依赖**：T1.1（spool 目录常量）

---

## 阶段 2：前端接入（P2）

> hook 状态成为前端第一优先级数据源；现有轮询全链保留为 fallback。

### T2.1 useClaudeRuntime hook + 定位/门槛切源

**状态**：⬜

**功能**：前端订阅 claude-runtime 状态；transcript 定位与 composer 门槛优先走 runtime，回落现有逻辑

**技术方案**：
- `shared/events.ts`：`EVENT_CLAUDE_RUNTIME_CHANGED` 常量
- `pnpm gen:bindings` 刷新类型（specta）
- 新 hook `useClaudeRuntime(sessionId)`（放 EmbeddedTerminal/NativeChat 同级）：
  - `listen("claude-runtime:changed")` 按 pane 过滤 → `{ status, previewText, notification, transcriptPath, claudeSessionId, lastUpdatedAt }`
  - 初始值经一次性命令查询（Tauri 命令 `claude_runtime_state(sessionId)` 读 store 快照，避免挂载空窗）
- `useTranscript` 定位段改造：
  - locate 优先取 runtime 的 transcriptPath（有值且 claudeSessionId 非空）→ **fallback** 现有 `ptyClaudeSession` 进程树路径（hook 缺席时兜底）
  - 顺带修复：新版 claude transcript 文件名 uuid ≠ session_id，cwd 推导路径不可靠（总览 §一）
  - `claudeStatus` 优先 runtime（无 runtime 数据时回落 session ref）
- composer 门槛（`canSend` / `isBusy` / `isWaiting`）：优先 runtime 状态派生

**依赖**：T1.3、T1.4

**验证**：装 hook 后开 chat 模式会话，发消息观察状态即时切换（无秒级轮询滞后）

---

## 阶段 3：发送体验（P3）

### T3.1 发送队列 + 乐观 echo + 流式气泡

**状态**：⬜

**功能**：chat 发送丝滑化——串行防交错、立即回显、生成中实时预览

**技术方案**：
- `chatSendQueue.ts`（新，NativeChat 同级）：
  - per-session 发送串行队列（对齐 orca `native-chat-pty-send-queue.ts`）：队列空闲时 start 同步执行；二次入队先取消上一次延迟 Enter（防正文粘行）；cancel 语义联动乐观 echo 清除
  - `EmbeddedTerminal.sendChatMessage` 改走队列
- 乐观 echo：
  - 发送时立即在消息列表尾部插入合成 user 消息（pending id）
  - transcript 真实 user turn 落地后 prune（按 id 剪除，对齐 orca `prunePendingSends`）
- 流式气泡：
  - runtime previewText → 合成 assistant 气泡（id `streaming`）
  - deriveStreamingText 规则对齐 orca `native-chat-streaming.ts`：working 且预览文本 > 最后一条 assistant 文本（不被包含）才显示；transcript 追上自然消失；非 working 恒不显示

**依赖**：T2.1

**单测**：队列交错（二次发送取消延迟 Enter）、echo prune、streaming derive 规则

---

## 阶段 4：交互卡片（P4）

### T4.1 审批卡 / 提问卡替代「切回终端」

**状态**：⬜

**功能**：claude 权限确认与自由提问在 chat 内以原生卡片直接回答，消除最大隔离感来源

**技术方案**：
- Notification 载荷解析：区分权限审批（含 tool_name / tool_input）与自由提问（message 含选项列表）
- `NativeChatInteractiveCard` 组件（NativeChat 同级）：
  - 审批卡：允许 / 拒绝 / 总是允许按钮
  - 提问卡：选项按钮 + 自由输入框
  - 激活时**替换 composer**（orca 同构：卡片自带输入区，避免双输入框）
  - 回答 = 写选项序号/文本 + `\r`（经 T3.1 发送队列，含步进延迟常量，对齐 orca `use-native-chat-interactive-send`）
- 卡片消失时机：Stop / User 事件清除 notification
- 移除现有「切回终端回答」banner（waiting 态改为卡片渲染）

**依赖**：T2.1（notification 载荷）、T3.1（经队列发送）

**验证**：触发 Bash 权限请求 → chat 内审批卡 → 允许 → 任务继续

---

## 阶段 5：直接启动 + resume（P5）

### T5.1 PTY 直接 spawn claude

**状态**：⬜

**功能**：chat 模式下打开终端直接进入 claude 会话（去除 shell 中转），归因钉死在 spawn 时刻

**技术方案**：
- `SpawnOpts` 增字段 `direct_command: Option<String>`（或 startup_command 语义扩展）
- `spawn_fresh` 分支：`direct_command` 在场时 `CommandBuilder::new(claude_bin)` 直接 spawn（env 打标沿用 T1.4；**无 shell_ready barrier**）
- `resolve_claude_bin()`：login shell `which claude` 探测 + `OnceLock` 缓存；失败回落现有 shell 注入路径（记 warn log）
- 前端 `EmbeddedTerminal`：chat 模式（`chat_mode_switch=Y` 且 `startup_code_cli=claude`）时传 direct 启动参数
- 语义变化（已确认接受）：claude 退出即 pane 退出（无 shell 回落）→ 走 exited UI；跑普通命令用附加 pane（现有设计附加 pane 恒裸 shell）

**依赖**：T1.4（env 打标在直接 spawn 路径同样生效）

---

### T5.2 退出重开 + `--resume` 恢复

**状态**：⬜

**功能**：claude 退出后「重开并启动 claude」支持恢复上次会话上下文

**技术方案**：
- exited 条「重开并启动 claude」→ `claude --resume <last_session_id>`（id 来自 T1.1 快照 store；无记录时裸启动）
- resume 参数注入 T5.1 的 direct_command 路径（`claude --resume <id>` 整串作为 direct 命令）
- 快照 hydrate 覆盖 app 重启场景（T1.1 已落盘）

**依赖**：T5.1、T1.1（快照）

**验证**：chat 模式打开 issue 直接进会话；exit 后重开 resume 恢复上下文

---

## 阶段 6：边界与回退（P6）

### T6.1 回退路径 + 模式热切换 + 全链验证

**状态**：⬜

**功能**：hook 链路任何一环失联时全链回落现有逻辑；模式切换热生效；全链 e2e 验证

**技术方案**：
- **installer 加固（T1.2 审查遗留 A/B，集中此处处理）**：
  - A：hooks 子树反序列化失败时用户条目整文件丢弃（read_settings 空模型起步太激进）——重构为 serde_json::Value 层手动遍历识别，任意形态都保得住用户内容
  - B：BTreeMap 字母序丢失用户键序，claude 自身改写 settings（插入序）后「内容相同跳过」失效、`.bak` 反复滚动——Value 层重构顺带解决（保留原键序）
- **hook 链路失联回退**：`useClaudeRuntime` 无数据（spool 目录空 / 事件从未到达）时，前端全链回落现有轮询（`useTranscript` 的 ptyClaudeSession 路径 + `useClaudeRunning` ps 探测）——现有代码保留为 fallback，不删
- `useClaudeRunning` 按钮置灰：优先 runtime 状态（pane 有 runtime 条目即以它为准），无条目 fallback 探测
- **模式热切换**：`chat_mode_switch` 开启 → 前端下次 spawn 前调 `ensure_workspace_hooks`；运行中会话不生效（claude 启动时读配置，UI 提示「重启会话后生效」）；关闭后残留脚本因 env guard 是 no-op（不卸载，orca 同策略）
- **e2e 全链验证**（chat 模式）：
  1. 打开 issue → 直接进 claude 会话（无 shell 中间层）
  2. chat 发消息 → 立即见 echo → 流式气泡 → transcript 落地替换
  3. 触发 Bash 权限 → chat 内审批卡 → 按钮允许 → 任务继续
  4. 停止 → 状态即时 idle（无秒级滞后）
  5. claude 退出 → 重开 → resume 恢复上下文
  6. 关 chat 开关 → 行为完全回到现状
- **隔离验证**：iTerm2 里跑同目录 claude → env guard 不写 spool，监控页不受扰
- **claude 无感验证**：杀掉 app 后 claude 继续跑，hook append 不报错不卡顿
- `cargo test` + `pnpm tsc --noEmit` + lint 全绿

**依赖**：T3.1、T4.1、T5.2

---

## 依赖关系总览

```
T1.1 骨架+store ──┬── T1.2 脚本+安装器
                 ├── T1.3 watcher+ingest ──┐
                 └── T1.4 env 打标 ────────┴── T2.1 前端订阅+切源
                                              ├── T3.1 发送队列+echo+流式 ──┐
                                              ├── T4.1 交互卡片 ──────────┤
                                              └── T5.1 直接spawn ── T5.2 resume ──┤
                                                                          T6.1 边界+e2e
```

## 与后续自动化扩展的关系

本清单 P1–P6 产物（hook 事件流 / env 归因 / transcript 权威路径 / 直接 spawn / resume）即自动化执行（`claude -p` headless 后台任务）的全部地基；自动化作为独立后续扩展（ClaudeRunner 进程管理 + stdout 流解析 + 任务队列），不在本清单范围。
