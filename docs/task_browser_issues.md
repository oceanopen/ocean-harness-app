# 浏览器自动化问题跟进

## 问题一：有头/无头切换后 tab 失联与激活态失同步

**现象**

- 两个 tab 页场景下，无头模式切换为有头模式后：页面列表出现点不响应的行（无法订阅/选中）。
- 在浏览器实例（真实 Chrome 窗口）内手动切换 tab，面板的「当前激活 tab」不跟随更新。

**定位结论**（两个独立缺陷，均在页面观察者）

1. 幽灵行：`attachProbe` 以 `Target.getTargets` 回填行信息，但解析结构体沿用 `/json/list` 的字段名（`id`），CDP `getTargets` 响应字段为 `targetId`——解析恒得空串，每次 attach 都向存储写入/更新一行空 targetId 的幽灵行（URL 取 attach 瞬间快照，外观与真实 tab 无异）。点击幽灵行 = 订阅空 targetId = 必死（「点不了」）。该回填块与后续 evaluate 探测重复，纯冗余。
2. 激活态断链：visibility 监听仅经 `addScriptToEvaluateOnNewDocument` 注入，只覆盖「之后新建的文档」；已加载文档无监听器，无导航的手动切 tab 的 visibilitychange 无从上报（「切 tab 不更新」）。

**修复**

- `observer.go`：删除 `getTargets` 回填块（初始状态由 attach 后的 evaluate 探测覆盖）；attach 时对当前文档补注同一监听脚本（`Runtime.evaluate`）。

**状态**：已修复（回归通过：双同 URL tab 无幽灵行、均可订阅）。

## 问题二：有头切回无头后 tab 丢失

**现象**

- 接问题一场景（2 tab，已切有头）：再从有头切回无头，页面列表只剩 1 行，预期仍为 2。

**定位结论**

- `observe()` 新建行时的「同 URL 旧行先行出列」去重：两个 tab 为同一 URL（如重复打开同一搜索页）时，第二个 target 建行会删除第一行——列表设计语义是「恒等当前真实 tab 集」，同 URL 多 tab 各占一行，该去重直接吃掉合法重复页。陈旧行的清运已由 reconcile 对账与页面关闭出列覆盖，去重无存在必要。

**修复**

- `pagehistory.go`：删除同 URL 去重逻辑；顺带清理死代码 `bump`/`touch`（无调用方，且活动序赋值有缺失）。

**状态**：已修复（回归通过：无头↔有头往返后双同 URL tab 均存活）。

## 附一：手动切 tab 的「当前页」跟随协议层不可观测

引擎为 playwright-mcp 拉起的 Chrome，启动参数含后台化抑制类开关（`--disable-backgrounding-occluded-windows` 等）。实测（同窗口双 tab 逐一直连 `Runtime.evaluate` + `Browser.getWindowForTarget`）：

- 同一窗口内前后台 tab 的 `document.visibilityState` 恒为 `visible`，切 tab 不触发 `visibilitychange`——可见态信号在窗口内被参数抹平。
- CDP 协议不存在 tab 激活事件；`/json/activate` 返回成功但目标页可见态不变，无任何可订阅信号。

结论：headless 下可见态区分仍有效，但 headed 下「面板当前页跟随手动切 tab」在现有引擎与协议下无可实现路径。当前页语义据此收敛为**自动化操作焦点页**：引擎调用（导航/输入/分析）的落点页锚为当前（`record()`）。

## 附二：订阅路径禁止写投影

前端未点选页时观看目标跟随投影 current（重订由投影 SSE 推送驱动），因此订阅侧任何投影写入都会自激成重订风暴：订阅 → 记流水锚定 → 发布投影 → 前端跟随重订 → 再记流水。无头切换后多页交错的窗口内即表现为操作历史被 `browser_live` 刷屏。约束：`SubscribeLive` 全路径（成功/失败）不落流水、不改投影；当前页锚定只由引擎调用路径产生。

## 问题三：操作历史排序应为最新在上

**现象**

- 页面行展开的操作历史按时间正序渲染（最旧在最上），期望倒序——最近操作在最上面。

**定位**

- 数据层 `pageHistoryStore.rows()` 的 `Actions` 按时间正序返回（环形淘汰保序），渲染层原样输出；修复在渲染侧倒序展示，数据层保持正序语义。

**状态**：已修复（`BrowserSessionCard.tsx` `pageActionRows` 倒序输出）。
