# 任务：ACP Go SDK 切换至 ironpark/acp-go

> **状态回写规则**：任务状态由执行方维护——🔲 未开始 / 🔄 进行中 / ✅ 完成；状态只记录在本文档一处；完成的任务在条目内补「实施定稿」记录实际落地差异，已完成任务的验收与定稿不回改。

## 状态总览

| 阶段 | 任务 | 状态 |
|---|---|---|
| P0 spike 验收（闸门） | T1 构建链路验收 | ✅ |
| P0 spike 验收（闸门） | T2 spike 集成验收 | ✅ |
| P1 正式接入 | T3 golden SSE 契约测试 | ✅ |
| P1 正式接入 | T4 internal/acp 换底 | ✅ |
| P1 正式接入 | T5 下游泄漏面对位 | ✅ |
| P1 正式接入 | T6 fakeagent 重写 | ✅ |
| P1 正式接入 | T7 测试迁移收绿 | ✅ |
| P1 正式接入 | T8 依赖收口与真握手 | ✅ |

## 需求背景

server 的 ACP 客户端当前基于 BrokkAi/acp-go v0.11.0，其生成模型丢弃 elicitation 关键字段，靠自研 raw-JSON wire 拦截器补偿。对三套 Go SDK（BrokkAi / coder/acp-go-sdk / ironpark/acp-go）对比后裁决：**切换至 ironpark/acp-go，不自研**；P0 spike 验收通过后按 P1 接入。原 coder 迁移方案废弃，其 8 任务骨架沿用。

## 背景与现状关键事实

**现状基线**

- `server/go.mod` 依赖 `github.com/BrokkAi/acp-go v0.11.0`；生成模型丢 elicitation requestedSchema/url（v0.12 亦未修），`server/internal/acp/elicitation.go` 以 raw-JSON wire 拦截器（`ElicitationWire`/`parseElicitationWire`/`elicitationWireInterceptor`）补偿
- coder/acp-go-sdk 已排除：全系列 `UnstableCreateElicitation` 变体缺 `sessionId`/`toolCallId`，规范内方法无 raw 拦截点（扩展钩子只认 `_` 前缀方法），手写装配层缺陷无修复迹象

**目标 ironpark/acp-go v0.2.0 已验证事实**

- MIT；`github.com/ironpark/acp-go`；schema 由官方 schema.json codegen（`schema/v1` + zod 校验器 + 快照/golden 测试）；单三方依赖 coder/websocket（stdio 主链路不引入运行时路径）
- **elicitation 全链路正确（三套中唯一）**：类型层 6 变体含 `sessionId`/`toolCallId`/`requestedSchema`/`url`（`CreateElicitationRequest{Form,URL,Custom}{Session,Request}`）；装配层表驱动生成派发 `acpconn.Request(ctx, schema.Validated(), params, h.CreateElicitation)`（`acp1/methods.gen.go`）；`elicitation/complete` 通知有类型
- `elicitation/create`/`session/request_permission`/`session/prompt` 走不限时调用（等待人类输入不受请求超时截断）；v1 prompt 的 ctx 取消发 `session/cancel` 且回合以代理取消答复收敛——与本项目软取消+收敛序诉求同构
- 约束：go ≥1.27.0 + `encoding/json/v2`（toolchain go1.27.1 满足版本线；GOEXPERIMENT=jsonv2 是否必需未验，T1 验收）

**防腐层（迁移契约，导出面不变）**

- `server/internal/acp/` 导出面：SpawnConfig/Launch/NewSession/SessionEvent/AgentClient/PendingPermission/PendingElicitation/PermissionMode 两档
- 不受影响面：`launch.go` 进程工具零库依赖；browser 域零协议依赖；agentcatalog 仅消费 `acp.SpawnConfig`
- 自研机制保留（不依赖库行为）：早到帧缓冲 TTL 5s/cap 64、软取消预算 10s + turnGen 代际、pending CAS-once、收敛序、事件背压 256（`server/internal/acp/events.go`）
- 泄漏面（BrokkAi 类型直达 wire，需对位替换）：`server/internal/acpsession/hub.go` Frame 的 Plan/Usage/Commands/Mode 字段 = SSE JSON 契约红线；`view.go`（约 25 个 schema 符号）；`manager.go`（IsAuthRequired/AcceptElicitation 系）；`server/internal/acpdoctor/doctor.go`（IsAuthRequired）
- 验收基建复用：`server/internal/acp/handshake_real_test.go`（`OCEAN_ACP_REAL_HANDSHAKE=1` 门禁：catalog→EnsureVendored→Launch→Initialize→NewSession→PromptText→end_turn 全链路）

## 技术决策

- **D1** 目标 SDK = ironpark/acp-go，只用 `acp1`（ACP v1）+ `schema/v1`；不启用 acp2（草案）/acphttp/acpmcp（本项目仅 stdio）。对端 vendored adapter 实发 `protocolVersion: 1`（v1 唯一，`claude-agent-acp/dist/acp-agent.js:1413` 硬编码，现 pin 0.93.0），全部已验证扩展面（claudeCode meta / configOptions / modes 双轨 / elicitation 形状）都在 v1；v2 为草案无生产对端，待官方转 stable 且 adapter 随 pin 升级时再评估（import acp1 不会把 acp2 链入二进制，依赖零成本）
- **D2** 自研不立项，降级为兜底路线（见文末）
- **D3** raw-JSON wire 拦截器退役：ironpark 类型层不丢字段，`elicitation.go` 补偿层整体删除
- **D4** 自研机制保留在 internal/acp，不改建到库行为上；取消链路映射到 prompt ctx cancel → `session/cancel` 语义；锁序红线（不得持 stateMu 发网络调用）保持
- **D5** 迁移骨架沿用已验证的 8 任务清单：golden 契约测试先行 → 换底 → 下游对位 → fakeagent 重写 → 测试迁移 → 真握手

## 风险

| # | 风险 | 缓解 |
|---|---|---|
| R1 | ironpark 单人维护、pre-1.0 API 漂移（v0.1→v0.2 已有 breaking） | go.mod 钉死版本；升级跟随 changelog 人工评审；防腐层保证换出成本有界 |
| R2 | json/v2 约束可能波及 release 构建链（CGO_ENABLED=0） | T1 前置验收；如需 GOEXPERIMENT 等构建环境变更，按环境级变更单独经用户确认 |
| R3 | claudeCode 扩展兼容未验证（init.Meta["claudeCode"] 读回、session/new _meta/configOptions 透传、modes 双轨） | T2 验收；硬阻塞则回兜底路线 |
| R4 | SSE JSON 契约回归 | T3 golden 测试先行守护 |
| R5 | 上游弃坑（bus factor 1） | MIT 允许 fork 接管，见兜底路线 |

## P0 spike 验收（闸门：任一硬阻塞 → 停止接入，回兜底路线评估）

### 任务 T1：构建链路验收

- 状态：✅
- 功能：确认 ironpark/acp-go 在本项目构建链可编译可链接
- 技术方案：`server/go.mod` 引入 `github.com/ironpark/acp-go v0.2.0`；`go build ./...` 探测 `encoding/json/v2` 是否要求 GOEXPERIMENT=jsonv2；CGO_ENABLED=0 复验 release 构建链（`pnpm server:release`）
- 依赖：无
- 验收：构建全绿；GOEXPERIMENT 结论明确（需要/不需要）；如需要，构建环境变更已单独经用户确认
- 实施定稿：**GOEXPERIMENT 不需要**——go1.27.1 下 `encoding/json/v2` 默认在 std 可用（库内引用为生产性，非仅测试），库全包编译 + `CGO_ENABLED=0 -ldflags '-s -w'` 链接探针（acp1/schema/v1 真实解码运行）均无实验开关通过，R2 环境变更分支未触发；依赖经 `go get` 引入（主模块 + `acphttp` 传递补 `coder/websocket v1.8.15`），暂为 `// indirect` 形态，T2 引用后 ironpark 转正；**T2 落地前勿跑 `pnpm setup`/`server:mod:tidy`**（未被引用的依赖会被清出 go.mod）；elicitation 全家族类型（6 变体 + `CompleteElicitationNotification`）在 schema/v1 确认在位

### 任务 T2：spike 集成验收

- 状态：✅
- 功能：ironpark 直连 vendored adapter 跑通最小链路并验证扩展兼容
- 技术方案：env 门禁 spike 测试（参照 `handshake_real_test.go` 门禁模式，独立文件不触碰生产代码）：catalog→VendoredSpawnConfig→ironpark acp1 连接 vendored claude-agent-acp 0.93.0→initialize（声明 elicitation 能力）→session/new（携带 configOptions 的 _meta 透传）→prompt→收流；断言：init 结果可读 `Meta["claudeCode"]`、modes 双轨字段形状（availableModes/currentModeId）、elicitation 请求解码含 `sessionId`/`toolCallId`
- 依赖：T1
- 验收：spike 测试在 `OCEAN_ACP_REAL_HANDSHAKE=1` 下全断言通过；claudeCode 扩展 / modes 双轨 / elicitation 全字段三项逐项有结论
- 实施定稿：**三项验收全过，无硬阻塞（P1 放行）**。①claudeCode 扩展：标记实际在 `agentCapabilities._meta.claudeCode = {promptQueueing:true}`（顶层 _meta 仅 steering，任务原假设的位置有误），ironpark `InitializeResponse.AgentCapabilities.Meta` typed 可读——移交 T4：现 `HasClaudeCodeExtension` 查顶层对真实 adapter 恒 false，换底时改读 agentCapabilities._meta。②modes 双轨：`NewSessionResponse.Modes{CurrentModeID, AvailableModes}` 直接可读；初始 currentModeId 只来自文件级 settings（newSession 仅透 resume，options.settings 只进 SDK programmatic settings，_meta 无法控制初始模式）。③elicitation 全字段：FormSession 变体解码 SessionID/ToolCallID/RequestedSchema 均 typed 不丢；触发路径 = prompt 显式点名 AskUserQuestion → adapter canUseTool 拦截转 form elicitation（client 须声明 elicitation.form，否则工具进 disallowedTools），且先收 tool_call update、toolCallId 与 elicitation 一致。_meta 透传闭环以 `_meta.systemPrompt` 注入名字 token 行为验证（ironpark Meta → wire → adapter 消费）。落地：`server/internal/acp/spike_ironpark_test.go`（TestIronparkSpike，与 TestRealHandshake 同门禁同装配链路），ironpark 转正 direct 依赖

## P1 正式接入（T2 通过后执行）

### 任务 T3：golden SSE 契约测试先行

- 状态：✅
- 功能：把 acpsession hub 的 SSE JSON 契约钉死，换底后下游零感知
- 技术方案：对 Frame 序列化输出建 golden 快照（Snapshot/Status/Entry/Plan/Usage/Commands/Mode 等字段），换底期间 golden 不许变
- 依赖：无（可与 P0 并行）
- 验收：golden 测试进常规轮次且当前 BrokkAi 实现下通过
- 实施定稿：`server/internal/acpsession/golden_sse_test.go`（TestGoldenSSEFrames，18 个子用例逐 FrameType 全字段装配）+ `testdata/sse_golden/*.json`（18 个基线）；比对按解码后语义等价（键序无关，DeepEqual），漂移 diff 以缩进 JSON 呈现；`-update` flag 回写基线。覆盖面：Frame 信封 seq/issueId/type、ViewSnapshot 全字段（modes/plan/usage/commands/entries 四类条目含 EntryDisplay/Quote/Files、pendings 双形态）、StatusPayload 四变体、ToolCallView 三种 ToolCallContent 变体（content/diff/terminal）+ locations/rawInput/rawOutput、PendingView permission（PermissionOption 目录 + ToolCallUpdate）与 elicitation form/url 两变体（ElicitationWire 全字段含 requestedSchema/elicitationId）、TurnPayload 三态（started/ended+stopReason/ended+error）——omitempty 取舍全部钉死。wire 形态依据：SSE handler 直接 `SSEvent("message", frame)` 直出，Frame 的 json.Marshal 即契约。常规轮次（无环境门禁）`go test ./internal/acpsession/` 全绿，gofmt/vet 干净

### 任务 T4：internal/acp 换底

- 状态：✅
- 功能：ACP 客户端实现从 BrokkAi 换到 ironpark acp1，导出面不变
- 技术方案：`client.go`/`session.go`/`interact.go`/`mode.go`/`events.go` 换 ironpark acp1 对位 API；`elicitation.go` 的 ElicitationWire/parseElicitationWire/elicitationWireInterceptor 整体删除（D3）；Initialize 的 elicitation 能力声明与 NewSession _meta 透传按 ironpark API 对位；软取消/收敛序映射 ctx cancel → `session/cancel`；锁序红线保持
- 依赖：T2
- 验收：internal/acp 编译通过；包内单测迁移后全绿；防腐层导出面签名零变化
- 实施定稿：**全绿落地，fakeagent 暂留 BrokkAi agent 侧（v1 协议兼容，T6 重写）**。连接装配 `acp1.ConnectAgent(background, acp.NewStdioTransport(stdout, stdin), newClient)`（AgentClient 实现 acp1.Client + ElicitationHandler，caps 显式装 Elicitation.Form + Session.ConfigOptions.Boolean）；HasClaudeCodeExtension 改读 `AgentCapabilities.Meta`（T2 定稿位置）；unknown session 路由拒绝用 `acp.InvalidParams`（-32602）。软取消映射：prompt 走 `ClientSession.Prompt(turnCtx)`（turnCtx 与调用方 ctx 解耦，库内建「ctx 取消 → 发 session/cancel 且请求不灭 → 代理应答收敛」），桥接 = 调用方 ctx done → **先同步直发 `handle.Cancel`（保 wire 序：cancel 通知先于取消应答落管道，不依赖竞速）** → settlePendings → turnCtx 收口（库 AfterFunc 兜底幂等重发）；预算超时改为「关闭 giveup 信号放弃 Wait」（库请求 detached 无强拆手段，库侧回合待代理应答后自清理，窗口期新回合被库 ErrTurnInProgress 拒 → 映射 ErrTurnActive），-32800/request-cancelled 仍归一 StopReasonCancelled。D3 落地形态细化：拦截器/ctx 透传/WireFromRequest 降级/PendingElicitation.Request() 整体删除；ElicitationWire DTO 保留（SSE 契约面），投影改为从 `params.RawJSON()` 直接解析（typed 变体虽全字段在位，但 requestedSchema 经 typed 解码再编码有键序/未知字段漂移风险，保真优先）；wire 解析恒成功（库已校验合法 wire），parseElicitationWire 返回值简化。mode.go：SetSessionMode 对位 + 本地目录预检保留，成功后 setModeID 确认合并（通知回写兜底），锁序红线保持；BrokkAi 双轨错误类型无对位（agent 侧报错透传）。早到帧缓冲/背压 256/CAS-once/turnGen 代际/收敛序全部原样保留；NewSessionParams.Meta 换 acp1.Meta。fakeagent 最小调整：permission 脚本在 RequestPermission 返回后检查 canceled 标记 → cancelled 终止（模拟真实 agent 尊重 session/cancel 的收敛语义，配合 cancel 先行直发保序）。测试迁移：chain_test/interact_test/handshake_real_test 的 BrokkAi 符号换 acp1 对位（chunk 判别走 `Update.Variant()` 类型开关、应答构造走 NewCreateElicitationResponse/NewElicitationContentValue、wire 解析测试改走 typed 变体构造 + CustomSession 覆盖未知 mode）；TestLoadSessionCapabilityRejected 断言待真机验证（能力未声明错误来源从库本地协商变 agent 报错，fakeagent 侧路径不变）。验证：`go build ./internal/acp/` + `go vet` + `go test -race -count=2 ./internal/acp/` 全绿；下游破损面精确限于 acpsession/acpdoctor（T5 范围）；go.mod 中 BrokkAi 仍被 fakeagent 引用（T8 出清）。**审查修复（4 项）**：①cancelTurn 预算定时器改 CAS-once 武装（同 gen 二次取消排布的陈旧定时器不得对已关闭 giveup 二次 close——状态翻转滞后于 close 会击穿 panic），TestCancelTurnSoftCancel 加双触发回归；②SessionUpdate 改锁内只查表、锁外分发（修正与 registerSession 不变量自述矛盾的旧继承模式——满通道入队持 sessMu 会队头阻塞其他会话的交互路由；迟到事件由 enqueue 的 terminated 闸门丢弃）；③注释收敛 5 处至 ≤3 内容行；④删除零调用死代码 initialization()。审查确认无问题项：软取消桥接主路径、锁序红线（无持 stateMu/turnMu 网络调用）、converge 幂等、早到帧缓冲边界、elicitation 三变体路由、D1/D3/D4 对齐、防腐层导出面零变化

### 任务 T5：下游泄漏面对位

- 状态：✅
- 功能：生产代码 BrokkAi 类型引用替换为 ironpark schema/v1 对位类型，SSE JSON 契约不变
- 技术方案：`acpsession/hub.go` Frame 的 Plan/Usage/Commands/Mode 换 schema/v1 对位类型；`view.go`（约 25 符号）、`manager.go`、`acpdoctor/doctor.go` 逐符号对位；若 ironpark 类型 JSON tag 与旧值不一致，在防腐层做显式映射而非改 Frame tag
- 依赖：T3、T4
- 验收：`go build ./...` 全绿；T3 golden 全部通过（SSE 契约零漂移）
- 实施定稿：**全绿落地，golden 全部通过（SSE 契约零漂移）**。统一 import `github.com/ironpark/acp-go/acp1`（对 schema/v1 全别名）。三处需显式映射/转换的契约差异：①ironpark 的 SessionUpdate 变体 marshal 携带 `"sessionUpdate"` 判别键而旧契约 plan/usage 是纯载荷——防腐层新增 `PlanView`/`UsageView` 映射类型（view.go，字段与旧契约逐字段一致含 `_meta`），applyUpdateLocked 从变体收敛，Frame tag 不动；②ironpark v1 无独立 `Plan`/`UsageUpdate` 顶层类型（计划/用量即变体本身，wire 形态相同）；③`ToolCallUpdate` 与 `SessionUpdateToolCallUpdate` 字段全同的两个类型经显式结构体转换对位。逐符号对位：applyUpdateLocked 改 `Update.Variant()` 类型开关（chunk×2/toolCall/toolCallUpdate/plan/usage/commands/mode 七变体，default 丢弃 user_message_chunk 等）；elicitation 事件删 WireFromRequest fallback（wire 恒非 nil）；RawInput/RawOutput `jsontext.Value` → `json.RawMessage` 转换（raw bytes 保真）；`IsAuthRequired` → `IsCode(err, ErrorCodeAuthRequired)`（同为 -32000）；RespondElicitation 的 any 直赋改 `elicitationContentValue` 四型收敛（string/float64/bool/[]string，其余显式拒绝）。测试对位：view_test/manager_test/doctor_test/golden_sse_test + bot 两测试文件（driver_acp_test/pending_gate_test）BrokkAi 符号全清，update 构造走 `NewSessionUpdate(variant)` 泛型 helper。验证：`go build ./...` + `go vet ./...` 全绿；`go test` acpsession/acpdoctor/acp/bot 全绿；BrokkAi 残留仅 fakeagent（T6 范围）

### 任务 T6：fakeagent 重写

- 状态：✅
- 功能：测试桩从 BrokkAi 接口面迁到 ironpark Agent 面
- 技术方案：按 ironpark acp1.Agent 接口重写 fakeagent；ironpark agent 侧全 typed，raw JSON 直发通道可删
- 依赖：T4
- 验收：依赖 fakeagent 的测试全绿
- 实施定稿：**全绿落地，消费者测试零改动**。装配 `acp1.NewAgentSideConnection(闭包存 conn, acp.NewStdioTransport(os.Stdin, os.Stdout))` + `conn.Start`；必需 4 方法全指针签名（CancelSession 为通知、与 Prompt handler 并发）；可选能力承载于可选接口 `SessionLoader`/`SessionModeSetter`。D3 落地：raw JSON 直发通道整体删除，elicitation 改 `NewCreateElicitationRequest(FormSession{...})` typed 直发（扁平 wire 形态由 typed 变体序列化自然保真），应答 union 走 `As[T]()` 判别（accept 的 `Content["choice"].As[string]` 回显）。能力门控：Initialize **手工填** AgentCapabilities（仅 load 置 LoadSession），保留「能力未声明 → 客户端 requireCapability 拒绝」路径——单类型恒实现 SessionLoader（库按接口实现路由、与能力广告解耦），非 load 脚本防御路径返回 `acp.MethodNotFound("session/load")`；T4 遗留的「TestLoadSessionCapabilityRejected 断言待真机验证」由此定论：拒绝来源是客户端能力预检，agent 侧报错仅纵深防御。软取消：CancelSession 置 canceled 标记 + permission 返回后查标记收敛 cancelled，与 client 侧「cancel 通知先行直发」构成确定性收敛（库保证响应等待其前读入通知处理完毕，非竞速）。9 个脚本名/回显文本/时序语义逐字保留；`ptr[T]` 泛型助手合并旧 strPtr/boolPtr。typed 换底固有形态：late-permission 请求 wire 携带零值 toolCall（值类型无法省略字段），客户端无活动回合路径不读该字段，无消费影响。验证：gofmt/vet/build 全净，acp/acpdoctor/acpsession/bot 四包 `go test` 全绿，`-race -count=2`（acp/acpsession）复验绿；BrokkAi 源码引用清零（go.mod 条目留 T8 出清）；审查（正确性/规范/简洁三组）无 ≥80% 置信度缺陷

### 任务 T7：测试迁移收绿

- 状态：✅
- 功能：acp / acpsession / acpdoctor 全部测试在 ironpark 底座迁移完成
- 技术方案：既有测试文件迁移；T2 spike 断言收编（并入常规 mock 测试或保留 env 门禁）；`pnpm server:dev` 产物下常规测试轮次全绿
- 依赖：T5、T6
- 验收：server 相关包 `go test ./...` 全绿
- 实施定稿：**全绿落地，T2 spike 断言收编进 TestRealHandshake（env 门禁保留）**。测试迁移主体已随 T4/T5/T6 完成（含 mock 链路对 spike 可 mock 断言的既有覆盖），本任务实测验收 + spike 收编。spike 独立文件删除——「独立于生产代码直连验证」前提在 T4 换底后失效，四组真机断言全部改走生产面对位：claudeCode 扩展（`HasClaudeCodeExtension` 由日志升级为硬断言）、modes 双轨形状（`session.Modes()` 快照：default/acceptEdits/plan/auto 目录齐全 + currentModeId 在表内）、`_meta.systemPrompt` 透传闭环（NewSessionParams.Meta 注名 token 问名轮）、elicitation 全字段（`SessionEvent.Elicitation` → ElicitationWire 断言 sessionId 会话归属、toolCallId 与先前 tool_call update 一致、requestedSchema form 载荷保真，accept 应答后 end_turn 收敛）。新增 runHandshakeTurn 轮级运行器：事件排空至收敛、权限事件防御性取消、回合应答后非阻塞排空尾随 chunk（chunk 入队先于应答解析的 select 竞速兜底）、轮级 180s 上限；原「只回复 OK」轮由问名轮取代。验收实测：`go test ./...` 全 server 包 exit=0（19 包 ok）、`-race` acp/acpsession/acpdoctor/bot 四包全绿、gofmt/vet/build 全净；BrokkAi 代码级引用清零（golden_sse_test 仅存历史背景注释）；T8 真机验收载体即本用例（elicitation 真机回归随之并入，T8 技术方案不变）

### 任务 T8：依赖收口与真握手验收

- 状态：✅
- 功能：BrokkAi 依赖出清，真链路验收
- 技术方案：go.mod 移除 BrokkAi/acp-go 及无引用间接依赖；`OCEAN_ACP_REAL_HANDSHAKE=1` 跑 `handshake_real_test.go` 全链路（catalog→EnsureVendored→Launch→Initialize→NewSession→PromptText→end_turn + 文本 chunk）；elicitation 路由回归（删补偿层后确认澄清表单仍按 `sessionId` 正确路由）
- 依赖：T7
- 验收：go.mod 无 BrokkAi 残留；真握手验收通过；elicitation 回归通过
- 实施定稿：**三项验收全过，8 任务收口**。依赖出清：`go mod tidy` 移除 BrokkAi（go.mod direct require 1 行 + go.sum 哈希 2 行；v0.11.0 自身零依赖，无引用间接依赖为空集故零连带），出清后 `go build` + `go vet` + `go test ./...` 全 server 包全绿。真握手：`TestRealHandshake` PASS（27.5s，claude-agent-acp 0.93.0 / 协议 v1），四组真机断言全过——claudeCode 扩展、modes 双轨目录、`_meta.systemPrompt` 透传闭环（轮 1 实机答出注入 token）、elicitation 全字段（轮 2 form 触发：sessionId 会话归属、toolCallId 与先前 tool_call update 一致、requestedSchema 载荷保真、accept 应答后 end_turn 收敛），elicitation 路由回归随之完成。过程发现并修复 T7 遗留测试缺陷：`runHandshakeTurn` 排空循环 default 分支仅赋值不退出，事件排空后主 goroutine 无限热自旋直至 go test 看门狗（两轮超时 dump 钉死同点）；修复为 default 直接 return 排空结果（仅 `handshake_real_test.go` 测试代码，生产面零改动）。连带核验库侧 Close 收敛序：`acpconn.Run` 在 conn.Done() 后 ExitGrace(5s) 兜底 transport.Close 解除 readLoop 阻塞，`agent.Close()` 先于 kill 的 converge 顺序有界无死锁，维持原序

## 兜底：自建 acp-go-sdk（不排期，记录进入条件）

触发条件（满足其一即启动）：
- P0 spike 出现不可绕过的硬阻塞（构建链 / 扩展兼容无解）
- ironpark 停止维护且需跟进 schema 演进 → 直接 fork 接管，改名 `github.com/<org>/acp-go-sdk`（MIT 允许），整体继承其生成器与测试体系

蓝本：ironpark 的实现路径即自建蓝本——官方 schema.json codegen + zod 校验 + golden 快照；连接层如需自写可移植 coder/acp-go-sdk 连接模型（Apache-2.0 署名）；elicitation 类型以官方 spec 为 SSOT（`sessionId` 为正式字段）。
