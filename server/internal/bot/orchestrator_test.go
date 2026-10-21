package bot

import (
	"context"
	"sync"
	"testing"

	"go.uber.org/zap"
)

/*
 * fakeReplyStream 测试替身：顺序记录帧（content/final），恒成功。写入方在 worker
 * goroutine（回合泵），读取方在测试 goroutine——互斥保护，跨 goroutine 断言走 snapshot。
 */
type fakeReplyStream struct {
	mu     sync.Mutex
	frames []fakeFrame
}

type fakeFrame struct {
	content string
	final   bool
}

func (f *fakeReplyStream) Flush(content string, final bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.frames = append(f.frames, fakeFrame{content: content, final: final})
	return nil
}

// snapshot 帧序只读拷贝（跨 goroutine 读取入口）。
func (f *fakeReplyStream) snapshot() []fakeFrame {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeFrame(nil), f.frames...)
}

func (f *fakeReplyStream) ByteLimit() int { return 20480 }

// MarkdownFrames 实现 MarkdownFrameStream：测试替身默认按 markdown 渠道组帧。
func (f *fakeReplyStream) MarkdownFrames() bool { return true }

/*
 * fakeCardReplyStream 卡能力流替身（CardReplyStream 实现）：帧记录之上追加卡事件记录
 * （SendCard 附卡帧 / UpdateCard 卡更新）——「不出卡」类负向断言的入口。
 */
type fakeCardReplyStream struct {
	fakeReplyStream
	mu      sync.Mutex
	cards   []fakeCardMsg
	sendErr error
}

type fakeCardMsg struct {
	spec    CardSpec
	content string
	final   bool
	update  bool
}

func (f *fakeCardReplyStream) SendCard(spec CardSpec, content string, final bool) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cards = append(f.cards, fakeCardMsg{spec: spec, content: content, final: final})
	return nil
}

func (f *fakeCardReplyStream) UpdateCard(spec CardSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cards = append(f.cards, fakeCardMsg{spec: spec, update: true})
	return nil
}

// cardSnapshot 卡事件序只读拷贝（跨 goroutine 读取入口）。
func (f *fakeCardReplyStream) cardSnapshot() []fakeCardMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeCardMsg(nil), f.cards...)
}

// newOrchestratorWithDB 空引擎编排器（指令步/门禁只消费 store，主路径未触达时 driver nil 安全）。
func newOrchestratorWithDB(t *testing.T) (*Orchestrator, *ConversationStore) {
	t.Helper()
	store := &ConversationStore{DB: newIssueTestDB(t)}
	return NewOrchestrator(store, nil, nil, nil, zap.NewNop()), store
}

// newOrchestratorWithDriver 指定引擎装配（闲置边界全链路用例：driver 兼任 routeProber）。
func newOrchestratorWithDriver(t *testing.T, driver ClaudeDriver) (*Orchestrator, *ConversationStore) {
	t.Helper()
	store := &ConversationStore{DB: newIssueTestDB(t)}
	return NewOrchestrator(store, driver, nil, nil, zap.NewNop()), store
}

/*
 * stubProberDriver 路由探针替身：ClaudeDriver + routeProber 双角色，注入探针结论与回合
 * 调用计数。calls 由 worker goroutine 写、测试 goroutine 读（waitFor 轮询），互斥保护。
 */
type stubProberDriver struct {
	mu    sync.Mutex
	calls int
	acp   bool
	err   error
}

func (s *stubProberDriver) RunTurn(_ context.Context, _ TurnRequest) (<-chan TurnEvent, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	events := make(chan TurnEvent, 1)
	events <- TurnEvent{Type: TurnDone, Result: "ok"}
	close(events)
	return events, nil
}

func (s *stubProberDriver) ProbeTarget(_ BotRuntimeConfig, _ string) (bool, error) {
	return s.acp, s.err
}

func (s *stubProberDriver) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// openAccess 测试用开放白名单（全链路用例过受理步——空策略 fail closed 会拦在白名单）。
func openAccess() AccessPolicy { return AccessPolicy{Mode: AccessModeOpen} }

// issueCmdCfg 测试用运行配置。
func issueCmdCfg() BotRuntimeConfig {
	return BotRuntimeConfig{BotID: "bot-1", WorkspaceID: "ws-1", WorkspaceDir: "/tmp/ocean-test", AccessPolicy: openAccess()}
}

/*
 * TestRunTurnWorkspaceGate 工作空间门禁（未选 workspace 的 bot）：已 markSeen（消息已被
 * 处理，不算失败、不触发会话自愈），纯指引文本终帧收口，不出回合（nil driver 不 panic
 * 即证明主路径未触达）。
 */
func TestRunTurnWorkspaceGate(t *testing.T) {
	o, store := newOrchestratorWithDB(t)
	rs := &fakeReplyStream{}
	o.HandleInbound(BotRuntimeConfig{BotID: "bot-1", AccessPolicy: openAccess()}, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "跑一下测试"}, func() (ReplyStream, error) {
		return rs, nil
	})
	frames := finalFrameOf(t, "门禁终帧", rs)
	if len(frames) != 2 || frames[0].content != "> 收到，处理中…" || frames[1].content != workspaceGateLead || !frames[1].final {
		t.Fatalf("门禁应占位帧后接指引终帧: %+v", frames)
	}
	if seen, err := store.HasSeen("bot-1", "single:u1", "m-1"); err != nil || !seen {
		t.Fatalf("门禁消息应 markSeen: seen=%v err=%v", seen, err)
	}
}

/*
 * TestRunTurnHelpCommand #帮助 指令步全链路（置于门禁之前——未选工作空间时恰是最需要
 * 指令列表的时刻）：占位帧后纯文本指令列表终帧收口，不出卡不出回合。
 */
func TestRunTurnHelpCommand(t *testing.T) {
	o, _ := newOrchestratorWithDB(t) // 无 workspace 数据：帮助步须先于门禁收口
	rs := &fakeCardReplyStream{}
	o.HandleInbound(BotRuntimeConfig{BotID: "bot-1", AccessPolicy: openAccess()}, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "#帮助"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "#帮助 终帧", func() bool {
		for _, f := range rs.snapshot() {
			if f.final {
				return true
			}
		}
		return false
	})
	frames, cards := rs.snapshot(), rs.cardSnapshot()
	if len(frames) != 2 || frames[0].content != "> 收到，处理中…" || frames[1].content != helpText() || !frames[1].final {
		t.Fatalf("帮助应占位帧后接纯文本指令列表终帧: %+v", frames)
	}
	if len(cards) != 0 {
		t.Fatalf("帮助不出卡: %+v", cards)
	}
}

/*
 * TestRunTurnMainPathFrames 主路径帧序：占位帧 → 执行期状态帧（填充静默期）→ 回合终帧；
 * 指令步/门禁等终帧收口路径不得出现状态帧（各收口用例的帧数断言即证明）。
 */
func TestRunTurnMainPathFrames(t *testing.T) {
	driver := &recordingDriver{}
	o, _ := newOrchestratorWithDriver(t, driver)
	rs := &fakeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "跑一下测试"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "回合完成", func() bool {
		frames := rs.snapshot()
		return len(frames) > 0 && frames[len(frames)-1].final
	})
	frames := rs.snapshot()
	if len(frames) != 3 || frames[0].content != "> 收到，处理中…" ||
		frames[1].content != "> 已开始处理，正在连接会话…" || frames[1].final ||
		frames[2].content != "ok" || !frames[2].final {
		t.Fatalf("主路径应含执行期状态帧且恰一终帧: %+v", frames)
	}
}
