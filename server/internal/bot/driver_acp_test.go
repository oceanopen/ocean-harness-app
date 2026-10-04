package bot

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/acpsession"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

// fakeSessions acpSessions 的帧脚本替身：每次 Subscribe 返回独立通道（对齐真实 hub 的
// 「受理后重开干净订阅」——RunTurn 建两段订阅，脚本帧按投递时点落位），Ensure/PromptQueued
// 行为与错误按字段注入，调用留痕供断言。
// push 时序无关：订阅建立前入 pending（Subscribe 时灌入新通道首部，模拟真实 hub「订阅
// 首帧快照 + 预推状态帧」），有订阅则推最新订阅通道。
// PromptQueued 替身默认即到即返（等位语义由 acpsession 包的 Manager 测试承载）；置
// promptBlockOnCtx 模拟等位阻塞（驱动应透传生命周期 ctx）；promptHook 在受理返回前
// 调用（模拟等位窗口内往首段订阅灌阻塞回合的帧）。
type fakeSessions struct {
	mu               sync.Mutex
	snap             acpsession.ViewSnapshot
	ensureErr        error
	promptErr        error
	promptBlockOnCtx bool
	promptHook       func()

	pending []acpsession.Frame
	subs    []*fakeSub

	ensureCalls int
	promptTexts []string
	cancelCalls int
	subscribes  int
}

// fakeSub 单次订阅的通道与关闭态（RunTurn 两段订阅各自独立退订）。
type fakeSub struct {
	ch     chan acpsession.Frame
	closed bool
}

func newFakeSessions(snap acpsession.ViewSnapshot) *fakeSessions {
	return &fakeSessions{snap: snap}
}

func (f *fakeSessions) Ensure(_ context.Context, _, _ string) (acpsession.ViewSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureCalls++
	return f.snap, f.ensureErr
}

func (f *fakeSessions) Subscribe(_ string) (<-chan acpsession.Frame, func()) {
	f.mu.Lock()
	f.subscribes++
	sub := &fakeSub{ch: make(chan acpsession.Frame, 64)}
	for _, frame := range f.pending {
		sub.ch <- frame
	}
	f.pending = nil
	f.subs = append(f.subs, sub)
	f.mu.Unlock()
	return sub.ch, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !sub.closed {
			sub.closed = true
			close(sub.ch)
		}
	}
}

func (f *fakeSessions) PromptQueued(ctx context.Context, _ string, text string) error {
	f.mu.Lock()
	f.promptTexts = append(f.promptTexts, text)
	promptErr := f.promptErr
	hook := f.promptHook
	f.mu.Unlock()
	if promptErr != nil {
		return promptErr
	}
	if f.promptBlockOnCtx {
		<-ctx.Done() // 模拟等位阻塞：ctx 取消才返回（驱动应透传生命周期 ctx，非 Background）
		return ctx.Err()
	}
	if hook != nil {
		hook() // 受理返回前的脚本时机：等位窗口帧落首段订阅（最新订阅此刻仍是它）
	}
	return nil
}

func (f *fakeSessions) Cancel(_ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelCalls++
	return nil
}

// push 投递脚本帧：订阅建立前入 pending（Subscribe 灌入，时序无关）；有订阅则推最新
// 订阅通道（缓冲远大于脚本帧数，满即阻塞为测试编写错误）。
func (f *fakeSessions) push(t *testing.T, frame acpsession.Frame) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.subs) == 0 {
		f.pending = append(f.pending, frame)
		return
	}
	f.subs[len(f.subs)-1].ch <- frame
}

// closeFrames 主动断开最新订阅的事件流（模拟 hub 关停 / 订阅溢出断开）。
func (f *fakeSessions) closeFrames() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.subs) == 0 {
		return
	}
	sub := f.subs[len(f.subs)-1]
	if !sub.closed {
		sub.closed = true
		close(sub.ch)
	}
}

// lastFrames 最新订阅的帧通道（断言辅助：受理失败路径只可能发生在首段订阅）。
func (f *fakeSessions) lastFrames() <-chan acpsession.Frame {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.subs) == 0 {
		return nil
	}
	return f.subs[len(f.subs)-1].ch
}

func (f *fakeSessions) stats() (ensureCalls, cancelCalls, subscribes int, prompts []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ensureCalls, f.cancelCalls, f.subscribes, append([]string(nil), f.promptTexts...)
}

// boundDriver 测试构造：绑定解析替身（生产装配在 T2.3 前恒未绑定——unbound 分支单测
// 覆盖，回合语义用例全部走绑定路径）。
func boundDriver(sessions acpSessions) acpDriver {
	return acpDriver{sessions: sessions, issue: func(string) (string, bool) { return "issue-acp", true }}
}

func readySnap() acpsession.ViewSnapshot {
	return acpsession.ViewSnapshot{Status: acpsession.StatusReady, Entries: []acpsession.ConversationEntry{}, Pendings: []acpsession.PendingView{}}
}

func startingSnap() acpsession.ViewSnapshot {
	return acpsession.ViewSnapshot{Status: acpsession.StatusStarting}
}

func turnStartedFrame() acpsession.Frame {
	return acpsession.Frame{Type: acpsession.FrameTurnStarted, Turn: &acpsession.TurnPayload{Active: true}}
}

func turnEndedFrame(reason string) acpsession.Frame {
	return acpsession.Frame{Type: acpsession.FrameTurnEnded, Turn: &acpsession.TurnPayload{StopReason: reason}}
}

func agentTextFrame(entryID, text string) acpsession.Frame {
	return acpsession.Frame{Type: acpsession.FrameEntry, Entry: &acpsession.ConversationEntry{EntryID: entryID, Kind: "agentMessage", Text: text}}
}

// recvEvent 收一个回合事件（超时失败，防测试悬挂）。
func recvEvent(t *testing.T, events <-chan TurnEvent) TurnEvent {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatalf("事件通道提前关闭")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatalf("等待回合事件超时")
		return TurnEvent{}
	}
}

// assertNoEvent 断言观察窗内无事件产出。
func assertNoEvent(t *testing.T, events <-chan TurnEvent) {
	t.Helper()
	select {
	case ev := <-events:
		t.Fatalf("意外收到事件: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

// assertClosed 断言帧通道已关闭（受理失败后的退订语义）。
func assertClosed(t *testing.T, ch <-chan acpsession.Frame, what string) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("%s：通道未关闭", what)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("%s：等待通道关闭超时", what)
	}
}

// waitFor 轮询等待条件成立（测试侧状态同步；生产时序不依赖此模式）。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("等待 %s 超时", what)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestAcpDriverTurnHappyPath(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)

	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	assertNoEvent(t, events)

	sessions.push(t, turnStartedFrame())
	sessions.push(t, agentTextFrame("t1-agentMessage", "Hel"))
	sessions.push(t, agentTextFrame("t1-agentMessage", "Hello!"))
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameEntry, Entry: &acpsession.ConversationEntry{
		EntryID: "tool:1", Kind: "toolCall", ToolCall: &acpsession.ToolCallView{ToolCallID: "tc1", Name: "Bash", Title: "运行命令"},
	}})
	// 同 id 工具 upsert 覆写帧不重复发进度事件
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameEntry, Entry: &acpsession.ConversationEntry{
		EntryID: "tool:1", Kind: "toolCall", ToolCall: &acpsession.ToolCallView{ToolCallID: "tc1", Name: "Bash", Status: "completed"},
	}})
	sessions.push(t, turnEndedFrame("end_turn"))

	ev := recvEvent(t, events)
	if ev.Type != TurnToolUse || ev.ToolName != "Bash" {
		t.Fatalf("工具进度事件不符: %+v", ev)
	}
	done := recvEvent(t, events)
	if done.Type != TurnDone || done.IsError || done.Result != "Hello!" {
		t.Fatalf("终态事件不符: %+v", done)
	}
	if done.SessionID != "" {
		t.Fatalf("ACP 驱动不得产出会话锚点: %+v", done)
	}
	if _, ok := <-events; ok {
		t.Fatalf("终态后事件通道应关闭")
	}
	_, _, _, prompts := sessions.stats()
	if len(prompts) != 1 || prompts[0] != "hi" {
		t.Fatalf("Prompt 受理内容不符: %v", prompts)
	}
}

func TestAcpDriverTurnArmedWindowIgnoresStale(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	// turnStarted 之前的条目帧（订阅窗口里的残留）不进入本轮回复。
	sessions.push(t, agentTextFrame("t0-agentMessage", "stale"))
	sessions.push(t, turnStartedFrame())
	sessions.push(t, turnEndedFrame("end_turn"))

	done := recvEvent(t, events)
	if done.Type != TurnDone || done.Result != "" {
		t.Fatalf("旧条目帧应被 armed 窗口隔离: %+v", done)
	}
}

func TestAcpDriverTurnWaitReadyViaSnapshotFrame(t *testing.T) {
	sessions := newFakeSessions(startingSnap())
	driver := boundDriver(sessions)
	// 订阅后置于 Ensure 受理：预推的 snapshot 帧即真实 hub 的「订阅首帧快照」（新于受理
	// 时点，模拟 spawn 在订阅完成前已就绪），waitSessionReady 以其为状态基线直接放行。
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameSnapshot, Snapshot: &acpsession.ViewSnapshot{Status: acpsession.StatusReady}})
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, turnEndedFrame("end_turn"))

	done := recvEvent(t, events)
	if done.Type != TurnDone {
		t.Fatalf("就绪后回合应正常完成: %+v", done)
	}
	_, _, _, prompts := sessions.stats()
	if len(prompts) != 1 {
		t.Fatalf("就绪后应恰受理一次 Prompt: %v", prompts)
	}
}

// TestAcpDriverTurnRebuildWaitReady 重建路径回归：Ensure 受理重建（starting）后订阅，
// 状态推进只认受理之后的 sessionStatus 帧——旧会话的失败态不得误报为本回合失败（旧因
// 回归用例，对应订阅后置修复）。
func TestAcpDriverTurnRebuildWaitReady(t *testing.T) {
	sessions := newFakeSessions(startingSnap())
	driver := boundDriver(sessions)
	// 受理重建（starting）后订阅；等就绪是 RunTurn 内同步步骤，sessionStatus ready 帧
	// 先入订阅通道缓冲，模拟 spawn 完成的状态帧在等待期到达（旧会话失败态无从混入）。
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameSessionStatus, Status: &acpsession.StatusPayload{Status: acpsession.StatusReady}})
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, agentTextFrame("t1-agentMessage", "重建后的回复"))
	sessions.push(t, turnEndedFrame("end_turn"))

	done := recvEvent(t, events)
	if done.Type != TurnDone || done.Result != "重建后的回复" {
		t.Fatalf("重建受理后应正常完成回合: %+v", done)
	}
	_, _, _, prompts := sessions.stats()
	if len(prompts) != 1 || prompts[0] != "hi" {
		t.Fatalf("就绪后应恰受理一次 Prompt: %v", prompts)
	}
}

func TestAcpDriverTurnSessionFailed(t *testing.T) {
	sessions := newFakeSessions(startingSnap())
	driver := boundDriver(sessions)

	// RunTurn 阻塞在帧驱动等就绪；推 failed 状态帧后同步报错（含落因）。
	type runResult struct {
		events <-chan TurnEvent
		err    error
	}
	result := make(chan runResult, 1)
	go func() {
		events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1"})
		result <- runResult{events, err}
	}()
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameSessionStatus, Status: &acpsession.StatusPayload{Status: acpsession.StatusFailed, Error: "vendored 缺失"}})

	var res runResult
	select {
	case res = <-result:
	case <-time.After(5 * time.Second):
		t.Fatalf("等待 RunTurn 返回超时")
	}
	if res.err == nil || !strings.Contains(res.err.Error(), "启动失败") || !strings.Contains(res.err.Error(), "vendored 缺失") {
		t.Fatalf("failed 终态应同步报错并落因: %v", res.err)
	}
	assertClosed(t, sessions.lastFrames(), "受理失败应退订")
}

func TestAcpDriverTurnSessionTerminatedDuringWait(t *testing.T) {
	sessions := newFakeSessions(startingSnap())
	driver := boundDriver(sessions)

	result := make(chan error, 1)
	go func() {
		_, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1"})
		result <- err
	}()
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameTerminated, Status: &acpsession.StatusPayload{Status: acpsession.StatusTerminated}})

	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "已终结") {
			t.Fatalf("terminated 终态应同步报错: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("等待 RunTurn 返回超时")
	}
}

func TestAcpDriverTurnEnsureFails(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	sessions.ensureErr = errors.New("issue 启动模式未配置为 ACP")
	driver := boundDriver(sessions)

	_, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1"})
	if err == nil || !strings.Contains(err.Error(), "issue 启动模式未配置为 ACP") {
		t.Fatalf("Ensure 失败应同步透传: %v", err)
	}
	// 订阅后置于 Ensure 受理：受理失败连订阅都未创建（无通道可退订），也不得触发取消。
	if _, cancel, subs, _ := sessions.stats(); cancel != 0 || subs != 0 {
		t.Fatalf("受理失败路径不得已建订阅或触发取消: cancel=%d subs=%d", cancel, subs)
	}
}

// TestAcpDriverTurnPromptRejected 排队受理被拒（T2.2：等位中会话终结 / 重建，Manager 侧
// 「排队回合未受理」）同步报错并退订——ErrTurnActive 即时拒绝语义已被排队取代，驱动不再
// 有回合冲突的专属文案映射。
func TestAcpDriverTurnPromptRejected(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	sessions.promptErr = errors.New("ACP 会话已终结，排队回合未受理")
	driver := boundDriver(sessions)

	_, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1"})
	if err == nil || !strings.Contains(err.Error(), "排队回合未受理") {
		t.Fatalf("排队受理失败应同步透传: %v", err)
	}
	assertClosed(t, sessions.lastFrames(), "受理失败应退订")
}

// TestAcpDriverTurnPromptQueuedContextCancel 等位期 ctx 取消（编排器 Stop 级联）：受理
// 同步报错且退订，不进入帧循环收集。
func TestAcpDriverTurnPromptQueuedContextCancel(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	sessions.promptBlockOnCtx = true
	driver := boundDriver(sessions)
	ctx, cancel := context.WithCancel(context.Background())

	result := make(chan error, 1)
	go func() {
		_, err := driver.RunTurn(ctx, TurnRequest{ConversationKey: "single:u1"})
		result <- err
	}()
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("等位期 ctx 取消应同步报错: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("等待 RunTurn 返回超时")
	}
	assertClosed(t, sessions.lastFrames(), "受理失败应退订")
}

// TestAcpDriverTurnQueuedWindowFramesDiscarded 等位窗口帧作废（T2.2 回归锁）：阻塞 bot
// 的桌面回合完整生命周期帧积压在首段订阅缓冲，受理后重开的干净订阅不得让它误终止本
// 回合（修复前：外来 turnEnded 提前 emitTurnDone，bot 交付外来回合文本、自身产出丢失）。
func TestAcpDriverTurnQueuedWindowFramesDiscarded(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	sessions.promptHook = func() {
		// 等位窗口：阻塞回合从 turnStarted 到 turnEnded 的完整帧序落首段订阅。
		sessions.push(t, turnStartedFrame())
		sessions.push(t, agentTextFrame("t9-agentMessage", "桌面回合的回复"))
		sessions.push(t, turnEndedFrame("end_turn"))
	}
	driver := boundDriver(sessions)

	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	// 本回合帧（受理后重开的干净订阅）。
	sessions.push(t, turnStartedFrame())
	sessions.push(t, agentTextFrame("t1-agentMessage", "bot 自己的回复"))
	sessions.push(t, turnEndedFrame("end_turn"))

	done := recvEvent(t, events)
	if done.Type != TurnDone || done.Result != "bot 自己的回复" {
		t.Fatalf("等位窗口帧应整体作废，本回合结果不得被外来终态顶替: %+v", done)
	}
}

// TestAcpDriverTurnArmedFromSnapshotFrame 首帧快照装填 armed：本回合 turnStarted 帧
// 广播早于重开订阅建立，collectTurn 收不到——armed 由快照 TurnActive 装填后正常收集。
func TestAcpDriverTurnArmedFromSnapshotFrame(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	snap := readySnap()
	snap.TurnActive = true
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameSnapshot, Snapshot: &snap})
	sessions.push(t, agentTextFrame("t1-agentMessage", "快照装填后的回复"))
	sessions.push(t, turnEndedFrame("end_turn"))

	done := recvEvent(t, events)
	if done.Type != TurnDone || done.Result != "快照装填后的回复" {
		t.Fatalf("快照 TurnActive 应装填 armed 并正常收集: %+v", done)
	}
}

// TestAcpDriverTurnInstantTurnFromSnapshot 极快收敛（快照 TurnActive=false）：受理的
// 回合在重开订阅建立前已终态，从快照直接合成终态（entries 末条 agentMessage 即全文）。
func TestAcpDriverTurnInstantTurnFromSnapshot(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	snap := readySnap()
	snap.StopReason = "end_turn"
	snap.Entries = []acpsession.ConversationEntry{
		{EntryID: "t1-agentMessage", Kind: "agentMessage", Text: "秒回"},
	}
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameSnapshot, Snapshot: &snap})

	done := recvEvent(t, events)
	if done.Type != TurnDone || done.IsError || done.Result != "秒回" {
		t.Fatalf("快照终态应直接合成回合结果: %+v", done)
	}
	if _, ok := <-events; ok {
		t.Fatalf("合成终态后事件通道应关闭")
	}
}

func TestAcpDriverTurnUnboundConversation(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := NewAcpDriver(sessions) // 生产装配：T2.3 前恒未绑定

	_, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1"})
	if err == nil || !strings.Contains(err.Error(), "绑定") {
		t.Fatalf("未绑定应同步报错给引导文案: %v", err)
	}
	if ensure, _, subs, _ := sessions.stats(); ensure != 0 || subs != 0 {
		t.Fatalf("未绑定不得触达会话域: ensure=%d subs=%d", ensure, subs)
	}
}

func TestAcpDriverTurnTurnErrorFrame(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameTurnEnded, Turn: &acpsession.TurnPayload{Error: "agent 进程崩溃"}})

	ev := recvEvent(t, events)
	if ev.Type != TurnError || !strings.Contains(ev.Err.Error(), "agent 进程崩溃") {
		t.Fatalf("协议层回合失败应转 TurnError: %+v", ev)
	}
}

func TestAcpDriverTurnCancelledStopReason(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, agentTextFrame("t1-agentMessage", "部分输出"))
	sessions.push(t, turnEndedFrame("cancelled"))

	done := recvEvent(t, events)
	if done.Type != TurnDone || !done.IsError || done.Result != "部分输出" {
		t.Fatalf("取消终态应转中断语义 TurnDone 并携带已产出文本: %+v", done)
	}
}

func TestAcpDriverTurnTerminatedMidTurn(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameTerminated, Status: &acpsession.StatusPayload{Status: acpsession.StatusTerminated, Error: "stderr 摘要"}})

	ev := recvEvent(t, events)
	if ev.Type != TurnError || !strings.Contains(ev.Err.Error(), "stderr 摘要") {
		t.Fatalf("回合中会话终结应转 TurnError 并带因: %+v", ev)
	}
}

func TestAcpDriverTurnFramesClosedMidTurn(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.closeFrames()

	ev := recvEvent(t, events)
	if ev.Type != TurnError || !strings.Contains(ev.Err.Error(), "事件流已断开") {
		t.Fatalf("事件流断开应转 TurnError: %+v", ev)
	}
}

func TestAcpDriverTurnContextCancel(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := driver.RunTurn(ctx, TurnRequest{ConversationKey: "single:u1"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	cancel()
	// 软取消恰一次（ctx.Done 重复触发不重复 Cancel）；终态帧随后到达收尾。
	waitFor(t, "软取消受理", func() bool {
		_, cancels, _, _ := sessions.stats()
		return cancels == 1
	})
	sessions.push(t, turnEndedFrame("cancelled"))

	done := recvEvent(t, events)
	if done.Type != TurnDone || !done.IsError {
		t.Fatalf("取消后的终态帧应转中断语义: %+v", done)
	}
	if _, cancels, _, _ := sessions.stats(); cancels != 1 {
		t.Fatalf("软取消应恰一次: %d", cancels)
	}
}

// TestAcpDriverRealManagerSpawnFailure 真实 Manager 链路验收：temp sqlite 种 workspace/issue
// （mode=acp）、空 vendored 目录 → Ensure 受理成功，spawn 链失败落 failed 帧 → 同步报错
// 「启动失败 + 落因」（受理模型、帧流、failed 落因三者的跨域联动各验一遍）。
func TestAcpDriverRealManagerSpawnFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.ProjectIssue{}, &model.IssueAcpSession{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	q := query.Use(db)
	ws := &model.Workspace{Name: "ws", Dir: t.TempDir(), LaunchSettings: `{"mode":"acp"}`}
	if err := q.Workspace.WithContext(context.Background()).Create(ws); err != nil {
		t.Fatalf("建 workspace: %v", err)
	}
	issue := &model.ProjectIssue{
		ID: "issue-bot-acp", ProjectID: 1, WorkspaceID: ws.ID, Name: "测试 issue",
		StateCode: enums.STATE_CODE_BACKLOG, Priority: enums.PRIORITY_NONE, LaunchSettings: "",
	}
	if err := q.ProjectIssue.WithContext(context.Background()).Create(issue); err != nil {
		t.Fatalf("建 issue: %v", err)
	}
	mgr, err := acpsession.NewManager(db, acpsession.Dirs{}, 0, zap.NewNop())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.StopAll)

	driver := acpDriver{sessions: mgr, issue: func(string) (string, bool) { return "issue-bot-acp", true }}
	_, err = driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "启动失败") {
		t.Fatalf("空 vendored 目录下 spawn 链失败应同步报「启动失败」: %v", err)
	}
}
