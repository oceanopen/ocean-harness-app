package bot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

// stubChannelRuntime 渠道运行时替身：只记 Start 次数（连接语义本身不进本测试域）。
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

// stubFailingFactory Create 恒失败的工厂替身（last_error 回写链路验证）。
type stubFailingFactory struct{}

func (f *stubFailingFactory) Channel() enums.Channel { return enums.CHANNEL_WECOM }

func (f *stubFailingFactory) Create() (ChannelRuntime, error) { return nil, errors.New("渠道创建失败") }

/*
 * newSupervisorHarness 装配被测 supervisor + 已注册渠道替身 + 单条启用 bot 行
 * （bot-1，open 策略，WorkspaceID 空 = 不触工作空间表）。
 */
func newSupervisorHarness(t *testing.T) (*Supervisor, *stubChannelRuntime) {
	t.Helper()
	db := newBotTestDB(t, &model.ImBot{})
	createBotRow(t, db, "bot-1", enums.YES_NO_YES)
	rt := &stubChannelRuntime{}
	s := NewSupervisor(db, 9100, nil, NewDeliveryRegistry(), zap.NewNop())
	s.Register(&stubChannelFactory{rt: rt})
	return s, rt
}

// createBotRow 建单条 bot 行（enabled 控制启用位，open 策略免凭据校验）。
func createBotRow(t *testing.T, db *gorm.DB, botID string, enabled enums.YesNo) {
	t.Helper()
	q := query.Use(db)
	if err := q.ImBot.WithContext(context.Background()).Create(&model.ImBot{
		ID: botID, Name: "bot", Channel: enums.CHANNEL_WECOM,
		AccessPolicy: `{"mode":"open"}`, Enabled: enabled,
	}); err != nil {
		t.Fatalf("建 bot 行: %v", err)
	}
}

// botRow 读回 bot 行（ApplyBot 入参）。
func botRow(t *testing.T, db *gorm.DB, botID string) *model.ImBot {
	t.Helper()
	q := query.Use(db)
	row, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.ID.Eq(botID)).First()
	if err != nil {
		t.Fatalf("读 bot 行: %v", err)
	}
	return row
}

// lastErrorOf 读回 bot 行的 last_error 列。
func lastErrorOf(t *testing.T, db *gorm.DB, botID string) string {
	t.Helper()
	return botRow(t, db, botID).LastError
}

// TestApplyBotStartsChannel 启用行 ApplyBot：渠道被拉起且 last_error 保持清空。
func TestApplyBotStartsChannel(t *testing.T) {
	s, rt := newSupervisorHarness(t)

	if err := s.ApplyBot(botRow(t, s.db, "bot-1")); err != nil {
		t.Fatalf("启用行应受理: %v", err)
	}
	if rt.startCount() != 1 {
		t.Fatalf("渠道应恰拉起一次: starts=%d", rt.startCount())
	}
	if msg := lastErrorOf(t, s.db, "bot-1"); msg != "" {
		t.Fatalf("成功受理应清空 last_error: %q", msg)
	}
}

// TestApplyBotChannelFailureWritesLastError 渠道启动失败：受理报错且 last_error 落库可见。
func TestApplyBotChannelFailureWritesLastError(t *testing.T) {
	db := newBotTestDB(t, &model.ImBot{})
	createBotRow(t, db, "bot-1", enums.YES_NO_YES)
	s := NewSupervisor(db, 9100, nil, NewDeliveryRegistry(), zap.NewNop())
	s.Register(&stubFailingFactory{})

	err := s.ApplyBot(botRow(t, db, "bot-1"))
	if err == nil || !strings.Contains(err.Error(), "渠道创建失败") {
		t.Fatalf("渠道启动失败应同步报错: %v", err)
	}
	if msg := lastErrorOf(t, db, "bot-1"); !strings.Contains(msg, "渠道创建失败") {
		t.Fatalf("失败应回写 last_error: %q", msg)
	}
}

// TestStartEnabledRunsEnabledBots 启动清单：enabled 行被拉起。
func TestStartEnabledRunsEnabledBots(t *testing.T) {
	s, rt := newSupervisorHarness(t)

	s.StartEnabled()
	if rt.startCount() != 1 {
		t.Fatalf("enabled 行应被启动: starts=%d", rt.startCount())
	}
}

/*
 * TestEnsureStartedAfterStopAll StopAll 后不得复活渠道（stopped 生命周期守卫；渠道工厂
 * 注册表不清空，无守卫时渠道会被重新拉起——本测试即失败）。
 */
func TestEnsureStartedAfterStopAll(t *testing.T) {
	s, rt := newSupervisorHarness(t)
	s.StopAll()

	cfg := BotRuntimeConfig{BotID: "bot-1", Channel: enums.CHANNEL_WECOM, AccessPolicy: AccessPolicy{Mode: AccessModeOpen}}
	if err := s.EnsureStarted(cfg); err == nil || !strings.Contains(err.Error(), "拒绝启动") {
		t.Fatalf("StopAll 后应拒绝启动: %v", err)
	}
	if rt.startCount() != 0 {
		t.Fatalf("StopAll 后不应拉起渠道: starts=%d", rt.startCount())
	}
}

// TestStopBotIdempotent 未在跑的 bot 停止为 no-op（幂等）。
func TestStopBotIdempotent(t *testing.T) {
	s, rt := newSupervisorHarness(t)

	s.StopBot("bot-ghost")
	s.StopBot("bot-ghost")
	if rt.startCount() != 0 {
		t.Fatalf("未知 bot 停止不应触碰渠道: starts=%d", rt.startCount())
	}
}
