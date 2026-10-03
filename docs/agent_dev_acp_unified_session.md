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

- **多 agent 中立**：内嵌 11 个 agent 的 catalog（claude-acp、codex-acp、gemini、cursor 等），数据源是 ACP 官方 registry 的**离线 pin 快照**（`resources/agent-catalog.json` + `scripts/prepare-agent-catalog.ts`），运行时不在线拉取；
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
- **IM 安全过滤（参考方向，非本项目拍板）**：Gold-Band `intervention.rs` requires_desktop 判定——危险权限动作（bypass 类、未知 kind 等）IM 端只保留「安全拒绝」、开启/放行必须回桌面。bot 与桌面 App 是等价客户端、无依赖关系，是否按此过滤 T2.4 再议（T3.2 同向收录）

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
| D8 | Go ACP client 直接依赖 `github.com/BrokkAi/acp-go`（pin v0.11.0）+ sidecar 会话域薄适配层 | Apache-2.0 / 零传递依赖 / wire 类型由官方 JSON Schema 自动生成 / 有 tag 可钉 / 自带 runner 进程管理；自研收窄为 spawn、能力协商、入站回调适配三件事。风险隔离：会话域薄适配层 + 钉精确 tag + license 干净随时可 fork；Go 工具链随 T1.0 升 1.27。选型对比终态：`coder/acp-go-sdk` 外部生态最大（Docker/Pomerium/Plural 等约 74 个 module 集成）但维护冻结（schema 停更、落后上游数版、issue/PR 无人处理、elicitation 标 UNSTABLE）；`ironpark/acp-go` 方法覆盖最全、协议追踪最新，但仓库无 tag 可钉且为个人项目；`beyond5959/acp-adapter` 为 agent 侧 adapter 非 client 库、claude 桥权限应答 no-op，排除。acp-go 胜出依据见附录四库对比终态与风险 §5.1 |

## 4. 决策点（P1–P6，已于 T0.0 拍板）

| # | 问题 | 拍板结果 |
|---|---|---|
| P1 | ACP 模式粒度：per-issue 配置 vs 全局默认 + issue 可覆盖 | **per-issue**——workspace 级设默认，issue 按需自行调整（覆盖范围为整个 launch_settings：mode / agentCode / permissionMode）；阶段 0 先落 workspace 层，issue 级覆盖为补充任务 T0.4 |
| P2 | bot↔issue 映射：显式绑定 + IM 内切换（如 `#issue-42 帮我…`） vs 自动挂 workspace 当前活跃 issue | **显式绑定 + IM 内切换** |
| P3 | 终端模式 ↔ ACP 模式跨模式会话接续（`--resume` 捞回）是否纳入一期 | **不入一期**——纯 ACP 会话先行，跨模式接续为后续增强 |
| P4 | agent 范围：一期 catalog 只放 claude-acp vs 直接全量 | **一期只落 claude**——catalog 仅启用 claude-acp 条目；通用架构（单一 adapter + catalog）不变，codex / opencode / pi 留条目模板与 spawn 策略口子（扩展 = 加条目），doctor 探测与 agent 选择面随一期收窄 |
| P5 | claude 二进制哲学：adapter 包自带 CLI vs 沿用本机探测链 | **vendored 自带 claude 全链路 SSOT**：终端直启 / ACP / bot headless / marketplace 统一消费 adapter vendored 安装内包的 SDK 平台原生 claude 二进制（`CLAUDE_CODE_EXECUTABLE` 显式注入 vendored 绝对路径），claude 路径探测链退役；终端手动路径（裸 shell 敲 `claude` / 手动按钮）保留本机 claude（shell 自解析 = 用户手敲语义）。代价（拍板接受）：claude 版本随 adapter pin 走、app 内 claude 升级能力随 app 发版；已知限制见 §5.6。Gold-Band 零分发路线（app 不带 node/adapter/claude 任何二进制，运行时 `npx -y` 现场拉起、SDK 平台包由 npm 在用户机按平台解析）不采用——本项目以 vendoring 压网络不确定性（T1.3），SDK 自带二进制由「恒不使用的死重」转为统一运行时 |
| P6 | ACP 模式权限表达：bot 级「执行权限」开关（allowedTools 白名单）是否废弃，改用 ACP 原生权限模式 | **彻底废弃**（含终端模式 headless 路径）——权限上移 `launch_settings.permissionMode`（一期 UI 两档：需要审批=acceptEdits / 自动=bypassPermissions，经 `session/set_mode` 下发 + agent mode 目录校验），桌面与 bot 共用；permissionMode 同样 workspace 设默认、issue 可按需修改（与 P1 同形态）；「执行权限」配置项连同 allowedTools 全链路移除（清理任务 T2.0），headless 驱动恒用默认白名单（调研依据见 §1.4） |

## 5. 风险与开放问题

1. **acp-go 依赖风险（D8）**：协议实现风险已由库消解（wire 类型由官方 JSON Schema 生成、进程管理 runner 现成），余下为供应链 churn——公开仓尚新、单人维护、零外部消费者、发版快（v0.8→v0.11），协议 v2 演进期 breaking change 属预期内。对冲事实：Brokk 公司 8 个仓库自治 bot 生产依赖该库（brokk-town monorepo 全部 go.mod 在用），作者响应快（issue 当天修复发版），v1 稳定面 schema pin 策略明文（跟随 Rust SDK，不裸追 schema 仓库）。缓解：钉精确 tag + 会话域薄适配层隔离（换库/fork 只动一层）+ Apache-2.0 随时可 fork；本项目生产路径锚定根包 v1 稳定门面（`SetMode`、prompt 阻塞至终态语义），v2 draft 面不进主干；超时/取消/进程组回收/崩溃恢复仍以 T1.1 实施定稿逐项收口；
2. **adapter 版本演进**：官方 adapter 两周三版（Gold-Band pin 0.81.2 → 现 0.84.0），catalog pin 策略 + 升级验证流程在 T1.2/T1.3 定稿；已知坑佐证：`claude-code-acp` 0.16.x 存在 MCP tool discovery 竞态（社区回钉 0.15.0），版本升级必须过 T1.4 握手回归；
3. **终端模式与 ACP 模式并存边界**：同一 issue 切换模式时的会话接续（`claude --resume` 可跨形态接续同一 session id）——按 P3 拍板不入一期；
4. **企微回调 5 秒窗口**：审批点击后更新原卡必须在 5 秒内完成，跨 sidecar 重启的边界场景在 T3.2 测试覆盖。
5. **bypassPermissions 的运行时开启路径（P6 新增关注）**：ACP 模式下权限模式可经 `session/set_mode` 运行时变更（headless 时代 argv 定死、不存在该路径），IM 端若不过滤即构成远程提权面。缓解：T2.4/T3.2 IM 安全过滤（bypass 类动作仅桌面可操作）+ mode 值对 agent 上报目录校验；autoAccept（client 代点放行）记为后续增强、一期不提供。
6. **vendored 自带 claude 的版本主权（P5 定稿终态引入）**：claude 二进制版本随 adapter pin 走（如 0.84.0 → SDK 0.3.284 → claude 2.1.284），app 终端内的 claude 升级能力随 app 发版；升级动作与 adapter 升级同流程（升 pin → refresh → vendor → 真握手回归）。已知限制：从未安装过 claude 的机器无法在应用内完成首次 OAuth 登录（终端手动路径无本机 claude 可敲，vendored 二进制登录需交互式 TUI）——「受管登录会话」（PTY 直启 vendored claude 供登录）记为后续增强，现阶段 doctor `-32000` 文案引导。

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
- **取值语义**：workspace 启动设置单源，无全局回落——全局 `terminal_startup_code_cli` key 已删除（设置页「启动设置」分区同步移除）；mode 未配置 / `terminal-manual` / `acp` 终端侧均不自动启动，`terminal-auto` = 主 pane 直接 spawn claude；issue 级覆盖（T0.4）叠加时未覆盖键回落 workspace。
- JSON 列而非多列 = D6（即将承载 mode + agentCode + 后续 agent 级配置，避免反复迁移）：

```json
{
  "mode": "none | terminal-manual | terminal-auto | acp",
  "agentCode": "claude-acp",
  "autoCommand": "claude",
  "permissionMode": "acceptEdits | bypassPermissions"
}
```

- 启动模式四档语义（T0.3 定稿）：`none`（默认——issue 就绪后**不进入任何会话**，出「选择启动方式」面板，用户临场三选一再启动；选择仅本次有效，不回写配置不记忆）/ `terminal-manual`（**自动打开终端**：直接进入裸 shell，不自动拉起 Agent，经终端工具条自行选择）/ `terminal-auto`（选中 issue 打开主终端时直接 spawn 所选 Agent）/ `acp`（issue 主窗口 ACP 会话）。`autoCommand` 仅 terminal-auto 档消费（一期 claude = vendored 自带二进制，Rust 直启按 token 解析 vendored 绝对路径；新 CLI 直启在此扩展）；ACP Agent = `agentCode`（catalog 条目）
- `permissionMode`（P6）：仅 ACP 模式消费——经 `session/set_mode` 下发（T1.5），一期 UI 两档映射（需要审批=acceptEdits / 自动=bypassPermissions）；终端模式忽略该字段
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
- 列形态 `launch_settings TEXT NOT NULL DEFAULT ''`——空串（而非 NULL）= 未配置 → 消费端按未配置处理（NOT NULL 列 + 空串哨兵，对齐 `sub_dir_list` 空串跳过解析范式）
- 响应形态偏离方案原样：GetList/GetInfo/Create/Update 从「DO model 直出」改为 `WorkspaceResponseData` + `FromModel`（launch_settings 反序列化为结构，空串/损坏 → nil）——对齐 im_bot 域 JSON 列响应范式，避免 string 形态漏给前端
- 请求侧 `LaunchSettings *WorkspaceLaunchSettings` 指针可空（nil = 未配置/清空），DTO 透传不校验取值域（mode/agentCode/permissionMode 枚举由前端表单约定）
- `agentCode` 定型为枚举：Go 侧 `enums.AgentCode`（dal/enums/workspace.go，四常量；launch_settings JSON 内部枚举无独立写库路径故无 Valuer，值域校验经 DTO binding oneof）+ TS 字面量联合；T1.2 catalog 落地后取值域 SSOT 移交 catalog、oneof 同步放宽（保持「加 agent = 加条目零代码」）
- 前端 `WorkspaceService.ts` 同步补 `WorkspaceLaunchSettings` 类型与字段（对象直传）；与 T2.0 同批实施，表变更一次完成

#### T0.2 WorkspaceDrawer 启动设置表单

**状态**：✅

**功能**：workspace 编辑抽屉增加「启动设置」区块

**技术方案**：
- `WorkspaceService.ts` 模型/请求字段补 `launchSettings`
- `WorkspaceDrawer.tsx` 表单区块：mode 三选一（终端手动 / 终端自动 / ACP）+ ACP 模式下 agentCode 选择（选项常量占位含四个 agent，P4 收窄后一期仅 claude-acp 有效，T1.2 落地后切换 catalog 数据源并收窄选项面）+ ACP 模式下执行模式两选一（需要审批=acceptEdits / 自动=bypassPermissions，写 `permissionMode`，P6 拍板）
- 空值 = 跟随全局回落（不强制填写），存量用户行为不突变

**依赖**：T0.1

**实施定稿**：启动模式下拉首项「不自动启动」即空值位（`value=''`，提交时不传 launchSettings，后端落空串；空值语义 = 未配置，行为同手动）；MUI 对空串选项需显式双配置——`select.displayEmpty`（空值是有效选项，选中项文本正常渲染）+ `inputLabel.shrink`（InputBase 的 filled 判定不含空串，label 显式常驻收缩，否则与内容重影）；ACP 会话展开 Agent（默认 claude-acp）与执行模式（默认需要审批=acceptEdits）两下拉，切回终端模式不丢草稿；autoCommand 一期不出输入框（固定 claude，消费端以 mode 判断）；新增文案中文直出不加 i18n key（对齐 ImBotDrawer 约定，i18n 维持既有页面维度）；`services/index.ts` barrel 补导出 `WorkspaceLaunchSettings`

#### T0.3 消费端接入 workspace 级值

**状态**：✅

**功能**：终端启动行为改由 workspace 级配置单源驱动（无全局回落）

**技术方案**：
- 终端直启值由 workspace 启动设置单源派生：`terminal-auto` = 主 pane 直接 spawn claude，手动 / acp / 未配置 = 不自动启动
- `EmbeddedTerminal.tsx` 删全局 key hook，改接收父层（DevWorkbenchPage）解析下传的 `startupCli`——**保住「就绪闸门 → fit 实测 → 以实测尺寸 spawn」时序不破**，解析中经 enabled 闸门哑会话等待（编码规则 1）
- `WorkspaceInitGate.tsx` 删内部 hook 改 prop，步骤③文案按直启值区分
- `acp` 模式本阶段无消费端（T1.6 落地），选中时终端侧按「不自动启动」处理
- 分屏 pane 行为维持现状（仅主 pane 受启动设置影响）

**依赖**：T0.1、T0.2

**实施定稿**：用户拍板取消回落链——全局 `terminal_startup_code_cli` key（appConfig 类型/常量/parse、设置页「启动设置」分区、i18n 死键）全删，workspace 单源。启动模式四档（语义见阶段 0 设计要点），none 档经新增 `LaunchModePicker.tsx` 的 `TerminalLaunchFlow` 分流容器实现延迟决策（key 随 issue 挂载，临场选择态随 key 重置）。派生：DevWorkbenchPage 模块级 `terminalStartupCli(mode, autoCommand)` 纯函数（仅 auto → autoCommand ?? 'claude'，其余 null）；`startupCli: string | null` 照 `workspaceDir` null 语义范式透传——null = 明确不直启（正常渲染裸 shell），「启动配置解析中」的等待在父层消化（workspacesPending 时 TerminalLaunchFlow 不挂载、空占位），EmbeddedTerminal 挂载即终值、`enabled: configReady`（directCommand 参与 attachKey 无中途变化路径）；度量配置闸门 keys 缩为字号/行高两项。WorkspaceInitGate 仅文案消费 + 过渡浮层开关 `terminalTransitionEnabled`（children 为终端才播，none 档面板不叠浮层）；EmbeddedTerminal connecting 态叠「正在启动终端中…」蒙层（TerminalView 保持挂载跑 fit，纯视觉 pointer-events none）

#### T0.4 issue 级 launch_settings 覆盖

**状态**：✅

**功能**：issue 可按需覆盖 workspace 级启动设置（P1 / P6 拍板终态：整个 launch_settings——mode / agentCode / autoCommand / permissionMode——均可 issue 级调整）

**技术方案**：
- `t_issues` 加 `launch_settings` JSON 列（同构 workspace，D6），改表走既有约定（编辑原迁移 + `pnpm server:gorm:gen`）
- 字段级合并：issue 级只覆盖显式写过的键，未覆盖键回落 workspace 级，再回落全局 `terminal_startup_code_cli`
- 消费端读取链从 T0.3 的 workspace 级升级为链式解析（终端 spawn 闸门时序不变，编码规则 1 不破坏）
- 覆盖 UI 落 issue 主窗口的模式切换入口（与 T1.6 ACP 视图协同最顺，实施顺序可后置、不阻塞阶段 0 交付）

**依赖**：T0.3

**决策关联**：P1、P6

**实施定稿**：
- 列形态同构 workspace（`launch_settings TEXT NOT NULL DEFAULT ''`，空串 = 未覆盖）
- 响应装配：`ProjectIssueResponseData` 加浅层同名字段 `LaunchSettings *WorkspaceLaunchSettings`（Go json 浅深度胜出，遮蔽嵌入 DO 的 string 形态），装配收口在 assembleWithType 一处；MCP `buildIssueUpdateRequest` 回填 cur.LaunchSettings 避免全量更新清掉覆盖
- 字段级合并：`shared/launchSettings.ts` 的 `mergeLaunchSettings(base, override)`（issue 显式键覆盖 workspace，mode 显式 'none' 是有效覆盖值 ≠ 未覆盖）；terminal/acp 两个 Agent 选项常量与展示名映射同文件收编（WorkspaceDrawer / 抽屉 / 工作台三处共用）
- 覆盖 UI 落 **issue 编辑抽屉**（非原方案的 issue 主窗口模式切换——主窗口入口随 T1.6 一并评估）：模式下拉首项「跟随工作空间」（未覆盖，空串走 MUI displayEmpty + shrink 双配置）+ 四档按档展开 Agent 下拉；「跟随」提交不传 launchSettings（清除覆盖）
- 消费端：DevWorkbenchPage 以合并值派生 startupCli / 启动分流 / 浮层开关；issue 或 workspaces 任一未就绪即空占位等待（TerminalLaunchFlow 不挂载），挂载即终值

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
| `npx-adapter` | claude（一期唯一启用） | catalog 原始形态 `npx -y @agentclientprotocol/claude-agent-acp@<pin>`；实际 spawn 指向受管目录 vendored 入口（T1.3 一次性安装）；包名+版本二元组整体 pin，不跟随 registry 包名迁移，以本项目真握手验证结论为准 |
| `native-acp` | codex / opencode / pi（预留，随 P4 扩展启用） | 直连各自 CLI，零 adapter；条目命令以 registry 离线快照为准（Gold-Band catalog 的「PATH 可执行文件」模板同构） |
| `in-process-go` | **预留，不实现** | Go 进程内实现 ACP agent 角色（`driver_claude.go` 的 stream-json 积累可复用） |

- 为什么官方 adapter 起步而不是直接 Go 自研——成本分布：

| | 官方 npx adapter | Go 进程内 adapter |
|---|---|---|
| 环境成本 | 需 node（adapter 经 vendoring 随包分发，无 npx，见 T1.3/T1.4） | 零新增 |
| 协议维护 | **零**（claude CLI 协议演进由官方 adapter 团队追，两周三版的跑步机不归我们） | **全扛**（企微 SDK 转义 Go 划算是因为企微协议三年不变；claude 控制协议是反例） |
| 供应链维护 | 版本 pin + 升级验证（catalog 锁版本，刷新脚本统一升） | 无 |

- **Go 进程内 adapter 的触发条件**（满足任一再启动，届时沉没成本接近零）：① doctor 数据显示相当比例用户机器缺 node（原生安装器装 claude 的那批）；② 目标场景包含离线/内网环境；③ node 中间层出现真实痛点（启动延迟、僵尸进程、每会话多一个 node 进程的内存开销）。
- **node 依赖**（D4）：不随 app 打包（包体 +几十 MB、三平台构建矩阵变重）；目标用户装 Claude Code，npm 安装路径必然带 node，原生安装器路径可能没有——不猜，doctor 探测，缺失时 UI 明确引导。

#### T1.0 server Go 工具链升级（1.25.7 → 1.27）

**状态**：✅

**功能**：为 acp-go 依赖铺路——两候选库均要求 Go ≥1.27（BrokkAI 1.27.1 / ironpark 1.27.0）

**技术方案**：`server/go.mod` go directive 升至 `1.27.1`；CI 已用 `go-version-file: server/go.mod` 锁版（`ci.yml` / `release-assets.yml`），改 go.mod 即自动同步，无需另改 workflow；`go build ./...` + `go vet ./...` 全量验证。环境事实：本机 go 为 gvm 管理的 1.25.12、`GOTOOLCHAIN=auto`，go.mod 声明 1.27.1 后可自动下载并切换工具链（build/vet 已实测通过）——届时无需单独安装新版 go

**依赖**：无

**决策关联**：D8

**实施定稿**：升级前经 `GOTOOLCHAIN=go1.27.1 go version` 实测确认本地可自动下载并切换 1.27.1 工具链（用户要求先验证再改）；go.mod 仅 go directive 一行变更（mod tidy 零额外变更），切换后 build / vet / internal 全量 test 回归通过

---

#### T1.1 Go ACP client 核心

**状态**：✅

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

**实施定稿**：
- 包落 `server/internal/acp/`，一域一文件：`launch.go`（进程层：SpawnConfig/AgentProcess/进程组回收/stderr 分类与 64KiB tail）、`interact.go`（PendingPermission/PendingElicitation 挂起态，CAS-once 应答）、`session.go`（sessionRuntime：回合互斥/软硬取消/事件入队）、`client.go`（AgentClient：握手/建会话/入站路由/收敛）、`mode.go`（SetPermissionMode 下发）、`events.go`（事件类型与背压契约）、`stderr.go`（三分类 + 环形 tail）、`process_unix.go`/`process_windows.go`（平台隔离）；测试三层——`fakeagent/` 真 stdio 脚本化假 agent（TestMain 现场编译）+ 单元/链路集成 + 环境变量门控的真握手用例
- T1.1 只认 `SpawnConfig`（catalog 是 T1.2 的翻译层），验收以 SpawnConfig 直拉真 adapter 完成：`@zed-industries/claude-code-acp` 0.16.1 实测跑通 initialize → session/new → prompt → update 收流 → 进程回收——spawn 表原记 `@agentclientprotocol/claude-agent-acp` 有误，T1.2 catalog pin 以实测包名为准
- 能力协商面：不广告 fs/terminal（未广告方法库回 -32601）；广告 configOptions（set_mode 新轨）与 elicitation form（url 不广告）；`HasClaudeCodeExtension` 探测 initialize 响应 `_meta.claudeCode` 供 T1.3/T1.5 claude 专属分支——0.16.1 adapter 不上报该 _meta，分支休眠属预期
- **CancellablePermissions 未采用**：库的 ctx→cancelled 自动应答包装不覆盖本项目语义组合——取消须先结算未决交互（取消赢过用户迟到应答）、wait 须把 ctx 取消译为 cancelled outcome 且 handler 恒返回 nil error（库把 handler error 转 -32800 拆连接）；自研 `pending.wait` 逐条满足
- 取消与超时收口（§5.1 清单）：软取消 = `session/cancel` + 立即结算未决交互，10s 预算内等 agent 回 cancelled 终态，超时硬取消（拆回合 ctx、以错误返回）；库对 session/cancel 以 -32800 终止在途请求，归一为干净 cancelled 终态；握手/建会话 60s、关会话 10s，prompt 与交互应答不设超时（回合时长与用户决策时长不可预估，由 ctx 与取消语义驱动）
- 锁序红线：不得持 stateMu 发起网络调用——通知回写跑在连接读循环 goroutine 且需同一把锁，持锁等响应会互锁；`SetPermissionMode` 走「快照送验 → 确认后合并」；`sessionValue()` 对 Modes 浅拷贝出私有副本（CurrentModeID 是唯一回写字段，隔离库经快照指针的就地写与消费方读的竞争；AvailableModes 目录创建后只读可共享）
- 收敛序单点 `converge()`（Close 与进程死亡监听共用、并发各步幂等）：逐会话结算未决交互 → conn.Close（join 全部入站 handler）→ 进程组回收（WaitDelay 5s 收口孙进程管道）→ 逐会话终结哨兵；`Err()` 以 closedByCaller 标记区分主动收尾（nil）与意外死亡（stderr 摘要证据面）；事件通道不关闭，以 Terminated 哨兵终结，满则阻塞、收敛期经 closing 通道逃生防 join 死锁
- elicitation 路由：`CreateElicitationRequest` 顶层无 sessionId，typed 侧会话归属解析自 `Form.Session.SessionID`；请求级 scope 回 -32602；库 typed `ElicitationFormMode` 丢 requestedSchema/url/elicitationId 字段（上游生成缺口）——带 sessionId 的 form/url 载荷经 T1.7 入站拦截层原始参数解码补齐（见其定稿），typed 侧路由不受影响

#### T1.2 agent catalog（离线 pin）

**状态**：✅

**功能**：agent 目录数据源与刷新脚本，一期只启用 claude-acp

**技术方案**：
- 结构照 Gold-Band：官方 ACP registry 的离线 pin 快照 + 刷新脚本，运行时不在线拉取；落在 sidecar 内嵌资源
- 每条目声明 spawn 策略与启动参数（`npx-adapter` / `native-acp`；`in-process-go` 仅预留枚举）
- 一期只落 claude-acp 条目（P4 收窄：codex / opencode / pi 留条目模板与 spawn 策略口子，扩展 = 补条目 + registry 快照，不在本文硬编码）；catalog/doctor 的 UI 面板仍砍到最小
- adapter 版本 pin 进 catalog（参考 0.8x 线），升级走刷新脚本统一升 + 验证流程定稿（风险 §5.2）

**实施定稿**：
- 刷新脚本 `scripts/prepare-agent-catalog.ts`（`pnpm server:catalog:refresh`）：从官方 ACP registry 取 agent 定义，逐条目查 npm 校验 pin 存在并取 engines.node，pin `@agentclientprotocol/claude-agent-acp`（registry 包名已由 `@zed-industries/claude-code-acp` 迁移至此，真握手实测定名；包名拍板于脚本 pin 包名表，版本住 root package.json devDependencies 精确版本，T1.3 起本地 `pnpm up --save-exact` 升级，当前 0.84.0）产出 `server/internal/agentcatalog/agent-catalog.json`；registry 原始快照同落 `acp-registry.snapshot.json` 溯源；以 SOURCE_DATE_EPOCH 固定时间戳，重复运行产物一致（幂等可 diff）。升级流程 = 重跑脚本 → `go test` 过内嵌产物断言 → T1.4 握手回归
- 条目显式落 `code` 字段（= id，即 launch_settings.agentCode 取值）；`nodeMinVersion` 一期 22（node:child_process 直拉 npx 需较新 node，T1.4 doctor 消费，空 = 不校验）
- `agentcatalog` 包：go:embed 内嵌 + `Load` 进程级单例；parse 校验结构版本 / 条目非空 / id+code 唯一非空 / strategy 枚举 / enabled 条目 command 非空，宁失败不静默；查表单一路径 `GetAgentCatalogInfoByCode`（匹配条目 code）；`Entry.SpawnConfig(cwd)` 翻译条目为 acp.SpawnConfig（npx-adapter / native-acp 现同为 argv 直拼，差异留给 T1.3 vendored 入口改写；in-process-go 预留枚举翻译报错）；claude 专属注入（可执行文件 / login PATH）由调用方追加，catalog 层不感知（D5）
- `enums.AgentCode` 定型为开放命名 string 类型（非闭合字面量联合）：常量仅在代码出现分支判断时补充（一期仅 `AGENT_CODE_CLAUDE_ACP`）；合法取值域 SSOT = catalog enabled 条目 code，扩展新 agent 不改映射；原四常量闭合枚举（dal/enums/workspace.go）删除；消费端查记录一律走 `GetAgentCatalogInfoByCode`
- HTTP 面 `POST /api/agentCatalog/getList`（chain controller + 标准包裹），投影含 code/label/version/strategy/enabled/nodeMinVersion；前端 `state/agentCatalog` 查询域（keys/queries/barrel）消费，`shared/agentCode.ts` 类型映射对齐；下拉可选面 = enabled 条目投影（顺序即 catalog 顺序），原 `AGENT_CODE_OPTIONS` 常量删除
- `LaunchSettingsFields` 受控组件：启动设置字段组（启动模式 + terminal-auto 档「终端 Agent」+ acp 档「ACP Agent / 执行模式」）两抽屉共用——WorkspaceDrawer（workspace 单源）与 ProjectIssueDrawer（issue 覆盖档加「跟随工作空间」displayEmpty 首项 + 字段级回落），差异仅 `showFollowOption`；agentCode 选中态以 '' = 未显式选择，渲染期派生首个 enabled 条目（`useEffectiveAcpAgentCode`，编码规则 1，无 effect 回填）；目录升级后的遗留值以禁用项显式呈现、保存不静默改写；issue 覆盖的提交草稿渲染期派生一次，dirty 比较与提交共用同一派生（修 dirty 漏检）
- 真握手测试改走 catalog：`handshake_real_test.go` 以 `GetAgentCatalogInfoByCode(string(enums.AGENT_CODE_CLAUDE_ACP))` 取条目翻译 SpawnConfig 拉起真 adapter；测试落外部测试包 `acp_test` 避免与 acp 包循环依赖

**依赖**：无

**决策关联**：D2、D3、P4

#### T1.3 adapter vendoring 与 claude 路径一致性

**状态**：✅

**功能**：压掉官方方案的网络不确定性，并保证 ACP 模式与终端模式用同一个本机 claude

**技术方案**：
- Vendoring：首次在受管目录（如 `~/.ocean-harness/acp-adapters/`）pnpm 安装固定版本，之后 spawn 指向 vendored 入口——「每次 npx 拉包」变「一次性安装」，离线可用，升级走自己的版本策略（仅覆盖 `npx-adapter` 策略即 claude；codex / opencode / pi 走 native CLI 直连，无需 vendoring）
- claude 路径一致性：vendored 安装内包的 SDK 平台原生 claude 二进制（`node_modules/@anthropic-ai/claude-agent-sdk-<平台triple>/claude`）为全链路 SSOT，经 `CLAUDE_CODE_EXECUTABLE` 显式注入 adapter——终端直启（Rust 直读打包 resources）/ ACP / bot headless / marketplace 消费同一份二进制，零版本漂移、零 claude 探测失败面；终端手动路径保留本机 claude（shell 自解析）。clibin 收窄为 login shell PATH 解析（node 等运行时兜底），claude 三级探测链退役

**依赖**：T1.2

**决策关联**：D3、P5

**实施定稿**：
- 载体与安装链（公司网络约束：运行时零 npm / registry 依赖）——构建期 `pnpm server:acp:vendor`（`scripts/prepare-acp-adapters.ts`）按 catalog pin 逐条目 `npm install --omit=dev` 落 `app/resources/acp-adapters/<id>/<version>/`（幂等：安装后校验 node_modules 内 package.json 版本命中即跳过；临时目录 + rename 原子落地，npm 中断不产生以目标名存在的半成品），tauri `beforeDevCommand` / `beforeBuildCommand` 前置执行，`bundle.resources` 随应用打包（`.gitignore` 排除产物）；运行期 `agentcatalog.EnsureVendored` 在受管根 `app_data_dir/acp-adapters/` 按需复制安装：marker（`.ocean-vendored`，内容 = 版本）幂等命中复用，半成品/漂移整树重制，版本切换后清理同 id 旧版本（受管目录恒只保留当前 pin），临时目录 + rename 原子落地，symlink 原样重建（npm `.bin` 相对链两端布局同构即有效）、权限位显式补齐
- 目录注入：Rust `http_server.rs` 派生 `GO_SERVER_ACP_RESOURCES_DIR`——`Resource` 基准按平台解析（macOS = `.app/Contents/Resources`、Windows = exe 目录，均含打包资源；dev 下 bundle.resources 不复制进 target），dev 经 `tauri::is_dev` 分支以 crate 目录为基准指向仓库内 staging、release 解析至打包资源，缺失不注入；终端直启侧同口径（`pty::cli_bin::ensure_resources_base` 装配期经 AppHandle 解析一次，不以 exe 路径拼接）；`app_data_dir` 派生 `GO_SERVER_ACP_ADAPTERS_DIR`（受管根由 Go 自建，Rust 不预创建）；config 侧两目录均为可选字段（缺失不阻断启动，EnsureVendored 调用期报未配置；air 自测可经 yaml 模拟注入）
- spawn 翻译：`Entry.VendoredSpawnConfig` 读 vendored 包 package.json 的 bin 字段（npm 字符串 / map 双形态，map 多键取与包名尾段同名键）→ argv `node <vendored 入口>`；node 经 `resolveNodeBin` 落成绝对路径（LookPath → `clibin.LoginPath` 的 login PATH 逐目录兜底——exec 对相对 argv[0] 按 sidecar 自身 PATH 查找，spawn env 覆盖不影响查找，GUI 拉起场景 PATH 常缺 nvm/volta）；spec→包名还原与构建脚本同规则（args 尾部非 flag 参数去 @版本尾缀）；native-acp 条目不走 vendoring（SpawnConfig 直连），vendored 系 API 对其显式报错防误用
- claude 路径一致性（P5 定稿终态：vendored 自带 claude 全链路 SSOT）：`agentcatalog.VendoredClaudeBin` 按平台 triple（GOOS/GOARCH 映射，Windows 取 `claude.exe`）定位 vendored 安装内包的 SDK 平台原生 claude 二进制并校验存在性；`agentcatalog.ResolveVendoredClaudeBin` 一步式封装（catalog 条目 → EnsureVendored → 二进制解析，成功进程级缓存）供 bot driver 与 marketplace 共用，vendoring 两目录由 main 启动期经 `agentcatalog.SetVendoredDirs` 注入（agentcatalog 不反向依赖 config/global——`global → bot` 依赖方向不可逆，bot/marketplace 不得 import global；doctor 分步编排与真握手测试仍走显式目录传参不经此缓存）；acp 侧 `ClaudeEnvOverrides(base, claudeBin)` 由消费方拼装注入（client 本体不感知 claude，D5）：`CLAUDE_CODE_EXECUTABLE` = vendored 二进制绝对路径——spike 实证 adapter 的 `claudeCliPath()` 以该 env 为最高优先级直传 SDK `pathToClaudeCodeExecutable`；`PATH` = login shell PATH 全量覆盖（best-effort，对齐 bot turnEnv 惯例，GUI 拉起场景 claude 派生的工具子进程同享一致环境）。Rust 直启侧不依赖 Go 受管副本：直读打包 resources（dev = 仓库 staging，release = 打包资源目录，同 http_server 资源基准），版本目录 semver 取最高；解析失败回落裸 shell。终端手动路径保留本机 claude（shell 自解析）
- 验证：真握手测试升级为生产链路验收（EnsureVendored → VendoredSpawnConfig → ClaudeEnvOverrides → 真握手，0.84.0 实测跑通 initialize → session/new → prompt → 收流 → 回收），升级 = `pnpm up @agentclientprotocol/claude-agent-acp@<新版> --save-exact`（adapter 版本决策住 root package.json devDependencies 精确版本，包名迁移仍走脚本 pin 包名表 + 真握手）→ `pnpm server:catalog:refresh` → `pnpm server:acp:vendor` → 本用例 → 提交；vendored 安装层单元测试覆盖命中 / 资源缺失 / 半成品重制 / 版本切换清理 / symlink+权限位 / bin 双形态 / node 路径解析

#### T1.4 doctor 握手探测

**状态**：✅

**功能**：agent 可用性判据 = 真实握手跑通；结果供 UI 引导与（阶段 3）bot 徽标消费

**技术方案**：
- 三项探测：node 可用且版本达下限（spawn 直查 vendored node，不经 npx；下限 = catalog `nodeMinVersion`，当前 22）、vendored 安装就绪且内包 claude 二进制存在（`EnsureVendored` + `VendoredClaudeBin`，P5 定稿终态：claude 消费 vendored 自带二进制，无独立本机探测项）、native agent CLI 可用（codex / opencode / pi 各自 PATH 解析——随 P4 扩展启用，一期仅 claude 时跳过）
- 握手判据：真拉起子进程跑 `initialize → session/new → available_commands`（agent 主动推送，硬门槛等待）再清理，跑得通才 healthy（照 Gold-Band `src/acp/client.rs:2673-2731`）
- node 缺失/版本过低时 UI 明确引导（后端生成文案，版本号动态取 catalog `nodeMinVersion`）：「node 版本过低（vX.Y.Z）：ACP 模式需要 Node.js ≥22，或切换终端模式」，不运行时报错

**依赖**：T1.1、T1.3

**决策关联**：D4

**实施定稿**：
- 探测链（新包 `internal/acpdoctor`，依赖 acp + agentcatalog——login PATH 由 acp 内部经 clibin 消费，本包不直接依赖 clibin；维持 agentcatalog → acp 单向不被破坏）：策略门槛（一期仅 npx-adapter）→ node 解析（`agentcatalog.ResolveNodeBin`，与 vendored spawn 同一 SSOT）+ `node --version` 实测 + `nodeMinVersion` 主版本比较 → `EnsureVendored` + `VendoredClaudeBin`（失败落 StageClaude）+ `VendoredSpawnConfig` + `ClaudeEnvOverrides(cfg.Env, claudeBin)`（与生产拉起零差异：「能探测通过」即「能按生产链路拉起」）→ 真握手 → 整组回收；探测会话 cwd 用一次性临时目录；单条目总预算 90s 硬截断必出结论，命令推送等待窗口 10s
- 结论四态：healthy / unhealthy / unknown（unknown = 从未探测 ≠ 失败）+ checking（受理在跑的派生态，由 in-flight 集合在 Snapshot 推导，不入缓存）；进程内缓存随 sidecar——重启即 unknown，由启动后台探测重填（main 启动序列 5.6 步 `RunStartupProbe`，串行逐条目，非阻塞不延迟 HTTP 监听）
- API 面异步受理 + 轮询（探测典型 3–10s 且不固定，同步接口会长时间挂住请求）：POST `/api/doctor/check` 受理即返回受理清单 + 受理时刻快照（空 agentCode = 全部 enabled 条目；在跑条目单飞 join 不重跑），POST `/api/doctor/getInfo` 全量四态快照；前端 `state/doctor` 域 `useDoctorReports` 条件轮询（存在 checking/unknown 时 1s，全落定自动停），`useDoctorCheck` 受理后整域失效
- 失败原因由后端生成用户可读中文（前端直显）：node 过低提示携带实测版本与 catalog 下限；`initialize`/`session/new` 的 −32000 特判为「claude 未登录」引导；unhealthy 附失败 stage（claude/node/vendored/spawn/initialize/session/commands）与进程异常退出的 stderr 证据摘要
- UI 消费（一期最小内联，`LaunchModePicker`）：ACP 选项保持置灰，提示随四态派生（checking/unknown/快照未就绪 → 「正在检测 ACP 运行环境…」；healthy → T1.6 上线预告；unhealthy → 后端原因直显），行尾附「重新检测」按钮（受理期按钮 loading，与探测异步解耦，收敛经轮询自动呈现）；阶段 3 bot 徽标待接
- 协议勘误定稿：available_commands 是 agent 在 session/new 后主动推送的 `session/update`（非客户端请求型方法，客户端无从拉取），doctor 以硬门槛等待其到达；握手序列为 initialize → session/new → available_commands（原方案笔误 setup_session）；healthy 附带 CommandCount（推送的命令数，握手附带收益）。已知限制：推送若早于 acp 客户端的会话注册会被当早期帧丢弃（acp 层 TODO(T1.5) 早到帧缓冲），doctor 表现为 commands 阶段超时——真实 agent 推送在 CLI 往返后（百毫秒级）大概率避开，且可经手动重新检测重试，T1.5 落地后彻底消除
- 测试：fakeagent 增 doctor-happy 脚本（session/new 响应落 wire 后 200ms 推送——客户端处理完响应才注册会话运行时，早于响应的推送被当早期帧丢弃）；单测覆盖版本解析/下限校验/−32000 映射/等待窗口四路径/策略门槛，编排测试以 `probeFunc` 替换点覆盖受理单飞（gate channel 确定性挂起，不依赖时序）/四态快照/启动探测填充；握手集成经真实 stdio 三路径（doctor-happy healthy / 无推送落败 commands / 拉不起落败 spawn）；生产链路全真用例双门槛 gating（`OCEAN_ACP_REAL_HANDSHAKE=1` + vendoring 两目录注入），实机实测 healthy（0.84.0 真握手跑通）

#### T1.5 sidecar ACP 会话域与事件流

**状态**：✅

**功能**：sidecar 持有并管理 ACP session 生命周期，issue↔session 绑定，事件流推 panel

**技术方案**：
- 会话域：ACP session 的创建/复用/回收；issue → ACP session 绑定关系落库
- 事件流经 sidecar HTTP/SSE 推 panel（`session/update` / `request_permission` / `elicitation` 事件）
- SSE hub 背压策略：关键事件（Permission / Elicitation / Terminated / 回合终态）不丢弃不合并；高频 delta（agent/thought message chunk）允许合帧；快照类（usage / plan / available_commands）以最新替换——internal/acp 事件通道为阻塞背压契约（满则拖住连接层读循环），hub 侧必须消化消费速率差
- 消费 workspace `launch_settings.agentCode`（T0.1 链路）决定拉起哪个 agent
- 权限模式（P6）：会话创建后按 launch_settings `permissionMode`（workspace 设默认、issue 可按需修改，T0.4）经 `session/set_mode` 下发，值对 agent 上报的 mode 目录校验（防硬编码失效）；一期两档「需要审批」（acceptEdits）/「自动」（bypassPermissions）
- 并发基线：单会话内回合串行（跨入口回合锁在 T2.2 升级）
- **D7 受控反转**：issue↔session 绑定是对当初随 chat 视图删除 `claude_session_ref` 决策的受控反转（`app/src/pty/claude_state.rs` 注释），不是回退——实施定稿须明示此点

**依赖**：T1.1、T0.1

**决策关联**：D1、D7；P3（跨模式 `--resume` 接续不入一期）；P6（permissionMode 经 set_mode 下发）

**实施定稿**：
- **D7 受控反转明示**：`t_issue_acp_sessions` 绑定表是对「随 chat 视图删除 `claude_session_ref`」的受控反转而非回退——彼时删除成立的前提是 sidecar 不持有会话、绑定无语义承载点；本域由 sidecar 亲自持有 ACP 会话生命周期后，issue↔session 绑定重新成为必要状态，故以独立表重新落绑定（不复用旧列名/旧结构），两决策各自在其前提下属实
- 分层装配：新包 `server/internal/acpsession`（依赖 acp + agentcatalog + dal，单向）；main 启动序列 5.7 步 `NewManager` 装配 `global.AcpSessions`（启动即清扫全部运行时锚——sidecar 重启后旧锚失效，无 resume 语义 P3 同向）；service/controller 层仅受理转发；`types` 只放 Request DTO，响应 shape SSOT 是 `acpsession.ViewSnapshot`（前端按此镜像 TS，不二次镜像进 types）
- 绑定锚点（D7 承载）：`t_issue_acp_sessions`（issueId 唯一锚 + agent_code + acp_session_id + last_error），`BindingStore` 四操作——GetOrCreate（受理建行）/ SaveReady（落锚清错）/ SaveError（落因清锚，update-only 不复活已删行）/ Delete（issue 删除级联）；错误证据 `last_error` 跨重启可见（对齐 bot 域）
- 进程模型：per-issue 独立 AgentClient 进程（用户选定：会话失败互不牵连，进程退出即会话终结）；生产拉起链 `spawnAgentSession` 与 acpdoctor.CheckEntry 逐段同源（catalog → EnsureVendored → VendoredClaudeBin → VendoredSpawnConfig → ClaudeEnvOverrides → Launch → Initialize → NewSession → SetPermissionMode），「探测通过」与「生产拉起」同一条链；握手步 -32000 特判 claude 未登录引导（与 doctor 同文案语义，常量两域各自维护）
- 受理模型（对齐 issueWorkspace init 与 acpdoctor 的异步受理惯例）：`Ensure` 同步解析配置（校验失败立即反馈）→ starting/ready join 返现状 / failed-terminated 删旧重建 → 建锚行失败回滚 entry → 广播 starting → 后台 spawn 链（分钟级上限不占 HTTP 请求），前端经 SSE 或 getInfo 轮询跟进
- launch_settings 解析（`resolveSessionConfig`）：workspace 基线 + issue 覆盖的逐字段合并（override 空串键回落，mode 显式 "none" 是有效覆盖值）；mode 非 acp 拒绝；回落链 agentCode → 首个 enabled catalog 条目、permissionMode → acceptEdits、cwd → `<ws.Dir>/<issueId>`（兜底建目录）；issueId 合法性前置校验（拒空/穿越/分隔符，与 service 层 issueWorkspace 校验同语义）；两档 permissionMode 字面值在解析层校验，agent mode 目录匹配在 acp 层 `SetPermissionMode` 内建校验（legacy 预检 + configOptions 库校验，错误中文列出可用值）
- 会话视图（`sessionView`）：后端积累投影（用户选定：建连即全量快照，前端不重放 delta）——chunk 按 `(kind, 回合)` 聚合条目、工具调用按 toolCallId 覆写 upsert（部分更新指针字段合并、孤儿 update 先到建空壳）、快照类（plan/usage/commands/modes）最新替换；`ViewSnapshot` 即 `/api/acpSession/getInfo` 响应与 SSE snapshot 帧的 shape SSOT
- SSE 事件流：`GET /api/acpSession/events?issueId=` 长连；帧信封 `{seq 按 issue 单调, issueId, type}` 12 帧型（snapshot/sessionStatus/entry/planUpdated/usageUpdated/commandsUpdated/modeUpdated/pendingOpened/pendingClosed/turnStarted/turnEnded/terminated）；订阅在 hub 锁内生成快照首帧（seq 连续无缝隙）；单订户 256 帧缓冲，溢出即断开订阅（HTTP 连接不断，EventSource 自动重连取新快照），pump 永不阻塞于慢客户端（保住 acp 事件通道背压契约）
- 回合串行与挂起交互：视图侧 `beginTurn` 前置闸门（活动回合拒绝，与 acp.ErrTurnActive 同语义）；权限/表单挂起由视图登记 pendingID 注册表（acp 层无枚举 API），`RespondPermission` 对未知 optionId 前置校验不消耗挂起（摘除后应答失败的残余竞态回填，杜绝前后端挂起视图与 agent 等待态三方失同步）；回合终态 `endTurn` 对齐 acp settlePendings 补发 pendingClosed（未决挂起随回合收尾清空）；**帧序注记**：内容帧经 pump 广播、回合终态帧经收尾 goroutine 广播，二者并发——`turnEnded` 可能先于该回合内容帧到达订阅端，entry 帧整体覆写语义 + 快照兜底下无顺序假设，前端不得依赖帧序因果
- HTTP 面：`/api/acpSession/` 六 POST（ensure / getInfo / prompt / cancel / respondPermission / respondElicitation，`{code,msg,data}` 包裹）+ GET events（SSE）；issue 删除级联 `Discard`（删 entry + 删锚行，ready 态经 client.Close 交 pump 哨兵路径广播终态，其余状态补发「会话已删除」）；SIGTERM 顺序 BotSupervisor.StopAll → AcpSessions.StopAll（含 hub.closeAll 断 SSE 长连）→ HTTP Shutdown（SSE 不断会拖住 Shutdown）
- 测试：merge/resolve 纯函数、view 十二变体消费与聚合/合并/终态、hub 快照首帧与 seq 连续/issue 隔离/慢订户断开（分批投递——锁内瞬时广播下健康订户同样触溢出，这是语义而非缺陷）/退订关停、manager 集成以 `spawnFunc` 替换点拉起真实 fakeagent（TestMain 现场编译，复用 acp 包脚本 happy/permission/slow-cancel/crash）覆盖全链受理→回合→落锚、spawn 失败落因与重试重建、进程意外死亡落「异常退出」证据、审批应答全链、回合闸门与软取消、Discard 级联、BindingStore 生命周期（临时 sqlite AutoMigrate）；`-race` 连跑通过

#### T1.6 issue 主窗口 ACP 视图

**状态**：✅

**功能**：issue 主窗口新增 ACP 模式（第三执行模式）：会话视图 + 底部任务描述输入框

**技术方案**：
- 模式切换：读 launch_settings `mode`（P1 拍板：workspace 设默认、issue 级覆盖经 T0.4）；终端模式两分支零改动
- 会话视图：渲染 `session/update` 消息流与工具调用（参照 Gold-Band ConversationComposer 交互）
- 底部输入框回车 → `session/prompt`（经 T1.5 会话域）
- 服务地址从 Rust `http_server_status` 命令获取，不硬编码端口；SSE 订阅遵循现有前端服务范式

**实施定稿**：
- state 域 `state/acpSession/`（一域一目录约定）：SSE 增量帧经 `applyFrame` 纯归约写入 TanStack Query 缓存（view key 即 SSOT），读查询零 refetch、推送驱动；`useAcpSessionEvents` 订阅随挂载建断，断线 EventSource 自愈重连取新快照；服务地址经 Rust `http_server_status` 注入（`services/AcpSessionService`），不硬编码端口
- `AcpSessionView` 挂载即幂等 ensure（ref 防重入）+ 按 snapshot.status 分支渲染（starting 全屏进度 / failed+terminated 原因与重启 / idle+ready 会话视图），全程无「先临时值后纠正」路径（编码规则 1）
- `MessageList` 四类条目渲染（user 高亮块 / agentMessage markdown / agentThought 淡化 / toolCall 折叠卡 `ToolCallItem`——标题行+状态徽章+可折叠 pretty JSON 载荷）+ 贴底实测滚动策略（onScroll 实时测距 48px 阈值判定贴底，仅贴底态 entries 更新才归位，上翻查阅不被打断）
- `PromptComposer` Enter 发送 / Shift+Enter 换行；回合态输入禁用、发送钮切「停止」（软取消，受理期按钮 loading 可见）；发送失败 toast 保留输入
- 公共 TS 依赖版本（typescript/vite/vitest/@types/node）迁 `pnpm-workspace.yaml` catalog，包内以 `catalog:` 引用

**依赖**：T1.5、T0.3

#### T1.7 桌面侧审批与选择交互

**状态**：✅

**功能**：`session/request_permission` 与 `elicitation/*` 在桌面侧呈现为弹窗/面板——阶段 1 即获得审批能力，不等 bot

**技术方案**：
- request_permission → 审批弹窗（选项呈现 + 结果回写）
- elicitation → 表单/选择面板
- 交互结果经 T1.5 会话域回写 ACP 会话
- 阶段 2 T2.4 在此之上做双入口收敛

**实施定稿**：
- **上游缺陷补偿**：acp-go 生成模型把 spec 中 `ElicitationFormMode` 的 `requestedSchema`/`url`/`elicitationId` 字段丢弃（v0.11/v0.12 同病，schema.json 有而生成类型无），form 载荷线上解码静默失真且上游无修复。补偿：acp 包入站 Handler 链装配 `elicitationWireInterceptor`（`Connect` 前包装），对 `elicitation/create` 原始参数宽松解码出全保真 `ElicitationWire`，经 ctx value 透传 `CreateElicitation` → 挂起登记 → `PendingView.Request`（`WireFromRequest` 作生成模型可得字段的降级投影兜底）；fakeagent 演示链改走 `client.Call` 原生通道发扁平 form wire（绕开 typed 丢失面），manager 集成测试 `TestRespondElicitationFlow` 全链证明 requestedSchema 落到视图
- **呈现形态（用户拍板）**：不弹窗，落 MessageList 与输入区之间的**固定挂起区**平铺 `PendingCard`，全部应答后整区消失；permission 卡复用 `ToolCallItem` 呈现工具详情，选项按 kind 分色按钮组（allow 绿/reject 红、`*_always` 实心+「记住」标识）；elicitation 卡 message + **适配器子集渲染器**（`renderElicitationPlan`：string+oneOf→radio、array+items.anyOf→checkbox、string/number/boolean→text/number/switch，越界形态降级 unknown 只读不参与应答；空值跳过语义——accept 缺字段即跳过、switch 仅 true 下发）；url/未知模式兜底 message+跳过（url 的 accept 语义是「站外已完成」，应用内不支撑）
- 应答经 T1.5 六端点 respondPermission/respondElicitation；应答成功不动缓存——卡片移除由 SSE pendingClosed 帧驱动（无乐观删除回滚面）；应答中整卡禁用+所点按钮 loading（等待态可见优先）；纯函数层 `elicitationForm.ts` 单测覆盖 adapter 真实形态样本

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

**本项目**：`server/internal/bot/`（`orchestrator.go:73-239` 回合编排、`driver_claude.go:26-103` headless spawn、`stream.go:36-140` 回复泵、`channel.go:17-44` 渠道契约、claude 二进制 = vendored 自带（`agentcatalog/claudebin.go`）、`supervisor.go:106-108` 装配）、`server/internal/bot/wecom/`（`channel.go:223-236` 消费循环、`reply.go:15-60` 流式回复、`inbound.go:33-46` 会话键）、`app/src/shared/app_config.rs:51` 配置表、`packages/web/src/shared/appConfig.ts:71-79` 启动 key SSOT、`EmbeddedTerminal.tsx:86-117/180-194` 消费与 spawn、`t_workspaces`（`workspaces.gen.go`）。

**Gold-Band**：`src/app/intervention.rs:366-426,915-1030`（干预命令服务 + 权限选项映射）、`src/acp/adapter.rs:52-180`（通用 adapter + 能力协商）、`src/acp/client.rs:2673-2731`（doctor）、`src/acp/permission.rs / elicitation.rs`（pending 落盘等待）、`src/im/inbound.rs:192-261`（入站动作 + 幂等收敛）、`src/im/connectors/wecom.rs:612-1243`（vote_interaction 卡构造、终态更新）、`resources/agent-catalog.json` + `scripts/prepare-agent-catalog.ts`（catalog pin）、`src-tauri/src/im_runtime.rs:1159-1246`（IM→命令服务汇合）。**License：AGPL-3.0-only**（仅行为语义参照，禁止代码级移植）。

**开源库选型（D8 依据，四库对比终态）**：`github.com/BrokkAi/acp-go`（Apache-2.0，**选定**，pin v0.11.0，Go ≥1.27.1，零传递依赖，wire 类型由官方 JSON Schema 生成 + parity/fuzz/真 agent integration 测试矩阵（矩阵含 `npx claude-agent-acp` 与 codex-acp），含 runner 进程管理与 cookbook；Brokk 公司仓库自治 bot 集群生产自用）、`github.com/coder/acp-go-sdk`（Apache-2.0，外部生态最大——Docker/Pomerium/Plural 等约 74 个 module 集成，连接层含通知-响应屏障等高质量细节，零依赖；维护冻结，schema 停更落后上游，issue/PR 无人处理，elicitation 标 UNSTABLE，无主动 Close 原语）、`github.com/ironpark/acp-go`（MIT，方法覆盖最全、协议追踪最新，acp1/acp2 双包 + HTTP/WebSocket draft + v2→v1 回落 router；无 tag 可钉，个人项目）、`github.com/beyond5959/acp-adapter`（MIT，agent 侧可执行 adapter 非 client 库，claude 桥权限应答 no-op、tool_call 不上报、停更，排除）、官方 TS SDK `agentclientprotocol/sdk`（npm，协议语义权威参照）。

**参考集成项目（阶段 2/3 审批卡/表单卡同构参照）**：`pomerium/agentops`（client 侧把 `session/request_permission` 渲染为 Slack Block Kit 交互卡并路由回 ACP 会话——企微审批卡同构）、`docker/docker-agent`（agent 侧 elicitation 桥与能力协商矩阵）。

**权限语义调研（P6 / §1.4 依据）**：本项目 `packages/web/src/windows/panel/ImBotsPage/ImBotDrawer.tsx:44-60`（执行权限两档 ↔ allowedTools 转译）、`server/internal/bot/types.go:17-42`（白名单 SSOT 与空值语义）、`server/internal/bot/driver_claude.go:32-50`（argv 拼装）；Gold-Band `src/acp/permission.rs:107-307`（pending/response 落盘与 first-writer-wins 预约）、`src/app/intervention.rs:915-1100`（选项归类与 requires_desktop 安全过滤）、`src/acp/client.rs:4927-4977`（mode 经 set_config_option 下发）、`src/acp/adapter.rs:58-113`（disallowedTools deny-list 注入）；官方 `github.com/agentclientprotocol/claude-agent-acp`（src/acp-agent.ts：`_meta.claudeCode.options` 透传面与 permissionMode 排除、src/permissions/*：mode 目录与 allow_always 持久化、Node ≥22、两仓库合并事实）、ACP 协议文档 `docs/protocol/v1/tool-calls.mdx`（`RequestPermissionOutcome` / `PermissionOptionKind`）、`BrokkAi/acp-go`（agent/host.go `Permissions` interface、v2/permissions.go `CancellablePermissions`）。
