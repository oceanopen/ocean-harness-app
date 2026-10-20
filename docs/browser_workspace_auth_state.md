# 浏览器工作空间共享登录态

登录态按工作空间共享一份，浏览器实例保持 per-task 隔离（profile 目录各自独立、并行互不干扰），登录态经注入/导出在实例间流转。

## 数据布局

- 共享文件：`~/.ocean-harness/browser/auth-states/<workspaceId>.json`（Playwright storageState 格式：cookies + origins[].localStorage，0600）。D3 修订：凭据仅此一份落盘，不进数据库。
- profile 命名空间（前后端各单点 SSOT：`browser/paths.go` ParseProfileNamespace / 前端 `derive.ts`）：
  - `issue-<issueId>` → 经 DB（t_project_issues.workspace_id）解析归属工作空间，享受共享；
  - `workspace-<id>` → 直通 id（MCP 工具面经 workspaceId 入参派生：profile 与 issueId 均留空时生效，issueId 优先；面板等工作空间级会话入口未来接入时即享）；
  - 其余（default/自定义名）→ 无共享登录态。
- 文件读写侧容错：缺失 = 无共享；损坏 JSON = 导出侧按空态自愈重建、注入侧跳过；错误文案不含文件内容。

## 注入（spawn 时）

runSpawn 在置 ready 前锁外同步执行注入——ready 即放行外部调用，注入晚于 ready 会与首导航竞速，此刻外部调用一律 waitSpawn 等待，结构上保证「先注入后导航」。注入走引擎工具 `browser_set_storage_state`（先清空再恢复 cookies + 逐 origin localStorage）。

跳过条件：共享文件缺失 / 空态 / 与上次注入内容同 hash（entry.authInjectedHash，跨 idle 重建留存——重开遇同内容不二次清空重注）。失败仅告警降级，会话照常就绪，登录态退回 profile 目录自管（重新登录后导出链路自愈回写）。

## 导出（运行期 + 收尾）

- 运行期：per-profile goroutine 60s ticker（authSyncInterval），与引擎生命周期绑定（Exited 即退）；每轮先复查 entry 仍持有该 handle，callMu 忙则跳过本轮。
- 收尾：Close（页面捕获后、引擎关停前）与无头切换（旧引擎关停前）各导出一次；best-effort 不阻塞关闭。
- 导出走引擎工具 `browser_storage_state` → 与共享文件并集合并 → per-workspace 键控锁内原子写（tmp + rename）。

合并语义：cookie 按 (name, domain, path)、localStorage 按 (origin, name) 判重，引擎侧覆盖同名、共享文件独有保留（同工作空间并行会话不互相覆盖丢失）；双侧剔除已过期项；已删除的 cookie 不传播。

## 沙箱与 staging

引擎文件沙箱只放行 output-dir（`downloads/<profile>/`）与 cwd，共享文件路径不可直传引擎工具（`--allow-unrestricted-file-access` 会向 MCP 逃生舱暴露任意文件读写，禁用）。注入/导出一律经 `downloads/<profile>/` 内 staging 文件中转，用毕即删。

## 已知边界

- 引擎崩溃（watchExit 路径）前最后 ≤60s 的登录变更只存在于 profile 目录，重开注入会以共享文件（可能陈旧）覆盖，需重登一次。
- storageState 的 localStorage 收集只含活页 origin——关页后不再打开的 origin 不进导出（引擎语义边界）。
- 注入使 spawn 增加 1-3s（有共享文件时）；无共享文件零开销。
