package bot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/BrokkAi/acp-go/schema"
	"go.uber.org/zap"

	"ocean-harness/server/internal/acpsession"
)

// gateTestRoute 快路径单测构造：resolve 与 sessions 均为注入替身（headless/acp 引擎
// 不参与快路径，置 nil 安全）。
func gateTestRoute(resolve func(botID, workspaceID int, conversationKey string) (routeTarget, error), sessions acpSessions) *driverRoute {
	return &driverRoute{resolve: resolve, sessions: sessions}
}

func acpResolve(botID, workspaceID int, conversationKey string) (routeTarget, error) {
	return routeTarget{IssueID: "i-1", ACP: true}, nil
}

func headlessResolve(botID, workspaceID int, conversationKey string) (routeTarget, error) {
	return routeTarget{}, nil
}

// permOptions 与 fakeagent permissionOptions 同款（allow/reject，kind 完整覆盖两档）。
func permOptions() []schema.PermissionOption {
	return []schema.PermissionOption{
		{OptionID: "allow", Kind: schema.PermissionOptionKindAllowOnce, Name: "Allow"},
		{OptionID: "reject", Kind: schema.PermissionOptionKindRejectOnce, Name: "Reject"},
	}
}

func permPendingView(id uint64) acpsession.PendingView {
	title := "Bash: echo hi"
	return acpsession.PendingView{
		PendingID: id,
		Kind:      "permission",
		Options:   permOptions(),
		ToolCall:  &schema.ToolCallUpdate{ToolCallID: "tc-1", Title: &title},
	}
}

func gateMsg(text string) InboundMessage {
	return InboundMessage{ConversationKey: "single:u1", SenderID: "u1", Text: text}
}

func gateCfg() BotRuntimeConfig {
	return BotRuntimeConfig{BotID: 1, WorkspaceID: 1, WorkspaceDir: "/tmp/ocean-test"}
}

// gateSnap 快路径消费的会话快照夹具（就绪态 + 给定挂起——挂起仅在就绪会话上可应答）。
func gateSnap(pendings ...acpsession.PendingView) acpsession.ViewSnapshot {
	return acpsession.ViewSnapshot{Status: acpsession.StatusReady, Pendings: pendings}
}

// 数字命中：按全局顺序编号应答（双挂起四选项，回复 3 = 第二挂起的 allow），source=bot，
// 确认文案带中文标签与工具标题。
func TestPendingGateDigitRespond(t *testing.T) {
	sessions := newFakeSessions(gateSnap(permPendingView(11), permPendingView(12)))
	route := gateTestRoute(acpResolve, sessions)

	text, handled := route.TryRespondPending(gateCfg(), gateMsg("3"))
	if !handled {
		t.Fatal("数字应答应命中快路径")
	}
	want := "✅ 已应答审批：允许（Bash: echo hi）"
	if text != want {
		t.Fatalf("确认文案不符，got %q want %q", text, want)
	}
	calls := sessions.responds()
	if len(calls) != 1 {
		t.Fatalf("应恰一次应答调用，got %+v", calls)
	}
	if calls[0].pendingID != 12 || calls[0].optionID != "allow" || calls[0].source != acpsession.PendingSourceBot {
		t.Fatalf("应答参数不符（pendingID/optionID/source），got %+v", calls[0])
	}
}

// 后到方：acpsession 收敛哨兵的文案直接作为终帧（「该审批已由桌面端应答（…）」）。
func TestPendingGateAlreadyHandled(t *testing.T) {
	sessions := newFakeSessions(gateSnap(permPendingView(11)))
	sessions.respondErr = &acpsession.PendingAlreadyHandledError{Kind: "审批", Source: acpsession.PendingSourcePanel, Label: "拒绝"}
	route := gateTestRoute(acpResolve, sessions)

	text, handled := route.TryRespondPending(gateCfg(), gateMsg(" 1 "))
	if !handled {
		t.Fatal("哨兵错误也应命中快路径（终帧收口）")
	}
	if text != "该审批已由桌面端应答（拒绝）" {
		t.Fatalf("后到方文案不符，got %q", text)
	}
	if len(sessions.responds()) != 1 {
		t.Fatal("应恰一次应答调用")
	}
}

// 超范围序号：回用法提示（带编号选项列表），不发应答。
func TestPendingGateOutOfRange(t *testing.T) {
	sessions := newFakeSessions(gateSnap(permPendingView(11)))
	route := gateTestRoute(acpResolve, sessions)

	text, handled := route.TryRespondPending(gateCfg(), gateMsg("9"))
	if !handled {
		t.Fatal("超范围数字应命中快路径（用法提示）")
	}
	if !strings.Contains(text, "序号超出范围") || !strings.Contains(text, "1. 允许（Bash: echo hi）") {
		t.Fatalf("用法文案应带编号列表，got %q", text)
	}
	if len(sessions.responds()) != 0 {
		t.Fatal("超范围不应发应答")
	}
}

// 只有表单挂起：IM 指回桌面（表单应答面随 T3.3），不发应答。
func TestPendingGateOnlyElicitation(t *testing.T) {
	sessions := newFakeSessions(gateSnap(acpsession.PendingView{PendingID: 21, Kind: "elicitation"}))
	route := gateTestRoute(acpResolve, sessions)

	text, handled := route.TryRespondPending(gateCfg(), gateMsg("1"))
	if !handled {
		t.Fatal("只有表单挂起时应命中快路径（桌面指引）")
	}
	if text != elicitationPendingText {
		t.Fatalf("表单挂起应回桌面指引，got %q", text)
	}
	if len(sessions.responds()) != 0 {
		t.Fatal("表单挂起不应发权限应答")
	}
}

// 未命中回落主路径的五种情形：非数字 / 非 ACP 会话 / 路由读库失败 / 无任何挂起 /
// 会话未就绪（视图残留挂起不可应答，交主路径重建）。
func TestPendingGateMisses(t *testing.T) {
	sessions := newFakeSessions(gateSnap(permPendingView(11)))
	route := gateTestRoute(acpResolve, sessions)

	for _, text := range []string{"允许一下", "1a", "#issue", ""} {
		if _, handled := route.TryRespondPending(gateCfg(), gateMsg(text)); handled {
			t.Fatalf("非纯数字 %q 不应命中", text)
		}
	}

	headlessRoute := gateTestRoute(headlessResolve, sessions)
	if _, handled := headlessRoute.TryRespondPending(gateCfg(), gateMsg("1")); handled {
		t.Fatal("非 ACP 会话不应命中")
	}

	errRoute := gateTestRoute(func(botID, workspaceID int, conversationKey string) (routeTarget, error) {
		return routeTarget{}, errors.New("读库失败")
	}, sessions)
	if _, handled := errRoute.TryRespondPending(gateCfg(), gateMsg("1")); handled {
		t.Fatal("路由读库失败应回落主路径（fail closed 交回合报错）")
	}

	empty := gateTestRoute(acpResolve, newFakeSessions(gateSnap()))
	if _, handled := empty.TryRespondPending(gateCfg(), gateMsg("1")); handled {
		t.Fatal("无任何挂起时数字是普通正文，不应命中")
	}

	deadSnap := acpsession.ViewSnapshot{Status: acpsession.StatusTerminated, Pendings: []acpsession.PendingView{permPendingView(11)}}
	dead := gateTestRoute(acpResolve, newFakeSessions(deadSnap))
	if _, handled := dead.TryRespondPending(gateCfg(), gateMsg("1")); handled {
		t.Fatal("会话未就绪/已终结时视图残留挂起不可应答，应回落主路径")
	}
	if len(sessions.responds()) != 0 {
		t.Fatal("全场景不应发应答")
	}
}

// parseDigitReply 纯数字判定域。
func TestParseDigitReply(t *testing.T) {
	cases := []struct {
		in    string
		n     int
		valid bool
	}{
		{"1", 1, true},
		{" 12 ", 12, true},
		{"0", 0, true},
		{"01", 1, true},
		{"1a", 0, false},
		{"a1", 0, false},
		{"１", 0, false}, // 全角不识别
		{"", 0, false},
		{" ", 0, false},
		{"-1", 0, false},
		{"99999999999999999999", 0, true}, // 溢出按超范围处理
	}
	for _, c := range cases {
		n, ok := parseDigitReply(c.in)
		if ok != c.valid || n != c.n {
			t.Fatalf("parseDigitReply(%q) = (%d,%v), want (%d,%v)", c.in, n, ok, c.n, c.valid)
		}
	}
}

// stubTurnDriver 编排器接线测试的引擎替身：留痕 + 立即完成。
type stubTurnDriver struct {
	mu    sync.Mutex
	turns []string
}

func (d *stubTurnDriver) RunTurn(_ context.Context, _ TurnRequest) (<-chan TurnEvent, error) {
	d.mu.Lock()
	d.turns = append(d.turns, "turn")
	d.mu.Unlock()
	ch := make(chan TurnEvent, 1)
	ch <- TurnEvent{Type: TurnDone, Result: "ok"}
	close(ch) // 泵以通道关闭为回合事件流终界
	return ch, nil
}

func (d *stubTurnDriver) turnCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.turns)
}

// stubGate 快路径替身。
type stubGate struct {
	text    string
	handled bool
	calls   int
}

func (g *stubGate) TryRespondPending(_ BotRuntimeConfig, _ InboundMessage) (string, bool) {
	g.calls++
	return g.text, g.handled
}

// 编排器接线——命中：终帧即快路径文案，无占位帧、不出回合（引擎零调用）。
func TestOrchestratorPendingGateHit(t *testing.T) {
	store := &ConversationStore{DB: newIssueTestDB(t)}
	driver := &stubTurnDriver{}
	gate := &stubGate{text: "✅ 已应答审批：允许", handled: true}
	o := NewOrchestrator(store, driver, gate, zap.NewNop())

	rs := &fakeReplyStream{}
	cfg := BotRuntimeConfig{BotID: 1, WorkspaceID: 1, WorkspaceDir: "/tmp/ocean-test", AccessPolicy: AccessPolicy{Mode: AccessModeOpen}}
	o.HandleInbound(cfg, gateMsg("1"), func() (ReplyStream, error) { return rs, nil })

	if gate.calls != 1 {
		t.Fatalf("快路径应恰判定一次，got %d", gate.calls)
	}
	frames := rs.snapshot()
	if len(frames) != 1 || !frames[0].final || frames[0].content != "✅ 已应答审批：允许" {
		t.Fatalf("命中应恰发一帧终态快路径文案（无占位帧）: %+v", frames)
	}
	waitFor(t, "引擎零调用", func() bool { return driver.turnCount() == 0 })
	if n := driver.turnCount(); n != 0 {
		t.Fatalf("命中不应出回合，got %d", n)
	}
}

// 编排器接线——重投幂等（快路径产生变更性动作却不入队，幂等前置到判定之前）：同 msgid
// 的数字消息二次推送不再进快路径判定，回「已处理过」终帧。
func TestOrchestratorPendingGateRedelivery(t *testing.T) {
	store := &ConversationStore{DB: newIssueTestDB(t)}
	driver := &stubTurnDriver{}
	gate := &stubGate{text: "✅ 已应答审批：允许", handled: true}
	o := NewOrchestrator(store, driver, gate, zap.NewNop())

	rs := &fakeReplyStream{}
	cfg := BotRuntimeConfig{BotID: 1, WorkspaceID: 1, WorkspaceDir: "/tmp/ocean-test", AccessPolicy: AccessPolicy{Mode: AccessModeOpen}}
	msg := InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "1"}
	open := func() (ReplyStream, error) { return rs, nil }

	o.HandleInbound(cfg, msg, open)
	if gate.calls != 1 {
		t.Fatalf("首投应恰判定一次，got %d", gate.calls)
	}
	o.HandleInbound(cfg, msg, open) // 渠道重投同 msgid
	if gate.calls != 1 {
		t.Fatalf("重投不得二次判定（变更性动作幂等），got %d", gate.calls)
	}
	frames := rs.snapshot()
	if len(frames) != 2 || !frames[0].final || frames[0].content != "✅ 已应答审批：允许" {
		t.Fatalf("首投应发快路径终帧: %+v", frames)
	}
	if !frames[1].final || frames[1].content != "该消息已处理过，重复推送已忽略。" {
		t.Fatalf("重投应回已处理终帧: %+v", frames)
	}
	if n := driver.turnCount(); n != 0 {
		t.Fatalf("两投均不应出回合，got %d", n)
	}
}

// 编排器接线——未命中：占位帧照发、回合照常出（原路径无回归）。
func TestOrchestratorPendingGateMiss(t *testing.T) {
	store := &ConversationStore{DB: newIssueTestDB(t)}
	driver := &stubTurnDriver{}
	gate := &stubGate{text: "", handled: false}
	o := NewOrchestrator(store, driver, gate, zap.NewNop())

	rs := &fakeReplyStream{}
	cfg := BotRuntimeConfig{BotID: 1, WorkspaceID: 1, WorkspaceDir: "/tmp/ocean-test", AccessPolicy: AccessPolicy{Mode: AccessModeOpen}}
	o.HandleInbound(cfg, gateMsg("1"), func() (ReplyStream, error) { return rs, nil })

	if gate.calls != 1 {
		t.Fatalf("快路径应恰判定一次，got %d", gate.calls)
	}
	waitFor(t, "回合完成（终帧到达）", func() bool {
		for _, f := range rs.snapshot() {
			if f.final {
				return true
			}
		}
		return false
	})
	frames := rs.snapshot()
	if len(frames) < 2 || frames[0].content != "正在思考…" {
		t.Fatalf("未命中应照发占位帧并出回合: %+v", frames)
	}
	waitFor(t, "引擎恰一回合", func() bool { return driver.turnCount() == 1 })
}

// 编排器接线——nil gate：既有装配（无 ACP 会话域）零影响，数字消息照常出回合。
func TestOrchestratorPendingGateNil(t *testing.T) {
	store := &ConversationStore{DB: newIssueTestDB(t)}
	driver := &stubTurnDriver{}
	o := NewOrchestrator(store, driver, nil, zap.NewNop())

	rs := &fakeReplyStream{}
	cfg := BotRuntimeConfig{BotID: 1, WorkspaceID: 1, WorkspaceDir: "/tmp/ocean-test", AccessPolicy: AccessPolicy{Mode: AccessModeOpen}}
	o.HandleInbound(cfg, gateMsg("1"), func() (ReplyStream, error) { return rs, nil })

	waitFor(t, "回合完成", func() bool { return driver.turnCount() == 1 })
}
