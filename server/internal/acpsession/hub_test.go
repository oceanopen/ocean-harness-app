package acpsession

import (
	"testing"
	"time"
)

// readFrame 带超时读一帧（测试内消费辅助）。
func readFrame(t *testing.T, frames <-chan Frame) (Frame, bool) {
	t.Helper()
	select {
	case frame, ok := <-frames:
		return frame, ok
	case <-time.After(5 * time.Second):
		t.Fatal("读帧超时")
		return Frame{}, false
	}
}

func TestHubSubscribeSnapshotAndSeq(t *testing.T) {
	h := newHub()
	seq := 0
	frames, cancel := h.subscribe("i-1", func() *ViewSnapshot {
		seq++
		return &ViewSnapshot{Status: StatusReady, AcpSessionID: "sess-x"}
	})
	defer cancel()
	// 首帧即快照。
	frame, ok := readFrame(t, frames)
	if !ok || frame.Type != FrameSnapshot || frame.Seq != 1 || frame.Snapshot == nil || frame.Snapshot.AcpSessionID != "sess-x" {
		t.Fatalf("首帧应为快照且 seq=1，got %+v ok=%v", frame, ok)
	}
	// 广播帧 seq 单调连续（快照与增量之间无缝隙）。
	h.broadcast("i-1", []Frame{{Type: FrameEntry}, {Type: FrameTurnStarted}})
	for i, want := range []FrameType{FrameEntry, FrameTurnStarted} {
		frame, ok = readFrame(t, frames)
		if !ok || frame.Type != want || frame.Seq != uint64(i+2) {
			t.Fatalf("增量帧应为 %s seq=%d，got %+v", want, i+2, frame)
		}
	}
}

func TestHubIssueIsolation(t *testing.T) {
	h := newHub()
	framesA, cancelA := h.subscribe("i-a", nil)
	defer cancelA()
	framesB, cancelB := h.subscribe("i-b", nil)
	defer cancelB()
	h.broadcast("i-a", []Frame{{Type: FrameEntry}})
	if _, ok := readFrame(t, framesA); !ok {
		t.Fatal("i-a 订阅者应收到帧")
	}
	select {
	case frame := <-framesB:
		t.Fatalf("i-b 订阅者不应收到 i-a 的帧，got %+v", frame)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHubSlowSubscriberDisconnected(t *testing.T) {
	h := newHub()
	slow, cancelSlow := h.subscribe("i-1", nil)
	defer cancelSlow() // 幂等无害（通道可能已被关）
	healthy, cancelHealthy := h.subscribe("i-1", nil)
	defer cancelHealthy()

	// 慢订户不读；健康订户并发持续消费（否则自己在广播循环内也会溢出被断开）。
	total := subscriberBuffer + 10
	received := make(chan struct{}, total)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < total; i++ {
			select {
			case _, ok := <-healthy:
				if !ok {
					return
				}
				received <- struct{}{}
			case <-time.After(5 * time.Second):
				return
			}
		}
	}()

	frames := make([]Frame, total)
	for i := range frames {
		frames[i] = Frame{Type: FrameEntry}
	}
	// 分批投递（批间让出调度）：broadcast 锁内瞬时投递，单批超缓冲会让健康订户在
	// 消费 goroutine 腾空前也触溢出——分批保证它持续消费不触线，慢订户累计满即断。
	for start := 0; start < total; start += 20 {
		end := start + 20
		if end > total {
			end = total
		}
		h.broadcast("i-1", frames[start:end])
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("健康订户未在时限内收满全部帧")
	}
	close(received)
	if got := len(received); got != total {
		t.Fatalf("健康订户应收满 %d 帧，got %d", total, got)
	}
	// 慢订户：读空缓冲后通道关闭（ok=false）。
	drained := 0
	for {
		_, ok := readFrame(t, slow)
		if !ok {
			break
		}
		drained++
	}
	if drained != subscriberBuffer {
		t.Fatalf("慢订户应缓冲 %d 帧后断开，got %d", subscriberBuffer, drained)
	}
}

func TestHubUnsubscribeAndCloseAll(t *testing.T) {
	h := newHub()
	frames, cancel := h.subscribe("i-1", nil)
	cancel()
	cancel() // 幂等
	if _, ok := readFrame(t, frames); ok {
		t.Fatal("退订后通道应关闭")
	}
	frames2, cancel2 := h.subscribe("i-2", nil)
	defer cancel2()
	h.closeAll()
	if _, ok := readFrame(t, frames2); ok {
		t.Fatal("closeAll 后通道应关闭")
	}
	// 关停后广播静默（不再有订户，也不 panic）。
	h.broadcast("i-2", []Frame{{Type: FrameEntry}})
}
