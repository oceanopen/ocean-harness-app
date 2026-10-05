# Bot 体验打磨与绑定闭环——方案定稿与任务清单

> 状态：方案定稿（技术决策 D1–D8 已拍板；任务待实施）
> 范围：bot 会话显示链路（IM 来源样式区分 / 人设回合级注入）、IM 卡片化绑定（工作空间 / 任务）、
> 任务工具栏 IM 机器人模块、思考内容滚动摘要、表结构与配置项全局梳理
> 前序基线：`docs/agent_dev_acp_unified_session.md`（T0–T3.5 已全部交付——ACP 统一会话、审批卡、
> 表单卡、流式呈现、doctor 徽标）
> 参照：`~/Project/Gold-Band`（回合级人设 / 显示-发送文本分离的行为语义参照，AGPL-3.0 禁代码移植）
>
> **状态标记**：⬜ 待开始 | 🔲 进行中 | ✅ 已完成
>
> **状态回写规则（内置，无需人工提醒）**：任务实现完成后，执行方（开发 Agent）在总结阶段
> 直接把对应任务状态改为 ✅ 并补「实施定稿」段落（只记实施终态与相对方案的偏离，作为
> 当前事实；不写日期、不写「何时补充/修订了什么」的演变叙述——变更历史归 git）——回写
> 状态但不主动 git commit，提交动作必须经用户明确确认。实施中若需偏离方案，先修订本文
> 对应设计段落再动代码。

---

## 1. 背景与动机

### 1.1 前序基线与本次定位

ACP 统一会话方案（`agent_dev_acp_unified_session.md`）T0.0–T3.5 已全部交付：bot 经 sidecar
ACP 会话域与桌面共享同一 claude 会话，审批 / 表单企微卡、流式正文、doctor 健康徽标可用。
基本功能体验 OK，本方案进入**体验打磨期**：补齐六项体验缺口 + 一次表结构 / 配置项全局梳理。

### 1.2 现状关键事实（改造依据）

| # | 事实 | 代码位置 |
|---|---|---|
| 1 | `t_im_bots.system_prompt` / `model` 列已有：headless 经 `ComposeSystemPrompt` → `--append-system-prompt` / `--model` 消费；**ACP 驱动完全忽略两者**（T2.1 偏离①拍板「bot prompt 存废另行决策」，本方案兑现该决策） | `server/internal/bot/prompt.go:11`、`driver_claude.go:53-58`、`driver_acp.go:45-46` |
| 2 | `<user_message>` 围栏在 bot 编排器 `BuildTurnPrompt` 组装，**模型可见**（IM 来源区分 + `</` 中和防逃逸 + 引用围栏 + 附件 manifest 的注入防御体系）；panel 的 user 条目原样直显 `entry.text`，标签因此裸露在桌面视图 | `server/internal/bot/prompt.go:34-69`、`acpsession/view.go beginTurn`、`AcpSessionView/MessageList.tsx:56-61` |
| 3 | `agentThought` 帧携带**本回合累计全量文本**（视图按 (回合,kind) 聚合，与 agentMessage 同构）；IM 侧只发静态「💭 思考中…」状态行（T3.4 拍板的轻量形态，本方案升级为滚动摘要） | `acpsession/view.go:373-387`、`driver_acp.go:235-239` |
| 4 | 指令步可直接出卡：`applyIssueCommand` 持有的 `ReplyStream` 即企微回复流（实现 `CardReplyStream`），`SendCard(spec, content, final=true)` 一帧带卡收口；卡片点击走交互快车道 → `TryHandleInteraction` 纯决策 → 置灰 + 终帧。**回合外主动推卡（`CardPusher`）仍是 TODO 未实现**——绑定卡全部有入站锚（指令/意图/门禁/探针），不需要它 | `bot/orchestrator.go:188`、`bot/card.go:44-61`、`wecom/channel.go:29-32` |
| 5 | 绑定模型：workspace 绑定在 **bot 行**（`t_im_bots.workspace_id`，Start 时解析进 cfg）；issue 绑定在**会话行**（`t_im_bot_conversations.bound_issue_id`，T2.3 拍板 per-conversation，每回合现读） | `bot/supervisor.go:188-214`、`bot/driver_route.go:72-124` |
| 6 | HTTP 面无 `boundIssueId` 透出、无绑定端点；update 是整表单显式列 `Updates` map（新列不进 map 即天然不被抽屉保存误伤） | `service/im_bot.go:95-128`、`controller/im_bot.go` |
| 7 | MCP 面无 workspace/issue 列表工具；bot 域直查 DO 层有先例（`#issue` 的 `resolveIssueTarget`：uuid 短前缀 + 标题子串双通道）——卡片列表数据源走直查，不绕 MCP | `mcpservers/mcp_tool/mcp_ocean_harness_tools.go`、`bot/issue_command.go:73-99` |
| 8 | panel 无 bot→panel 事件通道（唯一 SSE 是 acpSession；其余为 Rust 单源 Tauri 事件）；工具栏模块实时性沿用「mutation invalidate + 手动刷新」范式（子任务面板同款） | `state/README.md:44-47`、`IssueSubTaskPanel.tsx:39-41` |
| 9 | 「归档」语义 = stateCode 流转 DONE/CANCELLED（无独立 archived 列）；「任务列表（非删除非归档）」过滤 = `state_code ∉ 终态集`，与前端 `isDevIssue` 同判定语义 | `service/issue_workspace_archive.go`、`state/devWorkbench/derive.ts:10-15` |

### 1.3 六项体验诉求

1. **思考内容上 IM**：静态「思考中」文案 → 滚动展示思考本体；
2. **三层配置审视**：bot / workspace / issue 配置查缺补漏；
3. **bot 提示词启用**：ACP 路径消费人设，仅 IM 来源回合生效，应用端回合不限定；
4. **`<user_message>` 标签退役（显示侧）**：panel 以样式区分 IM 来源，不再裸显标签；
5. **卡片化绑定**：IM 内说「工作空间」/「任务列表」→ 单选卡 → 选中即绑定；未绑定时的引导自动附卡；
6. **任务工具栏 IM 模块**：工作台右侧工具条展示本工作空间 bot 列表，绑定高亮 / 切换 / 补绑，与 IM 侧绑定形成双向闭环。

## 2. 调研结论（定稿依据）

- **ACP `session/prompt` 无 system prompt 通道**（官方 schema：`PromptRequest` 仅
  sessionId / prompt / _meta）——人设只能走回合级注入或会话级私有约定。
- **Gold-Band 双通道先例**：① 回合级 `UserPromptRole`——人设随单条用户消息以
  「以下是用户指定的角色定义…」模板包装注入（`prompts/zh-CN/runtime/user_role_message.md`），
  正是「仅特定回合生效」形态；② 会话级 `_meta.systemPrompt.append` 私有约定（session/new 注入，
  仅支持该约定的 agent 生效）——会话与桌面共享时会波及桌面回合，与「应用端不限定」冲突。
- **显示 / 发送文本分离先例**：Gold-Band `ConversationPromptInput.display_text`（UI 显示原文）
  与包装后发送文本分离，`<hidden>` 块由前端解析为折叠 UI；Claude Code 官方 `<system-reminder>`
  同为「标签包裹 + 前端解析还原」范式。本方案 D2 同构。
- **开源 bot 人设四模式**：回合前缀注入（OpenClaw envelope）/ 会话级 system prompt（SDK
  append / `_meta` 约定）/ CLAUDE.md 项目级（官方 Slack，人设跟 workspace 走不跟 bot 走）/
  per-origin 会话分离（opencode Slack，原文透传零标注）。「同一会话多入口共享且仅 IM 生效」
  → 回合级注入是主流解（D3）。
- **Gold-Band IM 侧不展示 thinking、不接受自由文本**（纯通知 + 卡片回执通道）——思考 IM 呈现
  无先例，属自主设计（D4 轻量滚动摘要）。

## 3. 目标架构总览

```
绑定模型：保持会话级（T2.3 现状，零表结构变更）——原「上移 bot 级」方案否决（D1）

阶段 1（T1.1–T1.3）：会话显示链路与人设注入（诉求 3+4）
  acpsession 条目元数据（source + display）→ SSE → TS 镜像 → panel IM 徽标样式渲染
  driver_acp 回合级 <im_context> 注入（SSOT = ComposeSystemPrompt 产物）

阶段 2（T2.1–T2.2）：IM 卡片化绑定（诉求 5）
  wsbind / taskbind 卡（TaskID 无状态自包含 + 列表指纹防错绑）
  触发链：#workspace / #issue 裸指令 · 整条消息意图匹配 · workspace 门禁附卡 · ACP 未绑探针附卡

阶段 3（T3.1–T3.3）：工具栏与配置完善（诉求 6+2）
  boundIssueId 透出 + bindIssue 端点 → 工作台工具栏 IM 模块（高亮/切换/解绑/补绑）
  ImBotDrawer 人设与模型作用范围文案

阶段 4（T4.1）：思考滚动摘要（诉求 1）
  agentThought 差分 → 「💭 + 尾部摘要」状态行覆写
```

每阶段独立可交付：阶段 1、4 相互独立；阶段 2、3 相互独立，均直接消费会话级绑定现状承载，
无表结构前置任务。

## 4. 技术决策记录（已定稿）

| # | 决策 | 理由 |
|---|---|---|
| D1 | **绑定保持会话级（T2.3 现状；原「上移 bot 级」方案否决）** | bot 是多会话载体（私聊 + 多群聊并存），会话才是绑定与并行的自然单元：私聊绑任务 A、群聊绑任务 B → 不同 ACP 会话进程完全并行。bot 级绑定会把全部会话耦合到单一任务，失去 bot 多会话服务的意义；工具栏 / 卡片需求在会话级上重新表达（T3.1/T3.2），而非上移绑定粒度迁就 |
| D2 | **围栏保留模型侧，显示侧元数据分离** | `<user_message>` 围栏是注入防御 + 人设作用域的载体（模型必须可见来源），不能删；panel 裸显标签是显示层缺陷——照 Gold-Band display_text / 官方 system-reminder 范式，条目带 source + display 元数据，前端按样式渲染 |
| D3 | **回合级人设注入（仅 ACP 路径）** | ACP 无 per-prompt system prompt 通道（§2）；共享会话下「仅 IM 回合生效」的唯一主流解；`ComposeSystemPrompt` 产物（人设 + IM 守则 + 内容边界）为 SSOT，headless 继续走 `--append-system-prompt`，ACP 包成 `<im_context>` 块随 bot 回合携带，桌面回合零影响 |
| D4 | **思考状态行滚动摘要** | `agentThought` 帧已是累计全量，差分取尾部 ~72 字进状态行覆写；正文 / 工具行到来自然让位；泵 / 编排器零改动；完整流式会刷屏且被终帧覆写，信息价值低 |
| D5 | **绑定卡复用现有 interaction 管线** | 指令步 `SendCard` 终帧带卡 + 点击走交互快车道 → `TryHandleInteraction` → 置灰 + 终帧，全部现成；新 TaskID 标记族 `wsbind-<指纹>` / `taskbind-<指纹>`（候选列表哈希指纹，点击时重查列表比对指纹，列表已变则提示重取卡——无状态自包含且防索引错绑）；taskbind 落库点击来源会话行（`ConversationStore.SaveBoundIssueID` 现成，每回合现读免重启），wsbind 落库 bot 行 workspace_id 并经 `ApplyBotAsync` 延迟热重载（终帧发出后再 Stop→Start，避开渠道回调自停时序） |
| D6 | **意图匹配 = 整条消息模式匹配** | 「工作空间」「工作空间列表」「帮我列下工作空间列表」「任务列表」「看下任务列表」「切换任务」等整条消息仅表达列出 / 切换意图才触发（礼貌词 + 动词 + 核心名词正则全匹配）；**不做子串包含**——正常任务消息提到「工作空间」不误触；宁漏勿误，未命中一律按正常消息出回合 |
| D7 | **工具栏绑定走独立会话级端点** | `POST /api/imBot/bindIssue {botId, conversationKey, issueId}`（issueId 空 = 解绑，作用于具体会话行）；workspace 关联复用现有 update 全表单链路（前端持全量模型）；`bound_issue_id` 不进 update 的 Updates map，抽屉保存天然不误伤；绑定变更每回合现读，无需热重载 |
| D8 | **headless 链路零改动** | system prompt（`--append-system-prompt`）/ 围栏 / 静态思考线维持现状；两引擎 IM 呈现基线差异按设计接受（headless 无 ACP 视图，显示链路改造对其无感） |

## 5. 表结构与配置项梳理结论

### 5.1 表结构

| 表 | 变更 | 说明 |
|---|---|---|
| `t_im_bot_conversations.bound_issue_id` | 保留（现状承载） | 会话级绑定锚（D1）：(bot, 会话) 粒度，空 = 未绑定；issue 删除级联清列 T2.3 已交付 |
| `t_im_bot_conversations.claude_session_id` | 保留 | headless `--resume` 锚，ACP 不写（与绑定锚正交） |
| `t_im_bot_conversations.seen_message_ids` | 保留 | 幂等滚窗，与绑定无关 |
| `t_im_bots.model` / `system_prompt` | 保留 + 语义标注 | model 仅终端模式生效（ACP 会话模型跟随桌面端）；system_prompt 两引擎均生效（D3 落地后） |
| `t_workspaces` / `t_issues` 的 `launch_settings` | 保留 | 无冗余；权限 / 模式体系（P1/P6）不受本方案影响 |
| `t_issue_acp_sessions` | 保留 | issue↔ACP 会话锚，与 bot 绑定正交 |

本方案零表结构变更（会话级绑定锚即现状列）。

### 5.2 配置项

- **ImBotDrawer**：人设 helperText 更新为作用范围说明（仅 IM 来源回合生效；ACP 随每条 IM 消息注入、终端模式注入系统提示词）；模型 helperText 标注仅终端模式生效（T3.3）。
- **WorkspaceDrawer / ProjectIssueDrawer**：无变更（launch_settings 体系已闭环）。
- 无新增 bot 级配置项——人设既有列即承载「bot 特化」诉求，不引入工具白名单类回潮配置。

## 6. 风险与开放问题

1. **每回合 token 开销**：ACP bot 回合恒带 `<im_context>`（固定守则 ~300 字 + 人设 ≤8000 字）。
   共享会话下「仅 IM 生效」的必要代价（Gold-Band UserPromptRole 同款）；人设留空时仅固定段。
2. **会话级绑定的并行语义**：不同会话绑不同 issue = 不同 ACP 会话进程完全并行；同一会话内
   消息串行（每会话 FIFO 队列，IM 有序语义）；不同会话绑同一 issue = 共享 ACP 会话回合
   FIFO 排队（「共享会话」设计本意，与桌面 / bot 双入口同理）——绑同一任务即为共同推进，
   排队不是耦合。工具栏会话明细无昵称存储，以「私聊 / 群聊 + 最近活跃时间 + key 短码」呈现。
3. **意图匹配边界**：正则模式匹配覆盖常见口语变体，非 NLU；「任务」裸词不触发（须「任务列表」
   或动词 + 任务），防误触。
4. **ApplyBotAsync 延迟热重载时序**：workspace 卡点击落库后延迟 ~1s 再 Stop→Start，窗口内旧 cfg
   的在途回合仍用旧 workspace（可接受；落库即生效的是下下回合）。需测试覆盖「点击 → 置灰 →
   终帧 → 重启」全序。
5. **任务列表卡上限**：非终态 issue 超 20 截断（企微 vote 选项上限），同帧文本附完整标题列表，
   超出部分提示 `#issue <关键词>` 精确绑定。
6. **企微选项文案截断**：vote 选项 11 字（协议限制），完整标题在同帧 content 文本；卡面标题截断
   可读性靠同帧文本补足。
7. **panel 显示兼容**：`Display` 元数据缺失（旧 sidecar + 新前端混跑窗口）回落原样直显 Text，
   不做标签解析兜底（混跑窗口极短，历史条目随 sidecar 重启即消失）。

---

## 7. 任务清单（进度 SSOT）

> 任务粒度：模块 + 功能 + 技术方案，一个任务 ≈ 一次可独立验收的交付。
> 执行顺序：T1.1 → T1.2 → T1.3 → T2.1 → T2.2 → T3.1 → T3.2 → T3.3 → T4.1
> （阶段 1 与阶段 4 可并行；阶段 2、3 相互独立，均直接消费会话级绑定现状，无表结构前置）。

### 阶段 1：会话显示链路与人设注入

**设计要点**：
- 数据流：`InboundMessage` → orchestrator 组 `TurnRequest{Prompt(围栏全文), SystemPrompt
  (ComposeSystemPrompt 产物), Display(展示元数据)}` → driver_acp 附 `<im_context>` 块 +
  `PromptQueued(text, meta)` → acpsession 条目存 `{Text=全文, Source, Display}` → SSE entry
  帧 → 前端按 Source/Display 样式渲染。
- **`<im_context>` 组装**（driver_acp）：`prompt + "\n\n<im_context>\n" +
  neutralizeTagClose(req.SystemPrompt) + "\n</im_context>"`——内容经 `</` 中和防逃逸（人设是
  用户配置文本，可信但不排除含闭合序列）；SystemPrompt 为空理论不达（固定段恒非空），防御性
  判空跳过。
- **Display 结构**（acpsession）：`EntryDisplay{Text(正文原文), Quote *{Author, Text,
  Truncated}, Files []{Name, Path}}`；nil = 无展示元数据，前端回落 Text 原样。
- headless 驱动不消费 Display（字段忽略），`--append-system-prompt` 路径不变（D8）。

#### T1.1 acpsession 条目元数据扩展

**状态**：✅

**功能**：会话条目携带消息来源与展示元数据（后端显示链路基座）

**技术方案**：
- `acpsession` 新类型：`PromptMeta{Source string, Display *EntryDisplay}`（Source 取值
  `"panel" | "bot"`，SSOT 常量同 `PendingSource*` 惯例）；`ConversationEntry` 增
  `Source string` 与 `Display *EntryDisplay`（json omitempty，SSE entry 帧与快照自动携带）
- `Manager.Prompt(issueID, text)`：panel 语义固定，内部 `meta{Source:"panel"}`——HTTP 面
  零改动；`Manager.PromptQueued(ctx, issueID, text, meta PromptMeta)` 扩参（bot 路径）
- `view.beginTurn(text, meta)`：Text 存全文（围栏 + im_context 原样），Source / Display 落条目
- bot 域 `acpSessions` 接口（`driver_acp.go:21`）同步扩参；测试 fake 同步

**依赖**：无

**验收**：acpsession 单测（beginTurn 元数据落条目 / 帧序列化携带 / 空 Display 兼容）；
`go test ./internal/acpsession/... -race` 通过

**实施定稿**：按方案落地，零偏离；补充三点终态事实——① 来源常量新设
`EntrySourcePanel/EntrySourceBot`（取值与 `PendingSource*` 一致但语义独立：本来源随
user 条目进 SSE 帧与快照供前端消费，`PendingSource*` 仍为不进 client DTO 的服务端内部
追踪字段，两组各自维护）；② bot 驱动本任务即随受理传 `Source: bot`（Display 留待
T1.2 从 `TurnRequest.Display` 接入），`Manager.Prompt` 内部固定 `Source: panel`，
HTTP 面 / service / controller 零改动；③ 测试新增 `TestViewBeginTurnPromptMeta`
（元数据落条目 / entry 帧与快照携带 / panel 零 Display 与全零 meta 的 wire omitempty
兼容），排队主链路测试尾部补双入口来源装配断言（首轮 tick 条目落在两 user 条目之间，
按文本定位断言），fakeSessions 增 `promptMetas` 调用留痕备 T1.2 透传断言。

#### T1.2 ACP 回合人设注入与展示元数据传递

**状态**：✅

**功能**：bot 人设在 ACP 路径生效（仅 IM 来源回合），panel 展示元数据透传

**技术方案**：
- `TurnRequest` 增 `Display *acpsession.EntryDisplay`；orchestrator `runTurn` 组装
  `buildTurnDisplay(msg)`（正文 trim 原文 / Quote / Files 映射，纯函数落 `prompt.go`）
- `driver_acp.RunTurn`：prompt 尾附 `<im_context>` 块（见阶段 1 设计要点）；调用
  `PromptQueued(ctx, issueID, full, PromptMeta{Source:"bot", Display: req.Display})`
- headless `driver_claude.go` 零改动（SystemPrompt 继续走 argv，Display 忽略）

**依赖**：T1.1

**验收**：`driver_acp_test` 新增用例（im_context 附带与格式 / 人设含 `</` 中和 / Display 透传 /
SystemPrompt 空跳过）；bot 全量测试 `-race` 通过

**实施定稿**：按方案落地，零偏离；补充三点终态事实——① 人设块组装提炼为具名纯函数
`acpTurnPrompt(req)`（driver_acp.go；空判定用 `TrimSpace`，对齐 driver_claude 的 argv 门槛
语义），受理文本 = 围栏全文 + `<im_context>` 块，SystemPrompt 空防御性原文受理；②
`buildTurnDisplay` 在 `#issue` 绑定+首回合步改写正文之后与 `BuildTurnPrompt` 同点组装
（显示跟随生效正文），bot 路径恒返回非 nil 全新对象——纯文本消息若 Display 为 nil，panel
（T1.3）会回落裸显围栏全文，与诉求 4 相悖；空正文不占位（占位文案仅模型侧围栏）；③ 测试
新增 `TestAcpDriverImContextInjection`（附带与格式 / 人设含 `</` 中和 / 空则跳过三子用例）、
`TestAcpDriverPromptMetaDisplay`（Display 原指针随 PromptMeta 透传，Source 恒 bot）、
`TestBuildTurnDisplay`（纯文本 / 引用附件映射 / 空正文仅有引用不占位），fakeSessions 增
`metas()` 访问器；存量 driver_acp 用例均不传 SystemPrompt，空跳过路径使其 prompt 断言
零扰动。

#### T1.3 panel 会话视图 IM 来源样式渲染

**状态**：⬜

**功能**：桌面 ACP 视图以样式区分 IM 来源消息，不再裸显围栏标签

**技术方案**：
- TS 类型镜像：`services/AcpSessionService.ts` 的 ConversationEntry 增 `source?` /
  `display?{text, quote?{author,text,truncated}, files?{name,path}[]}`（reducer 的 entry
  覆写式 upsert 整对象替换，新字段天然透传，归约层零改动）
- `MessageList.tsx` user 分支：`source==='bot' && display` → IM 徽标（企微标识 + 「IM」
  caption）+ 气泡正文取 `display.text` + 引用块（作者 + 摘要，弱化样式）+ 附件 chips（名称，
  tooltip 路径）；否则维持现状直显（兼容窗口，风险 §6.7）
- 样式对齐既有范式：user 气泡右对齐不变，IM 徽标与 agentThought 淡化同级视觉语言

**依赖**：T1.1

**验收**：`pnpm web:build`（tsc 类型检查）+ `pnpm web:lint` 通过；IM 消息在 panel 呈现徽标 +
正文 + 引用块 + 附件，无 XML 标签裸露

---

### 阶段 2：IM 卡片化绑定

**设计要点**：
- **卡构造**（`bot/binding_card.go`）：workspace 卡 = 全部 workspace 列表（名称选项，单选）；
  任务卡 = bot workspace 域内非终态 issue（`state_code ∉ 终态集`，排序取前 20，同帧 content
  附完整标题列表）。选项文案企微截 11 字，完整信息在同帧文本。
- **TaskID 指纹防错绑**：`wsbind-<hash8>` / `taskbind-<hash8>`，hash8 = 有序目标 id 串的
  8 位十六进制指纹（fnv 或前 8 位 hex 摘要）。点击时经 `Interaction.DeliveryID` 识别 → 重查
  当前列表 → 指纹比对：一致则按 `ActionIndex`/`Selections` 下标解析目标并绑定；不一致回
  「列表已变化，请重新发送指令获取新卡片」。无内存注册表，跨重启 / 迟到点击自然降级（与
  perm/elicit 卡同哲学）。
- **点击消费**（driverRoute `TryHandleInteraction` 扩展，按前缀分流）：
  - `wsbind-`：重查 workspace 列表 + 指纹 → 落库 `t_im_bots.workspace_id` → 置灰（原卡全量
    spec 由重查列表重建，指纹一致保证字段一致）+ 终帧「✅ 已关联工作空间「名」」→
    `ApplyBotAsync(botID)` 延迟热重载（supervisor 注入窄接口，goroutine + ~1s 延迟，确保终帧
    先发；风险 §6.4）
  - `taskbind-`：重查非终态 issue 列表 + 指纹 → 校验归属 → 落库点击来源会话行
    （`ConversationStore.SaveBoundIssueID` 现成，点击消息自带 ConversationKey）→ 置灰 +
    终帧「✅ 已绑定任务「名」，直接发消息即可下达任务」（绑定每回合现读，无需重载）
  - 目标已删 / 跨 workspace / 下标越界：提示文案不置灰不落库
- **触发链**（orchestrator `runTurn` 固定时序，插在 workspace 门禁前后按需）：
  1. `#workspace` 裸指令 或 workspace 意图命中 → workspace 卡终帧（**门禁之前**——未绑
     workspace 的 bot 也要能出这张卡）；
  2. workspace 门禁（`WorkspaceDir` 空）→ 指引文本 + workspace 卡同帧（替代纯文本）；
  3. `#issue` 裸指令 → 任务列表卡（原用法文案并入卡描述 / 同帧文本）；`#issue <kw>` 多命中 →
     候选卡（替代 top5 文本）；唯一命中 / 解绑语义不变（会话级，现状粒度）；
  4. 任务意图命中（「任务列表」「看下任务列表」「切换任务」等）→ 任务卡；
  5. ACP 未绑探针：driverRoute 暴露 `ProbeTarget(cfg, conversationKey) (acp bool,
     boundIssueID string, err error)`（resolve 内部重构复用），orchestrator 在组 prompt 前
     类型断言调用——ACP 且未绑 → 任务卡 + 终帧（driver 内未绑定报错保留兜底）；
  6. 意图匹配 `matchBindingIntent(text)`：整条消息正则全匹配（礼貌词 `(请|麻烦|帮我|给我)` +
     动词 `(列出?一下?|看一?下?|查看|显示|切换|选择|切到)` + 核心词 `(工作空间|任务列表)`
     （裸「任务」不触发）+ 可选后缀 `(列表|卡片?|吧)` + 尾部标点），宁漏勿误（D6）。
- 终态集 SSOT：`state_code` 终态判定（DONE / CANCELLED）在 bot 域定义常量集，与前端
  `isDevIssue` 语义对齐（§1.2 事实 9）。

#### T2.1 绑定卡构造与点击消费

**状态**：⬜

**功能**：wsbind / taskbind 卡片族——构造、无状态 TaskID 指纹、点击落库与置灰

**技术方案**：
- 新增 `server/internal/bot/binding_card.go`：卡构造纯函数（workspace 列表卡 / issue 候选卡 /
  置灰重建共用同构造 SSOT，对齐 `permission_card.go` 惯例）+ 指纹计算与校验
- `driver_route.go`：`TryHandleInteraction` 按 TaskID 前缀分流新增两支（决策链：指纹比对 →
  目标校验 → 落库 → 返回置灰 spec + 文案）；taskbind 落库走 `ConversationStore.SaveBoundIssueID`
  （(bot, conversationKey) 粒度），wsbind 落库 bot 行 workspace_id（`query.Use` 直查更新，
  对齐 `resolveIssueTarget` 直查 DO 惯例）
- `supervisor.go`：实现 `ApplyBotAsync(botID)`（goroutine + 延迟 ApplyBot），经构造器注入
  driverRoute 第四依赖（窄接口，nil = 跳过热重载仅落库）
- wecom 适配器零改动（vote 卡复用；`wecom/card.go` 上限校验已覆盖）

**依赖**：无

**验收**：`binding_card_test` 覆盖指纹往返 / 列表变化拦截 / spec 构造；`driver_route_test` 覆盖
点击决策矩阵（命中 / 指纹不符 / 目标已删 / 越界 / ApplyBotAsync 注入缺失降级）；全量 `-race` 通过

#### T2.2 绑定卡触发链接线

**状态**：⬜

**功能**：指令 / 意图 / 门禁 / 探针四类入口把绑定卡送到用户手上

**技术方案**：
- `issue_command.go` 扩展或新增 `bot/commands.go`：`#workspace` 解析（裸 = 出卡；无参数子
  指令）、`#issue` 裸指令改出卡、多命中改候选卡、意图匹配函数（正则表 + 用例表驱动）
- `orchestrator.runTurn` 时序插步（见阶段 2 设计要点 1–5，全部「指令步」形态：终帧收口、
  不入队、不占并发槽、幂等前置已由受理层保证）；指令步出卡经 `CardReplyStream` 类型断言，
  无卡能力回落纯文本（渠道中立）
- `driver_route.go` 暴露 `ProbeTarget`（resolve 重构为内部函数 + 公开探针；绑定读会话行即
  现状语义）；orchestrator 经 `routeProber` 接口类型断言消费（nil 跳过——headless 路由不实现）
- 任务列表查询：bot 域直查 DO（`workspace_id = cfg.WorkspaceID AND state_code NOT IN 终态集`，
  sort_order 排序 cap 20）

**依赖**：T2.1

**验收**：`orchestrator_test` 覆盖四入口出卡 / 意图命中与不命中表 / 门禁附卡 / 探针出卡不出回合；
`commands` 解析与意图正则用例表；全量 `-race` 通过

---

### 阶段 3：工具栏与配置完善

#### T3.1 会话级绑定 HTTP 面

**状态**：⬜

**功能**：bot 会话列表透出 + 会话级绑定 / 解绑端点（工具栏数据链）

**技术方案**：
- `dal/types/im_bot.go`：新增 `ImBotConversationData{ConversationKey, ChatType（私聊/群聊，
  key 前缀派生）, BoundIssueId, BoundIssueName, LastMessageAt}`；
  `ImBotBindIssueRequest{BotId int, ConversationKey, IssueId string}`
- `service/im_bot.go`：`GetConversations(botID)`（会话列表 + 绑定 issue 名称批量 join，对齐
  workspaceNames 惯例）；`BindConversationIssue`（issueId 空 = 解绑；非空校验 issue 存在且
  `issue.workspace_id == bot.workspace_id`，bot 无 workspace 报「请先为机器人选择工作空间」；
  作用于 (bot, conversationKey) 行，行不存在即拒——绑定以会话已开启为前提；不触发
  applyRuntime——绑定每回合现读）
- `controller/im_bot.go` + `router.go`：注册 `POST /api/imBot/getConversations` /
  `POST /api/imBot/bindIssue`
- `packages/web/src/services/ImBotService.ts`：模型字段 + 请求类型同步

**依赖**：无

**验收**：service 层单测（会话列表装配 / 绑定 / 解绑 / 归属校验 / 无 workspace 拒绝 / 会话行
不存在拒绝）；`go vet` + 全量测试通过

#### T3.2 任务工具栏 IM 机器人模块（bot 聚合 + 会话明细）

**状态**：⬜

**功能**：工作台右侧工具条 IM 机器人模块——bot 聚合态 + 会话明细绑定管理（桌面侧闭环入口）

**技术方案**：
- `WorkbenchTools/toolRegistry.tsx` 注册 `{id:'imBots', title:'IM 机器人', icon: SmartToy,
  exclusive:true}`；新组件目录 `WorkbenchTools/ImBotPanel/`（参照 `IssueSubTaskPanel` 范式：
  PanelToolbar + 列表 / 空态 / 错误分支 + 手动刷新 invalidate）
- 数据：`useImBots`（既有缓存）按 `ctx.issue.workspaceId` 过滤 bot 列表；每 bot 经 T3.1
  会话列表查询拉明细，前端聚合派生「绑定当前 issue 的会话数」；`state/imBots` 增会话查询域 +
  `useBindConversationIssue` mutation（成功 invalidate 会话域）
- bot 卡片聚合态：存在绑定当前任务的会话 → 高亮「N 个会话已关联」chip；无 → 置灰；展开列出
  会话明细行（「私聊 · 3 分钟前活跃」「群聊 · 昨天」+ key 短码 + 各自绑定任务名；会话无昵称
  存储，以 chat type + lastMessageAt + key 尾短码呈现）
- 会话条目操作：「关联当前任务」（该会话改绑当前 issue，绑其他任务时显示所绑任务名 + 切换）/
  「解除关联」——作用于 (bot, conversationKey)
- 「+」补绑：下拉列出未属于本工作空间的 bot（`workspaceId !== 当前`，含未配置），提交 =
  update 全表单（workspaceId = 当前，secret 留空沿用）+ 确认提示（bot 原属其他工作空间时明示
  迁移）；复用 `useUpdateImBot`——新 bot 尚无会话，任务绑定待会话开启后在会话条目上操作
  （语义自洽：绑定以会话存在为前提）
- doctor 健康徽标为可选增强（`acpHealthChip` 若复用需从 ImBotPage 提取共享，不阻塞本任务）

**依赖**：T3.1

**验收**：`pnpm web:build` + `pnpm web:lint` 通过；聚合 / 展开 / 会话绑定切换 / 解绑 / 补绑 /
空态交互完整

#### T3.3 bot 抽屉配置文案完善

**状态**：⬜

**功能**：配置项作用范围说明对齐实际语义（诉求 2 收尾）

**技术方案**：
- `ImBotDrawer.tsx`：人设 helperText →「仅对 IM 来源回合生效：ACP 模式随每条 IM 消息注入，
  终端模式注入系统提示词；桌面端回合不受影响」；模型 helperText →「仅终端模式生效；ACP 模式
  会话模型跟随桌面端」

**依赖**：无

**验收**：`pnpm web:build` + `pnpm web:lint` 通过

---

### 阶段 4：思考滚动摘要

#### T4.1 思考内容 IM 滚动摘要

**状态**：⬜

**功能**：IM 侧滚动展示思考内容尾部摘要（诉求 1）

**技术方案**：
- `driver_acp.go collectTurn` 的 `agentThought` 分支：`thoughtSent` 水位线差分（范式同
  agentMessage：`strings.HasPrefix` 校验 + 前缀错配重置，多字节安全），每次增长发
  `TurnEvent{Type: TurnStatus, Status: "💭 " + tail(frame.Entry.Text, 72)}`（`tail` 为
  rune 安全尾部截取，前缀省略号标识截断）
- 泵侧 `lastSent` 去重天然兜底重复帧；正文 TurnText / 工具状态行到来自然让位（既有语义）；
  泵 / 编排器 / wecom 零改动
- panel 侧不受影响（agentThought 条目全量淡化展示为既有能力）

**依赖**：无（可与阶段 1 并行）

**验收**：`driver_acp_test` 新增用例（差分滚动含多字节 / 前缀错配重置 / 与正文让位时序）；
全量 `-race` 通过

---

## 附：调研引用索引（关键代码位置）

**本项目（现状依据）**：`server/internal/bot/prompt.go:11-69`（ComposeSystemPrompt /
BuildTurnPrompt 围栏）、`bot/driver_acp.go:45-46,161-315`（SystemPrompt 忽略注释 / collectTurn
帧收集与出卡链）、`bot/driver_route.go:72-145`（resolve 路由与绑定解析）、
`bot/orchestrator.go:188-235,277-379`（指令步 / runTurn 时序）、`bot/card.go:30-95`
（CardSpec / CardReplyStream / interactionHandler / CardPusher TODO）、`bot/issue_command.go`
（#issue 双通道解析）、`bot/supervisor.go:188-214`（botRuntimeConfig）、
`bot/wecom/card.go`（vote / multiple_interaction 双向翻译）、
`service/im_bot.go:63-128`（Create/Update 显式列 Updates）、
`service/project_issue.go:344-399`（删除级联）、`service/workspace.go:150-161`（workspace
级联置 0）、`acpsession/view.go:36-41,373-387`（ConversationEntry / thought 累计）、
`acpsession/manager.go:301-360`（Prompt/PromptQueued）、`acpsession/hub.go:18-50`（帧型）、
`packages/web/.../AcpSessionView/MessageList.tsx:54-104`（四类条目渲染）、
`packages/web/.../WorkbenchTools/toolRegistry.tsx:40-62`（工具注册表）、
`packages/web/.../ImBotsPage/{ImBotPage,ImBotDrawer}.tsx`（bot 配置与徽标）、
`server/internal/migrations/migrations/20260730001_init_tracker.sql`（表结构基线）。

**Gold-Band（行为语义参照，AGPL-3.0 禁代码移植）**：`src/provider/mod.rs:54-165`（UserPromptRole
回合级人设 + agent_prompt_text）、`src/prompts/zh-CN/runtime/user_role_message.md`（角色包装
模板）、`src/acp/client.rs:3346-3406,3511-3525`（`_meta.systemPrompt.append` 私有约定 +
`<hidden>` 降级包装）、`web/src/components/acp/{hiddenPromptSections.ts,HiddenPromptMessageContent.tsx}`
（hidden 块前端解析折叠——display 分离范式）、`src/im/projection.rs`（IM 出站投影，无 thinking
的实证）。

**协议与开源依据**：ACP 官方 schema（`PromptRequest` 无 system prompt 字段、`_meta` 扩展位，
agentclientprotocol.com/protocol/schema）；Claude Agent SDK `modifying-system-prompts`
（`--append-system-prompt` 追加语义 / mid-session 改指令规则）；OpenClaw
（envelope 消息来源包装 + bootstrap 人设体系，docs.openclaw.ai）；官方 Claude in Slack
（thread→session、人设靠 repo 配置——CLAUDE.md 模式的对照）。
