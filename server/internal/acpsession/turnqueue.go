package acpsession

import "sync"

// turnQueue per-entry 跨入口回合等位队列（T2.2）：桌面 prompt 与 bot 回合两入口抢同一
// ACP 会话时，后到的排队受理者（PromptQueued）不再立即拒绝而是 FIFO 等位，当前回合结束
// （release）后自动开跑。UI 即时受理（Prompt）不经队列——回合空隙上桌面先到先得，等位者
// 被插队后重新排队继续等（用户优先）。
// 锁序约定：q.mu 独立于 manager/entry/view 的锁，持 q.mu 时不获取其他锁；close/release
// 允许在其他锁持有内调用（单向，不成环）。
type turnQueue struct {
	mu        sync.Mutex
	waiters   []chan struct{} // FIFO；容量 1，release 投递唤醒（缓冲保证不丢）
	closed    chan struct{}   // close 广播关闭：entry 重建 / Discard / StopAll 时等位者退出
	closeOnce sync.Once
}

func newTurnQueue() *turnQueue {
	return &turnQueue{closed: make(chan struct{})}
}

// join 队尾入队，返回专属唤醒 chan。调用方（PromptQueued）固定「先入队、再尝试闸门」——
// 「已在队列」与 turnActive 检查的竞态由此消解：回合结束的 release 不会漏掉已入队者，
// 未等位即获槽位则 depart 出队（容量 1 的缓冲让这种抢先投递不丢不塞）。
func (q *turnQueue) join() <-chan struct{} {
	w := make(chan struct{}, 1)
	q.mu.Lock()
	defer q.mu.Unlock()
	q.waiters = append(q.waiters, w)
	return w
}

// depart 自摘（幂等：已被 release 弹出则为 no-op）。未等位即获槽位与 ctx 取消两条路径共用。
func (q *turnQueue) depart(w <-chan struct{}) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.waiters {
		if q.waiters[i] == w {
			q.waiters = append(q.waiters[:i], q.waiters[i+1:]...)
			return
		}
	}
}

// release 弹队首唤醒（空队列 / 已关闭 no-op）。由回合收尾路径（acceptTurn 的后台
// goroutine）在 endTurn 之后调用——endTurn 置 turnActive=false 与 release 的先后即
// 「槽位释放」时序：被唤醒者重试闸门要么看到已释放的槽位，要么被 UI 即时受理插队后
// 重新排队。
func (q *turnQueue) release() {
	q.mu.Lock()
	defer q.mu.Unlock()
	select {
	case <-q.closed:
		return // 已关闭：等位者经 done 退出，不再投递
	default:
	}
	if len(q.waiters) == 0 {
		return
	}
	w := q.waiters[0]
	q.waiters = q.waiters[1:]
	w <- struct{}{} // 容量 1，非阻塞不丢
}

// close 广播关闭（幂等）：全部等位者经 done 退出（entry 重建 / Discard / StopAll 时旧
// entry 上的等待不跨会话续接）。关闭后 join 的等位者在 select 上立即命中 closed 分支。
func (q *turnQueue) close() {
	q.closeOnce.Do(func() { close(q.closed) })
}

// done 关闭信号 chan（等位 select 分支）。
func (q *turnQueue) done() <-chan struct{} {
	return q.closed
}
