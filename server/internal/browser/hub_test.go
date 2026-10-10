package browser

import (
	"testing"
	"time"
)

// readFrame 5s 超时读帧（镜像 acpsession/hub_test.go 惯例）。
func readFrame(t *testing.T, frames <-chan Frame) Frame {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatal("帧通道已关闭")
		}
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("5s 内未收到帧")
		return Frame{}
	}
}

func TestHubSubscribeSnapshotAndSeq(t *testing.T) {
	h := newHub()
	frames, cancel := h.subscribe(func() *BrowserViewSnapshot {
		return &BrowserViewSnapshot{Sessions: []SessionView{{Profile: "default", Status: StatusReady}}}
	})
	defer cancel()

	snap := readFrame(t, frames)
	if snap.Type != FrameSnapshot || snap.Seq != 1 {
		t.Fatalf("首帧 = type=%s seq=%d, want snapshot seq=1", snap.Type, snap.Seq)
	}
	if len(snap.Snapshot.Sessions) != 1 || snap.Snapshot.Sessions[0].Profile != "default" {
		t.Fatalf("首帧快照 = %+v, want default 一行", snap.Snapshot.Sessions)
	}

	h.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{{Profile: "default", Status: StatusIdle}}}})
	h.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{{Profile: "default", Status: StatusReady}}}})
	if f := readFrame(t, frames); f.Seq != 2 || f.Type != FrameSessions {
		t.Fatalf("第二帧 = type=%s seq=%d, want sessions seq=2", f.Type, f.Seq)
	}
	if f := readFrame(t, frames); f.Seq != 3 || len(f.Sessions) != 1 {
		t.Fatalf("第三帧 = seq=%d sessions=%d, want seq=3 sessions=1", f.Seq, len(f.Sessions))
	}
}

func TestHubSlowSubscriberDisconnected(t *testing.T) {
	h := newHub()
	emptySnap := func() *BrowserViewSnapshot { return &BrowserViewSnapshot{} }
	slow, cancelSlow := h.subscribe(emptySnap)
	defer cancelSlow()
	fast, cancelFast := h.subscribe(emptySnap)
	defer cancelFast()
	readFrame(t, slow)
	readFrame(t, fast)

	// 分批灌满慢订户缓冲 + 10（批间让出调度；健康订户逐批消费不溢出）。
	total := subscriberBuffer + 10
	for batch := 0; batch < total; batch += 20 {
		end := min(batch+20, total)
		frames := make([]Frame, 0, end-batch)
		for i := batch; i < end; i++ {
			frames = append(frames, Frame{Type: FrameSessions, Sessions: []SessionView{{Profile: "p"}}})
		}
		h.broadcast(frames)
		for i := batch; i < end; i++ {
			readFrame(t, fast)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 慢订户读空 256 帧缓冲后被断开。
	for i := 0; i < subscriberBuffer; i++ {
		select {
		case _, ok := <-slow:
			if !ok {
				t.Fatalf("慢订户在第 %d 帧提前断开", i)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("慢订户第 %d 帧超时", i)
		}
	}
	select {
	case _, ok := <-slow:
		if ok {
			t.Fatal("慢订户读空缓冲后应被断开")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("慢订户断开信号超时")
	}
}

func TestHubUnsubscribeAndCloseAll(t *testing.T) {
	h := newHub()
	emptySnap := func() *BrowserViewSnapshot { return &BrowserViewSnapshot{} }
	framesA, cancelA := h.subscribe(emptySnap)
	framesB, _ := h.subscribe(emptySnap)
	readFrame(t, framesA)
	readFrame(t, framesB)

	cancelA()
	cancelA() // 幂等

	h.closeAll()
	// 关停后 broadcast 为 no-op，不 panic。
	h.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{{Profile: "p"}}}})

	for _, frames := range []<-chan Frame{framesA, framesB} {
		select {
		case _, ok := <-frames:
			if ok {
				t.Fatal("通道应已关闭")
			}
		default:
			t.Fatal("通道应已关闭（读到零值）")
		}
	}
}
