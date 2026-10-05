package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BrokkAi/acp-go/schema"
	"go.uber.org/zap"

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

	respondErr   error // RespondPermission 注入错误（nil = 应答成功）
	respondCalls []respondCall

	elicitRespondErr   error // RespondElicitation 注入错误（nil = 应答成功）
	elicitRespondCalls []elicitRespondCall

	ensureCalls int
	promptTexts []string
	promptMetas []acpsession.PromptMeta // PromptQueued 元数据留痕（T1.1 来源/展示透传断言）
	cancelCalls int
	subscribes  int
}

// respondCall RespondPermission 调用留痕（审批快路径断言用）。
type respondCall struct {
	pendingID uint64
	optionID  string
	source    string
}

// elicitRespondCall RespondElicitation 调用留痕（表单卡点击断言用，T3.3）。
type elicitRespondCall struct {
	pendingID uint64
	action    string
	content   map[string]any
	source    string
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

func (f *fakeSessions) PromptQueued(ctx context.Context, _ string, text string, meta acpsession.PromptMeta) error {
	f.mu.Lock()
	f.promptTexts = append(f.promptTexts, text)
	f.promptMetas = append(f.promptMetas, meta)
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

// Get 快照直读（审批快路径消费；测试以 snap 字段装填挂起投影）。
func (f *fakeSessions) Get(_ string) acpsession.ViewSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap
}

// RespondPermission 应答留痕 + 注入错误（审批快路径消费）。
func (f *fakeSessions) RespondPermission(_ string, pendingID uint64, optionID, source string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.respondCalls = append(f.respondCalls, respondCall{pendingID: pendingID, optionID: optionID, source: source})
	return f.respondErr
}

// RespondElicitation 应答留痕 + 注入错误（表单卡点击消费，T3.3）。
func (f *fakeSessions) RespondElicitation(_ string, pendingID uint64, action string, content map[string]any, source string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.elicitRespondCalls = append(f.elicitRespondCalls, elicitRespondCall{pendingID: pendingID, action: action, content: content, source: source})
	return f.elicitRespondErr
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

func (f *fakeSessions) responds() []respondCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]respondCall(nil), f.respondCalls...)
}

func (f *fakeSessions) elicitResponds() []elicitRespondCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]elicitRespondCall(nil), f.elicitRespondCalls...)
}

// boundDriver 测试构造：引擎是纯执行者（T2.3 起目标 issue 由路由层经 TurnRequest.IssueID
// 回填，解析缝已删），替身不再持有绑定知识；绑定路径用例的请求已批量携带 issue-acp，
// unbound 分支由 TestAcpDriverTurnUnboundConversation 单测覆盖。
func boundDriver(sessions acpSessions) acpDriver {
	return acpDriver{sessions: sessions}
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

func thoughtFrame(entryID, text string) acpsession.Frame {
	return acpsession.Frame{Type: acpsession.FrameEntry, Entry: &acpsession.ConversationEntry{EntryID: entryID, Kind: "agentThought", Text: text}}
}

func toolCallFrame(toolCallID, name string) acpsession.Frame {
	return acpsession.Frame{Type: acpsession.FrameEntry, Entry: &acpsession.ConversationEntry{
		EntryID: "tool:" + toolCallID, Kind: "toolCall", ToolCall: &acpsession.ToolCallView{ToolCallID: toolCallID, Name: name},
	}}
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

	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
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

	// 差分流式（T3.4）：累计全量帧差分为增量序列（Hel → lo!），终态 Result 为全文。
	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "Hel" {
		t.Fatalf("首个增量事件不符: %+v", ev)
	}
	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "lo!" {
		t.Fatalf("续接增量事件不符: %+v", ev)
	}
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

// TestAcpDriverTurnTextStreaming 差分流式（T3.4）：agentMessage 帧携带本回合累计全量，
// 差分转 TurnText 增量序列；多字节文本不切半个字符（sent 恒为上次完整串的完整前缀，
// len(sent) 落字符边界）。
func TestAcpDriverTurnTextStreaming(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, agentTextFrame("t1-agentMessage", "你好"))
	sessions.push(t, agentTextFrame("t1-agentMessage", "你好，世界🌍"))
	sessions.push(t, agentTextFrame("t1-agentMessage", "你好，世界🌍！"))
	sessions.push(t, turnEndedFrame("end_turn"))

	for _, want := range []string{"你好", "，世界🌍", "！"} {
		if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != want {
			t.Fatalf("增量事件不符：want %q got %+v", want, ev)
		}
	}
	done := recvEvent(t, events)
	if done.Type != TurnDone || done.Result != "你好，世界🌍！" {
		t.Fatalf("终态事件应为回合全文: %+v", done)
	}
}

// TestAcpDriverTurnTextAfterToolUse 工具调用后续流：泵在 TurnToolUse 清屏，差分基线不回退
// ——后续增量只含工具后的新文本（与 headless 同构），终态 Result 仍为回合全文。
func TestAcpDriverTurnTextAfterToolUse(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, agentTextFrame("t1-agentMessage", "先说结论"))
	sessions.push(t, toolCallFrame("tc1", "Bash"))
	sessions.push(t, agentTextFrame("t1-agentMessage", "先说结论，验证通过"))
	sessions.push(t, turnEndedFrame("end_turn"))

	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "先说结论" {
		t.Fatalf("工具前增量事件不符: %+v", ev)
	}
	if ev := recvEvent(t, events); ev.Type != TurnToolUse || ev.ToolName != "Bash" {
		t.Fatalf("工具进度事件不符: %+v", ev)
	}
	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "，验证通过" {
		t.Fatalf("工具后增量应只含新文本: %+v", ev)
	}
	done := recvEvent(t, events)
	if done.Type != TurnDone || done.Result != "先说结论，验证通过" {
		t.Fatalf("终态事件应为回合全文: %+v", done)
	}
}

// TestAcpDriverThoughtStatusLine 思考提示（T3.4）：agentThought 帧出「💭 思考中…」状态行
// （重复帧泵侧 lastSent 去重），正文 TurnText 到来后自然让位；思考内容本体不进 IM。
func TestAcpDriverThoughtStatusLine(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, thoughtFrame("t1-agentThought", "先分析问题"))
	sessions.push(t, thoughtFrame("t1-agentThought", "先分析问题，再给方案"))
	sessions.push(t, agentTextFrame("t1-agentMessage", "方案如下"))
	sessions.push(t, turnEndedFrame("end_turn"))

	for i := 0; i < 2; i++ {
		if ev := recvEvent(t, events); ev.Type != TurnStatus || ev.Status != "💭 思考中…" {
			t.Fatalf("思考状态行不符: %+v", ev)
		}
	}
	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "方案如下" {
		t.Fatalf("思考后的正文增量事件不符: %+v", ev)
	}
	if done := recvEvent(t, events); done.Type != TurnDone || done.Result != "方案如下" {
		t.Fatalf("终态事件不符: %+v", done)
	}
}

// TestAcpDriverSnapshotBaselineArmed 快照基线衔接（T3.4 加固）：armed 快照 entries 已含
// 本回合产出正文（订阅建立前的理论窗口），基线装填后后续帧差分只发增量，终态全文完整。
func TestAcpDriverSnapshotBaselineArmed(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	snap := readySnap()
	snap.TurnActive = true
	snap.Entries = []acpsession.ConversationEntry{
		{EntryID: "t1-user", Kind: "user", Text: "hi"},
		{EntryID: "t1-agentMessage", Kind: "agentMessage", Text: "已产出"},
	}
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameSnapshot, Snapshot: &snap})
	sessions.push(t, agentTextFrame("t1-agentMessage", "已产出+续写"))
	sessions.push(t, turnEndedFrame("end_turn"))

	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "+续写" {
		t.Fatalf("快照基线后增量应只含新增部分: %+v", ev)
	}
	done := recvEvent(t, events)
	if done.Type != TurnDone || done.Result != "已产出+续写" {
		t.Fatalf("终态事件应为回合全文: %+v", done)
	}
}

// TestAcpDriverSnapshotStaleEntriesExcluded 基线 user 锚过滤（审查修复回归）：armed 快照
// 含上回合完整问答 + 本回合 user 锚（受理同步写、其后无产出是常态）——上回合条目不进
// 基线，本回合首帧即全量发出。
func TestAcpDriverSnapshotStaleEntriesExcluded(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	snap := readySnap()
	snap.TurnActive = true
	snap.Entries = []acpsession.ConversationEntry{
		{EntryID: "t0-user", Kind: "user", Text: "上一回合的提问"},
		{EntryID: "t0-agentMessage", Kind: "agentMessage", Text: "上一回合的长篇大论……"},
		{EntryID: "t1-user", Kind: "user", Text: "hi"},
	}
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameSnapshot, Snapshot: &snap})
	sessions.push(t, agentTextFrame("t1-agentMessage", "短"))
	sessions.push(t, agentTextFrame("t1-agentMessage", "短的回复"))
	sessions.push(t, turnEndedFrame("end_turn"))

	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "短" {
		t.Fatalf("上回合条目经 user 锚排除，首帧应全量发出: %+v", ev)
	}
	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "的回复" {
		t.Fatalf("增量应正常续接: %+v", ev)
	}
	done := recvEvent(t, events)
	if done.Type != TurnDone || done.Result != "短的回复" {
		t.Fatalf("终态事件应为本回合全文: %+v", done)
	}
}

// TestAcpDriverZeroTextTurnNotContaminated 零正文终态不串回合（审查修复回归）：本回合
// 零 agentMessage 帧取消收尾时终态 Result 为空——快照里的上回合回复不得被当作本回合
// 已产出文本交付（Result 与水位线均经 user 锚过滤装填）。
func TestAcpDriverZeroTextTurnNotContaminated(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	snap := readySnap()
	snap.TurnActive = true
	snap.Entries = []acpsession.ConversationEntry{
		{EntryID: "t0-user", Kind: "user", Text: "上一回合的提问"},
		{EntryID: "t0-agentMessage", Kind: "agentMessage", Text: "已完成配置修改"},
		{EntryID: "t1-user", Kind: "user", Text: "hi"},
	}
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameSnapshot, Snapshot: &snap})
	sessions.push(t, thoughtFrame("t1-agentThought", "思考中"))
	sessions.push(t, turnEndedFrame("cancelled"))

	if ev := recvEvent(t, events); ev.Type != TurnStatus || ev.Status != "💭 思考中…" {
		t.Fatalf("思考状态行不符: %+v", ev)
	}
	done := recvEvent(t, events)
	if done.Type != TurnDone || !done.IsError || done.Result != "" {
		t.Fatalf("零正文取消终态：Result 应为空（上回合回复不得串入）: %+v", done)
	}
}

// TestAcpDriverWatermarkPrefixMismatchReset 水位线前缀校验（审查修复回归）：已发前缀串与
// 帧文本错配（条目覆写换回合的理论态——更短/等长不同/头部不一致）即重置全量重发，
// 不切中段、不静默丢首块；前缀恢复后差分正常续接。
func TestAcpDriverWatermarkPrefixMismatchReset(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	snap := readySnap()
	snap.TurnActive = true
	snap.Entries = []acpsession.ConversationEntry{
		{EntryID: "t1-user", Kind: "user", Text: "hi"},
		{EntryID: "t1-agentMessage", Kind: "agentMessage", Text: "旧内容"},
	}
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameSnapshot, Snapshot: &snap})
	sessions.push(t, agentTextFrame("t1-agentMessage", "崭新内容"))
	sessions.push(t, agentTextFrame("t1-agentMessage", "崭新内容续"))
	sessions.push(t, turnEndedFrame("end_turn"))

	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "崭新内容" {
		t.Fatalf("前缀错配应全量重发（不切中段不丢首块）: %+v", ev)
	}
	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "续" {
		t.Fatalf("前缀恢复后增量应正常续接: %+v", ev)
	}
	done := recvEvent(t, events)
	if done.Type != TurnDone || done.Result != "崭新内容续" {
		t.Fatalf("终态事件应为本回合全文: %+v", done)
	}
}

func TestAcpDriverTurnArmedWindowIgnoresStale(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
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
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
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
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, agentTextFrame("t1-agentMessage", "重建后的回复"))
	sessions.push(t, turnEndedFrame("end_turn"))

	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "重建后的回复" {
		t.Fatalf("正文增量事件不符: %+v", ev)
	}
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
		events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp"})
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
		_, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp"})
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

	_, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp"})
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

	_, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp"})
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
		_, err := driver.RunTurn(ctx, TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp"})
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

	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	// 本回合帧（受理后重开的干净订阅）。
	sessions.push(t, turnStartedFrame())
	sessions.push(t, agentTextFrame("t1-agentMessage", "bot 自己的回复"))
	sessions.push(t, turnEndedFrame("end_turn"))

	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "bot 自己的回复" {
		t.Fatalf("正文增量事件不符: %+v", ev)
	}
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
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	snap := readySnap()
	snap.TurnActive = true
	sessions.push(t, acpsession.Frame{Type: acpsession.FrameSnapshot, Snapshot: &snap})
	sessions.push(t, agentTextFrame("t1-agentMessage", "快照装填后的回复"))
	sessions.push(t, turnEndedFrame("end_turn"))

	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "快照装填后的回复" {
		t.Fatalf("正文增量事件不符: %+v", ev)
	}
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
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	snap := readySnap()
	snap.StopReason = "end_turn"
	snap.Entries = []acpsession.ConversationEntry{
		{EntryID: "t1-user", Kind: "user", Text: "hi"},
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
	driver := NewAcpDriver(sessions) // 生产装配：路由层未回填（IssueID 空 = 未绑定引导分支）

	_, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: ""})
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
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp"})
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
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, agentTextFrame("t1-agentMessage", "部分输出"))
	sessions.push(t, turnEndedFrame("cancelled"))

	if ev := recvEvent(t, events); ev.Type != TurnText || ev.Delta != "部分输出" {
		t.Fatalf("取消前正文增量事件不符: %+v", ev)
	}
	done := recvEvent(t, events)
	if done.Type != TurnDone || !done.IsError || done.Result != "部分输出" {
		t.Fatalf("取消终态应转中断语义 TurnDone 并携带已产出文本: %+v", done)
	}
}

func TestAcpDriverTurnTerminatedMidTurn(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp"})
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
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp"})
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

	events, err := driver.RunTurn(ctx, TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp"})
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
	db := newBotTestDB(t, &model.Workspace{}, &model.ProjectIssue{}, &model.IssueAcpSession{})
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

	driver := acpDriver{sessions: mgr}
	_, err = driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-bot-acp", Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "启动失败") {
		t.Fatalf("空 vendored 目录下 spawn 链失败应同步报「启动失败」: %v", err)
	}
}

// pendingOpenedFrame / pendingClosedFrame 挂起帧脚本辅助（审批选项复用 pending_gate_test
// 的 permOptions——同包共享，不另造轮子）。
func pendingOpenedFrame(p acpsession.PendingView) acpsession.Frame {
	return acpsession.Frame{Type: acpsession.FramePendingOpened, Pending: &p}
}

func pendingClosedFrame(pendingID uint64) acpsession.Frame {
	return acpsession.Frame{Type: acpsession.FramePendingClosed, PendingID: pendingID}
}

// TestAcpDriverPendingStatusLine 审批挂起的回合内呈现（T2.4）：pendingOpened 出编号状态行
// （文案与数字快路径同源），pendingClosed 切「已处理，继续执行…」，随后正文与终态不受扰。
func TestAcpDriverPendingStatusLine(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	title := "Bash: echo hi"
	sessions.push(t, turnStartedFrame())
	sessions.push(t, pendingOpenedFrame(acpsession.PendingView{
		PendingID: 7, Kind: "permission", Options: permOptions(),
		ToolCall: &schema.ToolCallUpdate{ToolCallID: "tc-7", Title: &title},
	}))
	sessions.push(t, pendingClosedFrame(7))
	sessions.push(t, agentTextFrame("t1-agentMessage", "审批后的回复"))
	sessions.push(t, turnEndedFrame("end_turn"))

	wantStatus := "⏳ Agent 请求审批，回复数字应答：\n1. 允许（Bash: echo hi）\n2. 拒绝（Bash: echo hi）"
	ev := recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Status != wantStatus {
		t.Fatalf("审批状态行不符: %+v", ev)
	}
	ev = recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Status != "✅ 审批已处理，继续执行…" {
		t.Fatalf("审批处理状态行不符: %+v", ev)
	}
	if ev = recvEvent(t, events); ev.Type != TurnText || ev.Delta != "审批后的回复" {
		t.Fatalf("审批后正文增量事件不符: %+v", ev)
	}
	done := recvEvent(t, events)
	if done.Type != TurnDone || done.Result != "审批后的回复" {
		t.Fatalf("终态事件不符: %+v", done)
	}
}

// TestAcpDriverElicitationPendingStatus 表单挂起呈现（不可表示形态）：指回桌面，关闭帧
// 同步状态行。
func TestAcpDriverElicitationPendingStatus(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, pendingOpenedFrame(acpsession.PendingView{PendingID: 9, Kind: "elicitation"}))
	sessions.push(t, pendingClosedFrame(9))
	sessions.push(t, turnEndedFrame("end_turn"))

	ev := recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Status != elicitPendingDesktopText {
		t.Fatalf("表单挂起状态行不符: %+v", ev)
	}
	ev = recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Status != "✅ 表单已处理，继续执行…" {
		t.Fatalf("表单处理状态行不符: %+v", ev)
	}
	if done := recvEvent(t, events); done.Type != TurnDone {
		t.Fatalf("终态事件不符: %+v", done)
	}
}

// elicitPendingFrame 表单挂起帧夹具（携带可分类 schema；view 构造同包共用于
// elicitation_card_test.go 的 elicitPendingView）。
func elicitPendingFrame(pendingID uint64, schema json.RawMessage) acpsession.Frame {
	return pendingOpenedFrame(elicitPendingView(pendingID, schema))
}

// anchoredReadySnap 携会话代锚的就绪快照（出卡条件的 acpSess 依据）。
func anchoredReadySnap() acpsession.ViewSnapshot {
	snap := readySnap()
	snap.AcpSessionID = testAcpSessionID
	return snap
}

// TestAcpDriverElicitationCardEmission 表单卡出卡（T3.3）：可表示 schema 随状态行附卡
// （Submit 语义 + elicit TaskID marker + 选项面），状态行指向卡上提交。
func TestAcpDriverElicitationCardEmission(t *testing.T) {
	sessions := newFakeSessions(anchoredReadySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, elicitPendingFrame(9, schemaOf(radioField("db", "数据库", "mysql", "postgres"))))
	sessions.push(t, turnEndedFrame("end_turn"))

	ev := recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Status != elicitPendingCardText {
		t.Fatalf("表单卡状态行不符: %+v", ev)
	}
	if ev.Card == nil {
		t.Fatalf("可表示表单应随状态行出卡: %+v", ev)
	}
	if !ev.Card.Submit || ev.Card.Multiple || len(ev.Card.Questions) != 0 || len(ev.Card.Options) != 2 {
		t.Fatalf("表单卡 spec 不符: %+v", ev.Card)
	}
	if want := elicitCardTaskID("issue-acp", testAcpSessionID, 9); ev.Card.TaskID != want {
		t.Fatalf("TaskID 不符: %q want %q", ev.Card.TaskID, want)
	}
	if done := recvEvent(t, events); done.Type != TurnDone {
		t.Fatalf("终态事件不符: %+v", done)
	}
}

// TestAcpDriverElicitationCardUnrepresentableWithAnchor 代锚在场但 schema 不可表示：不出卡，
// 状态行指回桌面（分类失败不判错）。
func TestAcpDriverElicitationCardUnrepresentableWithAnchor(t *testing.T) {
	sessions := newFakeSessions(anchoredReadySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, elicitPendingFrame(9, schemaOf(checkboxField("langs", "语言", "go"), otherField("langs_custom", "langs"))))
	sessions.push(t, turnEndedFrame("end_turn"))

	ev := recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Status != elicitPendingDesktopText || ev.Card != nil {
		t.Fatalf("不可表示表单应指回桌面且不出卡: %+v", ev)
	}
	if done := recvEvent(t, events); done.Type != TurnDone {
		t.Fatalf("终态事件不符: %+v", done)
	}
}

// TestAcpDriverElicitationCardAfterPermissionCard 卡位占用：审批卡先出后表单挂起打开
// （可表示）——不再出卡，表单行指回桌面。
func TestAcpDriverElicitationCardAfterPermissionCard(t *testing.T) {
	sessions := newFakeSessions(anchoredReadySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, pendingOpenedFrame(acpsession.PendingView{PendingID: 3, Kind: "permission", Options: permOptions()}))
	second := recvEvent(t, events)
	if second.Type != TurnStatus || second.Card == nil {
		t.Fatalf("审批挂起应先出审批卡: %+v", second)
	}
	sessions.push(t, elicitPendingFrame(9, schemaOf(radioField("db", "数据库", "mysql", "postgres"))))
	sessions.push(t, turnEndedFrame("end_turn"))
	ev := recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Card != nil {
		t.Fatalf("卡位占用后表单不应再出卡: %+v", ev)
	}
	if want := "⏳ Agent 请求审批，回复数字应答：\n1. 允许\n2. 拒绝\n" + elicitPendingDesktopText; ev.Status != want {
		t.Fatalf("混合挂起状态行不符: %q", ev.Status)
	}
	if done := recvEvent(t, events); done.Type != TurnDone {
		t.Fatalf("终态事件不符: %+v", done)
	}
}

// TestAcpDriverElicitationSuccessorAfterAnswered 首表单已答后的接续表单（串行逐题阻塞的
// 正常产物）：卡位已被首卡占用，次表单虽可表示也不出卡，状态行按挂起粒度指回桌面——
// 不得指向已置灰首卡（误导用户点旧卡，decline 探测虽安全但次表单悬空）。
func TestAcpDriverElicitationSuccessorAfterAnswered(t *testing.T) {
	sessions := newFakeSessions(anchoredReadySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, elicitPendingFrame(9, schemaOf(radioField("db", "数据库", "mysql", "postgres"))))
	if ev := recvEvent(t, events); ev.Type != TurnStatus || ev.Status != elicitPendingCardText || ev.Card == nil {
		t.Fatalf("首表单应出卡并指向卡上提交: %+v", ev)
	}
	sessions.push(t, pendingClosedFrame(9))
	if ev := recvEvent(t, events); ev.Type != TurnStatus || ev.Status != "✅ 表单已处理，继续执行…" {
		t.Fatalf("首表单处理状态行不符: %+v", ev)
	}
	sessions.push(t, elicitPendingFrame(10, schemaOf(radioField("env", "环境", "dev", "prod"))))
	sessions.push(t, turnEndedFrame("end_turn"))
	ev := recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Status != elicitPendingDesktopText || ev.Card != nil {
		t.Fatalf("接续表单应指回桌面且不再出卡: %+v", ev)
	}
	if done := recvEvent(t, events); done.Type != TurnDone {
		t.Fatalf("终态事件不符: %+v", done)
	}
}

// TestAcpDriverMixedPendingStatus 混合挂起：审批打开后表单再开，状态行合并展示——审批的
// 数字应答指引不被表单提示覆写。
func TestAcpDriverMixedPendingStatus(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, pendingOpenedFrame(acpsession.PendingView{PendingID: 3, Kind: "permission", Options: permOptions()}))
	sessions.push(t, pendingOpenedFrame(acpsession.PendingView{PendingID: 4, Kind: "elicitation"}))
	sessions.push(t, turnEndedFrame("end_turn"))

	ev := recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Status != "⏳ Agent 请求审批，回复数字应答：\n1. 允许\n2. 拒绝" {
		t.Fatalf("首挂起状态行不符: %+v", ev)
	}
	want := "⏳ Agent 请求审批，回复数字应答：\n1. 允许\n2. 拒绝\n" + elicitPendingDesktopText
	ev = recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Status != want {
		t.Fatalf("混合挂起应合并展示（审批指引不覆写）: %+v", ev)
	}
	if done := recvEvent(t, events); done.Type != TurnDone {
		t.Fatalf("终态事件不符: %+v", done)
	}
}

// TestAcpDriverMultiPendingNumbering 多挂起编号：跨挂起全局顺序（第二挂起选项编号续接），
// 关闭一挂起后重编（与快路径 Get 快照剩余序一致）。
func TestAcpDriverMultiPendingNumbering(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, pendingOpenedFrame(acpsession.PendingView{PendingID: 1, Kind: "permission", Options: permOptions()}))
	sessions.push(t, pendingOpenedFrame(acpsession.PendingView{PendingID: 2, Kind: "permission", Options: permOptions()}))
	sessions.push(t, pendingClosedFrame(1))
	sessions.push(t, turnEndedFrame("end_turn"))

	// 第二挂起打开后：全局四选项（1-4，无工具标题——该挂起无 ToolCall）。
	ev := recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Status != "⏳ Agent 请求审批，回复数字应答：\n1. 允许\n2. 拒绝" {
		t.Fatalf("首挂起状态行不符: %+v", ev)
	}
	ev = recvEvent(t, events)
	want := "⏳ Agent 请求审批，回复数字应答：\n1. 允许\n2. 拒绝\n3. 允许\n4. 拒绝"
	if ev.Type != TurnStatus || ev.Status != want {
		t.Fatalf("双挂起全局编号不符: %+v", ev)
	}
	// 关闭首挂起后状态行只提示「已处理」（重编目录留给下一次 opened；终态随回合收敛）。
	ev = recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Status != "✅ 审批已处理，继续执行…" {
		t.Fatalf("关闭状态行不符: %+v", ev)
	}
	if done := recvEvent(t, events); done.Type != TurnDone {
		t.Fatalf("终态事件不符: %+v", done)
	}
}
