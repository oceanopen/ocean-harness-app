# ACP 统一会话与 IM 无缝交互——方案定稿与任务清单

> 状态：方案定稿（技术决策 D1–D8 已定；P1–P6 已经 **T0.0 拍板**，结果见 §4）
> 范围：issue 执行模式扩展（ACP 第三模式）、workspace 级启动配置、bot 复用 ACP 会话、企微模板卡片交互
> 参考：`~/MyFiles/Project/Gold-Band`（ACP runtime + IM 远程干预的已验证实现）
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

### 1.1 现状：两套 claude 驱动链路，互不相通

| | 桌面工作台（panel） | IM bot（企微） |
|---|---|---|
| claude 驱动方式 | 用户在 PTY 终端里手动交互运行（Rust 只做会话快照观察，`app/src/sessions/`） | sidecar 每回合 spawn headless `claude -p`（`server/internal/bot/driver_claude.go:26`） |
| 会话归属 | claude 自己的 TUI / session 存储 | `t_im_bot_conversations.claude_session_id` 独立锚点 |
| 权限控制 | claude TUI 在终端内问（只有看得见终端的人能批） | 无——`--permission-mode acceptEdits` 一口气跑完 + `--allowedTools` 白名单 |
| 共享的东西 | 仅两项环境级复用：同一 workspace 目录、同一 MCP 工具链（`OCEAN_HARNESS_PORT` 同源注入） | 同左 |

**根因**：本项目桌面端是「终端优先、被动观察」架构——Rust 侧不拥有 claude 进程，自然没有可供 bot 接入的 runtime 会话。bot 只能独立 spawn headless 进程（`server/README.md:109` 明文写了与 PTY「互不干扰」）。

### 1.2 Gold-Band 对比结论（已调研定稿）

Gold-Band 的「工作台 + bot 无缝切换」不在 bot 侧，而在桌面架构——**单事实源、双入口**：

- 桌面 Runtime 通过 ACP（Agent Client Protocol，JSON-RPC over stdio）**亲自驱动** claude CLI；
- Claude 需要授权/追问时**暂停并落盘 pending 状态文件**，等 response 文件出现才续跑（`src/acp/permission.rs` / `elicitation.rs`）；
- 桌面按钮和 IM 卡片走**同一个** `InterventionCommandService`，谁先点谁生效（CAS + first-writer-wins），后点方收到「已在另一端处理」；
- **IM 侧没有独立 bot 会话**，Claude 普通回复不进 IM，只有审批卡 / 表单卡 / markdown 通知三类（明确不做自然语言 ChatOps）。

其他可借鉴要点：

- **多 agent 中立**：内嵌 11 个 agent 的 catalog（claude-acp、codex-acp、gemini、cursor 等），数据源是 ACP 官方 registry 的**离线 pin 快照**（`resources/agent-catalog.json` + `scripts/prepare-agent-catalog.mjs`），运行时不在线拉取；
- **单一通用 adapter**：没有 per-agent 适配层，所有 agent 共用一个 ACP adapter，差异只有启动命令 + `initialize` 能力协商（`src/acp/adapter.rs:52-113`）；
- **doctor 真实握手探测**：可用性判据 = 真拉起子进程跑通 `initialize → setup_session → available_commands`（`src/acp/client.rs:2673-2731`），不健康的 agent 在 UI 置灰不可选；
- **企微协议踩坑已固化**：`template_card_event` 嵌套结构、`submit_button.disable` 字段不存在（发无效字段报 42049）、回调 5 秒更新窗口、42045 平台码等，全部以真实 PoC 校准并写成协议测试——照抄参数可省一轮踩坑。

### 1.3 企微 SDK 卡片能力现状：全家桶已在依赖里，零接线

`wecom-aibot-go-sdk@v0.1.0` 已具备但未调用：

| SDK 能力 | 位置 | 现状 |
|---|---|---|
| `ReplyStreamWithCard`（流式 + 模板卡片） | SDK `client.go:332` | 未用 |
| `UpdateTemplateCard`（回调 5 秒窗口内更新卡片终态） | SDK `client.go:366` | 未用 |
| `TemplateCard*` 类型族（含下拉选择器） | SDK `types/api.go:85+` | 未用 |
| `OnTemplateCardEvent` 按钮点击回调 | SDK `message-handler.go:124` | **未注册，点击事件被静默丢弃**（`wecom/channel.go:100` 只注册了消息类回调） |

渠道无关核心的 `ReplyStream` 抽象当初就预留了卡片语义（`server/internal/bot/channel.go:17` 注释：「飞书卡片编辑流同构」）。

### 1.4 权限语义现状与 ACP 变迁（P6 调研依据）

**现状链路（headless）**：bot 抽屉「执行权限」两档（可执行命令 = 空数组回落默认白名单 9 项含 Bash；不可执行命令 = 剔除 Bash 的 8 项，`ImBotDrawer.tsx:44-60`）→ `t_im_bots.allowed_tools` JSON 列 → `TurnRequest.AllowedTools` → spawn argv `--allowedTools Read,Glob,...`（`driver_claude.go:32-50`，`--permission-mode acceptEdits` 硬编码）。「不可执行命令」有效的原理是 headless 副作用：未列入白名单的工具在 headless 下无人应答权限请求 = 自动拒绝，白名单因此成为事实上的能力边界。

**ACP 模式下该等价关系消失**（官方 adapter 源码 + 协议 schema 实测，来源见附录）：

| | headless（现状） | ACP 模式 |
|---|---|---|
| 配置落点 | spawn argv（--allowedTools + acceptEdits 硬编码） | `session/new` 的 `_meta.claudeCode.options` + settings.json 四层合并（user/project/local/managed，watcher 热加载）+ `session/set_mode` |
| allowedTools 语义 | 事实能力边界（未列入 = 无人应答即拒） | 协议本义 = **免审批放行名单**，不限制可用工具集；未列入的工具不会被拒，而是触发 `session/request_permission` 弹给 client 应答 |
| 真正禁用某工具 | 靠不列入白名单 | `disallowedTools`：工具从模型上下文整体移除，agent 不可见、无法经审批卡解禁 |
| 免审批手段 | 无 | `autoAccept`（client 代点第一个 allow 选项）+ `permissionMode=bypassPermissions`（agent 免问） |
| 权限模式 | argv 硬编码 acceptEdits | settings `permissions.defaultMode` 兜底 + 会话级 `session/set_mode`（值须对 agent 上报 mode 目录校验） |

**照搬「剔除白名单」会漏水**（被剔除的工具反而弹成审批卡、用户一点即放行），故 P6 拍板（T0.0）：废弃 bot 级白名单语义——**含终端模式 headless 路径**，「执行权限」配置项与 allowedTools 全链路移除（清理任务 T2.0），权限表达上移 ACP 原生 mode（`launch_settings.permissionMode`，workspace 设默认、issue 可按需修改）。行为变化提示：现状 headless bot 实际零弹窗（9 工具全在白名单 + acceptEdits），ACP 化「需要审批」档后 Bash 等非编辑操作会弹审批卡——这正是 ACP 模式的核心价值（IM 审批）；要现状式无打扰选「自动」档，或靠 allow_always（「允许并记住」）逐步收敛弹窗频率；清理后 readonly bot 恒用默认白名单（Bash 恒在）。

**Gold-Band 借鉴**（行为语义参照，AGPL 禁代码移植）：

- **零 allowedTools**：全库无任何工具白名单/预批准机制，权限请求一律运行时交互审批；唯一注入是 deny-list（`session/new` 强制 `disallowedTools` 追加 Monitor）——deny 而非 allow 的哲学
- **autoAccept 会话级开关**：开启后 client 自动选第一个 allow 选项即时应答（读会话快照 override，中途开启立即生效）——「不想被打扰」的表达方式；本项目记为后续增强，一期不提供（见 §5 风险 5）
- **permissionMode 会话级下发**：经 `session/set_config_option`（configId="mode"）下发，值必须对 agent 上报的 mode 目录校验（防硬编码失效）
- **IM 安全过滤**：危险权限动作（bypass 类、未知 kind 等）IM 端只保留「安全拒绝」、开启/放行必须回桌面（`intervention.rs` requires_desktop 判定）——本项目 T2.4/T3.2 收录

**术语校准**：权限应答枚举实为 `RequestPermissionOutcome`（`{outcome:"cancelled"}` | `{outcome:"selected", optionId}`）；`updateTo` / `SessionPermissionUpdateOutcome` **不是 ACP 协议字段**（`updatedPermissions` 是 Claude Agent SDK 字段，adapter 内部消化）；allow_always 的记忆范围协议不定义、由 agent 落地——claude-agent-acp 落地为 SDK `PermissionUpdate`（destination: session / localSettings / userSettings / projectSettings，选「允许并记住」即持久生效）。

**adapter 生态事实**：`zed-industries/claude-code-acp` 已合并至 `agentclientprotocol/claude-agent-acp`（单一活跃 lineage，当前 0.84.0，内包 Claude Agent SDK 0.3.284，**Node ≥22**）；无任何权限类 CLI 旗标/env（仅 `CLAUDE_CODE_EXECUTABLE` 等基础设施变量），权限配置只走 settings.json / `_meta.claudeCode.options` / `session/set_mode` 三条路；`permissionMode` 被 adapter 刻意从 `_meta` 透传面排除（mode 由 ACP mode 机制管理）。

---

## 2. 目标架构总览

```
阶段 0（T0.0–T0.3）：workspace 级 launch_settings
  t_workspaces 加 JSON 列 + Drawer 表单 + 消费链改造
  语义升级：mode 枚举（terminal-manual / terminal-auto / acp）+ agentCode
  ACP 权限：permissionMode（需要审批 / 自动，P6）

阶段 1（T1.1–T1.7）：Go sidecar 通用 ACP client + issue 主窗口 ACP 模式
  ├─ 通用 ACP client（协议实现，非移植；command+args 驱动，能力协商分支）
  ├─ agent catalog（离线 pin，一期只启用 claude-acp）
  ├─ doctor 握手探测（claude / node / adapter 三项）
  ├─ issue 主窗口 ACP 视图（底部任务描述入口，回车触发 session/prompt）
  └─ 事件流经 sidecar HTTP/SSE 到 panel

阶段 2（T2.0–T2.4）：bot 复用 ACP 会话
  ├─ bot 会话锚定 issue 的 ACP session（per-session 跨入口回合锁）
  ├─ bot↔issue 显式绑定 / IM 内切换
  └─ 企微/桌面双入口审批收敛（first-writer-wins）

阶段 3（T3.1–T3.5）：交互打磨
  流式/思考（复用 PumpReply）· 审批 vote_interaction 卡 · 单选/多选 elicitation 卡
  · UpdateTemplateCard 终态置灰 · doctor 健康度进 bot 状态徽标
```

每阶段独立可交付、不空转：阶段 1 完成即获得桌面侧审批/选项交互能力；阶段 2 只是把「第二个客户端」从桌面窗口换成企微。

---

## 3. 技术决策记录（已定稿）

| # | 决策 | 理由 |
|---|---|---|
| D1 | ACP client 放 Go sidecar | bot 进程内直连；issue/MCP/HTTP 全在 sidecar；放 Rust 要两次跨进程 |
| D2 | 单一通用 adapter，无 per-agent 适配层 | ACP 标准化红利：加 agent = 加 catalog 条目，零代码（Gold-Band 同构） |
| D3 | adapter 官方 npx 起步，Go 自研条件触发 | 协议追赶跑步机归官方；自研留触发条件（node 缺失率 / 离线 / 中间层痛点），届时 driver 积累可复用 |
| D4 | 不随 app 打包 node；doctor 探测 + UI 引导 | 包体与构建矩阵代价不值；npm 装 claude 的用户机器必带 node |
| D5 | 多 agent 中立，Claude 专属走能力协商 | 阶段 3 后 bot 天然 agent 中立，审批/选项卡交互层全复用 |
| D6 | `launch_settings` 用 JSON 列而非多列 | mode + agentCode + 后续 agent 配置持续扩展，避免反复迁移 |
| D7 | issue↔session 绑定受控反转（原 `claude_session_ref` 链曾随 chat 视图删除） | ACP runtime 亲自持有会话后语义成立，实施定稿须明示（见 T1.5） |
| D8 | Go ACP client 直接依赖 `github.com/BrokkAi/acp-go`（pin v0.11.0）+ sidecar 会话域薄适配层 | Apache-2.0 / 零传递依赖 / wire 类型由官方 JSON Schema 自动生成 / 有 tag 可钉 / 自带 runner 进程管理；自研收窄为 spawn、能力协商、入站回调适配三件事。风险隔离：会话域薄适配层 + 钉精确 tag + license 干净随时可 fork；Go 工具链随 T1.0 升 1.27 |

## 4. 决策点（P1–P6，已于 T0.0 拍板）

| # | 问题 | 拍板结果 |
|---|---|---|
| P1 | ACP 模式粒度：per-issue 配置 vs 全局默认 + issue 可覆盖 | **per-issue**——workspace 级设默认，issue 按需自行调整（覆盖范围为整个 launch_settings：mode / agentCode / permissionMode）；阶段 0 先落 workspace 层，issue 级覆盖为补充任务 T0.4 |
| P2 | bot↔issue 映射：显式绑定 + IM 内切换（如 `#issue-42 帮我…`） vs 自动挂 workspace 当前活跃 issue | **显式绑定 + IM 内切换** |
| P3 | 终端模式 ↔ ACP 模式跨模式会话接续（`--resume` 捞回）是否纳入一期 | **不入一期**——纯 ACP 会话先行，跨模式接续为后续增强 |
| P4 | agent 范围：一期 catalog 只放 claude-acp vs 直接全量 | 一期 catalog 启用 **claude、codex、opencode、pi** 四个 agent；通用架构（单一 adapter + catalog）不变，doctor 探测与 agent 选择面随之扩大 |
| P5 | claude 二进制哲学：adapter 包自带 CLI vs 沿用本机探测链 | **本机 `resolveClaudeBin` 注入 `CLAUDE_CODE_EXECUTABLE` + adapter vendored 安装（T1.3）**。Gold-Band 为零分发路线（app 不带 node/adapter/claude 任何二进制，catalog 编译进二进制，运行时 `npx -y` 现场拉起，SDK 平台包自带 claude 由 npm 在用户机按平台解析、use_local_claude 默认关）——本项目不采用 |
| P6 | ACP 模式权限表达：bot 级「执行权限」开关（allowedTools 白名单）是否废弃，改用 ACP 原生权限模式 | **彻底废弃**（含终端模式 headless 路径）——权限上移 `launch_settings.permissionMode`（一期 UI 两档：需要审批=acceptEdits / 自动=bypassPermissions，经 `session/set_mode` 下发 + agent mode 目录校验），桌面与 bot 共用；permissionMode 同样 workspace 设默认、issue 可按需修改（与 P1 同形态）；「执行权限」配置项连同 allowedTools 全链路移除（清理任务 T2.0），headless 驱动恒用默认白名单（调研依据见 §1.4） |

## 5. 风险与开放问题

1. **acp-go 依赖风险（D8）**：协议实现风险已由库消解（wire 类型由官方 JSON Schema 生成、进程管理 runner 现成），余下为供应链 churn——公开仓仅 3 周、发版快（v0.8→v0.11），协议 v2 演进期 breaking change 属预期内。缓解：钉精确 tag + 会话域薄适配层隔离（换库/fork 只动一层）+ Apache-2.0 随时可 fork；超时/取消/进程组回收/崩溃恢复仍以 T1.1 实施定稿逐项收口；
2. **adapter 版本演进**：官方 adapter 两周三版（Gold-Band pin 0.81.2 → 现 0.84.0），catalog pin 策略 + 升级验证流程在 T1.2/T1.3 定稿；已知坑佐证：`claude-code-acp` 0.16.x 存在 MCP tool discovery 竞态（社区回钉 0.15.0），版本升级必须过 T1.4 握手回归；
3. **终端模式与 ACP 模式并存边界**：同一 issue 切换模式时的会话接续（`claude --resume` 可跨形态接续同一 session id）——按 P3 拍板不入一期；
4. **企微回调 5 秒窗口**：审批点击后更新原卡必须在 5 秒内完成，跨 sidecar 重启的边界场景在 T3.2 测试覆盖。
5. **bypassPermissions 的运行时开启路径（P6 新增关注）**：ACP 模式下权限模式可经 `session/set_mode` 运行时变更（headless 时代 argv 定死、不存在该路径），IM 端若不过滤即构成远程提权面。缓解：T2.4/T3.2 IM 安全过滤（bypass 类动作仅桌面可操作）+ mode 值对 agent 上报目录校验；autoAccept（client 代点放行）记为后续增强、一期不提供。

---

## 6. 任务清单（进度 SSOT）

> 任务粒度：模块 + 功能 + 技术方案，一个任务 ≈ 一次可独立验收的交付。
> 跨阶段可并行项已用依赖关系标出（如 T3.1 不阻塞于阶段 1/2）。

### 前置

#### T0.0 拍板确认 P1–P6 决策点

**状态**：✅

**内容**：与用户逐项确认 §4 的六个决策点。各决策点「倾向」即本任务清单当前采用的工作假设——拍板结果与倾向不一致时，先修订受影响任务的技术方案与关联设计段落，再开工对应任务。

**依赖**：无（一切开发任务的前置；阶段 0/1 中不依赖拍板结果的部分可先行）

**实施定稿**：P1–P6 拍板结果即 §4 表；受影响任务已按结果就地修订（P1 → 新增 T0.4；P4 → spawn 策略表 / T0.2 / T1.2 / T1.3 / T1.4；P6 → 阶段 0 permissionMode / T2.0 / T2.1）。纯决策任务，无代码交付物。

---

### 阶段 0：workspace 级 launch_settings

**设计要点**：

- 现状：「应用嵌入终端 - 启动设置」是全局 appConfig 单值 `terminal_startup_code_cli`（`'none' | 'claude'`，默认 `none`，存 `app.db` 的 `app_config` 表，`app/src/shared/app_config.rs:51`，SSOT 在 `packages/web/src/shared/appConfig.ts:71-79`）；消费端两处——`EmbeddedTerminal.tsx:86-117`（仅主 pane 直启，分屏 pane 恒裸 shell）与 `WorkspaceInitGate.tsx:94`（引导文案）；`t_workspaces` 仅 `name / dir / description` 三列，无配置承载。
- **取值优先级**：`workspace.launch_settings`（新）→ 全局 `terminal_startup_code_cli`（过渡回落）→ 默认 `none`；终态再叠 issue 级覆盖（issue → workspace → 全局，P1 拍板，T0.4 补充）。
- JSON 列而非多列 = D6（即将承载 mode + agentCode + 后续 agent 级配置，避免反复迁移）：

```json
{
  "mode": "terminal-manual | terminal-auto | acp",
  "agentCode": "claude-acp",
  "autoCommand": "claude",
  "permissionMode": "acceptEdits | bypassPermissions"
}
```

- `permissionMode`（P6）：仅 ACP 模式消费——经 `session/set_mode` 下发（T1.5），一期 UI 两档映射（需要审批=acceptEdits / 自动=bypassPermissions）；终端模式两档忽略该字段
- 改表约定：直接编辑原迁移文件、不新建 goose 迁移、不考虑历史数据（本项目既有约定），改完跑 `pnpm server:gorm:gen`。
- **本阶段是阶段 2 的前置**：bot 发起会话（T2.1/T2.3）将直接读同一份 workspace 级 launch_settings（含 `permissionMode`，桌面/bot 双入口共用权限语义，P6）；`agentCode` 字段与阶段 1 catalog（T1.2）耦合，故与阶段 1 排在一起做。

#### T0.1 `t_workspaces` 新增 launch_settings 列（数据链路）

**状态**：✅

**功能**：workspace 级启动配置的存储与后端读写链路

**技术方案**：
- 按改表约定编辑原迁移文件为 `t_workspaces` 加 `launch_settings` JSON 列，跑 `pnpm server:gorm:gen` 重新生成 DO 层（`workspaces.gen.go`）
- `server/internal/dal/types/workspace.go` DTO + tracker/workspace 域服务透传
- 空值语义：NULL → 消费端走回落链（不把 NULL 当非法态）

**依赖**：无

**决策关联**：D6；P1（issue 级若再覆盖一层，影响读取优先级链的最终形态，拍板后修订）；P6（`permissionMode` 字段随 launch_settings 落库）

**实施定稿**：
- 列形态 `launch_settings TEXT NOT NULL DEFAULT ''`——空串（而非 NULL）= 未配置 → 消费端回落链（NOT NULL 列 + 空串哨兵，对齐 `sub_dir_list` 空串跳过解析范式）
- 响应形态偏离方案原样：GetList/GetInfo/Create/Update 从「DO model 直出」改为 `WorkspaceResponseData` + `FromModel`（launch_settings 反序列化为结构，空串/损坏 → nil）——对齐 im_bot 域 JSON 列响应范式，避免 string 形态漏给前端
- 请求侧 `LaunchSettings *WorkspaceLaunchSettings` 指针可空（nil = 未配置/清空），DTO 透传不校验取值域（mode/agentCode/permissionMode 枚举由前端表单约定）
- `agentCode` 定型为枚举：Go 侧 `enums.AgentCode`（dal/enums/workspace.go，四常量；launch_settings JSON 内部枚举无独立写库路径故无 Valuer，值域校验经 DTO binding oneof）+ TS 字面量联合；T1.2 catalog 落地后取值域 SSOT 移交 catalog、oneof 同步放宽（保持「加 agent = 加条目零代码」）
- 前端 `WorkspaceService.ts` 同步补 `WorkspaceLaunchSettings` 类型与字段（对象直传）；与 T2.0 同批实施，表变更一次完成

#### T0.2 WorkspaceDrawer 启动设置表单

**状态**：✅

**功能**：workspace 编辑抽屉增加「启动设置」区块

**技术方案**：
- `WorkspaceService.ts` 模型/请求字段补 `launchSettings`
- `WorkspaceDrawer.tsx` 表单区块：mode 三选一（终端手动 / 终端自动 / ACP）+ ACP 模式下 agentCode 选择（一期四选项 claude-acp / codex / opencode / pi，P4 拍板；catalog 未就绪前先以常量占位，T1.2 落地后切换数据源）+ ACP 模式下执行模式两选一（需要审批=acceptEdits / 自动=bypassPermissions，写 `permissionMode`，P6 拍板）
- 空值 = 跟随全局回落（不强制填写），存量用户行为不突变

**依赖**：T0.1

**实施定稿**：启动模式下拉首项「跟随全局」即空值位（`value=''`，提交时不传 launchSettings，后端落空串）；MUI 对空串选项需显式双配置——`select.displayEmpty`（空值是有效选项，选中项文本正常渲染）+ `inputLabel.shrink`（InputBase 的 filled 判定不含空串，label 显式常驻收缩，否则与内容重影）；ACP 会话展开 Agent（默认 claude-acp）与执行模式（默认需要审批=acceptEdits）两下拉，切回终端模式不丢草稿；autoCommand 一期不出输入框（固定 claude，消费端以 mode 判断）；新增文案中文直出不加 i18n key（对齐 ImBotDrawer 约定，i18n 维持既有页面维度）；`services/index.ts` barrel 补导出 `WorkspaceLaunchSettings`

#### T0.3 消费端接入 workspace 级值

**状态**：⬜

**功能**：终端启动行为改由 workspace 级配置驱动，全局 key 降级为回落值

**技术方案**：
- 取值优先级落地：`launch_settings.mode`（`terminal-manual` ≈ 现 `none`、`terminal-auto` ≈ 现 `claude`）→ 全局 `terminal_startup_code_cli` → 不启动
- `EmbeddedTerminal.tsx`（现 86–117 行全局 key hook）改为接收父层解析好的终值——**保住「就绪闸门 → fit 实测 → 以实测尺寸 spawn」时序不破**，workspace 值未就绪时闸门不放行（编码规则 1）
- `WorkspaceInitGate.tsx`（现 94 行）引导文案同步改
- `acp` 模式本阶段无消费端（T1.6 落地），选中时终端侧按「不自动启动」处理
- 分屏 pane 行为维持现状（仅主 pane 受启动设置影响）

**依赖**：T0.1、T0.2

#### T0.4 issue 级 launch_settings 覆盖

**状态**：⬜

**功能**：issue 可按需覆盖 workspace 级启动设置（P1 / P6 拍板终态：整个 launch_settings——mode / agentCode / permissionMode——均可 issue 级调整）

**技术方案**：
- `t_issues` 加 `launch_settings` JSON 列（同构 workspace，D6），改表走既有约定（编辑原迁移 + `pnpm server:gorm:gen`）
- 字段级合并：issue 级只覆盖显式写过的键，未覆盖键回落 workspace 级，再回落全局 `terminal_startup_code_cli`
- 消费端读取链从 T0.3 的 workspace 级升级为链式解析（终端 spawn 闸门时序不变，编码规则 1 不破坏）
- 覆盖 UI 落 issue 主窗口的模式切换入口（与 T1.6 ACP 视图协同最顺，实施顺序可后置、不阻塞阶段 0 交付）

**依赖**：T0.3

**决策关联**：P1、P6

---

### 阶段 1：通用 ACP client + issue 主窗口 ACP 模式

**设计要点**：

- issue 执行模式扩为三种，终端模式零改动、风险隔离：

```
issue 主窗口
├── 终端模式（现状不动）：PTY + EmbeddedTerminal，手动/自动启动 claude 均保留
└── ACP 模式（新增）：主窗口变为会话视图
    └── 底部任务描述输入框，回车 → session/prompt → ACP 驱动 claude
```

- **ACP client 放 Go sidecar**（D1）：bot 复用是刚需且 orchestrator 就在 sidecar（Gold-Band 的 IM runtime 与 ACP 同进程同理）；workspace/issue/MCP/HTTP 全在 sidecar，panel 走现有 HTTP + SSE 拿事件流；放 Rust 则阶段 2 还要架一层 Rust→Go 桥，两次跨进程。
- **实现定位：库依赖 + 薄适配层（D8）**——wire 类型与 stdio JSON-RPC 帧由 `github.com/BrokkAi/acp-go` 承担（Apache-2.0、零传递依赖、类型由官方 JSON Schema 自动生成、自带 runner 进程管理）。Go 侧自研收窄为三件事：catalog 驱动的 adapter spawn 与进程组管理、`initialize` 能力协商分支、三个入站回调（`session/update` / `session/request_permission` / `elicitation/create`）到会话域的适配。最小入站面有实证：Gold-Band 仅处理这 3 个入站方法，`fs/*` 等其余一概回 -32601 也能跑通 claude-acp。
- **参照物分层（license 意识）**：协议语义权威 = ACP 公开 schema + 官方 TS SDK `agentclientprotocol/sdk`（npm）；Go 实现备选 = `ironpark/acp-go`（MIT，同样要求 Go ≥1.27）；Gold-Band 仅行为语义与工程 checklist 参照（stderr 分类、会话路由背压、取消排水、doctor 握手流程）——**其代码 AGPL-3.0-only，禁止代码级移植**。
- **第一天就按通用 adapter 写**（D5/D2）：client 只认 `command + args`，Claude 专属行为全部走 `initialize` 能力协商分支（如检测 `/_meta/claudeCode` 扩展），不进主干代码。
- **spawn 策略表**（D3：官方 adapter 起步），catalog 中每个 agent 声明自己的启动方式，client 无感：

| 策略 | 适用 | 启动形态 |
|---|---|---|
| `npx-adapter` | claude（P4 启用之一） | catalog 原始形态 `npx -y @agentclientprotocol/claude-agent-acp@<pin>`；实际 spawn 指向受管目录 vendored 入口（T1.3 一次性安装） |
| `native-acp` | codex / opencode / pi（P4 启用） | 直连各自 CLI，零 adapter；条目命令以 registry 离线快照为准（Gold-Band catalog 的「PATH 可执行文件」模板同构） |
| `in-process-go` | **预留，不实现** | Go 进程内实现 ACP agent 角色（`driver_claude.go` 的 stream-json 积累可复用） |

- 为什么官方 adapter 起步而不是直接 Go 自研——成本分布：

| | 官方 npx adapter | Go 进程内 adapter |
|---|---|---|
| 环境成本 | 需 node + npx（见 T1.4） | 零新增 |
| 协议维护 | **零**（claude CLI 协议演进由官方 adapter 团队追，两周三版的跑步机不归我们） | **全扛**（企微 SDK 转义 Go 划算是因为企微协议三年不变；claude 控制协议是反例） |
| 供应链维护 | 版本 pin + 升级验证（catalog 锁版本，刷新脚本统一升） | 无 |

- **Go 进程内 adapter 的触发条件**（满足任一再启动，届时沉没成本接近零）：① doctor 数据显示相当比例用户机器缺 node（原生安装器装 claude 的那批）；② 目标场景包含离线/内网环境；③ node 中间层出现真实痛点（启动延迟、僵尸进程、每会话多一个 node 进程的内存开销）。
- **node 依赖**（D4）：不随 app 打包（包体 +几十 MB、三平台构建矩阵变重）；目标用户装 Claude Code，npm 安装路径必然带 node，原生安装器路径可能没有——不猜，doctor 探测，缺失时 UI 明确引导。

#### T1.0 server Go 工具链升级（1.25.7 → 1.27）

**状态**：⬜（执行时机：T1.1 开工前，经用户确认本地 go 环境就绪）

**功能**：为 acp-go 依赖铺路——两候选库均要求 Go ≥1.27（BrokkAI 1.27.1 / ironpark 1.27.0）

**技术方案**：`server/go.mod` go directive 升至 `1.27.1`；CI 已用 `go-version-file: server/go.mod` 锁版（`ci.yml` / `release-assets.yml`），改 go.mod 即自动同步，无需另改 workflow；`go build ./...` + `go vet ./...` 全量验证。环境事实：本机 go 为 gvm 管理的 1.25.12、`GOTOOLCHAIN=auto`，go.mod 声明 1.27.1 后可自动下载并切换工具链（build/vet 已实测通过）——届时无需单独安装新版 go

**依赖**：无

**决策关联**：D8

---

#### T1.1 Go ACP client 核心

**状态**：⬜

**功能**：sidecar 内通用 ACP client（JSON-RPC over stdio），可驱动任意 catalog 声明的 ACP agent

**技术方案**：
- 依赖 `github.com/BrokkAi/acp-go`（D8，pin v0.11.0）：wire 类型（官方 JSON Schema 生成）与 stdio JSON-RPC 帧由库承担；前置 T1.0（Go ≥1.27）
- 薄适配层（自研部分）：方法集经库类型使用——`initialize / session/new / session/prompt / session/request_permission / session/update / elicitation/*`；catalog 驱动的 adapter spawn + 进程组回收；Claude 专属行为走 `initialize` 能力协商分支，不进主干
- acp-go 自带 `Permissions` interface（`RequestPermission` 回调）与 `CancellablePermissions` 包装（ctx 取消自动回 cancelled，v2/permissions.go），入站审批回调适配直接可用
- 工程细节清单（风险 §5.1，实施定稿逐项收口）：超时、取消、进程组回收、崩溃恢复
- 行为语义参照 Gold-Band `src/acp/client.rs`、`src/acp/adapter.rs:52-180`（仅行为语义，AGPL-3.0 禁止代码移植）

**依赖**：T1.0

**决策关联**：D1、D2、D5

**验收**：以 catalog 条目拉起 claude-acp adapter，完成 initialize → session/new → 一轮 prompt → session/update 收流 → 进程正常回收

#### T1.2 agent catalog（离线 pin）

**状态**：⬜

**功能**：agent 目录数据源与刷新脚本，一期只启用 claude-acp

**技术方案**：
- 结构照 Gold-Band：官方 ACP registry 的离线 pin 快照 + 刷新脚本，运行时不在线拉取；落在 sidecar 内嵌资源
- 每条目声明 spawn 策略与启动参数（`npx-adapter` / `native-acp`；`in-process-go` 仅预留枚举）
- 一期启用 claude / codex / opencode / pi 四条目（P4 拍板），catalog/doctor 的 UI 面板仍砍到最小；native 三家的具体启动命令以 registry 快照为准，不在本文硬编码
- adapter 版本 pin 进 catalog（参考 0.8x 线），升级走刷新脚本统一升 + 验证流程定稿（风险 §5.2）

**依赖**：无

**决策关联**：D2、D3、P4

#### T1.3 adapter vendoring 与 claude 路径一致性

**状态**：⬜

**功能**：压掉官方方案的网络不确定性，并保证 ACP 模式与终端模式用同一个本机 claude

**技术方案**：
- Vendoring：首次在受管目录（如 `~/.ocean-harness/acp-adapters/`）`npm install` 固定版本，之后 spawn 指向 vendored 入口——「每次 npx 拉包」变「一次性安装」，离线可用，升级走自己的版本策略（仅覆盖 `npx-adapter` 策略即 claude；codex / opencode / pi 走 native CLI 直连，无需 vendoring）
- claude 路径一致性：沿用现有 `resolveClaudeBin` 三级探测链（`server/internal/bot/bin.go:38-68`）结果，经 adapter 的可执行文件指定口子（`CLAUDE_CODE_EXECUTABLE` 类环境变量）传入——ACP 模式与终端模式共享同一份 claude，不出现两套 CLI 版本漂移

**依赖**：T1.2

**决策关联**：D3、P5

#### T1.4 doctor 握手探测

**状态**：⬜

**功能**：agent 可用性判据 = 真实握手跑通；结果供 UI 引导与（阶段 3）bot 徽标消费

**技术方案**：
- 四项探测：claude 可用（`resolveClaudeBin`）、node/npx 可用（版本下限跟随 catalog pin 的 adapter engines 要求——当前 0.84.0 为 ≥22）、adapter 包就绪（vendored 入口存在）、native agent CLI 可用（codex / opencode / pi 各自 PATH 解析，P4——探测思路复用 `bin.go` 的 login shell 兜底）
- 握手判据：真拉起子进程跑 `initialize → setup_session → available_commands` 再清理，跑得通才 healthy（照 Gold-Band `src/acp/client.rs:2673-2731`）
- node 缺失时 UI 明确引导：「ACP 模式需要 Node.js ≥20，或切换终端模式」，不运行时报错

**依赖**：T1.1、T1.3

**决策关联**：D4

#### T1.5 sidecar ACP 会话域与事件流

**状态**：⬜

**功能**：sidecar 持有并管理 ACP session 生命周期，issue↔session 绑定，事件流推 panel

**技术方案**：
- 会话域：ACP session 的创建/复用/回收；issue → ACP session 绑定关系落库
- 事件流经 sidecar HTTP/SSE 推 panel（`session/update` / `request_permission` / `elicitation` 事件）
- 消费 workspace `launch_settings.agentCode`（T0.1 链路）决定拉起哪个 agent
- 权限模式（P6）：会话创建后按 launch_settings `permissionMode`（workspace 设默认、issue 可按需修改，T0.4）经 `session/set_mode` 下发，值对 agent 上报的 mode 目录校验（防硬编码失效）；一期两档「需要审批」（acceptEdits）/「自动」（bypassPermissions）
- 并发基线：单会话内回合串行（跨入口回合锁在 T2.2 升级）
- **D7 受控反转**：issue↔session 绑定是对当初随 chat 视图删除 `claude_session_ref` 决策的受控反转（`app/src/pty/claude_state.rs` 注释），不是回退——实施定稿须明示此点

**依赖**：T1.1、T0.1

**决策关联**：D1、D7；P3（跨模式 `--resume` 接续不入一期）；P6（permissionMode 经 set_mode 下发）

#### T1.6 issue 主窗口 ACP 视图

**状态**：⬜

**功能**：issue 主窗口新增 ACP 模式（第三执行模式）：会话视图 + 底部任务描述输入框

**技术方案**：
- 模式切换：读 launch_settings `mode`（P1 拍板：workspace 设默认、issue 级覆盖经 T0.4）；终端模式两分支零改动
- 会话视图：渲染 `session/update` 消息流与工具调用（参照 Gold-Band ConversationComposer 交互）
- 底部输入框回车 → `session/prompt`（经 T1.5 会话域）
- 服务地址从 Rust `http_server_status` 命令获取，不硬编码端口；SSE 订阅遵循现有前端服务范式

**依赖**：T1.5、T0.3

#### T1.7 桌面侧审批与选择交互

**状态**：⬜

**功能**：`session/request_permission` 与 `elicitation/*` 在桌面侧呈现为弹窗/面板——阶段 1 即获得审批能力，不等 bot

**技术方案**：
- request_permission → 审批弹窗（选项呈现 + 结果回写）
- elicitation → 表单/选择面板
- 交互结果经 T1.5 会话域回写 ACP 会话
- 阶段 2 T2.4 在此之上做双入口收敛

**依赖**：T1.5、T1.6

---

### 阶段 2：bot 复用 ACP 会话

**设计要点**：

- bot 的 `ClaudeDriver` 接口当初即注释「为将来引擎预留」（`server/internal/bot/driver.go:8`），ACP driver 成为第二个实现，渠道无关核心零改动：

```
bot 会话（企微）──┐
                  ├──→ 同一个 ACP session（sidecar 持有）──→ claude
issue 主窗口 ─────┘
```

- **无缝切换机制**（照抄 Gold-Band 的正确之处）：不共享进程句柄、不复制消息历史，共享「ACP 会话 + 待审批状态」——UI 和 bot 是同一会话的两个客户端，历史在 claude 自己的 session 存储。
- bot↔issue 映射现状：`single:<userid>` / `group:<chatid>` 会话键绑 workspace（`server/internal/bot/wecom/inbound.go:33-46`）。
- 双入口审批收敛：first-writer-wins + CAS，后点方收到「已在另一端处理」（照 Gold-Band `inbound.rs:239-261` 幂等收敛）。

#### T2.0 bot 执行权限配置项清理

**状态**：✅

**功能**：按 P6 彻底废弃 bot 级「执行权限」配置——allowedTools 全链路移除，headless 驱动恒用默认白名单

**技术方案**：
- 前端：`ImBotDrawer.tsx` 删「执行权限」下拉及 `EXEC_PERMISSIONS` / `TOOLS_WITHOUT_BASH` / 双向转译函数；`ImBotService.ts` 请求模型删 `allowedTools` 字段
- 改表：`t_im_bots.allowed_tools` 列移除（直接编辑原迁移文件 + `pnpm server:gorm:gen`，既有改表约定）
- server：`types.go` 删 `ParseAllowedTools` / `BotRuntimeConfig.AllowedTools` / `TurnRequest.AllowedTools`（`DefaultAllowedTools` 保留为 headless 恒用白名单）；supervisor 装配、service/DTO、orchestrator 的 TurnRequest 组装、`driver_claude.go` 的 `--allowedTools` argv 拼装同步清理
- 行为变化：现有 readonly bot 恢复 Bash（默认白名单），「访问模式」准入层不受影响

**依赖**：无（独立可交付，可提前于阶段 2 其余任务实施）

**决策关联**：P6

**实施定稿**：与 T0.1 同批实施（表变更一次完成：t_workspaces 加列 + t_im_bots 删列同一次迁移编辑 + gorm:gen 重生成）。`--allowedTools` argv 从条件拼装改为恒拼 `DefaultAllowedTools()`（白名单不可再按 bot 收紧）；`gen_model_im_bot.go` 的 JSON 列约定注释同步去掉 allowed_tools。dev 库按改表约定删除重建（goose 不重放已应用迁移）。

#### T2.1 bot ACP driver

**状态**：⬜

**功能**：`ClaudeDriver` 接口的 ACP 实现——bot 回合经 sidecar 内 ACP session 驱动 claude

**技术方案**：
- 实现 `server/internal/bot/driver.go` 接口，经 T1.5 会话域取 ACP session
- 渠道无关核心（orchestrator、回复合成）零改动
- headless spawn 路径（`driver_claude.go`）保留：workspace 未配 ACP 模式时 bot 走现状，两驱动并存
- 权限面（P6）：ACP driver 不消费 bot 级 `allowed_tools`——ACP 会话权限由 launch_settings `permissionMode` 统一表达（T1.5 下发）；bot 级「执行权限」配置项彻底废弃（含终端模式），headless 驱动恒用默认白名单，全链路清理见 T2.0

**依赖**：T1.5

**决策关联**：P6

#### T2.2 per-session 跨入口回合锁

**状态**：⬜

**功能**：UI / bot 两入口抢同一 ACP 会话时的回合互斥

**技术方案**：
- 现有 orchestrator 每会话队列（`server/internal/bot/orchestrator.go:115-135`）升级为 per-session 跨入口回合锁
- ACP 会话内回合天然串行，锁解决的是「两入口同时发起回合」的抢跑

**依赖**：T2.1

#### T2.3 bot↔issue 显式绑定与 IM 内切换

**状态**：⬜

**功能**：bot 会话显式绑定目标 issue（避免隐式状态跳变）

**技术方案**：
- 现状会话键绑 workspace；扩展为显式绑 issue + IM 内切换（如 `#issue-42 帮我…`，P2 已拍板确认此形态）
- bot 发起回合读 workspace launch_settings（阶段 0 铺好的链路）

**依赖**：T2.1

**决策关联**：P2

#### T2.4 双入口审批收敛

**状态**：⬜

**功能**：企微 / 桌面双入口审批 first-writer-wins

**技术方案**：
- CAS + first-writer-wins：谁先点谁生效，后点方收到「已在另一端处理」
- 审批 pending 状态由 T1.5 会话域持有，两入口读同一份状态
- IM 安全过滤（Gold-Band `intervention.rs` requires_desktop 同款语义）：bypass 类危险授权动作在 IM 端只保留「安全拒绝」类动作，开启/放行必须回桌面操作——防 `session/set_mode(bypassPermissions)` 被远程开启（依据见 §1.4）

**依赖**：T2.2、T1.7

---

### 阶段 3：交互打磨

**设计要点**：

- 接线总则：适配器注册 `OnTemplateCardEvent` → 规范化为交互事件进核心编排；出站侧在 `ReplyStream` 之外扩 `ReplyCard` / `UpdateCard` 能力接口（保持「核心不感知渠道」约束），SDK 零改动。
- 场景映射（SDK 能力均已具备、未接线）：

| 场景 | ACP 事件 | 企微落地 |
|---|---|---|
| 思考/流式 | `session/update`（textDelta） | 现有 PumpReply 覆写式中间帧直接复用（`stream.go:36-140`） |
| 人工审批 | `session/request_permission` | `vote_interaction` 单选卡（allow_once/allow_always/reject → 中文选项，默认选第一项）+ `UpdateTemplateCard` 置灰终态 |
| 单选/多选确认 | `elicitation/create` | checkbox mode 0/1 投票卡 / `multiple_interaction` 多题卡 |
| 工具执行状态 | `session/update`（toolCall） | 现有「🔧 正在执行…」状态行 |
| 超长输出 | — | 现有 `.bot-outbox` 落盘机制不变 |
| agent 健康度 | doctor | bot 状态徽标 |

- **企微协议参数照抄清单**（Gold-Band 真实 PoC 校准，适用于本阶段所有卡片任务）：文案截断（标题 26 / desc 30 / 选项 11 字）、`horizontal_content_list` ≤6 项、`task_id` 复用 deterministic delivery_id、按钮 key 只允许 `delivery_id:action_index`、原卡置灰保持 `card_type`/`task_id`/`submit_button.key` 一致（设 `checkbox.disable=true`，**不发不存在的 `submit_button.disable`**，否则 42049）、回调 5 秒更新窗口（跨 sidecar 重启边界见风险 §5.4）。

#### T3.1 企微卡片通道接线

**状态**：⬜

**功能**：打通企微模板卡片收发通道——出站卡片能力接口 + 点击事件入站

**技术方案**：
- wecom 适配器注册 `OnTemplateCardEvent`（SDK `message-handler.go:124`；现状 `wecom/channel.go:100` 只注册消息类回调，点击事件被静默丢弃），规范化为交互事件进核心编排
- 核心渠道契约（`channel.go:17` `ReplyStream` 抽象）之外扩 `ReplyCard` / `UpdateCard` 能力接口；企微实现用 SDK `ReplyStreamWithCard`（`client.go:332`）与 `UpdateTemplateCard`（`client.go:366`）
- 其他渠道（飞书）不实现新接口则不受影响；SDK 零改动

**依赖**：无（可与阶段 1 并行开发；产生真实卡片流量需 T2.4 就绪）

#### T3.2 审批 vote_interaction 卡

**状态**：⬜

**功能**：`session/request_permission` → 企微单选审批卡 + 终态置灰

**技术方案**：
- 选项映射 allow_once / allow_always / reject → 中文选项，默认选第一项
- 点击后 `UpdateTemplateCard` 置灰终态（5 秒窗口内），与桌面侧按 T2.4 first-writer-wins 收敛
- 遵守企微协议参数照抄清单（阶段 3 设计要点）
- IM 侧仅暴露安全动作：bypass 类危险授权动作 IM 端过滤（与 T2.4 安全过滤一致），仅桌面可操作

**依赖**：T3.1、T2.4

#### T3.3 elicitation 单选/多选卡

**状态**：⬜

**功能**：`elicitation/create` → 企微选择卡

**技术方案**：
- 单选：checkbox mode 0 投票卡；多选：checkbox mode 1 / `multiple_interaction` 多题卡
- 提交结果经 T1.5 会话域回写 ACP 会话；终态更新同 T3.2

**依赖**：T3.1、T2.4

#### T3.4 流式/思考与工具状态呈现

**状态**：⬜

**功能**：bot 侧复用现有回复泵呈现 ACP 流式事件

**技术方案**：
- `session/update`（textDelta）→ 现有 PumpReply 覆写式中间帧直接复用（`stream.go:36-140`）
- `session/update`（toolCall）→ 现有「🔧 正在执行…」状态行
- 超长输出：现有 `.bot-outbox` 落盘机制不变

**依赖**：T2.1

#### T3.5 doctor 健康度进 bot 状态徽标

**状态**：⬜

**功能**：bot 状态徽标展示 agent doctor 健康度

**技术方案**：
- T1.4 doctor 结果经 HTTP API → bot 状态徽标（不健康时置灰 + 引导文案）

**依赖**：T1.4、T2.1

---

## 附：调研引用索引（关键代码位置）

**本项目**：`server/internal/bot/`（`orchestrator.go:73-239` 回合编排、`driver_claude.go:26-103` headless spawn、`stream.go:36-140` 回复泵、`channel.go:17-44` 渠道契约、`bin.go:38-68` claude 探测、`supervisor.go:106-108` 装配）、`server/internal/bot/wecom/`（`channel.go:223-236` 消费循环、`reply.go:15-60` 流式回复、`inbound.go:33-46` 会话键）、`app/src/shared/app_config.rs:51` 配置表、`packages/web/src/shared/appConfig.ts:71-79` 启动 key SSOT、`EmbeddedTerminal.tsx:86-117/180-194` 消费与 spawn、`t_workspaces`（`workspaces.gen.go`）。

**Gold-Band**：`src/app/intervention.rs:366-426,915-1030`（干预命令服务 + 权限选项映射）、`src/acp/adapter.rs:52-180`（通用 adapter + 能力协商）、`src/acp/client.rs:2673-2731`（doctor）、`src/acp/permission.rs / elicitation.rs`（pending 落盘等待）、`src/im/inbound.rs:192-261`（入站动作 + 幂等收敛）、`src/im/connectors/wecom.rs:612-1243`（vote_interaction 卡构造、终态更新）、`resources/agent-catalog.json` + `scripts/prepare-agent-catalog.mjs`（catalog pin）、`src-tauri/src/im_runtime.rs:1159-1246`（IM→命令服务汇合）。**License：AGPL-3.0-only**（仅行为语义参照，禁止代码级移植）。

**开源库选型（D8 依据）**：`github.com/BrokkAi/acp-go`（Apache-2.0，**选定**，pin v0.11.0，Go ≥1.27.1，零传递依赖，wire 类型由官方 JSON Schema 生成，含 runner 进程管理与 cookbook）、`github.com/ironpark/acp-go`（MIT 备选，Go ≥1.27，多 transport，无 tag 可钉）、官方 TS SDK `agentclientprotocol/sdk`（npm，协议语义权威参照）。

**权限语义调研（P6 / §1.4 依据）**：本项目 `packages/web/src/windows/panel/ImBotsPage/ImBotDrawer.tsx:44-60`（执行权限两档 ↔ allowedTools 转译）、`server/internal/bot/types.go:17-42`（白名单 SSOT 与空值语义）、`server/internal/bot/driver_claude.go:32-50`（argv 拼装）；Gold-Band `src/acp/permission.rs:107-307`（pending/response 落盘与 first-writer-wins 预约）、`src/app/intervention.rs:915-1100`（选项归类与 requires_desktop 安全过滤）、`src/acp/client.rs:4927-4977`（mode 经 set_config_option 下发）、`src/acp/adapter.rs:58-113`（disallowedTools deny-list 注入）；官方 `github.com/agentclientprotocol/claude-agent-acp`（src/acp-agent.ts：`_meta.claudeCode.options` 透传面与 permissionMode 排除、src/permissions/*：mode 目录与 allow_always 持久化、Node ≥22、两仓库合并事实）、ACP 协议文档 `docs/protocol/v1/tool-calls.mdx`（`RequestPermissionOutcome` / `PermissionOptionKind`）、`BrokkAi/acp-go`（agent/host.go `Permissions` interface、v2/permissions.go `CancellablePermissions`）。
