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
5. **卡片化绑定**：IM 内 `#工作空间` / `#任务` 指令模糊搜索 → 单选卡 → 选中即绑定；未绑定时的引导自动附卡；
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
  wsbind / taskbind / taskunbind 卡（TaskID 无状态自包含 + 关键词与列表指纹防错绑）
  触发链：#工作空间 / #任务 指令模糊搜索出卡 · #任务解绑 确认卡 · workspace 门禁附卡 ·
  ACP 未绑探针附卡（自然语言意图匹配延后，见 D6）

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
| D5 | **绑定卡复用现有 interaction 管线** | 指令步 `SendCard` 终帧带卡 + 点击走交互快车道 → `TryHandleInteraction` → 置灰 + 终帧，全部现成；TaskID 标记族 `wsbind-<kwhex>-<指纹>` / `taskbind-<kwhex>-<指纹>`（搜索关键词 utf-8 hex + 匹配列表哈希指纹双因子：点击时解码关键词重查同款模糊查询比对指纹，列表已变则提示重取卡——无状态自包含且防索引错绑；关键词必须编码进投递锚，否则点击侧无法复原命中集，连下标到目标的映射都做不出）；taskbind 落库点击来源会话行（`ConversationStore.SaveBoundIssueID` 现成，每回合现读免重启），wsbind 落库 bot 行 workspace_id 并经 `ApplyBotAsync` 延迟热重载（终帧发出后再 Stop→Start，避开渠道回调自停时序） |
| D6 | **意图匹配延后（T2.2 实施拍板裁剪）** | 原案为整条消息正则全匹配（宁漏勿误）；实施时入口收敛为 `#工作空间` / `#任务` 指令——自然语言意图匹配（正则或 LLM 分类）待实际使用反馈后另行评估。裁剪依据：误触发代价（劫持整条消息不出回合）高于漏触发（Claude 文字兜底），是延后而非否定 |
| D7 | **工具栏绑定走独立会话级端点** | `POST /api/imBot/bindIssue {botId, conversationKey, issueId}`（issueId 空 = 解绑，作用于具体会话行）；workspace 关联复用现有 update 全表单链路（前端持全量模型）；`bound_issue_id` 不进 update 的 Updates map，抽屉保存天然不误伤；绑定变更每回合现读，无需热重载 |
| D8 | **headless 链路零改动** | system prompt（`--append-system-prompt`）/ 围栏 / 静态思考线维持现状；两引擎 IM 呈现基线差异按设计接受（headless 无 ACP 视图，显示链路改造对其无感） |

## 5. 表结构与配置项梳理结论

### 5.1 表结构

| 表 | 变更 | 说明 |
|---|---|---|
| `t_im_bot_conversations.bound_issue_id` | 保留（现状承载） | 会话级绑定锚（D1）：(bot, 会话) 粒度，空 = 未绑定；issue 删除级联清列 T2.3 已交付 |
| `t_im_bot_conversations.claude_session_id` | 保留 | headless `--resume` 锚，ACP 不写（与绑定锚正交） |
| `t_im_bot_conversations.seen_message_ids` | 保留 | 幂等滚窗，与绑定无关 |
| `t_im_bots.model` / `system_prompt` | 已退役（T5.1 删列） | model 仅终端模式生效的语义割裂退役（headless 回落 CLI 默认模型）；人设承载让位后续专门的提示词管理模块，固定 IM 守则 + 内容边界声明照常注入两引擎 |
| `t_workspaces` / `t_issues` 的 `launch_settings` | 保留 | 无冗余；权限 / 模式体系（P1/P6）不受本方案影响 |
| `t_issue_acp_sessions` | 保留 | issue↔ACP 会话锚，与 bot 绑定正交 |

本方案零表结构变更（会话级绑定锚即现状列）。

### 5.2 配置项

- **ImBotDrawer**：人设与模型两配置项已随 T5.1 退役（含 T3.3 两段 helperText 一并移除）；
  抽屉保留渠道 / 名称 / Bot ID / Secret / 工作空间 / 访问模式 / 启用。
- **WorkspaceDrawer / ProjectIssueDrawer**：无变更（launch_settings 体系已闭环）。
- 无新增 bot 级配置项——人设既有列即承载「bot 特化」诉求，不引入工具白名单类回潮配置。

## 6. 风险与开放问题

1. **每回合 token 开销**：ACP bot 回合恒带 `<im_context>`（固定守则 ~300 字 + 人设 ≤8000 字）。
   共享会话下「仅 IM 生效」的必要代价（Gold-Band UserPromptRole 同款）；人设留空时仅固定段。
2. **会话级绑定的并行语义**：不同会话绑不同 issue = 不同 ACP 会话进程完全并行；同一会话内
   消息串行（每会话 FIFO 队列，IM 有序语义）；不同会话绑同一 issue = 共享 ACP 会话回合
   FIFO 排队（「共享会话」设计本意，与桌面 / bot 双入口同理）——绑同一任务即为共同推进，
   排队不是耦合。工具栏会话明细无昵称存储，以「私聊 / 群聊 + 最近活跃时间 + key 短码」呈现。
3. **意图匹配边界**：意图匹配延后（D6），本期入口仅指令，无误触发面。
4. **ApplyBotAsync 延迟热重载时序**：workspace 卡点击落库后延迟 ~1s 再 Stop→Start，窗口内旧 cfg
   的在途回合仍用旧 workspace（可接受；落库即生效的是下下回合）。需测试覆盖「点击 → 置灰 →
   终帧 → 重启」全序。
5. **绑定卡候选上限**：workspace / 任务卡统一截 10（企微 vote 选项上限 20 内留余量），同帧文本
   附完整名称列表，触及上限提示 `#工作空间 <关键词>` / `#任务 <关键词>` 缩小范围。
6. **企微选项文案截断**：vote 选项 11 字（协议限制），完整标题在同帧 content 文本；卡面标题截断
   可读性靠同帧文本补足。
7. **panel 显示兼容**：`Display` 元数据缺失（旧 sidecar + 新前端混跑窗口）回落原样直显 Text，
   不做标签解析兜底（混跑窗口极短，历史条目随 sidecar 重启即消失）。

---

## 7. 任务清单（进度 SSOT）

> 任务粒度：模块 + 功能 + 技术方案，一个任务 ≈ 一次可独立验收的交付。
> 执行顺序：T1.1 → T1.2 → T1.3 → T2.1 → T2.2 → T3.1 → T3.2 → T3.3 → T4.1 → T5.1
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

**状态**：✅

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

**实施定稿**：按方案落地，一处呈现修正——IM 徽标不含企微专属标识：`entry.source` 取值域仅
`panel|bot`（无渠道维度），企微标记不可派生，改用通用 IM 图标（`ForumOutlined`，ImBotsPage
既有用例）+「IM」caption。补充三点终态事实——① user 分支拆为 `UserEntry` 组件做双态分发：
`source==='bot' && display` 走 `ImUserBubble`，否则回落原 panel 气泡直显 `entry.text`（混跑
兼容窗口，风险 §6.7），渲染主体集中单组件便于后续渠道维度细化；② 引用块置于正文之上（IM
回复惯例「先引用后正文」），`truncated` 尾缀「…」呈现截断（适配器侧已截断，前端不做二次
clamp）；空正文（纯引用/附件消息）不渲染正文行——占位文案只存在于模型侧围栏；③ TS 镜像
`display` 拆三个独立 interface（`AcpEntryDisplay/AcpEntryQuote/AcpEntryFile`）并补齐 services
barrel 导出；reducer 整对象覆写式 upsert 零改动，新字段经 SSE entry 帧与快照天然透传。

---

### 阶段 2：IM 卡片化绑定

**设计要点**（T2.2 实施拍板收敛为统一「搜索 → 卡片选择 → 提交绑定」模型；解绑同模型）：
- **统一模型**：指令 / 门禁 / 探针全部入口只做候选计算与出卡，绑定与解绑动作唯一入口都是
  卡点击——选择与提交逻辑收敛。旧 `#issue`「唯一命中直绑 + Body 首回合一步」链路与 ID
  前缀通道、`#任务 解绑` 子指令（撞词收窄特例）均退役——解绑改走独立指令 `#任务解绑`
  （恰等判定）出单选项确认卡，点击提交才清锚；`#任务` / `#工作空间` 后整段皆为搜索词
  （名称 LIKE 模糊，不支持 ID）。
- **卡构造**（`bot/binding_card.go`）：单选卡；候选 = 模糊匹配结果（关键词空 = 全列表），
  统一截前 10，同帧 content 附完整名称列表；任务卡候选口径 = bot workspace 域内非终态且
  顶级（`state_code ∉ 终态集` 且 `parent_id` 空——对齐前端 isDevIssue 左树口径；子任务不再
  经 #任务 搜索可绑，与左树「所见即所绑」一致）。选项文案企微截 11 字，完整信息在同帧文本。
- **TaskID 关键词 + 指纹防错绑**：`wsbind-<kwhex>-<hash8>` / `taskbind-<kwhex>-<hash8>`，
  kwhex = 搜索关键词 utf-8 hex 编码（空关键词 = 空段；关键词截限保 TaskID ≤128），hash8 =
  匹配后有序目标 id 串的 8 位十六进制指纹。点击时解码关键词 → 重查同款模糊查询 → 指纹比对：
  一致则按 `ActionIndex` 下标解析目标并绑定；不一致回「列表已变化，请重新发送指令获取新卡片」。
  关键词必须进 TaskID——不在投递锚里则点击侧无法复原命中集。无内存注册表，跨重启 / 迟到点击
  自然降级（与 perm/elicit 卡同哲学）。
- **点击消费**（driverRoute `TryHandleInteraction` 按前缀分流，决策链 T2.1 已交付）：
  - `wsbind-`：解码 kw 重查 + 指纹 → 落库 `t_im_bots.workspace_id` → 置灰（重查列表重建
    完整 spec，指纹一致保证字段一致）+ 终帧「✅ 已关联工作空间「名」」→ `ApplyBotAsync(botID)`
    延迟热重载（supervisor 注入窄接口，goroutine + ~1s 延迟，确保终帧先发；风险 §6.4）
  - `taskbind-`：解码 kw 重查 + 指纹 → 落库点击来源会话行（`ConversationStore.SaveBoundIssueID`
    现成，点击消息自带 ConversationKey）→ 置灰 + 终帧「✅ 已绑定任务「名」，直接发消息即可
    向该任务下达任务」（绑定每回合现读，无需重载）
  - `taskunbind-`：读当前绑定 → 指纹比对（锚 = 绑定 id 单元素序列；出卡后已解绑 / 换绑 /
    悬空锚均不符）→ 清锚（幂等）→ 置灰 + 终帧「✅ 已解绑任务「名」」
  - 列表变化 / 下标越界 / 落库失败：提示文案不置灰不落库（fail visible）
- **触发链**（orchestrator `runTurn` 固定时序插步，全部「指令步」形态：终帧收口、不入队、
  不占并发槽；出卡经 `CardReplyStream.SendCard` 类型断言，无卡能力回落纯文本）：
  1. `#帮助` 指令 → 指令列表纯文本终帧（置于全部绑定指令与门禁之前——不依赖任何前置
     状态，未选工作空间时恰是用户最需要指令列表的时刻）；
  2. `#工作空间 [kw]` 指令 → workspace 卡终帧（**门禁之前**——未绑 workspace 的 bot 也要能
     出这张卡）；
  3. `#任务解绑` 指令 → 解绑确认卡终帧（**门禁之前**——清锚不依赖 workspace，未选工作
     空间的 bot 也要能解绑历史锚点；未绑定回纯文本教学）；
  4. workspace 门禁（`WorkspaceDir` 空）→ 指引文本 + workspace 卡同帧（指引为中性 lead
     前缀，统一前置于出卡 / 库空教学 / 读库失败一切形态文本）；
  5. `#任务 [kw]` 指令 → 任务卡（唯一命中同样出卡）；
  6. ACP 未绑探针：driverRoute 暴露 `ProbeTarget(cfg, conversationKey) (acp bool,
     boundIssueID string, err error)`（resolve 重构为具名方法复用），orchestrator 在组 prompt
     前经 `routeProber` 接口类型断言调用（不实现则跳过）——ACP 且未绑 → 任务卡 + 终帧
     （driver 内未绑定报错保留兜底；探针读库失败不拦截，落回主路径由 resolve 报错收口）。
- 终态集 SSOT：`state_code` 终态判定（DONE / CANCELLED）在 bot 域定义常量集，与前端
  `isDevIssue` 语义对齐（§1.2 事实 9）。

#### T2.1 绑定卡构造与点击消费

**状态**：✅

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

**实施定稿**：按方案落地，一处落位修正——点击分流两支加在 `permission_card.go` 的
`TryHandleInteraction`（driverRoute 方法实际所在文件，方案写作 driver_route.go），消费逻辑
全部收在 `binding_card.go` 域内。补充三点终态事实——① `ApplyBotAsync` 延迟后重读 bot 行按
service 层 `applyRuntime` 同款禁用语义分流：启用 → `ApplyBot`（成败照常回写 `last_error`）、
停用 → `StopBot`（幂等）、行已删 → 静默跳过；延迟值在调用时点捕获为局部变量再进 goroutine
（测试缩短/还原包级变量不构成数据竞争）。② 终态集 `issueTerminalStateCodes` 元素类型取
`[]driver.Valuer`（`StateCode` 字段表达式 `NotIn` 的形参面，枚举原生实现该接口），SSOT 位置
不变。③ 点击决策矩阵测试落在 `binding_card_test.go`（域内文件，`driver_route_test` 仅适配
构造器第四依赖），另新增 `supervisor_test.go` 五例：延迟重载拉起渠道（含 supervisor 以
botApplier 注入的装配断言）/ 行已删跳过 / 停用走 StopBot / 延迟窗口内停用（醒来重读行
的核心语义回归防护）/ StopAll 后不复活渠道。质量审查后补三点终态——workspace 卡候选与
任务卡共用企微 vote 选项上限（常量更名 `bindingOptionLimit`，防超限卡被适配器拒收）；
`Supervisor` 增 stopped 生命周期守卫（StopAll 置位 → EnsureStarted 拒绝 + applier 醒后
早退，杜绝关机窗口复活渠道连接）；指纹计算收敛 `strings.Join` 等价简化。

#### T2.2 绑定卡触发链接线

**状态**：✅

**功能**：指令 / 门禁 / 探针入口把绑定卡送到用户手上（统一「搜索 → 卡片 → 提交绑定」模型）

**技术方案**：
- 新增 `server/internal/bot/commands.go`：`parseWorkspaceCommand`（裸 = 全列表卡；带参数 =
  名称模糊搜索词）；`parseIssueCommand` 简化为 `{Unbind, Keyword}`（整段关键词，Body 一步
  链路与 ID 前缀通道删除）；关键词字节截断规整（保 TaskID ≤128）
- `binding_card.go`：TaskID 扩为 `<prefix><kwhex>-<fp8>` 编解码（空关键词 = 空段）；候选查询
  加 keyword 参数（名称 LIKE，空 = 全列表）；`bindingOptionLimit` 20→10；点击消费改「解码
  kw → 同款查询 → 指纹 → 下标落库」
- `orchestrator.runTurn` 时序插步（见阶段 2 设计要点触发链 1–5，统一 `sendBindingCard`
  收口：查候选 → 零候选纯文本终帧 / `SendCard` 一帧带卡收口 / 无卡能力回落纯文本）
- `driver_route.go` 暴露 `ProbeTarget`（resolve 重构为具名方法 + 公开探针）；orchestrator 经
  `routeProber` 接口类型断言消费（不实现则跳过——headless 路由不实现）

**依赖**：T2.1

**验收**：`orchestrator_test` 覆盖指令出卡（`#工作空间` / `#任务` 裸与带 kw / 解绑保留）/
门禁附卡 / 探针出卡不出回合 / 无卡回落；`commands` 解析用例表；`binding_card` kw 编解码与
点击链路；全量 `-race` 通过

**实施定稿**：按方案落地；实施评审拍板两项方案外变更——① 指令改名对齐用户面词汇：
`#workspace` → `#工作空间`、`#issue` → `#任务`（token 常量 SSOT，出卡/空态/帮助全部文案
从常量派生；预发布期旧名直接退役无别名，解析回归用例锁死旧名不再命中）；② 新增 `#帮助`
指令（TrimSpace 恰等判定，置于绑定指令与门禁之前，列出全部指令及触发方式；企微回复流
无 markdown，纯文本 `\n- ` 行式列表）。门禁指引重构为中性 lead 前缀：不预设「点击下方
卡片」，统一前置于出卡 / 库空教学 / 读库失败一切形态文本。补充五点终态事实——
`SaveBoundIssueID` 返回受影响行数，taskbind 点击 0 行（会话行不在场）回绑定失败不置灰，
解绑幂等忽略行数；指令邻接判定按首 rune 解码（`utf8.DecodeRuneInString`），全角空格
U+3000 为合法分隔；ACP 引擎未绑定引导文案改 `#任务 <标题关键词>` 搜索语义（ID 前缀教学
随通道退役），正常链路探针步先行递卡收口、该文案为无卡链路兜底；探针步经 `routeProber`
类型断言消费，headless 直连装配与测试替身未实现时自然跳过；测试覆盖指令解析形态域（含
旧 token 退役 / 全角空格 / #帮助 恰等判定）、指令/门禁/探针全链路出卡与回落、点击
0 行 / 指纹不符 / 越界矩阵，`-race` 与全仓测试通过。
解绑形态拍板（同模型收敛）：`#任务 解绑` 子指令与撞词收窄特例退役，独立指令
`#任务解绑`（token 从 `#任务` 常量派生，TrimSpace 恰等判定；带后缀/粘连不命中，对
`#任务` 是粘连形态互不抢消息）同样卡片化——触发步置于门禁之前（清锚不依赖
workspace），出单选项确认卡（TaskID `taskunbind-<空关键词段>-<绑定 id 单元素指纹>`，
关键词恒空）；点击消费与绑定卡同构：读当前绑定 → 指纹比对（出卡后已解绑/换绑/悬空锚
均不符，回重取卡文案不落库）→ 清锚（幂等忽略行数）→ 置灰 + 终帧「✅ 已解绑任务
「名」」；未绑定回教学终帧；`applyIssueCommand` 退役，`#任务` 指令步直调
`sendBindingCard` 统一出卡口。

---

### 阶段 3：工具栏与配置完善

#### T3.1 会话级绑定 HTTP 面

**状态**：✅

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

**实施定稿**：按方案落地，零偏离；补充四点终态事实——① ChatType wire 值取 `single | group`
（会话键前缀派生，service 层 `chatTypeOfKey` 就地解析、不依赖 bot 包；键格式 SSOT 仍是
`bot.FormatConversationKey`）；② 会话列表排序 = `last_message_at` 倒序 + `id` 倒序（从未活跃
殿后，SQLite NULL 最小值语义下 DESC 天然置尾）；绑定任务名批量 join 收敛 `boundIssueNames`
（对齐 workspaceNames 惯例，悬空锚/查失败按空名兜底不阻塞列表）；③ 会话行写入走 service 层
直查 `query.Use` `UpdateSimple`（未绕 bot 域 ConversationStore——后者 `context.Background()`
与 service 请求上下文语义不符，且对齐 Delete 级联直查惯例）；行不在场 0 行拒绝，绑定与解绑
同拒；只写 `bound_issue_id` + `updated_at`，`claude_session_id` 不扰动；bindIssue 无返回体
（对齐 delete/restart）；④ 测试三用例（`im_bot_test.go`）：列表装配/排序/chatType 派生/
bot 不存在拒绝、绑定解绑主链路（含续聊锚不扰动与列表口径回读断言）、六路拒绝矩阵（issue
不存在 / 跨工作空间 / bot 无 workspace / 会话行不在场绑定与解绑 / bot 不存在）。

#### T3.2 任务工具栏 IM 机器人模块（bot 聚合 + 会话明细）

**状态**：✅

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

**实施定稿**：按方案落地，四处方案外拍板/落位修正——① 目录落位
`DevWorkbenchPage/components/ImBotPanel/`（与 IssueSubTaskPanel / WorkspaceFilePanel 同级，
工具面板组件目录既有惯例），非方案字面的 `WorkbenchTools/ImBotPanel/`，toolRegistry 仍是
唯一注册扩展点；② 实施澄清拍板：聚合卡加连接状态徽标（doctor 徽标维持可选增强不做），
`stateChip` 从 ImBotsPage 提取为 `ImBotsPage/imBotStateChip.ts` 共享导出；③ 实施澄清拍板：
会话行全部操作（关联 / 切换 / 解绑）统一面板级确认弹窗二次确认（方案未规定弹窗形态），
补绑迁移确认弹窗按方案保留——四形态文案由 `confirmSpec` 纯函数派生，pending 禁关闭防并发、
失败保留弹窗错误可见、重开弹窗清残留错误；④ T3.1 前端遗留缺口随任务补齐：
`services/index.ts` 补 `ImBotBindIssueRequest` / `ImBotConversationModel` /
`ImBotGetConversationsRequest` 三类型导出，imBots 域增 `conversations(botId)` key 与
`useImBotConversations`（enabled 门槛）/ `useBindImBotIssue`（成功按 botId invalidate 会话
key）两 hook。组件三文件——`ImBotPanel.tsx`：PanelToolbar（「机器人 N」计数 + 「+」补绑
Popover 候选 = 非本工作空间 bot 含未配置、无候选禁用提示项 + 刷新 invalidate root 整域）
+ 四分支骨架 + 统一确认弹窗（面板级单飞行中操作，在飞请求经 `bindPending` 下传 bot 卡做
行级 loading，卡内按 botId 自筛）；`ImBotBotCard.tsx`：聚合头（name + connState 徽标 +
「N 个会话已关联」success / 「未关联」置灰 chip + 加载中小 spinner + 展开箭头）+ 会话明细
行（私聊/群聊 · 相对活跃文案 · key 尾 6 位短码 + 所绑任务态行 + 关联/切换/解除按钮）；
`format.ts`：中文直出纯函数（活跃文案分档 刚刚/N 分钟前/N 小时前/昨天/N 天前/超一周落
YYYY-MM-DD/null=从未活跃、chatType 文案、短码；相对文案语义对齐 shared/time
formatRelativeTime 但本地实现——i18n t 注入不适用工具面板中文直出约定）。补绑提交 =
`useUpdateImBot` 全表单（workspaceId = 当前，secret 空串沿用；update 就地替换列表缓存，
面板过滤自动收敛新卡）。质量审查后补三点终态——① 刷新按钮提取共享组件
`components/RefreshIconButton.tsx`（IconButton small + Autorenew spin，fetching 禁用+旋转），
收敛 IssueSubTaskPanel / WorkspaceFilePanel / ImBotPanel 三份逐字拷贝；② ConversationRow
操作按钮合并为单按钮三态 label（解除关联 / 切换为当前任务 / 关联当前任务），解绑态色
inherit；③ `workspaceShortLabel` 收敛 workspace 名缺失回落 `#id` 规则（面板内两处：迁移
确认文案 + 补绑候选二级文案）。正确性审查零缺陷（弹窗快照并发等 low 置信度观察记录不修）；
规范/抽象一致性审查零发现，两条低置信度备注经拍板顺手修复——bot 卡展开箭头补自身
onClick（键盘可激活，stopPropagation 防与父行点击冒泡双 toggle 抵消）、state 域 README
键工厂整域失效根命名 all → root 三处对齐实作（全库九域一律 root，先于本任务的文档漂移）。

#### T3.3 bot 抽屉配置文案完善

**状态**：✅

**功能**：配置项作用范围说明对齐实际语义（诉求 2 收尾）

**技术方案**：
- `ImBotDrawer.tsx`：人设 helperText →「仅对 IM 来源回合生效：ACP 模式随每条 IM 消息注入，
  终端模式注入系统提示词；桌面端回合不受影响」；模型 helperText →「仅终端模式生效；ACP 模式
  会话模型跟随桌面端」

**依赖**：无

**验收**：`pnpm web:build` + `pnpm web:lint` 通过

**实施定稿**：按方案落地，零偏离；补充三点终态事实——① 模型字段原无 helperText（仅
placeholder），本任务新增方案文案、placeholder「留空 = CLI 默认」保留；② 「终端模式」措辞与
ImBotsPage 既有注释词汇一致（ImBotsPage.tsx doctor 徽标注释同款表述）；③ 两输入框与两段
helperText 已随 T5.1 配置项退役整体删除（本任务交付被其终结）。

---

### 阶段 4：思考滚动摘要

#### T4.1 思考内容 IM 滚动摘要

**状态**：✅

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

**实施定稿**：按方案落地，一处命名细化——尾部截取助手定名 `tailRunes`（方案字面 `tail`，
显式表达 rune 安全语义），上限常量 `thoughtTailRunes = 72`。补充两点终态事实——① 驱动侧
差分仅增长时发事件（重复帧零事件，水位线即主去重），泵侧 `lastSent` 去重降为兜底防线；
② 测试形态：`TestAcpDriverThoughtStatusLine`（T3.4 静态文案）重构为
`TestAcpDriverThoughtScrollingSummary`（差分滚动含多字节 / 超 72 rune 截断省略号 / 重复帧
不重发 / 正文让位时序），新增 `TestAcpDriverThoughtWatermarkReset`（前缀错配重置与续接，
范式对齐 agentMessage 同款用例），`TestAcpDriverZeroTextTurnNotContaminated` 思考行断言随
滚动语义同步为摘要直出（「💭 思考中」，短串无省略号）。

---

### 阶段 5：配置项裁剪（体验审视跟进）

#### T5.1 bot 级 model / system_prompt 配置项退役

**状态**：✅

**功能**：删除 bot 行 model / system_prompt 两列及全链路配置面——model 仅终端模式生效的
语义割裂是配置异味（headless 回落 CLI 默认模型）；人设承载让位后续专门的提示词管理模块
统一关联（本次不预建）。

**技术方案**：
- 迁移退役两列；固定 IM 守则 + 内容边界声明（ComposeSystemPrompt 固定段）不受影响照常
  注入两引擎（`--append-system-prompt` / `<im_context>`）；`neutralizeTagClose` 防线保留
  （未来提示词管理模块会重新引入外部内容）
- DTO / service / BotRuntimeConfig / TurnRequest.Model / `--model` argv / web（Drawer 两
  输入框、ImBotService 类型、ImBotPanel 补绑 payload）全链路移除

**依赖**：无

**验收**：`pnpm server:gorm:gen` + `go -C server test ./... -race` + `pnpm web:build` +
`pnpm web:lint` 全部通过

**实施定稿**：按方案落地，零偏离。终态事实——`ComposeSystemPrompt` 无参化（固定段 SSOT，
函数签名即提示词管理模块的接入点）；`TurnRequest.SystemPrompt` 字段保留（承载固定守则
产物，`<im_context>` 注入链与 `--append-system-prompt` argv 零改动）；本任务同时终结
T3.3 交付的两段 helperText（随字段一起退役）；相对方案的偏离——两列未追加独立 DROP 迁移，
经 tracker 基线迁移内联退役（预发布期基线可改，DO 层已重生成对齐）。

---

## 附：调研引用索引（关键代码位置）

**本项目（现状依据）**：`server/internal/bot/prompt.go:11-69`（ComposeSystemPrompt /
BuildTurnPrompt 围栏）、`bot/driver_acp.go:45-46,161-315`（SystemPrompt 忽略注释 / collectTurn
帧收集与出卡链）、`bot/driver_route.go:72-145`（resolve 路由与绑定解析）、
`bot/orchestrator.go:188-235,277-379`（指令步 / runTurn 时序）、`bot/card.go:30-95`
（CardSpec / CardReplyStream / interactionHandler / CardPusher TODO）、`bot/commands.go` +
`bot/issue_command.go`（#工作空间 / #任务 指令解析）、`bot/supervisor.go:188-214`
（botRuntimeConfig）、
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
