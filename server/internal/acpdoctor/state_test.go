package acpdoctor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"ocean-harness/server/internal/agentcatalog"
)

// resetDoctorState 清空包级缓存与在跑集合并恢复 probeFunc（测试间隔离；包级状态为
// 进程内单例，测试串行运行、不复用 t.Parallel）。
func resetDoctorState(t *testing.T) {
	t.Helper()
	clearDoctorState()
	t.Cleanup(func() {
		probeFunc = CheckEntry
		clearDoctorState()
	})
}

func clearDoctorState() {
	stateMu.Lock()
	defer stateMu.Unlock()
	reports = map[string]Report{}
	inflight = map[string]*checkRun{}
}

// healthyProbe 构造固定 healthy 结论的探测替身（计数经原子量供单飞断言）。
func healthyProbe(calls *atomic.Int64) func(context.Context, agentcatalog.Entry, Dirs, *zap.Logger) Report {
	return func(_ context.Context, entry agentcatalog.Entry, _ Dirs, _ *zap.Logger) Report {
		calls.Add(1)
		return Report{AgentCode: entry.Code, Status: StatusHealthy, CheckedAt: time.Now(), CommandCount: 3}
	}
}

// settlePoll 等待条件成立（10ms 轮询，超时 Fatal）——异步受理的确定性收敛断言。
func settlePoll(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

func entryState(states []EntryState, code string) EntryState {
	for _, s := range states {
		if s.AgentCode == code {
			return s
		}
	}
	return EntryState{}
}

func TestRequestCheckUnknownCode(t *testing.T) {
	resetDoctorState(t)
	if _, err := RequestCheck("no-such-agent", Dirs{}, zap.NewNop()); err == nil {
		t.Fatal("catalog 无此条目应报错（宁失败不静默）")
	}
}

func TestRequestCheckAcceptsEnabledEntries(t *testing.T) {
	resetDoctorState(t)
	// 即时替身：本测试只关心受理范围编排；真实 CheckEntry 预检耗数百毫秒，若放任其
	// 跨测试存续，会在后续测试中途 delete(inflight)/写缓存（孤儿探测污染 + data race）。
	var calls atomic.Int64
	probeFunc = healthyProbe(&calls)
	accepted, err := RequestCheck("", Dirs{}, zap.NewNop())
	if err != nil {
		t.Fatalf("空 agentCode 应受理全部 enabled 条目: %v", err)
	}
	var want []string
	for _, entry := range agentcatalog.Enabled() {
		want = append(want, entry.Code)
	}
	if len(accepted) != len(want) {
		t.Fatalf("受理清单 = %v, want %v", accepted, want)
	}
	for i := range want {
		if accepted[i] != want[i] {
			t.Fatalf("受理清单 = %v, want %v", accepted, want)
		}
	}
	// 收敛后再返回：不留孤儿探测。
	settlePoll(t, 5*time.Second, func() bool { return len(inflightCodes()) == 0 }, "受理探测未收敛")
}

func TestRequestCheckSingleFlightJoin(t *testing.T) {
	resetDoctorState(t)
	var calls atomic.Int64
	// 探测替身挂起在 gate 上：join 窗口确定性敞开（close 放行），不依赖任何时序假设。
	release := make(chan struct{})
	probeFunc = func(_ context.Context, entry agentcatalog.Entry, _ Dirs, _ *zap.Logger) Report {
		calls.Add(1)
		<-release
		return Report{AgentCode: entry.Code, Status: StatusHealthy, CheckedAt: time.Now(), CommandCount: 3}
	}

	if _, err := RequestCheck("claude-acp", Dirs{}, zap.NewNop()); err != nil {
		t.Fatalf("受理: %v", err)
	}
	settlePoll(t, 5*time.Second, func() bool { return calls.Load() == 1 }, "首次受理未开始探测")
	if _, err := RequestCheck("claude-acp", Dirs{}, zap.NewNop()); err != nil {
		t.Fatalf("受理（在跑 join）: %v", err)
	}
	close(release) // 放行在跑探测
	settlePoll(t, 5*time.Second, func() bool { return len(inflightCodes()) == 0 }, "探测未在时限内收敛")
	if calls.Load() != 1 {
		t.Fatalf("在跑受理应单飞 join（只跑一次），实际 %d 次", calls.Load())
	}
	// 收敛后再受理：新一轮探测（单飞只对在跑生效，不缓存去重）。
	probeFunc = healthyProbe(&calls)
	if _, err := RequestCheck("claude-acp", Dirs{}, zap.NewNop()); err != nil {
		t.Fatalf("再次受理: %v", err)
	}
	settlePoll(t, 5*time.Second, func() bool { return len(inflightCodes()) == 0 }, "第二轮探测未收敛")
	if calls.Load() != 2 {
		t.Fatalf("收敛后受理应重跑，实际累计 %d 次", calls.Load())
	}
}

func TestSnapshotFourStates(t *testing.T) {
	resetDoctorState(t)
	code := agentcatalog.Enabled()[0].Code

	// unknown：从未探测 ≠ 探测失败。
	states := Snapshot()
	if got := entryState(states, code); got.Status != StatusUnknown {
		t.Fatalf("从未探测应为 unknown，got %s", got.Status)
	}

	// checking：受理在跑（派生态），结论字段为旧结论（此时为零值）。
	var calls atomic.Int64
	probeFunc = healthyProbe(&calls)
	slowProbe := probeFunc
	probeFunc = func(ctx context.Context, entry agentcatalog.Entry, dirs Dirs, log *zap.Logger) Report {
		time.Sleep(150 * time.Millisecond)
		return slowProbe(ctx, entry, dirs, log)
	}
	if _, err := RequestCheck(code, Dirs{}, zap.NewNop()); err != nil {
		t.Fatalf("受理: %v", err)
	}
	if got := entryState(Snapshot(), code); got.Status != StatusChecking {
		t.Fatalf("受理在跑应为 checking，got %s", got.Status)
	}

	// healthy：收敛后带结论字段。
	settlePoll(t, 5*time.Second, func() bool { return entryState(Snapshot(), code).Status == StatusHealthy },
		"探测未在时限内收敛")
	got := entryState(Snapshot(), code)
	if got.CommandCount != 3 || got.CheckedAt.IsZero() {
		t.Fatalf("healthy 结论字段不完整: %+v", got)
	}
}

func TestSnapshotCheckingCarriesStaleReport(t *testing.T) {
	resetDoctorState(t)
	code := agentcatalog.Enabled()[0].Code

	// 预置旧结论（unhealthy）：启动探测填满缓存后手动重检的常规场景。
	stateMu.Lock()
	reports[code] = Report{AgentCode: code, Status: StatusUnhealthy, CheckedAt: time.Now(),
		Stage: StageSpawn, Reason: "旧结论"}
	stateMu.Unlock()

	// 受理在跑（gate 挂起）：Snapshot 必为 checking，且旧结论字段原样带出——
	// 若被旧 status 覆盖，前端轮询条件（checking/unknown）永不命中，新结论不可达。
	release := make(chan struct{})
	probeFunc = func(_ context.Context, entry agentcatalog.Entry, _ Dirs, _ *zap.Logger) Report {
		<-release
		return Report{AgentCode: entry.Code, Status: StatusHealthy, CheckedAt: time.Now(), CommandCount: 3}
	}
	if _, err := RequestCheck(code, Dirs{}, zap.NewNop()); err != nil {
		t.Fatalf("受理: %v", err)
	}
	got := entryState(Snapshot(), code)
	if got.Status != StatusChecking {
		t.Fatalf("已有旧结论且在跑应为 checking，got %s", got.Status)
	}
	if got.Reason != "旧结论" || got.Stage != StageSpawn {
		t.Fatalf("checking 应带出旧结论字段: %+v", got)
	}
	// 显式放行并收敛后再返回：不留「测试结束后才写报告」的孤儿 goroutine（会污染下一测试的缓存态）。
	close(release)
	settlePoll(t, 5*time.Second, func() bool { return len(inflightCodes()) == 0 }, "探测未在时限内收敛")
	if got := entryState(Snapshot(), code); got.Status != StatusHealthy || got.CommandCount != 3 {
		t.Fatalf("放行后应落为新结论，got %+v", got)
	}
}

func TestRunStartupProbeFillsAllEnabled(t *testing.T) {
	resetDoctorState(t)
	var calls atomic.Int64
	probeFunc = healthyProbe(&calls)

	RunStartupProbe(Dirs{}, zap.NewNop())
	want := len(agentcatalog.Enabled())
	settlePoll(t, 5*time.Second, func() bool {
		states := Snapshot()
		if len(states) != want {
			return false
		}
		for _, s := range states {
			if s.Status != StatusHealthy {
				return false
			}
		}
		return true
	}, "启动探测未在时限内填满全部 enabled 条目")
	if int(calls.Load()) != want {
		t.Fatalf("启动探测应逐条目各跑一次，实际 %d/%d", calls.Load(), want)
	}
}

// inflightCodes 当前在跑条目清单（断言辅助）。
func inflightCodes() []string {
	stateMu.RLock()
	defer stateMu.RUnlock()
	codes := make([]string, 0, len(inflight))
	for code := range inflight {
		codes = append(codes, code)
	}
	return codes
}
