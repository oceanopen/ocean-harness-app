package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"ocean-harness/server/internal/acpsession"
)

// cardClickMsg 点击事件消息夹具：DeliveryID/TaskID 同锚（渠道正常回传形态），0 基选项下标。
func cardClickMsg(taskID string, action int) InboundMessage {
	return InboundMessage{
		MessageID: "ev-" + taskID, ConversationKey: "single:u1", SenderID: "u1",
		Interaction: &Interaction{DeliveryID: taskID, ActionIndex: action, TaskID: taskID, RawKey: fmt.Sprintf("%s:%d", taskID, action)},
	}
}

// testAcpSessionID / interactSnap 点击决策链夹具：就绪快照携带会话代锚（决策链对照依据）。
const testAcpSessionID = "3f2a9c1e-0000-4000-8000-abcdef012345"

func interactSnap(pendings ...acpsession.PendingView) acpsession.ViewSnapshot {
	snap := gateSnap(pendings...)
	snap.AcpSessionID = testAcpSessionID
	return snap
}

// TaskID 编解码：合法往返（会话代锚取前 8 位）+ 非本域形态表（miss 交兜底）。
func TestPermissionCardTaskIDRoundTrip(t *testing.T) {
	for _, c := range []struct {
		issueID      string
		acpSessionID string
		pendingID    uint64
		wantToken    string
	}{
		{"i-1", "3f2a9c1e-0000-4000-8000-abcdef012345", 7, "3f2a9c1e"},
		{"3f2a9c1e-0000-4000-8000-abcdef012345", "short1", 18446744073709551615, "short1"}, // 短于 8 位原样成锚
		{"i-1", "sess-acp-1", 2, "sess-acp"},                                               // 非 UUID 会话 ID 同样可编
	} {
		taskID := permissionCardTaskID(c.issueID, c.acpSessionID, c.pendingID)
		if err := ValidateCardTaskID(taskID); err != nil {
			t.Fatalf("编码产物须过 TaskID 契约校验: %q: %v", taskID, err)
		}
		gotIssue, gotToken, gotPID, ok := parsePermissionCardTaskID(taskID)
		if !ok || gotIssue != c.issueID || gotToken != c.wantToken || gotPID != c.pendingID {
			t.Fatalf("往返不符: %q → (%q, %q, %d, %v)", taskID, gotIssue, gotToken, gotPID, ok)
		}
	}
	for _, bad := range []string{
		"",                   // 空
		"d-1",                // 无本域前缀
		"acp-i-1-perm-7",     // 无会话代锚（'@' 分隔缺失）
		"acp-@s1-perm-7",     // issueID 空
		"acp-i-1@-perm-7",    // 会话代锚空
		"acp-i-1@s1",         // 无 perm 段
		"acp-i-1@s1-perm-",   // pendingID 空
		"acp-i-1@s1-perm-0",  // pendingID 0（挂起 id 自 1 起）
		"acp-i-1@s1-perm-x",  // pendingID 非数字
		"acp-i-1@s1-perm-7-", // 多段取最右：最右段空
	} {
		if issueID, token, pid, ok := parsePermissionCardTaskID(bad); ok {
			t.Fatalf("非本域形态应 miss: %q → (%q, %q, %d)", bad, issueID, token, pid)
		}
	}
	// 多段 LastIndex 语义显式钉死：issueID 含 "-perm-" 子串时取最右段为 pendingID。
	if issueID, token, pid, ok := parsePermissionCardTaskID("acp-i-1@s1-perm-7-perm-9"); !ok || issueID != "i-1" || token != "s1-perm-7" || pid != 9 {
		t.Fatalf("LastIndex 切分不符: (%q, %q, %d, %v)", issueID, token, pid, ok)
	}
}

// permissionCardSpec 字段映射：标题/描述（工具标题与空回落）与选项（optionID + 中文标签 SSOT）；
// TaskID 含会话代锚（前 8 位）。
func TestPermissionCardSpec(t *testing.T) {
	pending := permPendingView(7)
	spec := permissionCardSpec("i-1", testAcpSessionID, pending)
	if spec.Title != "Agent 请求审批" {
		t.Fatalf("标题不符: %q", spec.Title)
	}
	if spec.Description != "Bash: echo hi" || spec.TaskID != "acp-i-1@3f2a9c1e-perm-7" {
		t.Fatalf("描述/TaskID 不符: %+v", spec)
	}
	if spec.Multiple || spec.Disabled {
		t.Fatalf("审批卡为单选未置灰: %+v", spec)
	}
	if len(spec.Options) != 2 {
		t.Fatalf("选项数不符: %+v", spec.Options)
	}
	if spec.Options[0].ID != "allow" || spec.Options[0].Text != "允许" ||
		spec.Options[1].ID != "reject" || spec.Options[1].Text != "拒绝" {
		t.Fatalf("选项映射不符（optionID + 中文标签）: %+v", spec.Options)
	}
	// 无工具标题的挂起：描述回落兜底文案（协议正常不产生，防御面）。
	bare := acpsession.PendingView{PendingID: 8, Kind: "permission", Options: permOptions()}
	if got := permissionCardSpec("i-1", testAcpSessionID, bare).Description; got != "Agent 请求授权执行操作" {
		t.Fatalf("空标题应回落兜底文案: %q", got)
	}
}

// 点击决策矩阵·miss 三情形：非本域锚 / 渠道回传对照锚不一致 / 非选项 key——均交兜底文案。
func TestTryHandleInteractionMiss(t *testing.T) {
	sessions := newFakeSessions(gateSnap(permPendingView(7)))
	route := gateTestRoute(acpResolve, sessions)

	// 非本域锚（T3.1 时代未知卡 / 其他域卡）。
	if _, _, handled := route.TryHandleInteraction(gateCfg(), cardClickMsg("d-1", 0)); handled {
		t.Fatal("非本域锚应 miss")
	}
	// 对照锚不一致（防串卡）。
	mismatch := cardClickMsg(permissionCardTaskID("i-1", testAcpSessionID, 7), 0)
	mismatch.Interaction.TaskID = "other-card"
	if _, _, handled := route.TryHandleInteraction(gateCfg(), mismatch); handled {
		t.Fatal("对照锚不一致应 miss")
	}
	// 非选项 key（提交按钮等）。
	nonOption := cardClickMsg(permissionCardTaskID("i-1", testAcpSessionID, 7), -1)
	if _, _, handled := route.TryHandleInteraction(gateCfg(), nonOption); handled {
		t.Fatal("非选项 key 应 miss")
	}
	if calls := sessions.responds(); len(calls) != 0 {
		t.Fatalf("miss 情形不得触达应答: %+v", calls)
	}
}

// 点击决策矩阵·跨 sidecar 重启边界（风险 §5.4）：快照未就绪（重启重建/agent 死亡）——失效
// 文案告知、不置灰、不出错。
func TestTryHandleInteractionSessionNotReady(t *testing.T) {
	sessions := newFakeSessions(acpsession.ViewSnapshot{Status: acpsession.StatusIdle})
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), cardClickMsg(permissionCardTaskID("i-1", testAcpSessionID, 7), 0))
	if !handled || update != nil || !strings.Contains(text, "已失效") {
		t.Fatalf("未就绪应回失效文案且不置灰: (%q, %+v, %v)", text, update, handled)
	}
}

// 点击决策矩阵·会话重建撞号（代锚核心用例）：pendingID 是 per-AgentClient 发号（重建后从
// 1 重记），新会话同号挂起在场时点击旧代卡——代锚不符即失效收口，不触达应答（不得误答
// 新请求）。
func TestTryHandleInteractionSessionRebuilt(t *testing.T) {
	snap := interactSnap(permPendingView(7)) // 新代会话：同号挂起在场（撞号形态）
	snap.AcpSessionID = "99999999-0000-4000-8000-abcdef012345"
	sessions := newFakeSessions(snap)
	route := gateTestRoute(acpResolve, sessions)

	stale := permissionCardTaskID("i-1", testAcpSessionID, 7) // 旧代锚
	text, update, handled := route.TryHandleInteraction(gateCfg(), cardClickMsg(stale, 0))
	if !handled || update != nil || !strings.Contains(text, "已失效") {
		t.Fatalf("旧代卡应失效收口且不置灰: (%q, %+v, %v)", text, update, handled)
	}
	if calls := sessions.responds(); len(calls) != 0 {
		t.Fatalf("代锚不符不得触达应答（防撞号误答）: %+v", calls)
	}
}

// 点击决策矩阵·命中：按选项下标应答（source=bot）、确认文案与数字快路径同源、置灰为
// 「原卡全量字段 + Disabled=true」。
func TestTryHandleInteractionRespond(t *testing.T) {
	sessions := newFakeSessions(interactSnap(permPendingView(7)))
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), cardClickMsg(permissionCardTaskID("i-1", testAcpSessionID, 7), 1))
	if !handled {
		t.Fatal("命中挂起应消费点击")
	}
	if want := "✅ 已应答审批：拒绝（Bash: echo hi）"; text != want {
		t.Fatalf("确认文案不符（与数字快路径同源）: %q want %q", text, want)
	}
	calls := sessions.responds()
	if len(calls) != 1 || calls[0].pendingID != 7 || calls[0].optionID != "reject" || calls[0].source != acpsession.PendingSourceBot {
		t.Fatalf("应答参数不符: %+v", calls)
	}
	if update == nil || !update.Disabled || update.TaskID != permissionCardTaskID("i-1", testAcpSessionID, 7) || len(update.Options) != 2 ||
		update.Options[0].Text != "允许" || update.Options[1].Text != "拒绝" || update.Title != "Agent 请求审批" {
		t.Fatalf("置灰 spec 须为原卡完整形态 + Disabled: %+v", update)
	}
}

// 点击决策矩阵·后到方（桌面先答的窄竞态：快照在手仍开放）：哨兵判别文案直出 + 置灰。
func TestTryHandleInteractionAlreadyHandled(t *testing.T) {
	sessions := newFakeSessions(interactSnap(permPendingView(7)))
	sessions.respondErr = &acpsession.PendingAlreadyHandledError{Kind: "审批", Source: acpsession.PendingSourcePanel, Label: "允许"}
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), cardClickMsg(permissionCardTaskID("i-1", testAcpSessionID, 7), 0))
	if !handled || text != "该审批已由桌面端应答（允许）" || update == nil || !update.Disabled {
		t.Fatalf("后到方应回判别文案并置灰: (%q, %+v, %v)", text, update, handled)
	}
}

// 点击决策矩阵·其他应答失败：失败文案 + 不置灰（卡片可重点重试）。
func TestTryHandleInteractionRespondError(t *testing.T) {
	sessions := newFakeSessions(interactSnap(permPendingView(7)))
	sessions.respondErr = errors.New("agent 进程无响应")
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), cardClickMsg(permissionCardTaskID("i-1", testAcpSessionID, 7), 0))
	if !handled || update != nil || text != "❌ 审批应答失败：agent 进程无响应" {
		t.Fatalf("应答失败应回失败文案且不置灰: (%q, %+v, %v)", text, update, handled)
	}
}

// 点击决策矩阵·挂起已关闭出快照（桌面先答 / 回合结算后的迟到点击）：经空 optionID 应答取
// closed-history 判别文案，不置灰（已关闭挂起重建不出完整卡）。
func TestTryHandleInteractionClosedPending(t *testing.T) {
	sessions := newFakeSessions(interactSnap()) // 快照无该挂起（已摘除），会话代一致
	sessions.respondErr = &acpsession.PendingAlreadyHandledError{Kind: "审批", Source: acpsession.PendingSourcePanel, Label: "拒绝"}
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), cardClickMsg(permissionCardTaskID("i-1", testAcpSessionID, 7), 0))
	if !handled || update != nil || text != "该审批已由桌面端应答（拒绝）" {
		t.Fatalf("已关闭挂起应出判别文案且不置灰: (%q, %+v, %v)", text, update, handled)
	}
	calls := sessions.responds()
	if len(calls) != 1 || calls[0].pendingID != 7 || calls[0].optionID != "" || calls[0].source != acpsession.PendingSourceBot {
		t.Fatalf("判别走空 optionID 应答（closed-history 查证）: %+v", calls)
	}
}

// 点击决策矩阵·选项下标越界：提示重重点，不触达应答。
func TestTryHandleInteractionActionOutOfRange(t *testing.T) {
	sessions := newFakeSessions(interactSnap(permPendingView(7)))
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), cardClickMsg(permissionCardTaskID("i-1", testAcpSessionID, 7), 5))
	if !handled || update != nil || !strings.Contains(text, "选项无效") {
		t.Fatalf("越界下标应提示且不置灰: (%q, %+v, %v)", text, update, handled)
	}
	if calls := sessions.responds(); len(calls) != 0 {
		t.Fatalf("越界不得触达应答: %+v", calls)
	}
}

// 回合内出卡条件：本回合首个 permission 挂起随状态行附卡（TaskID 编码 issue+pending），
// 后续 permission 与 elicitation 挂起回落纯状态行；状态行文案不变（数字通道仍覆盖全部挂起）。
func TestAcpDriverPermissionCardEmission(t *testing.T) {
	snap := readySnap()
	snap.AcpSessionID = "sess-acp-1"
	sessions := newFakeSessions(snap)
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, pendingOpenedFrame(permPendingView(7)))
	sessions.push(t, pendingOpenedFrame(permPendingView(8)))
	sessions.push(t, pendingOpenedFrame(acpsession.PendingView{PendingID: 9, Kind: "elicitation"}))
	sessions.push(t, turnEndedFrame("end_turn"))

	// 首个 permission 挂起：状态行 + 卡（TaskID 编码就绪快照的会话代锚）。
	ev := recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Card == nil {
		t.Fatalf("首个审批挂起应随状态行附卡: %+v", ev)
	}
	if ev.Card.TaskID != permissionCardTaskID("issue-acp", "sess-acp-1", 7) || ev.Card.Title != "Agent 请求审批" || len(ev.Card.Options) != 2 {
		t.Fatalf("审批卡 spec 不符: %+v", ev.Card)
	}
	// 第二个 permission 挂起：编号续接、无卡（企微一消息一卡）。
	ev = recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Card != nil || !strings.Contains(ev.Status, "3. 允许") {
		t.Fatalf("后续审批挂起应回落纯状态行（编号续接）: %+v", ev)
	}
	// elicitation 挂起追加：合并展示、无卡。
	ev = recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Card != nil || !strings.Contains(ev.Status, elicitationPendingText) {
		t.Fatalf("混合挂起应合并展示且无卡: %+v", ev)
	}
	if done := recvEvent(t, events); done.Type != TurnDone {
		t.Fatalf("终态事件不符: %+v", done)
	}
}

// 回合内出卡条件·会话代锚缺失：就绪快照无 AcpSessionID（防御面）不出卡、回落纯状态行，
// 数字应答通道不受影响。
func TestAcpDriverNoCardWithoutSessionAnchor(t *testing.T) {
	sessions := newFakeSessions(readySnap())
	driver := boundDriver(sessions)
	events, err := driver.RunTurn(context.Background(), TurnRequest{ConversationKey: "single:u1", IssueID: "issue-acp", Prompt: "hi"})
	if err != nil {
		t.Fatalf("受理失败: %v", err)
	}
	sessions.push(t, turnStartedFrame())
	sessions.push(t, pendingOpenedFrame(permPendingView(7)))
	sessions.push(t, turnEndedFrame("end_turn"))

	ev := recvEvent(t, events)
	if ev.Type != TurnStatus || ev.Card != nil || !strings.Contains(ev.Status, "1. 允许") {
		t.Fatalf("代锚缺失应回落纯状态行（不出卡）: %+v", ev)
	}
	if done := recvEvent(t, events); done.Type != TurnDone {
		t.Fatalf("终态事件不符: %+v", done)
	}
}

// failSendCardStream 卡发送失败的流替身（SendCard 恒败，记录后回落文本帧路径）。
type failSendCardStream struct {
	fakeCardStream
}

func (f *failSendCardStream) SendCard(spec CardSpec, content string, final bool) error {
	f.mu.Lock()
	f.order = append(f.order, "send-fail")
	f.mu.Unlock()
	return errors.New("卡通道故障")
}

// pumpAsync 后台起泵（测试侧控制事件流关停时机——先让 ticker 发帧再终态）。
func pumpAsync(rs ReplyStream, events chan TurnEvent) <-chan TurnOutcome {
	done := make(chan TurnOutcome, 1)
	go func() { done <- PumpReply(context.Background(), rs, events, nil, zap.NewNop()) }()
	return done
}

// 泵发卡·有卡流：卡随中间帧同帧下发（200ms tick 后、终帧前），成功后同内容不重复发文本帧。
func TestPumpReplySendsCard(t *testing.T) {
	rs := &fakeCardStream{}
	spec := permissionCardSpec("i-1", testAcpSessionID, permPendingView(7))
	status := "⏳ Agent 请求审批，回复数字应答：\n1. 允许（Bash: echo hi）\n2. 拒绝（Bash: echo hi）"

	events := make(chan TurnEvent, 1)
	events <- TurnEvent{Type: TurnStatus, Status: status, Card: &spec}
	done := pumpAsync(rs, events)
	time.Sleep(2*streamTickInterval + 50*time.Millisecond) // 等 ticker 至少一个周期发卡
	close(events)
	<-done

	frames := rs.snapshot()
	if len(frames) != 2 || frames[0].content != status || frames[0].final || !frames[1].final {
		t.Fatalf("应恰两帧（卡帧 + 终帧，无重复文本帧）: %+v", frames)
	}
	if want := []string{"send", "flush"}; len(rs.order) != 2 || rs.order[0] != want[0] || rs.order[1] != want[1] {
		t.Fatalf("写序不符（卡先于终帧）: %v", rs.order)
	}
	if len(rs.updates) != 0 {
		t.Fatalf("泵不发置灰帧（置灰只在点击链路）: %+v", rs.updates)
	}
}

// 泵发卡·发送失败回落：SendCard 报错后回落纯文本中间帧，状态行不丢。
func TestPumpReplyCardFailureFallsBackToText(t *testing.T) {
	rs := &failSendCardStream{}
	spec := permissionCardSpec("i-1", testAcpSessionID, permPendingView(7))

	events := make(chan TurnEvent, 1)
	events <- TurnEvent{Type: TurnStatus, Status: "⏳ Agent 请求审批…", Card: &spec}
	done := pumpAsync(rs, events)
	time.Sleep(2*streamTickInterval + 50*time.Millisecond)
	close(events)
	<-done

	frames := rs.snapshot()
	if len(frames) != 2 || frames[0].content != "⏳ Agent 请求审批…" || frames[0].final || !frames[1].final {
		t.Fatalf("失败应回落文本帧（中间帧 + 终帧）: %+v", frames)
	}
	// 写序：tick1 发卡失败（send-fail）→ 回落文本中间帧（flush）→ 回合收尾终帧（flush）。
	want := []string{"send-fail", "flush", "flush"}
	if len(rs.order) != len(want) {
		t.Fatalf("写序不符: %v want %v", rs.order, want)
	}
	for i := range want {
		if rs.order[i] != want[i] {
			t.Fatalf("写序不符: %v want %v", rs.order, want)
		}
	}
}

// 泵发卡·无卡流（纯文本渠道）：卡事件自动降级为纯文本帧，不 panic。
func TestPumpReplyCardOnPlainStream(t *testing.T) {
	rs := &fakeReplyStream{}
	spec := permissionCardSpec("i-1", testAcpSessionID, permPendingView(7))

	events := make(chan TurnEvent, 1)
	events <- TurnEvent{Type: TurnStatus, Status: "⏳ Agent 请求审批…", Card: &spec}
	done := pumpAsync(rs, events)
	time.Sleep(2*streamTickInterval + 50*time.Millisecond)
	close(events)
	<-done

	frames := rs.snapshot()
	if len(frames) != 2 || frames[0].content != "⏳ Agent 请求审批…" || !frames[1].final {
		t.Fatalf("无卡流应按纯文本两帧收口: %+v", frames)
	}
}

// 泵发卡·收尾丢弃：回合在首个 tick 前终态（挂起随回合结算），未发出的卡直接丢弃——只余
// 终帧，不出迟到的已结算卡。
func TestPumpReplyDropsCardAtTurnEnd(t *testing.T) {
	rs := &fakeCardStream{}
	spec := permissionCardSpec("i-1", testAcpSessionID, permPendingView(7))

	events := make(chan TurnEvent, 2)
	events <- TurnEvent{Type: TurnStatus, Status: "⏳ Agent 请求审批…", Card: &spec}
	events <- TurnEvent{Type: TurnDone, Result: "已自动结算"} // 立即终态，不等 tick
	close(events)

	out := PumpReply(context.Background(), rs, events, nil, zap.NewNop())

	frames := rs.snapshot()
	if len(frames) != 1 || !frames[0].final || frames[0].content != "已自动结算" {
		t.Fatalf("收尾应丢弃未发卡、只余终帧: %+v", frames)
	}
	if out.Err != nil || out.FinalText != "已自动结算" {
		t.Fatalf("终态语义不受卡丢弃影响: %+v", out)
	}
}
