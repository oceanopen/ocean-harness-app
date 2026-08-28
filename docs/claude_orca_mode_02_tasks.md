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

**状态**：✅

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

**单测**：✅ 全绿（22 个新增）——状态机迁移、token 围栏（含僵尸时序与 SessionStart 重绑例外）、子代理 SessionStart 丢弃、Notification 忽略、兜底绑定、permission_suggestions 提取、半行容忍、offset 对齐、淘汰选择

**实现要点**（基于 orca v1.4.178 复核 + claude 2.1.228 二进制核实，2026-08-25）：
- **launch_token 注入**：T1.2 脚本模板升级——stdin 读取后、append 前把 `WE_TERM_LAUNCH_TOKEN` 前插为载荷首字段（`{"launch_token":"…",` 拼接）；空对象/非 JSON 原样透传；内容恒定，既有工作区经 installer 内容差异自动升级（settings.json 不动）。shell 实测注入产物为合法 JSON。
- **围栏语义**（对齐 orca server.ts:1146 + 1129）：store token 在场且载荷 token 不符 → Drop；**tokened SessionStart 例外放行并重绑**（同 pane 重启新 claude 的首事件，挡住则永远无法换代）；载荷无 token（T1.4 前窗口）放行。
- **Notification 不入状态机**（orca v1.4.178 已移除：claude idle 时也发 "waiting for your input"，误置 waiting）；T1.2 的注册保留作观察渠道。MessageDisplay 只推 working 态——**流式 preview 拼接已砍**（对齐 orca：流式文本由 transcript 增量驱动，T3.1 接手；本仓 transcript/tail.rs 即等价物）。
- **子代理防御**：带 agent_id 的 SessionStart 忽略（Task 子进程不得翻转 pane 状态）。
- **兜底绑定**（orca providerSession 思路）：任意带 session_id/transcript_path 的事件可补绑（SessionStart 丢失时不至于全盲）。
- **冷启动跳到 EOF**：启动时既有 spool 文件 offset 对齐末尾完整行，历史行不重放（状态由快照 hydrate 恢复，重放反而污染）。
- **批后截断消竞态**：SessionStart 的 spool 截断请求经 ingest 返回值传回，drain 批处理结束后统一截断 + offset 归零——若批中截断，批末 offset 写回会覆盖 0 起点（旧坐标重读、事件双份）。
- **persist 仅 SessionStart 触发**：hydrate 重置其余全部字段，快照唯一有效信息是 session 绑定；避免流式期写盘风暴。
- **hydrate 陈旧态重置**：恢复的 state 重置 status=idle + 清 launch_token/preview_text/notification，保留 claude_session_id/transcript_path。
- **目录治理**：启动按 mtime 淘汰，保留最近 50 个 spool 文件。
- claude 版本注意：本机现为 brew cask 2.1.228（T1.2 实测时为 2.1.231，安装源不同）；2.1.228 二进制 strings 确认 7 注册事件 + delta/index/final + permission_suggestions + last_assistant_message 均在场。

**冒烟实测**（2026-08-25，dev app 手写 spool 载荷行）：watcher 启动消费 → SessionStart 绑定 + 快照落盘；UserPromptSubmit/PermissionRequest 正常消费（围栏内）；旧 token 僵尸 Stop 被围栏丢弃；半行留待下轮补齐后消费；新代际 SessionStart（token 换代）消费后 spool 文件截断清空 + 快照更新。真 claude 会话端到端待 T1.4 env 注入后验证。

---

### T1.4 PTY spawn env 打标

**状态**：✅

**功能**：PTY spawn 时注入归因/通道/代际三个 env 标，打通「pane → claude → hook 脚本」的归因链

**技术方案**：
- `pty/local_provider.rs` 的 `spawn_fresh`，`CommandBuilder.env` 注入：
  - `WE_TERM_PANE=<session_id>`（即 `issueId::paneId` 锚点，→ spool 文件名）
  - `WE_TERM_SPOOL_DIR=<app_data_dir>/claude-spool`（通道目标）
  - `WE_TERM_LAUNCH_TOKEN=<每次 spawn 新 uuid>`（代际标，围栏用）
- **无条件注入**（与 chat 模式开关无关）：N 模式下脚本未安装，env 存在但无 hook 消费，无害；模式热开启后已运行会话的下一个 hook 事件即可被归因
- 验证 zsh 包装分支（ZDOTDIR 换装）env 透传不被剥

**依赖**：T1.1（spool 目录常量）

**单测**：✅ 全绿（新增 3 个）——env 构造（PANE 恒定 / token per-spawn 唯一且 uuid 形态 / SPOOL_DIR 与常量同源）、PTY 透传（zsh 包装分支 spawn 后 echo 三标回显断言）、`#[ignore]` 真 claude 端到端（手动跑）

**实现要点**（2026-08-26）：
- **注入点在包装 if 块之后、`spawn_command` 之前**：zsh/bash 包装分支会重建 `CommandBuilder`（ZDOTDIR 换装 / --rcfile），注入在重建前会被剥；置于其后则裸 spawn / zsh 包装 / bash 包装 / fast 回退 4 条路径一处全覆盖。wrapper 文件本身只 unset ZDOTDIR 与 `_we_term_*` 临时变量，`WE_TERM_*` 进程继承透传无忧。
- **token 生成用 uuid crate v4**：uuid v1 已在依赖树（tauri 传递依赖），Cargo.toml 声明直接依赖零新增外部 crate。token 为 36 位连字符 uuid，无 JSON 转义字符（script.rs 拼接前提）。**不可复用 issueId**：issueId 跨 spawn 恒定，恰无法区分同 pane 重启换代——围栏失效即 orca #1146 僵尸事件坑。
- **SPOOL_DIR 依赖 `APP_DATA_DIR`（OnceLock）**：生产 setup 恒注入；缺席（个别单测）时跳过该项，hook 脚本 env guard 自然 no-op。PANE/TOKEN 无条件注入。
- `installer::install` 放开为 `pub(crate)`：无 AppHandle 核心逻辑，命令入口与 e2e 集成测试共用。

**实测**（2026-08-26）：单测 6+3 全绿（105 passed 全量回归无破坏）；真 claude 端到端（`cargo test claude_e2e -- --ignored`，25.76s）：受控工作区装 hooks → PTY spawn（env 随进程注入）→ `claude -p` 全链 → spool 收到 SessionStart→Stop 完整事件链、全部载荷行携带同一非空 launch_token——**T1.3 冒烟遗留的「真 claude 会话端到端」至此闭环**。

**审查修复**（2026-08-26，缺陷+架构双审）：生产代码零缺陷；3 项低severity 已修——透传测试 break 加「anchor 后 ≥36 字节」守卫（防分块切断 flaky panic）、e2e 注释补「勿 --include-ignored 并行」（OnceLock 抢注）、`WE_TERM_*` 三 env 名提为 `claude_runtime::script` pub const（注入侧单源，脚本模板保持字面量零插值）。

---

## 阶段 2：前端接入（P2）

> hook 状态成为前端第一优先级数据源；现有轮询全链保留为 fallback。

### T2.1 useClaudeRuntime hook + 定位/门槛切源

**状态**：✅

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

**实现要点**（2026-08-26）：
- Rust 命令 `claude_runtime_state`（claude_runtime/mod.rs，`State<ClaudeRuntimeStore>` 注入 + 复用 `to_payload`）已进 collect_commands；events.ts 补 re-export，常量链路（events.rs→bindings→events.ts）闭合。
- `useClaudeRuntime`（EmbeddedTerminal/ 下，与 usePtySession 同级）：**先 listen 后查快照**（useTranscript 订阅段同款约束）；快照晚到按 `lastUpdatedAt < 已见事件` 拒绝回退覆盖；unlisten 竞态降 debug（useConfigValue 范式）。
- **path 与 alive 解耦**：transcriptPath 用 runtime（hook 载荷直给，修 uuid ≠ session_id 的 cwd 推导不可靠）；alive/status 判定仍走 `ptyClaudeSession` 进程树（pid 级匹配可靠，查询保留）。进程树无 claude 但 runtime 有 path 时也能定位（claude 退出后历史可读，locate alive=false）。
- locate effect deps 只取 `runtime?.transcriptPath`（原始值）——流式事件（previewText 高频变）不触发重定位，仅新 SessionStart 换路径重订阅。
- **claudeStatus 渲染期合成**：`runtime != null ? map(runtime.status) : claudeStatus`（idle/working/waiting → Idle/Busy/Waiting），无 setState 时序问题；NativeChatView 门槛派生（判 PascalCase）零改动自动切源。`waitingFor` 保持 session ref 来源（T4.1 卡片接手 notification）。
- 手动工具测试 `install_hooks_for_dev_e2e`（installer tests，env 驱动 WE_E2E_BASE/WE_E2E_WS；T6.1 前端接自动安装后可删）。

**验证记录**（2026-08-26）：tsc / web:lint / cargo test（105 passed）全绿；dev app 实测（装 hook 会话状态即时切换 + uuid 定位修复 + 未装 hook fallback）按用户决定**延至 T3.1 完成后合并验证**（届时发送队列 + echo 就绪，验证面更完整）。

**审查修复**（2026-08-26，双 code-reviewer 全维度审查后 5 项全修，静态验证复跑全绿）：
- **[高] claudeStatus 合成补存活门槛**：runtime 条目永不清理（ingest 无删除路径 + hydrate 每次重启恢复），claude 退出后 `runtime != null` 恒成立 → claude-exited 视图下 canSend=true，聊天正文会被写进 shell 当命令执行。修复：合成叠加 locate 存活判定（`located && alive` 才输出状态，否则 null），同时兑现接口契约注释。**等待被 kill 场景（runtime 卡 waiting）的 waiting banner 也由该门槛一并收口**；runtime 条目的 Rust 侧清理/降级留 T6.1。
- **[中] listen-before-snapshot 真正落实**：原先只保证两 invoke 派发顺序，`listen()` 未 await——注册窗口内事件丢失后旧快照长驱直入。修复：快照查询链在 `await unlisten` 之后。
- **[低] sessionId 变化重置**：文档宣称的重置未实现；补渲染期 ref 对比重置（lastSessionRef，同 lastPathRef 范式）+ 快照 null 时清残留（闭死「旧 listener 清理间隙」微窗口漏值）。
- **[低] store.rs persist/persist_to 的 `#[allow(dead_code)]` 已失效**（ingest.rs 生产调用 + persist 内部调用）——删除。
- **[低] mod.rs re-export 收缩**为 `init`（persist 无该路径调用方，ingest 走 `store::persist`）。
- 范围外记录：useTranscript 订阅段（transcriptSubscribe 前）同款未-await listen 为 T2.2 既有形态，未动；claude 被 kill 无 Stop → runtime 卡 working 属 T6.1 fallback 范畴。

---

## 阶段 3：发送体验（P3）

### T3.1 发送队列 + 乐观 echo + 流式气泡

**状态**：✅

**功能**：chat 发送丝滑化——串行防交错、立即回显、生成中实时预览

**技术方案**：
- `chatSendQueue.ts`（新，EmbeddedTerminal 同级）：
  - per-session 发送串行队列（对齐 orca `native-chat-pty-send-queue.ts`，**据源码修正**：二次入队**等待**前序窗口（freeAt）而非取消其延迟 Enter——取消写法会静默丢首条消息；cancel 为显式中止 = 清 timer + onCancelUnsubmitted 清残留正文 + 退还自身窗口）；空闲时 start 同步执行；空闲后删 Map 条目
  - `EmbeddedTerminal.sendChatMessage` 改走队列；stopChat / 卸载 cancelChatSends
- 乐观 echo（`chatPending.ts`）：
  - 发送时立即在消息列表尾部插入合成 user 气泡（`pending:<id>`）；模块级缓存跨 overlay 重挂载存续
  - prune **按内容匹配而非 id**（真实 turn id 是 claude uuid 不可预知）：归一化文本 + 边界（只认发送点之后的消息，防绑旧 turn）+ occurrence（同文本连发各绑各的）+ assistant 前进兜底清除（替代 orca cancel↔echo 事件耦合）；串行队列下无需 glue 匹配
- 流式气泡（`chatStreaming.ts` + Rust ingest 拼接）：
  - 复活 T1.3 砍掉的 MessageDisplay delta/index 拼接：ingest `apply_preview_delta`（index 0 重置 / 顺序追加 / 重复丢弃 / 跳档重开）写 runtime preview_text，UserPromptSubmit 清残留；agent_id 事件丢弃（subagent 防混）
  - deriveStreamingText 规则对齐 orca `native-chat-streaming.ts`：working 且预览领先于最后 assistant 文本才显示；transcript 追上自然消失；非 working 恒不显示
- 消息合成：useTranscript 返回 `messages` = 真实 + echo + 流式气泡；MessageList 吸附滚动（在底部自动跟随，上滚脱附，滚回恢复）

**依赖**：T2.1

**单测**：vitest 19 例（chatSendQueue 6：等待不取消/cancel/幂等/多 session；chatPending 6：登记/匹配 prune/边界/occurrence/兜底；chatStreaming 7：门槛/包含隐藏/领先显示）；cargo ingest preview 拼接 5 例

**验证记录**（2026-08-26）：tsc / web:lint / vitest / cargo test 111 passed 全绿。审查修复（同日）：流式基准改回 orca「只看末条」语义（回扫上一回合会把短回复的流式整回合抑制）；echo 剪除改双层语义（渲染层 matching 即时隐藏防双份、缓存层 advanced 延迟剪除防 claude TUI 排队窗口误清，被吞 echo 由队列 cancel 显式清除——clearLastPendingSendByText 耦合）；边界缺失时间戳回落防历史重复文本误剪；ChatBanner/messageText/messagesAfterBoundary 收敛；chatSend.ts 上移 EmbeddedTerminal/（PTY 写入域）。dev app 实测（echo 即时上屏 / 流式气泡出现与替换 / 连发不丢 / 滚动脱附 / T2.1 延期项状态即时切换）待人工执行。

---

## 阶段 4：交互卡片（P4）

### T4.1 审批卡 / 提问卡替代「切回终端」

**状态**：✅

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

**实施记录（2026-08-26）**：
- 数据源升级为 **PreToolUse 新 hook**（架构澄清确认）：installer 注册集 7→8 事件
  （无 matcher）；ingest 仅 AskUserQuestion 进状态机（tool_name 剥非字母数字判型，
  容拼写变体）置 Waiting + notification，普通工具调用高频 Drop（working 态已由
  MessageDisplay 推进）。Notification 事件维持不入状态机。
- 按键语义（orca STA-1860 实测修正任务书表述）：AskUserQuestion 是方向键选择器，
  **按标签文本作答会静默答成首选项**——一律用 1 起始序号作答（数字即选中+提交）；
  多选勾完右方向键（`\x1b[C`）进 Submit 页签；自由文本走「Type something」行号
  （选项数+1）+ sanitize 文本 + 回车；多问含未答之问用右方向键跳过。
- 发送链**独立于 T3.1 chatSendQueue**（有意不共用）：选择器应答无清行/回车序列、
  提问卡在场时 composer 已替换无竞争写入；`chatInteractiveSend.ts` 自管 1s/组
  步进 timer（组 0 同步写），per-session 键控 + cancel 幂等 + 新链顶旧链。
- 审批卡动态按钮（「动态建议+回落」确认）：permission_suggestions 在场 → 序号
  按钮组；缺省回落「允许 '1' / 总是允许 '2' / 拒绝 ESC」。提问解析失败（载荷
  漂移）回落审批卡形态（'1'/'2' 即选首/次行，功能语义仍成立）。
- 提问卡全量多问题支持（用户确认取全量而非单问简化）：页签步进 + 单/多选 +
  常驻自由输入行 + 末问统一提交；作答后按内容键本地消隐（notification 要等
  claude 下一事件才清，去重键防卡复活；清空即复位）。
- 换新提问时中止旧应答链余组（防旧链导航键/回车写进新选择器——回车会提交
  新问首选项）；stopChat / 卸载同样 cancel。
- useTranscript 切源：`notification` 直通（runtime 优先 + locate 存活门槛收口），
  `waitingFor`（session ref 来源）移除；「切回终端回答」banner 退役。
- 单测：chatAsk（18）+ chatInteractiveSend（7）+ ingest PreToolUse（3）+ installer
  合并测试更新；全量 cargo test 114 过 / vitest 46 过 / tsc / eslint 清洁。
- 审查修复（缺陷/简洁性/架构三路 review，8 项全修）：已答记忆由内容键改为按
  **notification 实例**为界（HIGH——本仓无 PostToolUse 注册，同工具连续审批
  之间无事件清 notification，内容键（含 toolInput）会把第二次误判已答致卡片
  消隐 + composer 锁死；Rust 每次 Apply 都 emit 新对象，identity 变化即复位）；
  hook 未生效兜底链路恢复「切回终端」降级横幅（Waiting 且无 notification 时）；
  步进链改单 timer 链式推进（单组链不占状态、`cancelled` 死标志消除）；末问
  空答按钮文案与取消动作一致（「跳过」）；ingest 两 waiting 臂抽
  `into_waiting`；已答判定单源 `isAskAnswered`（删 hasAskAnswer 死导出）；
  ESC 常量单源 `APPROVAL_DENY`；`setQuestionActive` 直传。
- 已知边界（排期参考）：`ensure_workspace_hooks` 尚无前端调用点，T6.1 接线前
  已装工作区的 settings.json 不自动补 PreToolUse 注册——存量环境提问卡不生效。
- dev app 实测待办（与 T3.1/T2.1 遗留实测合并）：Bash 权限 → 审批卡允许 → 任务
  继续；AskUserQuestion → 多问题卡作答；作答后卡片消隐、状态回 Working。

---

## 阶段 5：直接启动 + resume（P5）

### T5.1 PTY 直接 spawn claude

**状态**：✅

**功能**：chat 模式下打开终端直接进入 claude 会话（去除 shell 中转），归因钉死在 spawn 时刻

**技术方案**：
- `SpawnOpts` 增字段 `direct_command: Option<String>`（或 startup_command 语义扩展）
- `spawn_fresh` 分支：`direct_command` 在场时 `CommandBuilder::new(claude_bin)` 直接 spawn（env 打标沿用 T1.4；**无 shell_ready barrier**）
- `resolve_claude_bin()`：login shell `which claude` 探测 + `OnceLock` 缓存；失败回落现有 shell 注入路径（记 warn log）
- 前端 `EmbeddedTerminal`：chat 模式（`chat_mode_switch=Y` 且 `startup_code_cli=claude`）时传 direct 启动参数
- 语义变化（已确认接受）：claude 退出即 pane 退出（无 shell 回落）→ 走 exited UI；跑普通命令用附加 pane（现有设计附加 pane 恒裸 shell）

**依赖**：T1.4（env 打标在直接 spawn 路径同样生效）

**实施记录（2026-08-27）**：
- 探测泛化为**按首 token 通用解析**（用户确认方案 A）：新 `pty/cli_bin.rs`
  `resolve_cli_bin(token)`——`$SHELL -l -i -c 'command -v <token>; /usr/bin/env'`
  一次探测同时拿 CLI 绝对路径（首个 `/` 开头行；builtin/alias 返回名字本身
  非路径，天然判失败回落）+ login PATH（`PATH=` 行解析，`/usr/bin/env` 外部
  二进制输出跨 shell 一致——`printf "$PATH"` 在 fish 下按列表空格连接会破坏
  PATH）。缓存**只存成功**（用户确认）：`LazyLock<Mutex<HashMap>>` per token，
  失败下次 spawn 重探（装上 CLI 免重启 app；OnceLock set 不可覆盖会钉死失败）。
  token 合法字符白名单（字母/数字/`_-.`）拒引号防 `-c` 拼串注入面，绝对路径
  形态也拒（生产只传 CLI 名）。
- **PATH harvest 注入 direct 子进程**（用户确认）：GUI app env 缺 nvm/volta 目录，
  npm/nvm 安装的 node-shebang CLI 直接 spawn 会因找不到 node 起不来；注入
  login PATH 后与用户终端行为一致（brew 自包含二进制不依赖，注入无害）。
- `spawn_fresh` direct 分支：整体替换 `CommandBuilder`（与 zsh/bash 包装分支
  同款重建形态），无 barrier；T1.4 env 注入点在全部重建之后，第 5 条 spawn
  路径一处覆盖。**失败回落**：整串顶替 `startup_effective` 走注入分支（包装/
  fast 降级链全继承）；direct 优先级高于 startup_command，成功时不再注入。
  附加参数原样透传（`claude --resume <id>` T5.2 同形零改动）。
- 前端：`usePtySession` args 增 `directCommand: string | null`（attachKey +
  effect deps，热切换重编排不打扰活会话——同 startupCodeCli 语义）；direct
  在场时不发 startupCommand。`EmbeddedTerminal` 传
  `directCommand: chatEnabled ? startupCodeCli : null`（chatEnabled 已含
  isMain + cli≠none + 开关，附加 pane 天然排除）。`reopen(true)` 一次性覆盖
  仅作用 startupCodeCli：非 chat 模式「重开并启动 claude」保持注入语义。
- 单测：cli_bin 5 例（真实 shell 解析外部命令/builtin None/不存在 None/非法
  token 拒绝/纯解析规则）+ local_provider 2 例（`env` 直启冒烟——ring 见
  WE_TERM_PANE 归因标 + 会话自然退出置位 exited；`echo` builtin 回落注入）；
  `#[ignore]` 真 claude e2e 1 例。
- 验证（2026-08-27）：cargo test 121 passed（3 ignored 为手动 e2e）/ vitest
  46 / tsc / eslint 全绿；`claude_e2e_direct_spawn_spool`（16.4s）——direct
  直启 `claude -p` → spool 收 SessionStart+Stop 全链且载荷同一 launch_token。
- 审查修复（2026-08-27，缺陷+简洁性/规范+架构双审，3 项全修）：**[高] 探测
  解析加哨兵隔离**——裸取「首个 `/` 开头行」会把 rc 文件噪声（绝对路径报错
  行）误判为 CLI 路径，且误判被「只缓存成功」钉死 → chat 模式全部 main pane
  spawn 永败直至重启；改为 `echo` 哨兵夹住 `command -v` 输出（rc 在 -c 脚本
  前跑完，时间上不可能插入），哨兵缺失亦判失败回落；解析单测扩 5 场景。
  **[minor] pty/mod.rs 子模块清单补全**（claude_state/shell_ready 前任务遗留，
  按声明序重排 7 项）。**[注释] fish 边界说明**——`command -v` builtin 判定
  仅 POSIX shell 成立（fish 直搜 PATH 返回真路径更优），头注与测试注释双补。
  修后 cargo test 121 复跑全绿。
- dev app 实测待办（与 T5.2/T6.1 合并）：chat 模式开 issue 直进 claude（无
  shell 提示符）、退出走 exited UI、附加 pane 仍裸 shell。

---

### T5.2 退出重开 + `--resume` 恢复

**状态**：✅

**功能**：claude 退出后「重开并启动 claude」支持恢复上次会话上下文

**技术方案**：
- exited 条「重开并启动 claude」→ `claude --resume <last_session_id>`（id 来自 T1.1 快照 store；无记录时裸启动）
- resume 参数注入 T5.1 的 direct_command 路径（`claude --resume <id>` 整串作为 direct 命令）
- 快照 hydrate 覆盖 app 重启场景（T1.1 已落盘）

**依赖**：T5.1、T1.1（快照）

**验证**：chat 模式打开 issue 直接进会话；exit 后重开 resume 恢复上下文

**实施记录（2026-08-27）**：
- **前端 only，Rust 零改动**（direct_command 整串 + `claude_runtime_state` 快照
  查询 + hydrate 跨重启保留 claudeSessionId 均已就绪）。
- 澄清确认三项：①chat 模式「重开」= 全新会话（同时是 resume 失效的逃生口），
  仅「重开并启动 claude」resume；②两模式统一——非 chat 模式有 runtime 记录
  同样 resume（注入路径），实现比按模式分支更简；③exited 条按钮文案不动
  （语义区分靠行为，文案优化留 T6.1 打磨）。
- `usePtySession.reopen` 泛化：`(claude?: boolean)` → `(claudeCommand?: string)`
  （'claude' 或 'claude --resume <id>'）；`startupOverrideRef` 更名
  `claudeOverrideRef`，**路由在 hook 内**——direct 模式（配置 directCommand
  在场）顶替 direct 串走 T5.1 直启，否则顶替 startupCodeCli 走注入（一处
  分流，调用方只给命令串）。
- `EmbeddedTerminal.reopenWithClaude`：点击时 `claudeRuntimeState(sessionId)`
  取 claudeSessionId，有值拼 `claude --resume ${id}`，无记录/查询失败裸
  'claude'（沿用本组件 res.status 消费范式）；id 失效时 claude 启动即退
  自然回落 exited UI。
- 验证（2026-08-27）：tsc / eslint / vitest 46 / cargo 121 回归全绿（Rust
  未动仅回归）。
- 审查修复（2026-08-27，缺陷+简洁性/规范+架构双审）：缺陷审查零发现（resume
  串拼接边界/路由四组合/一次性覆盖时序/异步链闭包/Rust 消费链含 T1.4 围栏
  resume 换代兼容均核验通过）；规范审查 3 项 minor 全修——usePtySession
  startupCodeCli 参数注释残留死标识符 startupOverrideRef 改为 claudeOverrideRef
  + 新路由口径；EmbeddedTerminal startClaude 注释引用已失效的 reopen(true)
  改指 reopenWithClaude；reopenWithClaude 两条失败路径补 console.warn（对齐
  组件 openSettings 惯例，实测失败可区分「无记录」与「查询报错」）。修后
  tsc / eslint 复跑清洁。
- dev app 实测待办（与 T5.1/T6.1 合并）：chat 模式 exit 后「重开并启动 claude」
  恢复上下文、「重开」开新会话、app 重启后 exited 条仍能 resume。

---

## 阶段 6：边界与回退（P6）

### T6.1 回退路径 + 模式热切换 + 全链验证

**状态**：✅（dev app 手动 e2e 清单已交付待执行，见下方实施记录末尾）

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

**实施记录（2026-08-28）**：

- **澄清确认四项**：① installer 加固取「保守保留 + 仅语义比较」——探索发现
  serde_json 未启用 preserve_order 时 `serde_json::Map` 底座同样是 BTreeMap，
  天真的 Value 层重构并不解决键序；启用 feature（indexmap 虽已在树，零新增
  crate）是全局行为换局部 diff 美观，性价比不足，放弃。② 安装触发范围取
  「CLI 集成即装」（startup_code_cli≠none 即装，含非 chat 注入模式）——注入
  模式同样获得 runtime 状态推送 / T5.2 resume / uuid 定位修复，与 T5.2「两
  模式统一」决策一致。③ 按钮切源公式弃 spec 字面「有条目即 runtime 为准」
  （runtime 条目永不删除 + claude 退出无 hook 事件 → 注入模式退出后按钮永久
  卡灰；条目删除又与 T5.2 resume 依赖残留 claudeSessionId 冲突），改为「探测
  真值 + runtime 加速 latch」。④ 热切换提示取「仅设置页文案」。
- **installer 加固**：三层 BTreeMap struct 模型（SettingsModel/HookDefinition/
  HookHandler）删除，改 Value 层手动遍历——只动认识的部分（8 注册事件的自有
  条目剥除/尾插），hooks 非对象 → 整树透传 + warn + 跳过安装（零写零备份）；
  事件值非数组 → 原样保留 + 跳过该事件；definition 非对象透传；语法损坏仍
  空起步 + .bak 兜底（T1.2 语义）。跳过判据改**语义比较**（磁盘 parse 成
  Value ≡ 合并产物，键序/格式/空白无关）——claude 自身改写 settings 后不再
  误写、.bak 不再反复滚动。单测 10 个：7 个语义等价迁移 + 3 新增对抗用例
  （语义损坏保内容 / 仅格式变化零写 / 非对象透传）+ needle 与脚本常量契约
  钉死（规范审查补）。`install_hooks_for_dev_e2e` 手动工具删除（前端接线后
  冗余）；T4.1 已知边界「存量工作区不自动补 PreToolUse 注册」随自动安装消解。
- **hook 自动安装接线**：`usePtySession.attach` 的 spawn 段（reattach 活会话
  命中即返回之后、ptySpawn 之前），`cliSpawn = directCommand != null ||
  startupCodeCli !== 'none'`（attach 收到的已是 override 路由后 effective 值
  ——非 CLI 用户点「重开并启动 claude」同样覆盖）→ `await` 幂等安装，失败
  （typedError 与 invoke reject 两类）仅 warn 不阻塞 spawn。挂点被 override
  路由位置决定：组件层复现条件会漏「chat 关 + cli=none + 重开并启动 claude」
  路径（架构审查确认）。活会话天然不重装（重启会话后生效）；StrictMode 双跑
  幂等无害。
- **useClaudeRunning 切源**：`running = active && (probed || latched)`——
  probed 进程树探测（存活真值，退出方向唯一可靠源）；latch 订阅
  claude-runtime:changed（按 pane 过滤，claudeSessionId 非空事件置位——SessionStart
  即时置灰零滞后），探测 false 清除（纠偏条目残留）；仅实时事件不查快照
  （挂载时陈旧快照会闪错误置灰）。active 转 false 渲染期 lastActiveRef 重置
  （防跨会话残留首帧误报）。
- **设置页文案**：help.chatModeSwitch（zh-CN/en）补「开启后已运行的终端会话
  需重开会话才会直接进入 Claude 并接入状态推送」。
- **回退链路确认**（T2.1 已建成，本任务核验）：runtime 缺席时 transcriptPath
  回落进程树路径、claudeStatus 回落 session ref、Waiting 无 notification 时
  「切回终端」降级横幅——全链 fallback 已内联成立，无需新写。
- **审查修复**（正确性 2 + 简洁性 6 + 规范/架构 3，全部修复）：[中] ensureWorkspaceHooks
  未捕获 invoke rejection（命令未注册/dev 旧二进制）会让 attach 整体 reject
  致 spawn 不发出——`safeAwait(logOnError(...))` 组合兜住两类失败模型；
  [低] probed/latched 跨 active 转换不重置致重开后首帧误报（lastActiveRef
  渲染期重置）；strip 函数 needle 判定一份三写收敛 retain 原地过滤（26→15
  行）；merge 两臂收敛 + install 非对象分支早退消死 clone；cliSpawn 命名
  消 'none' 魔法串两写；managed_definition 注释漂移（字母序非插入序）；
  workspace_fixture 注释失实；usePtySession 接口注释 '' 哨兵漂移订正。
  架构审查零发现（域边界/hook 职责/latch 数据流/域内聚/同类抽查全过）。
- **验证（2026-08-28）**：cargo test 126 passed（2 ignored 为真 claude e2e）/
  cargo fmt / tsc / eslint / vitest 46 全绿。
- **dev app 手动 e2e 清单**（合并 T2.1/T3.1/T4.1/T5.1/T5.2 遗留实测，待人工
  执行）：①chat 模式开 issue 直进 claude 会话（无 shell 提示符）；②chat 发
  消息 → echo 即时上屏 → 流式气泡 → transcript 落地替换；③触发 Bash 权限 →
  chat 内审批卡按钮允许 → 任务继续；AskUserQuestion → 多问题卡作答；④停止 →
  状态即时 idle（无秒级滞后）；⑤claude 退出 → 「重开并启动 claude」resume
  恢复上下文 /「重开」开新会话 / app 重启后仍能 resume；⑥关 chat 开关 → 行为
  完全回现状（overlay 摘除、注入模式轮询兜底）；⑦附加 pane 仍裸 shell；
  ⑧热切换：开开关 → 活会话不生效（文案提示）→ 重开会话生效；⑨隔离：iTerm2
  同目录跑 claude → 不写 spool、监控页不受扰；⑩无感：杀 app 后 claude 继续跑
  hook 不报错。

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

---

## 后记：chat 模式退役（2026-08-28）

> P1–P6 完成次日执行的功能重整。chat 模式（T2.1–T6.1 的展示层）当初即
> direct spawn 方案的**验证载体**——方案验证完毕，chat 体验本身有欠缺且
> 不可能穷举所有展示场景，整体退役；同时把 shell 注入中间层一并删除，
> direct spawn 成为唯一自动执行路径。上文 T1–T6 任务记录保留作为设计
> 演进的存档，其中 chat 专属实现（状态机/预览/通知/交互卡/发送队列/
> 乐观 echo/流式气泡/transcript 订阅）已按下表删除。

**三项变更**：
1. 「启动主终端时自动运行」选项确认不变：`none`（默认，开普通 shell）/
   `claude`（直启）——需求原文「node-默认」系 none 笔误，语义不变零改动。
2. **chat 模式全退役**：`terminal_chat_mode_switch` 配置项（DB 残值惰性无
   影响）、Terminal/Chat 切换按钮、chat overlay 与 NativeChat 全部组件/
   工具链/测试删除。
3. **claude 启动收敛两方式**：手动（配置 none → 裸 shell，用户经工具栏按钮
   或自敲命令启动）+ 自动（配置 claude → direct spawn 直启，不经中间层）；
   CLI 解析失败回落裸 shell（warn log）。T5.2 resume 的 override 路由随注入
   删除简化为**恒走 direct**（`effectiveDirect = override ?? directCommand`，
   含配置 none 的「重开并启动 claude」），usePtySession 整体去掉
   startupCodeCli 参数。

**删除清单**：
- 前端：`NativeChat/` 整目录（12 源 + 3 测试）、`useClaudeRuntime.ts`、
  `chatSend.ts`、`chatSendQueue.ts`、`chatInteractiveSend.ts`（+2 测试）、
  TerminalView/EmbeddedTerminal 的 chat 片段、chatModeSwitch 配置链
  （appConfig/设置页/i18n×2）。
- Rust 注入中间层：`pty/shell_ready.rs` 整文件（975 行——zsh 包装分支仅服务
  注入，普通 shell spawn 从不触碰，无行为回归）、local_provider 注入分支/
  barrier 接线/shell_basename/user_zdotdir_for_passthrough、
  `SpawnOpts.startup_command`、session.rs 的 barrier 参数/字段/写入门。
- Rust claude_runtime 裁剪：ingest 状态机收到「fence → SessionStart 绑定 →
  其余 Drop」；HookPayload 删 cwd/tool_name/tool_input/message/prompt/
  delta/index/final 字段；`ClaudeRuntimeStatus`/`ClaudeNotification` 类型删；
  state/payload 收缩为 launch_token/claude_session_id/transcript_path/
  updated_at（旧快照 serde 容忍多余键，零迁移，补兼容单测钉死）。
- **transcript 域整删**（chat 专用死代码，监控页走 claude_sessions 域零共享）：
  `transcript/` 5 文件、`shared/state/transcript.rs`、
  transcript_read/subscribe/unsubscribe 命令、Transcript* 类型、
  `EVENT_TRANSCRIPT_CHANGED`、`pty_claude_session` 命令 +
  claude_state 的 claude_session_ref/find_claude_under_shell/cwd_usable +
  `ClaudeSessionRef` 类型。

**保留地基**（后续自动化的支撑）：hook 脚本 + spool 通道 + watcher、env 三标
归因、installer 自动安装（**注册集 8→1 事件仅 SessionStart**，另加
RETIRED_EVENTS 退休清理——已装工作区升级时剥除旧 7 事件自有条目，防 hook
持续写 spool 噪声）、SessionStart 绑定 + 快照 hydrate + `claude_runtime_state`
查询（resume）、useClaudeRunning 的 runtime latch（按钮置灰加速）、「启动
claude」/「重开」/「重开并启动 claude」UI。

**验证（2026-08-28）**：cargo test 77 passed（1 ignored 为 direct 真 claude
e2e，断言已适配单事件形态）/ cargo fmt / tsc / eslint 全绿；前端测试文件
清零（原 46 个全属 chat），vitest 配 `passWithNoTests`。dev app 手动验证：
配置 claude 开 issue 直进会话、退出「重开并启动 claude」resume、配置 none
裸 shell + 按钮手动启动、已装工作区升级后 settings.json 仅剩 SessionStart
自有条目。

**三轮审查**（正确性 + 简洁性 + 规范/架构，全部修复）：
- 正确性零缺陷（悬空引用/行为回归/边界/兼容性逐一核验通过）。
- 简洁性 14 项全修：bindings 重生成收敛（`verify:bindings` 门禁）、注释漂移
  簇（barrier/注入路由/transcript 死指针/锚点旧形态/字号 13→12）、结构收敛
  （Decision 双标志恒 true 收敛为 `Apply { state }`、ingest 双重 match、
  HookPayload.source 死字段、store 重复测试、HOOK_EVENTS matcher 死泛化、
  uuid 断言提 helper、watch 同义反复测试抽 `effective_offset` 纯函数）、
  i18n startupCodeCli 文案改直启语义并恢复渲染。
- 规范/架构零关键发现；3 项全修：decodeStartupCodeCli 不可达 `??`、
  state.rs 锚点注释漂移、**幽灵 store 修复**（Exit 清理原走 app.manage 的
  恒空实例实际回收零会话——改为 `pty::shutdown_all_provider()` 走 provider
  真源，删除幽灵 manage，退出时真正回收全部 PTY 子进程）。
- 范围外记录：配置 none 手动启动路径不装 hooks（T6.1 既有设计，resume 回落
  裸 claude 兜底）。
