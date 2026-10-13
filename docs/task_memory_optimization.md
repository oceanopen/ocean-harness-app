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
| D2 | ToolCallItem：Collapse 加 `unmountOnExit` + 展开态状态提升到折叠子树之外 + 载荷懒渲染（首次展开才 pretty 化）+ 超长截断（>32KB 折叠展示 + 展开全文按钮） | 折叠即释放 DOM 是内存主治点；展开态不随卸载丢失；截断防单条超大载荷 |
| D3 | SSE 失败终态：events 端点订阅失败改真实 HTTP 状态码（会话不存在 404 / 已关闭 409），前端 phase 增加 failed 终态 + 手动重试；EventSource 对非 200 不自动重连（浏览器规范行为） | 治本消灭 3s 空转循环；browserSession/stream、acpSession/events、browserSession/events 同病同治 |
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

**任务 T2：ToolCallItem 折叠卸载 + 载荷懒渲染** ⬜
- 功能：折叠的工具调用不占 DOM，长会话 DOM 节点数降一个量级
- 技术方案：按 D2 实施（unmountOnExit + expanded 状态提升 + 懒渲染 + 32KB 截断）
- 依赖：无
- 验收：折叠后对应子树从 DOM 移除；重开会话历史中未展开条目零载荷 DOM；展开/收起状态切换无闪烁

**任务 T3：SSE 订阅失败终态** ⬜
- 功能：订阅不存在/已关闭会话不再无限重连空转
- 技术方案：按 D3 实施，三端点同治（browserSession/stream、acpSession/events、browserSession/events）；前端 `useBrowserFrameStream`/`useAcpSessionEvents` 补 failed 终态与手动重试
- 依赖：无
- 验收：对已关闭会话发起订阅 = 单次失败 + failed 终态呈现；网络恢复手动重试可达；正常流不回归

**任务 T4：ACP 适配器闲置回收** ⬜
- 功能：空闲任务的适配器 node 进程自动退出，多任务并行内存从 N×150MB 有界化
- 技术方案：按 D4 实施；`manager.go` per-entry idle 计时（Prompt/Subscribe 重置），到期 `client.Close()`；`Ensure` 既有锚点重建路径恢复会话；新 appConfig key 进 `appConfig.ts` SSOT
- 依赖：无
- 验收：idle 超时后对应 node 进程退出；重开该 issue 会话恢复（上下文延续，恢复延迟可接受）；阈值改配置即时生效；进行中 turn 不被误杀

### 第二批（B 组：治本）

**任务 T5：MessageList 虚拟化** ⬜
- 功能：数百条历史滚动流畅，挂载内存与会话长度解耦
- 技术方案：引入 virtua（`packages/web/package.json`），`MessageList.tsx:49` 全量 map 改 Virtualizer 动态高度渲染；保持新消息自动滚底、滚动位置保持、ToolCallItem 交互回归
- 依赖：无（先于 T6，列表数据源边界先定型）
- 验收：500 条消息会话滚动 60fps、内存稳定；自动滚底与手动上滚不冲突；Shiki 高亮不回归

**任务 T6：ACP 消息落库 + 分页 + 快照窗口** ⬜
- 功能：Go 内存不随会话长度线性涨；sidecar 重启后消息可回看
- 技术方案：按 D6 实施——改原迁移文件加 entries 表 → `pnpm server:gorm:gen`；`view.go` 写穿落库 + 内存热窗口；SSE 快照仅带热窗口；新增分页接口（issueId + beforeSeq + limit）；前端 MessageList 上滑拉历史 append 缓存
- 依赖：T5
- 验收：长会话 Go RSS 平稳；快照体积有界；重启 sidecar 后历史消息经分页可回看；新旧数据切换按「无历史数据」惯例处理

## 4. 风险

| # | 风险 | 缓解 |
|---|---|---|
| 1 | claude SDK 会话 resume 语义（回收重建后上下文是否延续）未实测 | T4 验收首项，不达标则降级为「回收后全新会话 + 锚点提示」再评审 |
| 2 | Go 侧直读 app.db 的 accessor 现状未核 | T4 实施时先核 SQLITE_DIR/APP_DB 注入链路，无现成 accessor 则补轻量只读查询 |
| 3 | EventSource 非 200 各浏览器行为差异 | 按 W3C 规范非 200 fail 连接不重连；实施时双平台（WKWebView/WebView2）实测 |
| 4 | virtua 动态高度与自动滚底集成 | T5 验收专项；不达标回退评估 react-window |
