# ACP 统一会话与 IM 无缝交互——方案定稿与任务清单

> 状态：方案定稿（技术决策 D1–D7 已定）；P1–P5 倾向已定，开工前经 **T0.0 拍板**确认
> 日期：2026-09-29（同日整理为「方案 + 任务清单」结构）
> 范围：issue 执行模式扩展（ACP 第三模式）、workspace 级启动配置、bot 复用 ACP 会话、企微模板卡片交互
> 参考：`~/MyFiles/Project/Gold-Band`（ACP runtime + IM 远程干预的已验证实现）
>
> **状态标记**：⬜ 待开始 | 🔲 进行中 | ✅ 已完成
>
> **状态回写规则（内置，无需人工提醒）**：任务实现完成后，执行方（开发 Agent）在总结阶段
> 直接把对应任务状态改为 ✅ 并补带日期的「实施定稿」段落（记录方案变更/偏离），同步提交
> ——不等用户单独指示。实施中若需偏离方案，先修订本文对应设计段落再动代码。

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

---

## 2. 目标架构总览

```
阶段 0（T0.0–T0.3）：workspace 级 launch_settings
  t_workspaces 加 JSON 列 + Drawer 表单 + 消费链改造
  语义升级：mode 枚举（terminal-manual / terminal-auto / acp）+ agentId

阶段 1（T1.1–T1.7）：Go sidecar 通用 ACP client + issue 主窗口 ACP 模式
  ├─ 通用 ACP client（协议实现，非移植；command+args 驱动，能力协商分支）
  ├─ agent catalog（离线 pin，一期只启用 claude-acp）
  ├─ doctor 握手探测（claude / node / adapter 三项）
  ├─ issue 主窗口 ACP 视图（底部任务描述入口，回车触发 session/prompt）
  └─ 事件流经 sidecar HTTP/SSE 到 panel

阶段 2（T2.1–T2.4）：bot 复用 ACP 会话
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
| D6 | `launch_settings` 用 JSON 列而非多列 | mode + agentId + 后续 agent 配置持续扩展，避免反复迁移 |
| D7 | issue↔session 绑定受控反转（原 `claude_session_ref` 链曾随 chat 视图删除） | ACP runtime 亲自持有会话后语义成立，实施定稿须明示（见 T1.5） |

## 4. 待拍板决策点（P1–P5，T0.0 逐项确认）

| # | 问题 | 倾向 |
|---|---|---|
| P1 | ACP 模式粒度：per-issue 配置 vs 全局默认 + issue 可覆盖 | per-issue，与现有终端模式配置位置同构（阶段 0 落在 workspace 级，issue 级是否再覆盖待定） |
| P2 | bot↔issue 映射：显式绑定 + IM 内切换（如 `#issue-42 帮我…`） vs 自动挂 workspace 当前活跃 issue | 显式绑定，避免隐式状态跳变 |
| P3 | 终端模式 ↔ ACP 模式跨模式会话接续（`--resume` 捞回）是否纳入一期 | 先做纯 ACP 会话，接续作为一期后的增强（纳入则多一轮边界测试） |
| P4 | agent 范围：一期 catalog 只放 claude-acp vs 直接全量 | 只放 claude-acp（Gold-Band 出厂同款），通用架构到位即可 |
| P5 | claude 二进制哲学：adapter 包自带 CLI（Gold-Band 默认）vs 沿用本机探测链 | 沿用本机 `resolveClaudeBin` 探测链，与终端模式共享同一份 claude，行为一致 |

## 5. 风险与开放问题

1. **Go ACP client 自研工作量**：协议面小但工程细节多（超时、取消、进程组回收、崩溃恢复）；Gold-Band client.rs ≈1.1 万行中大头是其 runtime VM，协议本体远小于此，预计 Go 侧可控，但需以 T1.1 实施定稿收口；
2. **adapter 版本演进**：官方 adapter 两周三版（Gold-Band pin 0.81.2 → 现 0.84.0），catalog pin 策略 + 升级验证流程在 T1.2/T1.3 定稿；
3. **终端模式与 ACP 模式并存边界**：同一 issue 切换模式时的会话接续（`claude --resume` 可跨形态接续同一 session id）——按 P3 倾向不入一期；
4. **企微回调 5 秒窗口**：审批点击后更新原卡必须在 5 秒内完成，跨 sidecar 重启的边界场景在 T3.2 测试覆盖。

---

## 6. 任务清单（进度 SSOT）

> 任务粒度：模块 + 功能 + 技术方案，一个任务 ≈ 一次可独立验收的交付。
> 跨阶段可并行项已用依赖关系标出（如 T3.1 不阻塞于阶段 1/2）。

### 前置

#### T0.0 拍板确认 P1–P5 决策点

**状态**：⬜

**内容**：与用户逐项确认 §4 的五个决策点。各决策点「倾向」即本任务清单当前采用的工作假设——拍板结果与倾向不一致时，先修订受影响任务的技术方案与关联设计段落，再开工对应任务。

**依赖**：无（一切开发任务的前置；阶段 0/1 中不依赖拍板结果的部分可先行）

---

### 阶段 0：workspace 级 launch_settings

**设计要点**：

- 现状：「应用嵌入终端 - 启动设置」是全局 appConfig 单值 `terminal_startup_code_cli`（`'none' | 'claude'`，默认 `none`，存 `app.db` 的 `app_config` 表，`app/src/shared/app_config.rs:51`，SSOT 在 `packages/web/src/shared/appConfig.ts:71-79`）；消费端两处——`EmbeddedTerminal.tsx:86-117`（仅主 pane 直启，分屏 pane 恒裸 shell）与 `WorkspaceInitGate.tsx:94`（引导文案）；`t_workspaces` 仅 `name / dir / description` 三列，无配置承载。
- **取值优先级**：`workspace.launch_settings`（新）→ 全局 `terminal_startup_code_cli`（过渡回落）→ 默认 `none`。
- JSON 列而非多列 = D6（即将承载 mode + agentId + 后续 agent 级配置，避免反复迁移）：

```json
{
  "mode": "terminal-manual | terminal-auto | acp",
  "agentId": "claude-acp",
  "autoCommand": "claude"
}
```

- 改表约定：直接编辑原迁移文件、不新建 goose 迁移、不考虑历史数据（本项目既有约定），改完跑 `pnpm server:gorm:gen`。
- **本阶段是阶段 2 的前置**：bot 发起会话（T2.1/T2.3）将直接读同一份 workspace 级 launch_settings；`agentId` 字段与阶段 1 catalog（T1.2）耦合，故与阶段 1 排在一起做。

#### T0.1 `t_workspaces` 新增 launch_settings 列（数据链路）

**状态**：⬜

**功能**：workspace 级启动配置的存储与后端读写链路

**技术方案**：
- 按改表约定编辑原迁移文件为 `t_workspaces` 加 `launch_settings` JSON 列，跑 `pnpm server:gorm:gen` 重新生成 DO 层（`workspaces.gen.go`）
- `server/internal/dal/types/workspace.go` DTO + tracker/workspace 域服务透传
- 空值语义：NULL → 消费端走回落链（不把 NULL 当非法态）

**依赖**：无

**决策关联**：D6；P1（issue 级若再覆盖一层，影响读取优先级链的最终形态，拍板后修订）

#### T0.2 WorkspaceDrawer 启动设置表单

**状态**：⬜

**功能**：workspace 编辑抽屉增加「启动设置」区块

**技术方案**：
- `WorkspaceService.ts` 模型/请求字段补 `launchSettings`
- `WorkspaceDrawer.tsx` 表单区块：mode 三选一（终端手动 / 终端自动 / ACP）+ ACP 模式下 agentId 选择（一期固定 claude-acp；catalog 未就绪前先以常量占位，T1.2 落地后切换数据源）
- 空值 = 跟随全局回落（不强制填写），存量用户行为不突变

**依赖**：T0.1

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
- **实现定位：协议实现，不是库移植**——ACP 是线协议 + 公开 schema（agentclientprotocol.com），Go client 照 schema 实现十来个 JSON-RPC 方法，无 TS 代码可抄；ACP 官方 SDK 是 Rust crate，**无官方 Go SDK**，Go 侧本就自研。
- **第一天就按通用 adapter 写**（D5/D2）：client 只认 `command + args`，Claude 专属行为全部走 `initialize` 能力协商分支（如检测 `/_meta/claudeCode` 扩展），不进主干代码。
- **spawn 策略表**（D3：官方 adapter 起步），catalog 中每个 agent 声明自己的启动方式，client 无感：

| 策略 | 适用 | 启动形态 |
|---|---|---|
| `npx-adapter` | claude（一期唯一启用） | `npx -y @agentclientprotocol/claude-agent-acp@<pin>`（官方 TS adapter，内包 Claude Agent SDK） |
| `native-acp` | gemini（`--acp`）、codex（原生路径） | 直连 CLI，零 adapter |
| `in-process-go` | **预留，不实现** | Go 进程内实现 ACP agent 角色（`driver_claude.go` 的 stream-json 积累可复用） |

- 为什么官方 adapter 起步而不是直接 Go 自研——成本分布：

| | 官方 npx adapter | Go 进程内 adapter |
|---|---|---|
| 环境成本 | 需 node + npx（见 T1.4） | 零新增 |
| 协议维护 | **零**（claude CLI 协议演进由官方 adapter 团队追，两周三版的跑步机不归我们） | **全扛**（企微 SDK 转义 Go 划算是因为企微协议三年不变；claude 控制协议是反例） |
| 供应链维护 | 版本 pin + 升级验证（catalog 锁版本，刷新脚本统一升） | 无 |

- **Go 进程内 adapter 的触发条件**（满足任一再启动，届时沉没成本接近零）：① doctor 数据显示相当比例用户机器缺 node（原生安装器装 claude 的那批）；② 目标场景包含离线/内网环境；③ node 中间层出现真实痛点（启动延迟、僵尸进程、每会话多一个 node 进程的内存开销）。
- **node 依赖**（D4）：不随 app 打包（包体 +几十 MB、三平台构建矩阵变重）；目标用户装 Claude Code，npm 安装路径必然带 node，原生安装器路径可能没有——不猜，doctor 探测，缺失时 UI 明确引导。

#### T1.1 Go ACP client 核心

**状态**：⬜

**功能**：sidecar 内通用 ACP client（JSON-RPC over stdio），可驱动任意 catalog 声明的 ACP agent

**技术方案**：
- 照 ACP 公开 schema 实现方法集：`initialize / session/new / session/prompt / session/request_permission / session/update / elicitation/*`
- 通用 adapter：client 只认 catalog 声明的 `command + args`；Claude 专属行为走 `initialize` 能力协商分支，不进主干
- 工程细节清单（风险 §5.1，实施定稿逐项收口）：超时、取消、进程组回收、崩溃恢复
- 行为语义参照 Gold-Band `src/acp/client.rs`、`src/acp/adapter.rs:52-180`（协议本体，非代码移植）

**依赖**：无

**决策关联**：D1、D2、D5

**验收**：以 catalog 条目拉起 claude-acp adapter，完成 initialize → session/new → 一轮 prompt → session/update 收流 → 进程正常回收

#### T1.2 agent catalog（离线 pin）

**状态**：⬜

**功能**：agent 目录数据源与刷新脚本，一期只启用 claude-acp

**技术方案**：
- 结构照 Gold-Band：官方 ACP registry 的离线 pin 快照 + 刷新脚本，运行时不在线拉取；落在 sidecar 内嵌资源
- 每条目声明 spawn 策略与启动参数（`npx-adapter` / `native-acp`；`in-process-go` 仅预留枚举）
- 一期只启用 claude-acp（Gold-Band 出厂同款），catalog/doctor 的 UI 面板砍到最小
- adapter 版本 pin 进 catalog（参考 0.8x 线），升级走刷新脚本统一升 + 验证流程定稿（风险 §5.2）

**依赖**：无

**决策关联**：D2、D3、P4

#### T1.3 adapter vendoring 与 claude 路径一致性

**状态**：⬜

**功能**：压掉官方方案的网络不确定性，并保证 ACP 模式与终端模式用同一个本机 claude

**技术方案**：
- Vendoring：首次在受管目录（如 `~/.ocean-harness/acp-adapters/`）`npm install` 固定版本，之后 spawn 指向 vendored 入口——「每次 npx 拉包」变「一次性安装」，离线可用，升级走自己的版本策略
- claude 路径一致性：沿用现有 `resolveClaudeBin` 三级探测链（`server/internal/bot/bin.go:38-68`）结果，经 adapter 的可执行文件指定口子（`CLAUDE_CODE_EXECUTABLE` 类环境变量）传入——ACP 模式与终端模式共享同一份 claude，不出现两套 CLI 版本漂移

**依赖**：T1.2

**决策关联**：D3、P5

#### T1.4 doctor 握手探测

**状态**：⬜

**功能**：agent 可用性判据 = 真实握手跑通；结果供 UI 引导与（阶段 3）bot 徽标消费

**技术方案**：
- 三项探测：claude 可用（`resolveClaudeBin`）、node/npx 可用（≥20）、adapter 包就绪（vendored 入口存在）
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
- 消费 workspace `launch_settings.agentId`（T0.1 链路）决定拉起哪个 agent
- 并发基线：单会话内回合串行（跨入口回合锁在 T2.2 升级）
- **D7 受控反转**：issue↔session 绑定是对当初随 chat 视图删除 `claude_session_ref` 决策的受控反转（`app/src/pty/claude_state.rs` 注释），不是回退——实施定稿须明示此点

**依赖**：T1.1、T0.1

**决策关联**：D1、D7；P3（跨模式 `--resume` 接续不入一期）

#### T1.6 issue 主窗口 ACP 视图

**状态**：⬜

**功能**：issue 主窗口新增 ACP 模式（第三执行模式）：会话视图 + 底部任务描述输入框

**技术方案**：
- 模式切换：读 workspace `launch_settings.mode`（含 P1 拍板结果）；终端模式两分支零改动
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

#### T2.1 bot ACP driver

**状态**：⬜

**功能**：`ClaudeDriver` 接口的 ACP 实现——bot 回合经 sidecar 内 ACP session 驱动 claude

**技术方案**：
- 实现 `server/internal/bot/driver.go` 接口，经 T1.5 会话域取 ACP session
- 渠道无关核心（orchestrator、回复合成）零改动
- headless spawn 路径（`driver_claude.go`）保留：workspace 未配 ACP 模式时 bot 走现状，两驱动并存

**依赖**：T1.5

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
- 现状会话键绑 workspace；扩展为显式绑 issue + IM 内切换（如 `#issue-42 帮我…`，具体形态按 P2 拍板结果修订）
- bot 发起回合读 workspace launch_settings（阶段 0 铺好的链路）

**依赖**：T2.1

**决策关联**：P2

#### T2.4 双入口审批收敛

**状态**：⬜

**功能**：企微 / 桌面双入口审批 first-writer-wins

**技术方案**：
- CAS + first-writer-wins：谁先点谁生效，后点方收到「已在另一端处理」
- 审批 pending 状态由 T1.5 会话域持有，两入口读同一份状态

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

**Gold-Band**：`src/app/intervention.rs:366-426,915-1030`（干预命令服务 + 权限选项映射）、`src/acp/adapter.rs:52-180`（通用 adapter + 能力协商）、`src/acp/client.rs:2673-2731`（doctor）、`src/acp/permission.rs / elicitation.rs`（pending 落盘等待）、`src/im/inbound.rs:192-261`（入站动作 + 幂等收敛）、`src/im/connectors/wecom.rs:612-1243`（vote_interaction 卡构造、终态更新）、`resources/agent-catalog.json` + `scripts/prepare-agent-catalog.mjs`（catalog pin）、`src-tauri/src/im_runtime.rs:1159-1246`（IM→命令服务汇合）。
