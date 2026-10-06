package bot

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

// stubChannelRuntime 渠道运行时替身：只记 Start 次数（ApplyBotAsync 的可观测面是
// 「延迟后渠道被重新拉起」，连接语义本身不进本测试域）。
type stubChannelRuntime struct {
	mu     sync.Mutex
	starts int
}

func (r *stubChannelRuntime) Start(_ BotRuntimeConfig, _ func(InboundMessage)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts++
	return nil
}

func (r *stubChannelRuntime) Stop() {}

func (r *stubChannelRuntime) Status() ChannelStatus { return ChannelStatus{State: "connected"} }

func (r *stubChannelRuntime) OpenReply(_ RouteInfo) (ReplyStream, error) {
	return nil, errors.New("stub 无回复流")
}

func (r *stubChannelRuntime) startCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.starts
}

// stubChannelFactory 注册表替身：恒回同一 runtime 实例。
type stubChannelFactory struct{ rt *stubChannelRuntime }

func (f *stubChannelFactory) Channel() enums.Channel { return enums.CHANNEL_WECOM }

func (f *stubChannelFactory) Create() (ChannelRuntime, error) { return f.rt, nil }

// shortenApplyDelay 缩短热重载延迟并注册还原（包级变量，生产 1s 太长进不了测试）。
func shortenApplyDelay(t *testing.T) {
	t.Helper()
	old := applyBotAsyncDelay
	applyBotAsyncDelay = 10 * time.Millisecond
	t.Cleanup(func() { applyBotAsyncDelay = old })
}

// newSupervisorHarness 装配被测 supervisor + 已注册渠道替身（bot 行恒 ID 1，
// WorkspaceID 0 = 不触工作空间表，故夹具库只建 ImBot 表）。
func newSupervisorHarness(t *testing.T) (*Supervisor, *stubChannelRuntime) {
	t.Helper()
	db := newBotTestDB(t, &model.ImBot{})
	mkBot(t, db, 0)
	rt := &stubChannelRuntime{}
	s := NewSupervisor(db, 9100, nil, zap.NewNop())
	s.Register(&stubChannelFactory{rt: rt})
	return s, rt
}

// TestApplyBotAsyncReload 启用行：延迟后重读行 → ApplyBot → 渠道被拉起；附带装配断言
// （supervisor 以 botApplier 第四依赖注入路由驱动）。
func TestApplyBotAsyncReload(t *testing.T) {
	shortenApplyDelay(t)
	s, rt := newSupervisorHarness(t)

	route, ok := s.orch.driver.(*driverRoute)
	if !ok || route.applier == nil {
		t.Fatal("NewSupervisor 应将自身以 botApplier 注入路由驱动")
	}

	s.ApplyBotAsync(1)
	waitFor(t, "延迟热重载拉起渠道", func() bool { return rt.startCount() == 1 })
}

// TestApplyBotAsyncBotDeleted 行已删：静默跳过（NotFound 不拉渠道不报错）。负向断言，
// 有界等待（约 15× 延迟窗）后收口。
func TestApplyBotAsyncBotDeleted(t *testing.T) {
	shortenApplyDelay(t)
	s, rt := newSupervisorHarness(t)

	q := query.Use(s.db)
	if _, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.ID.Eq(1)).Delete(); err != nil {
		t.Fatalf("删 bot 行: %v", err)
	}
	s.ApplyBotAsync(1)
	time.Sleep(150 * time.Millisecond)
	if rt.startCount() != 0 {
		t.Fatalf("行已删应跳过热重载: starts=%d", rt.startCount())
	}
}

// TestApplyBotAsyncDisabled 停用行：对齐 service 层 applyRuntime 语义走 StopBot
// （未在跑为 no-op），不拉渠道。
func TestApplyBotAsyncDisabled(t *testing.T) {
	shortenApplyDelay(t)
	s, rt := newSupervisorHarness(t)

	q := query.Use(s.db)
	if _, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.ID.Eq(1)).
		UpdateSimple(q.ImBot.Enabled.Value(enums.YES_NO_NO)); err != nil {
		t.Fatalf("停用 bot: %v", err)
	}
	s.ApplyBotAsync(1)
	time.Sleep(150 * time.Millisecond)
	if rt.startCount() != 0 {
		t.Fatalf("停用行应走 StopBot 而非拉渠道: starts=%d", rt.startCount())
	}
}

// TestApplyBotAsyncDisabledWithinWindow 核心语义回归防护：行变更落在延迟窗口内——
// goroutine 醒来后须重读行（非调用时点快照）走停用分支不拉渠道（若实现退化为调用时
// 快照，调用时行启用 → 拉渠道 → 本测试失败）。
func TestApplyBotAsyncDisabledWithinWindow(t *testing.T) {
	old := applyBotAsyncDelay
	applyBotAsyncDelay = 50 * time.Millisecond // 留足窗口内改行余量
	t.Cleanup(func() { applyBotAsyncDelay = old })
	s, rt := newSupervisorHarness(t)

	s.ApplyBotAsync(1) // goroutine 已起未醒
	q := query.Use(s.db)
	if _, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.ID.Eq(1)).
		UpdateSimple(q.ImBot.Enabled.Value(enums.YES_NO_NO)); err != nil {
		t.Fatalf("窗口内停用 bot: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if rt.startCount() != 0 {
		t.Fatalf("窗口内停用后醒来重读应走 StopBot 不拉渠道: starts=%d", rt.startCount())
	}
}

// TestApplyBotAsyncAfterStopAll StopAll 后 applier 不得复活渠道（stopped 生命周期守卫；
// 渠道工厂注册表不清空，无守卫时渠道会被重新拉起——本测试即失败）。
func TestApplyBotAsyncAfterStopAll(t *testing.T) {
	shortenApplyDelay(t)
	s, rt := newSupervisorHarness(t)
	s.StopAll()

	s.ApplyBotAsync(1)
	time.Sleep(150 * time.Millisecond)
	if rt.startCount() != 0 {
		t.Fatalf("StopAll 后不应复活渠道: starts=%d", rt.startCount())
	}
}
