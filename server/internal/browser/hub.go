package browser

import "sync"

// subscriberBuffer 单订户有界缓冲：满即断开（溢出策略见 broadcast）——投影广播永不
// 阻塞于慢 SSE 客户端。断开的客户端由 EventSource 自动重连取全新快照（对齐 acpsession
// hub 范式；不做 Last-Event-ID 续传——重连即新快照，无需帧历史缓存）。
const subscriberBuffer = 256

// hub 全局单源 SSE 订阅注册表（从 acpsession/hub.go 裁剪复制——去掉 issueID 维度）：
// seq 全局单调分配 + 建连即快照（首帧）+ 广播扇出。所有方法内部加锁；帧发送永不阻塞
// （溢出断开慢订户）。锁序：hub 锁内只经 snapshot 闭包碰 view 读锁，绝不与 entry.mu/
// m.mu 嵌套（broadcast 恒在锁外调用）。
type hub struct {
	mu     sync.Mutex
	subs   map[*subscriber]struct{}
	seq    uint64
	closed bool
}

type subscriber struct {
	ch chan Frame
}

func newHub() *hub {
	return &hub{subs: map[*subscriber]struct{}{}}
}

// subscribe 订阅并装配首帧：快照在 hub 锁内生成入订户通道（seq 连续分配，杜绝快照与
// 增量帧之间的缝隙）。snapshot 由调用方提供投影（Manager.Subscribe）。返回帧通道与
// 退订函数。
func (h *hub) subscribe(snapshot func() *BrowserViewSnapshot) (<-chan Frame, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sub := &subscriber{ch: make(chan Frame, subscriberBuffer)}
	h.subs[sub] = struct{}{}
	if snapshot != nil && !h.closed {
		h.seq++
		snap := snapshot()
		sub.ch <- Frame{Seq: h.seq, Type: FrameSnapshot, Snapshot: snap}
	}
	return sub.ch, func() { h.unsubscribe(sub) }
}

// broadcast 帧扇出：seq 逐帧分配后投递。慢订户（缓冲满）即断开——断开的是订阅而非
// HTTP 连接，客户端 EventSource 自动重连（重连即新快照）。
func (h *hub) broadcast(frames []Frame) {
	if len(frames) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for i := range frames {
		h.seq++
		frame := frames[i]
		frame.Seq = h.seq
		for sub := range h.subs {
			select {
			case sub.ch <- frame:
			default:
				delete(h.subs, sub)
				close(sub.ch)
			}
		}
	}
}

// unsubscribe 退订（SSE handler defer 调用；幂等）。
func (h *hub) unsubscribe(sub *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[sub]; !ok {
		return
	}
	delete(h.subs, sub)
	close(sub.ch)
}

// closeAll 关停全部订阅（SIGTERM 收尾：SSE handler 的通道关闭即返回，不拖 HTTP
// shutdown）。
func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for sub := range h.subs {
		close(sub.ch)
	}
	h.subs = map[*subscriber]struct{}{}
}
