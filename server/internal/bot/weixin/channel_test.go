package weixin

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"ocean-harness/server/internal/bot"
)

/*
 * TestPollLoopRejectsTerminal 非瞬态拒绝三振：pollLoop 返回 true（连续被拒终态，交自动
 * 重连），状态落 disconnected（拒绝间退避 2s/4s，本用例耗时约 6s）。
 */
func TestPollLoopRejectsTerminal(t *testing.T) {
	client, srv := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	rt := &channelRuntime{gen: 1}
	cg := connGen{log: zap.NewNop(), client: client, token: "tk", baseURL: srv.URL, gen: 1}
	if !rt.pollLoop(context.Background(), cg) {
		t.Fatal("三次非瞬态拒绝应判连续被拒终态（返回 true 交自动重连）")
	}
	st := rt.Status()
	if st.State != "disconnected" || !strings.Contains(st.LastError, "连续 3 次拉取消息被拒绝") {
		t.Fatalf("终态投影不符: %+v", st)
	}
}

/*
 * TestPollLoopStaleTokenNotRejected token 失效（-14）终态不属被拒族：pollLoop 返回 false，
 * 不触发自动重连（须人工重扫）。
 */
func TestPollLoopStaleTokenNotRejected(t *testing.T) {
	client, srv := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ret":-14}`))
	}))
	rt := &channelRuntime{gen: 1}
	cg := connGen{log: zap.NewNop(), client: client, token: "tk", baseURL: srv.URL, gen: 1}
	if rt.pollLoop(context.Background(), cg) {
		t.Fatal("token 失效终态不得判为被拒（不自动重连）")
	}
	if st := rt.Status(); st.State != "disconnected" || !strings.Contains(st.LastError, "重新扫码授权") {
		t.Fatalf("终态投影不符: %+v", st)
	}
}

/*
 * TestRejectRetryExhaustedTerminal 预算耗尽：维持 disconnected 终态，lastError 追加
 * 「已自动重连未成功」提示；耗尽分支同步返回（无等待、无建连）。
 */
func TestRejectRetryExhaustedTerminal(t *testing.T) {
	rt := &channelRuntime{
		gen:           7,
		state:         "disconnected",
		lastErr:       "连续 3 次拉取消息被拒绝: 403",
		rejectRetries: len(rejectRetryDelays),
	}
	rt.retryAfterRejects(connGen{gen: 7, log: zap.NewNop()})
	st := rt.Status()
	if st.State != "disconnected" || !strings.Contains(st.LastError, "已自动重连 3 次未成功") {
		t.Fatalf("耗尽后应维持终态并改写提示: %+v", st)
	}
}

/*
 * TestRejectRetryYieldsToManualStop 手动 Stop 优先：旧代重试在代际守卫处失效——不建连、
 * stopped 终态不被覆盖。
 */
func TestRejectRetryYieldsToManualStop(t *testing.T) {
	rt := &channelRuntime{gen: 7, state: "disconnected", lastErr: "被拒"}
	rt.Stop()
	rt.retryAfterRejects(connGen{gen: 7, log: zap.NewNop()})
	if st := rt.Status(); st.State != "stopped" {
		t.Fatalf("手动 Stop 后旧代重试不得复活连接: %+v", st)
	}
}

/*
 * TestRejectRetryRelaunchesConnection 预算内真实发起重连：退避后经 launch 重建（以不受信
 * baseUrl 令 launch 报错，佐证重连链路被走到且错误防御分支落终态）。
 */
func TestRejectRetryRelaunchesConnection(t *testing.T) {
	oldDelays, oldJitter := rejectRetryDelays, rejectRetryJitter
	rejectRetryDelays, rejectRetryJitter = []time.Duration{time.Millisecond}, time.Duration(0)
	t.Cleanup(func() { rejectRetryDelays, rejectRetryJitter = oldDelays, oldJitter })

	cfg := bot.BotRuntimeConfig{Credential: `{"token":"tk","baseUrl":"https://example.invalid/"}`}
	rt := &channelRuntime{gen: 1, state: "disconnected", lastErr: "被拒"}
	rt.retryAfterRejects(connGen{cfg: cfg, gen: 1, log: zap.NewNop()})
	st := rt.Status()
	if st.State != "disconnected" || !strings.Contains(st.LastError, "自动重连失败") {
		t.Fatalf("重连应真实发起（launch 被调用）: %+v", st)
	}
}

/*
 * TestRejectRetryWaitInterruptedByStop 在途退避等待被 Stop 提前唤醒（等待挂起零轮询），
 * 重试 goroutine 即刻退出不建连。
 */
func TestRejectRetryWaitInterruptedByStop(t *testing.T) {
	oldDelays, oldJitter := rejectRetryDelays, rejectRetryJitter
	rejectRetryDelays, rejectRetryJitter = []time.Duration{30 * time.Second}, time.Duration(0)
	t.Cleanup(func() { rejectRetryDelays, rejectRetryJitter = oldDelays, oldJitter })

	rt := &channelRuntime{gen: 1, state: "disconnected"}
	done := make(chan struct{})
	go func() {
		rt.retryAfterRejects(connGen{gen: 1, log: zap.NewNop()})
		close(done)
	}()
	rt.Stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop 应提前唤醒在途重连等待")
	}
	if st := rt.Status(); st.State != "stopped" {
		t.Fatalf("唤醒后不得建连: %+v", st)
	}
}
