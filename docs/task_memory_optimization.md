# 内存优化——任务清单

> 状态：实施中
> 范围：内存占用优化六个任务——操作台帧流双层节流与可见性闸门、ToolCallItem 折叠卸载与载荷懒渲染、SSE 订阅失败终态、ACP 适配器闲置回收、MessageList 虚拟化、ACP 消息落库与分页。本文档自含全部设计事实，是进度唯一 SSOT。
>
> **状态标记**：⬜ 待开始 | 🔲 进行中 | ✅ 已完成
>
> **状态回写规则（内置，无需人工提醒）**：任务开始执行时置 🔲；任务实现完成后，执行方
> （开发 Agent）在总结阶段直接把对应任务状态改为 ✅ 并补「实施定稿」段落（只记实施终态
> 与相对方案的偏离，作为当前事实；不写日期、不写「何时补充/修订了什么」的演变叙述——
> 变更历史归 git）——回写状态但不主动 git commit，提交动作必须经用户明确确认。头部
> 「状态」总览行随之同步：首个任务开始时改为**实施中**，全部任务 ✅ 后改为**已完结**。
> 实施中若需偏离方案，先修订本文对应设计段落再动代码。

---

## 1. 背景与现状关键事实

| # | 事实 | 代码位置 |
|---|---|---|
| 1 | 操作台 Stage 帧流订阅未传 `throttleMs`（=0 全帧率，Go 侧 screencast ~60fps 损伤驱动），迷你预览传 2000ms；订阅由 `issue != null && activatedProfile && ready` 驱动，无可见性闸门，浮层挂载即满帧率跑 | packages/web/src/windows/panel/DevWorkbenchPage/components/WorkbenchTools/BrowserPanel/BrowserStageOverlay.tsx:42；BrowserSessionCard.tsx:116；DevWorkbenchPage.tsx:567 |
| 2 | 前端帧消费为「每帧一次全流水线」：SSE 事件 → `setTick` 触发 React 子树重渲染 → effect 内 atob + 逐字节 Uint8Array + `createImageBitmap` + `drawImage`；`frameRef` 已是单槽 keep-latest | packages/web/src/state/browserSession/useBrowserFrameStream.ts:66-97；同目录 BrowserPanel/useFramePaint.ts:20-42 |
| 3 | Go 写侧节流仅在 `throttleMs>0` 时生效（帧泵内丢弃过期帧），协议已支持，无需改 Go | server/internal/controller/browser_session.go:184-219 |
| 4 | ToolCallItem 的 `<Collapse in={expanded}>` 未设 `unmountOnExit`，折叠态子树仍挂载——rawInput/rawOutput pretty-JSON 全文常驻 DOM；载荷全文渲染无截断 | packages/web/src/windows/panel/DevWorkbenchPage/components/AcpSessionView/ToolCallItem.tsx:44-51,57-93 |
| 5 | MessageList 全量 `entries.map` 渲染无虚拟化，项目无任何虚拟化依赖；每条 agent 消息一个 Streamdown（Shiki 双主题，每代码块数千 token span） | packages/web/src/windows/panel/DevWorkbenchPage/components/AcpSessionView/MessageList.tsx:49,140-156 |
| 6 | SSE 订阅失败走 `JsonFail` = HTTP 200 + `{code!=0}`，EventSource 视为成功按 ~3s 周期无限重连，每轮重连触发 `resolveLivePage` → 引擎 Forward 调用；同 pattern 存在于 browserSession 与 acpSession 的 events 端点 | server/internal/apis/response.go:24-30；server/internal/controller/browser_session.go:145-152 |
| 7 | ACP 适配器 per-issue 独占进程：`Manager.entries` 以 issueID 为 key，仅 Discard/StopAll/进程崩溃关闭；`Ensure` 已有幂等重建路径 + `t_issue_acp_sessions` 锚点表（存 session ID）；browser 域已有 idle 计时先例（订户 >0 暂停计时） | server/internal/acpsession/manager.go:50,104-118,429-471；server/internal/browser/manager.go:53 |
| 8 | ACP 会话消息全量驻留 Go 内存（`view.go entries` 无上限），sqlite 仅锚点表；SSE 建连下发全量快照；app.db（KV 配置）经 env 注入路径可由 Go 直读 | server/internal/acpsession/view.go:262-263；server/internal/acpsession/hub.go:19 |
| 9 | 配置体系：key 常量与默认值 SSOT 在 `packages/web/src/shared/appConfig.ts`，Rust 侧 get/set/reset_app_config（rusqlite app.db）；项目惯例「上限类常量命名对齐 iwFileMaxEntries」 | packages/web/src/shared/appConfig.ts；server/ workspaceFiles 域 |

## 2. 技术决策

| # | 决策 | 理由 |
|---|---|---|
| D1 | 帧流双层节流：Go 写侧 `throttleMs=300`（Stage 订阅参数）+ 前端 rAF 合帧消费（帧到只更新 frameRef，rAF 循环取最新帧绘制，绘制与 SSE 事件解耦）；浮层不可见时 `streamEnabled=false` 断流 | 写侧降帧率治 CPU 总量，rAF 合帧消灭「每帧一次 React 重渲染」，两层各自独立生效；rAF 是渲染闸门（keep-latest 语义），非时间性补丁，不违反编码规则 1 |
| D2 | ToolCallItem：Collapse 加 `unmountOnExit` + 展开态状态提升到折叠子树之外 + 载荷懒渲染（展开才渲染，挂载期内 memo，折叠零残留）+ 超长截断（>32768 UTF-16 码元（`tcPayloadMaxChars`）折叠展示 + 展开全文按钮） | 折叠即释放 DOM 是内存主治点；展开态不随卸载丢失；截断防单条超大载荷（按码元计：DOM 内存随码元线性，字节口径需 TextEncoder 切割无收益） |
| D3 | SSE 失败终态：订阅端点失败改真实 HTTP 状态码（不存在 404 / 已关闭 409），前端 phase 增加 failed 终态 + 手动重试；EventSource 对非 200 不自动重连（浏览器规范行为）。实施细化：browserSession/stream 订阅前预检 Row().Status（空串→404、idle/failed→409）；acpSession/events 预检 Get().Status（idle→404、terminated→409，failed 照常订阅），前端订阅加闸门（快照可订阅状态才连，治首开订阅先于 ensure 的竞态），ensure 受理回包在缓存为终态（idle/terminated）时落快照——终态无活流覆盖安全，保「重新启动」后闸门可重开；browserSession/events 全局单源订阅、无失败判定面，服务端与前端 hook 均零改动；Bind/Validate 参数错误维持 200 现状 | 治本消灭 3s 空转循环（预检在订阅之前，每次重连变成无副作用的即时 404/409）；404/409 为防御纵深，正常流由前端闸门保证 |
| D4 | 适配器闲置回收阈值走 appConfig 可配置（key `acpSessionIdleTimeoutMinutes`，默认 30），Go 侧经 app.db 直读；idle 判定 = 无进行中 turn 且 hub 无订户（对齐 browser 域先例） | 阈值属运营调优面，入配置体系是项目惯例；回收后 `Ensure` 走锚点重建 |
| D5 | 虚拟化库 = virtua（动态高度一等支持、体积小、维护活跃、React 19 兼容） | 对齐技术选型评估标准（流行度/稳定性/维护活跃度） |
| D6 | 消息落库：新增 entries 表（issue_id + seq + entry JSON），**直接编辑原迁移文件** + `pnpm server:gorm:gen`（不新建迁移、不考虑历史数据，项目拍板惯例）；Go 内存只留热窗口（最近 N 条，命名对齐 `iwFileMaxEntries` 惯例），SSE 快照仅带热窗口，分页 REST 供上滑加载 | 治本 Go 堆线性涨 + 全量快照下发；迁移策略遵循既有拍板 |

## 3. 任务清单

### 第一批（A 组：CPU 与 DOM 止血）

**任务 T1：操作台帧流双层节流 + 可见性闸门** ✅
- 功能：操作台浮层打开时帧率有上限、不可见时零帧，CPU 占用大幅下降
- 技术方案：`BrowserStageOverlay.tsx:42` 传 `throttleMs=300`；`useFramePaint.ts` 改 rAF 消费循环（frameRef 单槽不变，effect 事件分派只置脏标记，rAF 中取最新帧绘制）；`DevWorkbenchPage.tsx:567` 挂载点与 Overlay 内部补可见性判定驱动 `streamEnabled`
- 依赖：无
- 验收：Stage 打开期间 SSE 有效帧率 ≤ ~3fps；浮层关闭/不可见时无帧订阅；xterm 操作等交互不受影响

**实施定稿**：
- 落地面：新增 `packages/web/src/shared/useDocumentVisible.ts`（visibilitychange 订阅，同步初值）；`useBrowserFrameStream` 的 `tick: number` 改为 `hasFrame: boolean`（帧事件 `setHasFrame(true)` 同值跳过重渲染，仅首帧/清帧真正触发——消灭每帧一次 React 重渲染）；`useFramePaint` 改 rAF 消费循环（签名去 tick：循环以帧对象引用比对感知新帧，绘制中到达的帧不重入、下一轮取 frameRef 最新帧——合并跳帧语义天然成立；rAF 在 webview 隐藏时暂停，为可见性闸门之外的兜底）
- Stage 接线：`useBrowserFrameStream(profile, pageId, ready && visible, 300)`——写侧 300ms 节流（~3.3fps，验收口径 ~3fps 档达标）+ 窗口隐藏断流（面板关托盘/最小化即停订，恢复显示重订回放首帧）
- 闸门覆盖扩展（相对方案的补充，同问题两面）：`BrowserSessionCard` 的 `streamEnabled` 追加 `&& visible`——面板窗口隐藏时会话卡迷你预览（2s 节流档）同停；面板收起（panelVisible）闸门原样保留
- 适配面：`LiveViewport`/`MiniPreview` props tick→hasFrame（占位态切换由 hasFrame 驱动，绘制不再依赖 React 重渲染）；导入序经 lint:fix 修正
- 验收实证：`pnpm web:lint` 零报错、`pnpm web:test` 47 例全绿、`pnpm web:build`（tsc -b + vite）通过；实机帧率/CPU 观察项（Stage 打开期间风扇、窗口隐藏停订）留待用户 tauri:dev 观察
- 质量审查修复两处（均在 `useFramePaint.ts`）：① 致命——rAF 循环原在 effect 建立时判空 canvas 即返回，而消费组件按 hasFrame 条件渲染 canvas（占位态不挂载）且 effect 依赖为稳定 ref 永不重跑，循环永不启动；且 paint 闭包捕获 canvas 实例，重挂后向脱离 DOM 的旧元素绘制。定稿为**循环无条件启动 + 每轮现读 canvasRef.current**（null 跳过本轮，重挂天然落新元素）。② 防御——atob 对非法 base64 同步抛错会使 painting 旗标永久卡死（.catch 兜不住同步 throw），paint 同步段整体 try/catch。审查其余维度（清帧与引用比对交互、hasFrame 语义、闸门组合、输入路由、useDocumentVisible、DRY）均无发现

**任务 T2：ToolCallItem 折叠卸载 + 载荷懒渲染** ✅
- 功能：折叠的工具调用不占 DOM，长会话 DOM 节点数降一个量级
- 技术方案：按 D2 实施（unmountOnExit + expanded 状态提升 + 懒渲染 + 32KB 截断）
- 依赖：无
- 验收：折叠后对应子树从 DOM 移除；重开会话历史中未展开条目零载荷 DOM；展开/收起状态切换无闪烁

**实施定稿**：
- 落地面：新增 `payloadText.ts`（pretty 化 + 截断纯函数，`tcPayloadMaxChars = 32768`，截断点落高代理时回退一位防孤立半对）+ `payloadText.test.ts`（6 例：直显/pretty/循环引用兜底/截断边界/代理对）；`ToolCallItem.tsx` 的 `Collapse` 加 `unmountOnExit`（保留收起动画后卸载），`PayloadBlock` 改 `useMemo(prettyPayloadText)` + 截断态「展开全文/收起」切换（块内局部态随收起重置）+ 尾注「已截断，共 N 字符」
- D2 口径修订（实施前置已回写 D2 段落）：截断阈值由「>32KB」落定为「>32768 UTF-16 码元」（DOM 内存随码元线性，字节口径需 TextEncoder 切割无收益）；懒渲染语义落定为「展开才渲染 + 挂载期内 memo」——不在卸载子树外跨周期缓存 pretty 文本（跨周期缓存会把截断省下的等量文本顶回 JS 堆，破坏「折叠零残留」）
- 「展开态状态提升」项：现状 `expanded` 已在折叠子树之外（组件根），保持不动即满足 D2
- 验收实证：`pnpm web:lint` 零报错、`pnpm web:test` 53 例全绿（原 47 + 新增 6）、`pnpm web:build`（tsc -b + vite）通过；使用方两处（MessageList 历史列表 + PendingCard 权限卡）自动受益；DOM 节点数量级下降与展开/收起无闪烁留待实机 tauri:dev 观察

**任务 T3：SSE 订阅失败终态** ✅
- 功能：订阅不存在/已关闭会话不再无限重连空转
- 技术方案：按 D3 实施，三端点同治（browserSession/stream、acpSession/events、browserSession/events）；前端 `useBrowserFrameStream`/`useAcpSessionEvents` 补 failed 终态与手动重试
- 依赖：无
- 验收：对已关闭会话发起订阅 = 单次失败 + failed 终态呈现；网络恢复手动重试可达；正常流不回归

**实施定稿**：
- 服务端落地面：`apis/response.go` 新增 `JsonFailWithStatus`（`AbortWithStatusJSON` 真实状态码 + `Response{code!=0}`）+ `apis/api.go` Api wrapper（对齐 JsonFail 范式）；`controller/browser_session.go` Stream 订阅前预检 `browserStreamReject`（`Row().Status`：空串→404、idle→409、failed→409）；`controller/acp_session.go` Events 订阅前预检 `acpEventsReject`（`Get().Status`：idle→404、terminated→409）；两 reject 均纯函数 + 表驱动单测（新文件 `browser_session_test.go`/`acp_session_test.go`）
- 前端落地面：`useBrowserFrameStream` phase 增 failed 终态 + `failedMessage` + attempt/retry（连接层 error 仅在 `readyState === CLOSED` 时置终态，CONNECTING 瞬态自动重连不置；serverUrl 获取失败同为 failed）；`useAcpSessionEvents` 由「挂载即订、返回 void」重写为闸门式有状态订阅（签名加 `enabled`，返回 `AcpEventsStream`）；`reducer.ts` 新增 `isAcpSessionSubscribable` 纯函数（starting/ready/failed 可订阅，undefined/idle/terminated 不可）+ 单测；`queries.ts` ensure 受理回包在缓存为终态（idle/terminated）时落快照（D3 细化已前置回写）；UI 三处接线——Stage 浮层与 BrowserSessionCard 各加 failed Alert + 重试按钮、AcpSessionView hook 重排（view 先于 events）+ 会话视图顶部订阅失败横幅（不阻断历史呈现）
- 相对方案偏离：① browserSession/events 端点零改动——全局单源订阅、无失败判定面（D3 细化段已前置回写），任务清单「三端点同治」落定为两端点；② browser hook 的 readyState 判定内联于 onError（一行规范调用，抽离仪式化），仅 acp 闸门 `isAcpSessionSubscribable` 因承载竞态治理语义而抽离 + 测试；③ EventSource 非 200 → CLOSED 双平台（WKWebView/WebView2）实测留待实机（风险 #3）
- 验收实证：`go -C server test ./...` 全绿、`pnpm server:fmt` 通过（新增 wrapper 格式带正；`vendored_test.go` 为既有格式漂移被全仓 gofmt 顺手带正）、`pnpm web:lint` 零报错、`pnpm web:test` 54 例全绿（原 53 + 订阅闸门 1）、`pnpm web:build`（tsc -b + vite）通过；「对已关闭会话订阅 = 单次失败 + failed 终态 + 手动重试可达」的行为验收留待实机 tauri:dev 观察
- 质量审查修复三组：① 重要（ACP 重启死路）——缓存 failed + 订阅已终态失败时，「重新启动」的 setStarting 帧无人订阅收不到、onSuccess 不覆写 failed 缓存，且 failed/terminated 分支不渲染订阅失败横幅——无故障信号、无恢复控件，唯一切菜单整页重挂可解围；修复为 failed 分支补渲染订阅失败横幅 + 重试订阅（retry 重订后 setStarting 帧经订阅归约进缓存，链路打通；terminated 为闸门关闭态，ensure 回包翻闸自动重订，无需横幅；仅把 failed 加进覆写集不够——覆写后闸门布尔 true→true 不变、effect 不重跑，只会变成无限转圈）。② 低（接线测试）——acp Events 补 handler 级 httptest（装配临时 sqlite + NewManager，断言不存在会话 404 + code!=0 落线，预检被删/挪位即退化告警）；browser Stream 不加同款：NewManager 构造含真实孤儿清扫（扫 Root()/profiles 杀进程），测试装配不安全，状态映射已由 reject 纯函数表测覆盖。③ 简洁性三处——gap 断档分支冗余 setPhase('connecting') 删除（connect 复位段同值）、ensure 落快照条件合并为 `!isAcpSessionSubscribable(prev?.status)`（与订阅闸门同一 SSOT）、browserStreamReject 注释尾句修复叙事修剪
- 修复后复跑验证链全绿（go test ./... / server:fmt / web:lint / web:test 54 例 / web:build）
- 规范/分层审查修复三项：① starting 分支同样渲染订阅失败横幅 + 重试订阅——与已修 failed 分支死路同构（订阅拒绝落在缓存 starting 窗口——sidecar 重启清内存/discard/进程夭折/serverUrl 失败——时无 SSE 帧刷新、ensure 守卫拒绝覆写、getInfo 无自动 refetch，视图死转圈无信号无控件）；三处同款横幅抽同文件 `SubscriptionFailedAlert` 组件（starting/failed/idle+ready 分支共用，按钮文案统一「重试订阅」；会话消亡类拒绝重试恒败，整页重挂重新受理兜底）；② acp handler 级测试补临时库连接池收尾（对齐 launch_settings_test.go newTestDB 先例）；③ server/README.md「响应」段补 JsonFailWithStatus 例外登记（SSE 订阅前置失败真实状态码，REST 仍统一 200+code）。其余维度（导入三段式、命名对称性、(int,error) 模式、attempt-retry 先例同构、表驱动测试风格、预检纯函数落 controller 层、global 直读惯例、state 域职责切分）审查确认无发现；修复后复跑验证链全绿

**任务 T4：ACP 适配器闲置回收** ✅
- 功能：空闲任务的适配器 node 进程自动退出，多任务并行内存从 N×150MB 有界化
- 技术方案：按 D4 实施；`manager.go` per-entry idle 计时（Prompt/Subscribe 重置），到期 `client.Close()`；`Ensure` 既有锚点重建路径恢复会话；新 appConfig key 进 `appConfig.ts` SSOT
- 依赖：无
- 验收：idle 超时后对应 node 进程退出；重开该 issue 会话恢复（上下文延续，恢复延迟可接受）；阈值改配置即时生效；进行中 turn 不被误杀

**实施定稿**：
- Go 落地面：新增 `server/internal/appdb/`（app.db 只读访问中立包——key 常量、哨兵 `ErrAppDbNotInjected`/`ErrAppDbMissing`、`database/sql` 短连接 mode=ro + busy_timeout、key 未设返回 `("", nil)`；不依赖 global，dbPath 由调用方注入，规避 global → acpsession → appdb 成环）；`mcp_util/app_config_reader.go` 迁为薄包装（`%w` 保留哨兵 errors.Is 链）。`acp/client.go` 增 `LoadSession`（acp-go 能力协商）+ `registerSession` 抽取（New/Load 共用运行时注册与早到帧回放）+ `newOrResumedSession` 决策函数（非空锚走恢复，失败 Warn 降级全新会话）。`acpsession/manager.go` per-entry idle 计时：完全空闲判据 `fullyIdleLocked`（就绪 + 无活动回合 + 等位队列空 + 无订户，released/discarded 恒否），起表于回合终态/退订/就绪落态，停表于受理/新订户；到期 `reclaimIdle` 按当前配置重算剩余时长（改长顺延）复查后置 released → 摘 entry → Close，pump 终结哨兵见 released 早退（不落态、不清锚、不广播）；`Ensure` Peek 锚点 → spawn 链 `newOrResumedSession` 恢复同 id 会话；joinable 判据与 `readyError` 拒绝 released entry（重建路径以 map 身份检查与 reclaim 安全交接）。`binding.go` 增只读 `Peek`；`hub.subscriberCount`/`turnqueue.pending` 窄观测面
- 配置链：key `acp_session_idle_timeout_minutes`（snake_case 对齐 `github_pat` 既有键）——Rust `app_config.rs` 常量 + `.constant()` → `pnpm gen:bindings` → `appConfig.ts` re-export + `ACP_SESSION_IDLE_TIMEOUT_OPTIONS`/`parseAcpSessionIdleTimeoutMinutes`；Go 侧 `appdb` 字符串常量手动同步（注释标明两端同步义务）；`main.go` 注入 `cfg.AppDbPath`
- 前端落地面：AppConfigPage 第三行「ACP 会话闲置回收」（图标 + 说明文案 + Select），沿用草稿态范式纳入 dirty/reset/cancel/save；`settingOption.ts` 增 `acpSessionIdleTimeoutOptions`（`{{minutes}} 分钟` 插值）；i18n 中英对称
- 判据与呈现落定（相对 D4 细化）：判据比方案多等位队列维（bot 排队回合不持订户，漏判会杀排队回合）与 discarded/released 维；回收呈现 = 静默摘除归 idle、重开自动 resume（无 terminated 广播）
- 风险变化：风险 1 代码级降级——vendored adapter 0.84.0 声明 `loadSession:true` 且 `readResumedSession` 读 claude CLI 会话文件后 `replaySessionHistory` 以帧重放历史（上下文延续 + UI 历史恢复均成立；真机实测仍为手测项）；风险 2 解除——accessor 以中立 appdb 包落地
- 保守边界：`SweepSessionIDs` 保留（重启后清锚归零——重启 resume 留待评审，避免孤儿适配器进程）；阈值生效边界 = 改长立即顺延、改短在已闲置会话的下一次到期判定生效（≤旧期限；Go 无配置推送通道，不引入轮询）
- 测试落地面：fakeagent 增 `load` 脚本（声明 LoadSession 能力 + 回放「历史-1/历史-2」+ Prompt 与 happy 共支）；acp 包增能力拒绝/历史重放与恢复后回合两用例；manager_test 增三态决策/锚点接线/Peek/SubscriberCount/Pending 用例；idle_test 五用例——parse 同口径、真实 app.db 现读回落、阈值改长顺延回归锁、回收闭环（锚保留 → 重开 resume 同 id）、三守卫逐项隔离负例
- 质量审查修复四组：① 关键（锁序成环）——idle 判据在 entry.mu 内取 hub.mu，经快照闭包（hub.mu → m.mu）与 Ensure（m.mu → entry.mu）闭合成三方环，修复为订户数在 entry.mu 外预读作参数传入；② 重要（顺延死代码）——停表误清空闲起点致「改长顺延」分支不可达，修复为停表只停计时器、起点归属当前空闲段（起表设/离开清），并补顺延回归锁；③ 重要（回收窗口竞态）——released 置位到摘除之间 join/受理可命中将死 entry，修复为 joinable 与 readyError 拒绝 released；④ 测试有效性——闭环用例锚点断言前加回收收尾宽限。其余：判据收敛 `fullyIdleLocked`（收编 discarded 漂移）、`%s`→`%w`、测试装配参数化；race 检测揪出并修复两处测试自身阈值翻转竞态
- 验收实证：`go test ./...` 18 包全绿（acpsession 整包 `-race` 通过）、`gofmt`/`go vet` 干净、`pnpm web:lint` 零报错、`pnpm web:test` 54 例全绿、`pnpm web:build` 通过、`pnpm verify:bindings` 仅剩未提交 diff（内容恰为新增两常量，提交后即过）；实机四项手测（进程退出 / 重开恢复 / 阈值改 5 分钟即时生效 / 进行中 turn 不误杀）与 adapter 历史重放超 `earlyFrameCap=64` 完整性观察项留待 tauri:dev
- 规范/分层审查：导入三段式、Locked 后缀与注入点命名惯例、appdb 绕过 gorm 的合理性（app.db 归 Rust 所有）、前端草稿态/options 数组/SSOT 分层同轨、锁外预读自证、跨端常量同步风险四处标明——三维度零高置信度发现

### 第二批（B 组：治本）

**任务 T5：MessageList 虚拟化** ✅
- 功能：数百条历史滚动流畅，挂载内存与会话长度解耦
- 技术方案：引入 virtua（`packages/web/package.json`），`MessageList.tsx:49` 全量 map 改 Virtualizer 动态高度渲染；保持新消息自动滚底、滚动位置保持、ToolCallItem 交互回归
- 依赖：无（先于 T6，列表数据源边界先定型）
- 验收：500 条消息会话滚动 60fps、内存稳定；自动滚底与手动上滚不冲突；Shiki 高亮不回归

**实施定稿**：
- 落地面：virtua 0.53.5 进 catalog SSOT（`pnpm-workspace.yaml` + web 包 `catalog:` 引用）；`MessageList.tsx` 重写——滚动容器外接 `Virtualizer`（`scrollRef` + render-prop + `overflowAnchor:'none'`），限宽 860 居中列保留，顶部 12px 由 wrapper `paddingTop` 真实空隙 + 同源 `startMargin` 表达（startMargin 声明的是实际存在的偏移，二者常量同源），条目间 `gap` 折进条目 wrapper `paddingBottom:10` 并复原 flex 纵列语境（user 气泡 `alignSelf` 不回归）；贴底判定抽 `stickToBottom.ts` 纯函数（48px 严格小于，4 边界用例）；归底编排收敛单一入口 `followBottom`（`useCallback([entries])`，entries effect 与 `onResize` 共用——effect 直传函数写法；`scrollTop=scrollHeight` 直写废弃，改 `scrollToIndex(last, end)`）；工具调用展开态提升 `expandedToolCalls`（`Record<toolCallId, boolean>`，false 同缺省）+ `toolCallIdOf` 派生单一口径（读 key 与 ToolCallItem 合成 id 同源）+ `toggleToolCall` useCallback 稳定；`EntryItem` 包 `React.memo`（全仓首例，reducer 保证未变条目引用稳定，流式重渲染仅及增长条目）；`ToolCallItem` 受控化（`expanded`/`onToggle`，删内部 state），`PendingCard` PermissionCard 以单卡本地 state 接线（固定挂起区不受虚拟化影响）；`PayloadBlock` 的 `full` 局部态保持 T2 语义不动
- 相对方案偏离/细化：① 展开态处理经用户拍板走「状态提升」路线（备选 keepMounted 保挂未采用——离屏 DOM 常驻与 T2 语义冲突）；② startMargin 与 wrapper paddingTop 的同源约束为方案细化（虚拟器上方空隙须真实存在）；③ 归底时序三路径冗余闭环——entries effect 首滚 / virtua scrollToIndex 内部重试（订阅测高事件微任务重滚）/ onResize 贴底跟随（Shiki 异步测高补齐），审查以 virtua 0.53.5 内部实现反汇编核验；④ T6 前向边界已预留：prepend 时开 `shift` prop（注释标明），`entries` props 契约未变
- 验收实证：`pnpm web:lint` 零报错、`pnpm web:test` 58 例全绿（原 54 + stickToBottom 4）、`pnpm web:build`（tsc -b + vite）通过；500 条滚动流畅度/内存稳定/自动滚底与上翻不冲突/展开无闪烁留待实机 tauri:dev 观察（风险 #4 专项；不达标回退评估 react-window）
- 质量审查：缺陷检查零高置信度发现（贴底判定抗程序性滚动、初始挂载落真底、流式高频归底、展开态 key 同源、memo 正确性、virtua 契约、非 entry 帧不误触发 effect、依赖登记逐项核验）；简洁性修复三处（归底两处内联收敛 `followBottom`、`toolCallIdOf` 统一隐式键耦合、删 `threshold` 死参数）；规范/分层三维度（导入命名 px/catalog、hook 与 memo 范式、组件域组织与状态落点）零发现

**任务 T6：ACP 消息落库 + 分页 + 快照窗口** ⬜
- 功能：Go 内存不随会话长度线性涨；sidecar 重启后消息可回看
- 技术方案：按 D6 实施——改原迁移文件加 entries 表 → `pnpm server:gorm:gen`；`view.go` 写穿落库 + 内存热窗口；SSE 快照仅带热窗口；新增分页接口（issueId + pageInfo{pageSize, currentPage}）；前端 MessageList 上滑拉历史 append 缓存
- 依赖：T5
- 验收：长会话 Go RSS 平稳；快照体积有界；重启 sidecar 后历史消息经分页可回看；新旧数据切换按「无历史数据」惯例处理

## 4. 风险

| # | 风险 | 缓解 |
|---|---|---|
| 1 | claude SDK 会话 resume 语义（回收重建后上下文是否延续）未实测 | T4 验收首项，不达标则降级为「回收后全新会话 + 锚点提示」再评审 |
| 2 | Go 侧直读 app.db 的 accessor 现状未核 | T4 实施时先核 SQLITE_DIR/APP_DB 注入链路，无现成 accessor 则补轻量只读查询 |
| 3 | EventSource 非 200 各浏览器行为差异 | 按 W3C 规范非 200 fail 连接不重连；实施时双平台（WKWebView/WebView2）实测 |
| 4 | virtua 动态高度与自动滚底集成 | T5 验收专项；不达标回退评估 react-window |
