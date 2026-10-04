package acpsession

import "testing"

// fired 非阻塞检查唤醒 chan 是否已投递（消费式：命中即取走缓冲投递）。
func fired(w <-chan struct{}) bool {
	select {
	case <-w:
		return true
	default:
		return false
	}
}

func TestTurnQueueReleaseFIFO(t *testing.T) {
	q := newTurnQueue()
	w1, w2, w3 := q.join(), q.join(), q.join()
	q.release()
	if !fired(w1) || fired(w2) || fired(w3) {
		t.Fatal("首次 release 应唤醒队首 w1")
	}
	q.release()
	if !fired(w2) || fired(w3) {
		t.Fatal("二次 release 应唤醒 w2")
	}
	q.release()
	if !fired(w3) {
		t.Fatal("三次 release 应唤醒 w3")
	}
	q.release() // 空队列 no-op（不 panic 即通过）
}

func TestTurnQueueDepart(t *testing.T) {
	q := newTurnQueue()
	w1, w2 := q.join(), q.join()
	q.depart(w1)
	q.release()
	if fired(w1) || !fired(w2) {
		t.Fatal("depart 自摘后不应被唤醒，队首应轮到 w2")
	}
	// 已被 release 弹出后再 depart：幂等 no-op，不影响后续入队者。
	w3 := q.join()
	q.release() // 弹出 w3
	q.depart(w3)
	q.release() // 空队列 no-op
}

func TestTurnQueueClose(t *testing.T) {
	q := newTurnQueue()
	w1, w2 := q.join(), q.join()
	q.close()
	q.close() // 幂等
	if !fired(q.done()) {
		t.Fatal("close 后 done 应关闭")
	}
	// 关闭后 release 不再投递（等位者经 done 退出）。
	q.release()
	if fired(w1) || fired(w2) {
		t.Fatal("close 后 release 不应经唤醒 chan 投递")
	}
	// close 后 join：等位 select 立即命中 closed 分支（调用方语义 = 受理失败退出）。
	q.join()
	if !fired(q.done()) {
		t.Fatal("close 后 join 的等位者应立即看到关闭")
	}
}
