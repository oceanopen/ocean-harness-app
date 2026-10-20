# bot 掉线提醒与有限次自动重连

## 背景

企微 bot WS 被新连接接管后落 `disconnected` 终态不重连，恢复全靠用户手动点「重启连接」；且前端对掉线零感知（无轮询无推送，徽标滞留旧态、无显著提示）。微信渠道侧，网络族失败已有无限退避自愈，缺口仅「连续被拒」终态。

## 设计定稿

- **自动重连（有限次带退避）**：终态事件触发 ≤3 次重连（5s/20s/60s + 随机抖动），耗尽维持终态并在 lastError 追加「已自动重连 N 次未成功」。抖动用于双实例互踢错峰收敛；预算仅在人工 Start（启动/重启连接）处归零，自动路径不重置。
- **渠道对齐**：wecom 被接管事件、weixin 连续被拒终态接入同一重试骨架；weixin 凭据失效（-14）终态保持人工重扫语义，网络族自愈不动。
- **掉线感知（轻量）**：`useImBots` 稳定态常驻 30s 轻轮询（connecting 瞬态 2s 保留，失焦不轮询）+ ImBotPanel 标题红点（存在 enabled 且 disconnected 的 bot）。不做横幅/托盘/OS 通知。

## 实现要点

- **wecom**（`server/internal/bot/wecom/channel.go`）：`Start` 拆为预算重置 + `launch`（装配一代新连接，Start 与自动重连共用）；`OnDisconnectedEvent` 经代际守卫通过后 spawn `retryTakeover`；重试以保存的 cfg/onInbound 走 `launch` 等价路径。
- **weixin**（`server/internal/bot/weixin/channel.go`）：同款拆分；`pollLoop` 返回是否被拒终态，`run` 在 notifyStop 收尾后 spawn `retryAfterRejects`。
- **并发安全**：新增 `startMu` 串行化 Start/Stop/自动重连换代（手动操作优先，杜绝「守卫通过后被 Stop 抢先、重试复活已停实例」窗口）；等待用 `select`（`time.After` + 本代 stop 通道），Stop 即刻唤醒、挂起零轮询、goroutine 用毕即亡。
- **测试**（两渠道 `channel_test.go`）：耗尽落终态 / 手动 Stop 优先 / 在途等待被 Stop 唤醒 / weixin 补终态分类（非瞬态拒绝三振 vs token 失效不重连）与重连真实发起。

## 状态

- [x] wecom 被接管有限次带退避自动重连
- [x] weixin「连续被拒」终态接入同款有限重启
- [x] 前端常驻 30s 轻轮询（`packages/web/src/state/imBots/queries.ts`）
- [x] ImBotPanel 标题离线红点（`ImBotPanel.tsx`）
- [x] 验证收尾：`go test ./internal/bot/...` 全绿 + `pnpm web:lint` + `pnpm web:build` 通过
