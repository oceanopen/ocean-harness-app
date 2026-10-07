package bot

import (
	"testing"

	"go.uber.org/zap"
)

// fakeCardStream 嵌 fakeReplyStream 的卡片扩展流替身：额外记录 UpdateCard 调用与
// update/终帧 的先后序（拦截步同步执行于 HandleInbound 调用方 goroutine，断言随后同
// goroutine 直读，无跨 goroutine 面；锁语义沿用内嵌替身）。
type fakeCardStream struct {
	fakeReplyStream
	updates []CardSpec
	order   []string // "send" / "update" / "flush"，跨方法写次序断言
}

// Flush 覆写内嵌实现以补记次序（终帧不再无声于 order）。
func (f *fakeCardStream) Flush(content string, final bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.order = append(f.order, "flush")
	f.frames = append(f.frames, fakeFrame{content: content, final: final})
	return nil
}

func (f *fakeCardStream) SendCard(spec CardSpec, content string, final bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.order = append(f.order, "send")
	f.frames = append(f.frames, fakeFrame{content: content, final: final})
	return nil
}

func (f *fakeCardStream) UpdateCard(spec CardSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, spec)
	f.order = append(f.order, "update")
	return nil
}

// stubInteraction 拦截步 handler 替身（判定留痕 + 注入返回）。
type stubInteraction struct {
	calls   int
	text    string
	update  *CardSpec
	handled bool
}

func (h *stubInteraction) TryHandleInteraction(_ BotRuntimeConfig, _ InboundMessage) (string, *CardSpec, bool) {
	h.calls++
	return h.text, h.update, h.handled
}

// interactionMsg 拦截步用例的入站交互消息（Text/Quote/Files 恒空）。
func interactionMsg() InboundMessage {
	return InboundMessage{
		MessageID: "ev-1", ConversationKey: "single:u1", SenderID: "u1",
		Interaction: &Interaction{DeliveryID: "d-1", ActionIndex: 1, TaskID: "d-1", RawKey: "d-1:1"},
	}
}

func interactionCfg() BotRuntimeConfig {
	return BotRuntimeConfig{BotID: 1, WorkspaceID: 1, WorkspaceDir: "/tmp/ocean-test", AccessPolicy: AccessPolicy{Mode: AccessModeOpen}}
}

// grayCardSpec 置灰 update 返回值的标准形态：原卡完整 spec + Disabled=true（企微
// UpdateTemplateCard 整卡替换，仅 TaskID+Disabled 的最小 spec 会被适配器拒绝——见
// CardReplyStream.UpdateCard 契约注释）。
func grayCardSpec() *CardSpec {
	return &CardSpec{
		Title:    "审批请求",
		Options:  []CardOption{{ID: "allow", Text: "允许"}, {ID: "reject", Text: "拒绝"}},
		TaskID:   "d-1",
		Disabled: true,
	}
}

// nil handler（T3.1 生产装配形态）：恰一帧兜底终帧、无占位帧、不出回合，且 MarkSeen
// 生效（兜底处理也算已处理）。
func TestOrchestratorInteractionFallback(t *testing.T) {
	store := &ConversationStore{DB: newIssueTestDB(t)}
	driver := &stubTurnDriver{}
	o := NewOrchestrator(store, driver, nil, nil, zap.NewNop())

	rs := &fakeCardStream{}
	o.HandleInbound(interactionCfg(), interactionMsg(), func() (ReplyStream, error) { return rs, nil })

	frames := rs.snapshot()
	if len(frames) != 1 || !frames[0].final || frames[0].content != interactionFallbackText() {
		t.Fatalf("nil handler 应恰发一帧兜底终帧（无占位帧）: %+v", frames)
	}
	if n := driver.turnCount(); n != 0 {
		t.Fatalf("交互不应出回合，got %d", n)
	}
	seen, err := store.HasSeen(1, "single:u1", "ev-1")
	if err != nil || !seen {
		t.Fatalf("兜底处理应 MarkSeen: seen=%v err=%v", seen, err)
	}
}

// handler 命中含置灰：置灰先于终帧、终帧即 handler 文案、无占位帧、不出回合。
func TestOrchestratorInteractionHandled(t *testing.T) {
	store := &ConversationStore{DB: newIssueTestDB(t)}
	driver := &stubTurnDriver{}
	handler := &stubInteraction{text: "✅ 已应答审批：允许", update: grayCardSpec(), handled: true}
	o := NewOrchestrator(store, driver, nil, handler, zap.NewNop())

	rs := &fakeCardStream{}
	o.HandleInbound(interactionCfg(), interactionMsg(), func() (ReplyStream, error) { return rs, nil })

	if handler.calls != 1 {
		t.Fatalf("handler 应恰判定一次，got %d", handler.calls)
	}
	if len(rs.updates) != 1 || rs.updates[0].TaskID != "d-1" || !rs.updates[0].Disabled || len(rs.updates[0].Options) != 2 {
		t.Fatalf("置灰 spec 不符（须完整形态）: %+v", rs.updates)
	}
	if rs.updates[0].Description != "✅ 已应答审批：允许" {
		t.Fatalf("混合承载应把应答首行写进置灰 desc: %+v", rs.updates[0])
	}
	frames := rs.snapshot()
	if len(frames) != 1 || !frames[0].final || frames[0].content != "✅ 已应答审批：允许" {
		t.Fatalf("命中应恰发一帧 handler 文案终帧: %+v", frames)
	}
	if want := []string{"update", "flush"}; len(rs.order) != 2 || rs.order[0] != want[0] || rs.order[1] != want[1] {
		t.Fatalf("置灰应先于终帧，got %v", rs.order)
	}
	if n := driver.turnCount(); n != 0 {
		t.Fatalf("交互不应出回合，got %d", n)
	}
}

// handler 未命中（过期投递/未知锚）：回兜底终帧，handler 恰判定一次。
func TestOrchestratorInteractionHandlerMiss(t *testing.T) {
	store := &ConversationStore{DB: newIssueTestDB(t)}
	driver := &stubTurnDriver{}
	handler := &stubInteraction{handled: false}
	o := NewOrchestrator(store, driver, nil, handler, zap.NewNop())

	rs := &fakeCardStream{}
	o.HandleInbound(interactionCfg(), interactionMsg(), func() (ReplyStream, error) { return rs, nil })

	if handler.calls != 1 {
		t.Fatalf("handler 应恰判定一次，got %d", handler.calls)
	}
	frames := rs.snapshot()
	if len(frames) != 1 || !frames[0].final || frames[0].content != interactionFallbackText() {
		t.Fatalf("未命中应回兜底终帧: %+v", frames)
	}
	if n := driver.turnCount(); n != 0 {
		t.Fatalf("交互不应出回合，got %d", n)
	}
}

// 重投幂等：同 msgid 二次推送不进 handler 判定（T3.2 起判定含变更性动作），二投回
// 「已处理过」终帧。
func TestOrchestratorInteractionRedelivery(t *testing.T) {
	store := &ConversationStore{DB: newIssueTestDB(t)}
	driver := &stubTurnDriver{}
	handler := &stubInteraction{text: "✅ 已应答审批：允许", handled: true}
	o := NewOrchestrator(store, driver, nil, handler, zap.NewNop())

	rs := &fakeCardStream{}
	open := func() (ReplyStream, error) { return rs, nil }
	msg := interactionMsg()

	o.HandleInbound(interactionCfg(), msg, open)
	o.HandleInbound(interactionCfg(), msg, open) // 渠道重投同 msgid

	if handler.calls != 1 {
		t.Fatalf("重投不得二次判定（变更性动作幂等），got %d", handler.calls)
	}
	frames := rs.snapshot()
	if len(frames) != 2 || !frames[0].final || frames[0].content != "✅ 已应答审批：允许" {
		t.Fatalf("首投应发 handler 终帧: %+v", frames)
	}
	if !frames[1].final || frames[1].content != msgAlreadySeenText {
		t.Fatalf("重投应回已处理终帧: %+v", frames)
	}
	if n := driver.turnCount(); n != 0 {
		t.Fatalf("两投均不应出回合，got %d", n)
	}
}

// 纯文本渠道（流无卡能力）：update 非 nil 时不 panic、静默跳过置灰，终帧照发。
func TestOrchestratorInteractionPlainStream(t *testing.T) {
	store := &ConversationStore{DB: newIssueTestDB(t)}
	driver := &stubTurnDriver{}
	handler := &stubInteraction{text: "✅ 已应答审批：允许", update: grayCardSpec(), handled: true}
	o := NewOrchestrator(store, driver, nil, handler, zap.NewNop())

	rs := &fakeReplyStream{}
	o.HandleInbound(interactionCfg(), interactionMsg(), func() (ReplyStream, error) { return rs, nil })

	frames := rs.snapshot()
	if len(frames) != 1 || !frames[0].final || frames[0].content != "✅ 已应答审批：允许" {
		t.Fatalf("无卡能力的流应跳过置灰照发终帧: %+v", frames)
	}
}

// 白名单拒绝的点击：群聊静默零帧（沿用策略拒绝语义，不进拦截步）。
func TestOrchestratorInteractionGroupDenied(t *testing.T) {
	store := &ConversationStore{DB: newIssueTestDB(t)}
	driver := &stubTurnDriver{}
	o := NewOrchestrator(store, driver, nil, nil, zap.NewNop())

	cfg := interactionCfg()
	cfg.AccessPolicy = AccessPolicy{Mode: AccessModeAllowlist, AllowUsers: []string{"u2"}}
	msg := interactionMsg()
	msg.ChatType = ChatGroup
	msg.ConversationKey = "group:chat1"

	rs := &fakeCardStream{}
	o.HandleInbound(cfg, msg, func() (ReplyStream, error) { return rs, nil })

	if frames := rs.snapshot(); len(frames) != 0 {
		t.Fatalf("群聊策略拒绝应零帧（静默）: %+v", frames)
	}
	if n := driver.turnCount(); n != 0 {
		t.Fatalf("拒绝不应出回合，got %d", n)
	}
}

// cardActionIndex 单题单选卡应答下标解析：直点原样、提交事件取定位键匹配勾选集首项、
// 定位键不符跳过（多题形态防御）、无勾选 false。
func TestCardActionIndex(t *testing.T) {
	if idx, ok := cardActionIndex(&Interaction{ActionIndex: 2}); !ok || idx != 2 {
		t.Fatalf("直点应原样取用: (%d, %v)", idx, ok)
	}
	submit := &Interaction{DeliveryID: "tk", ActionIndex: -1, Selections: []InteractionSelection{
		{QuestionKey: "tk", OptionIndexes: []int{1}},
	}}
	if idx, ok := cardActionIndex(submit); !ok || idx != 1 {
		t.Fatalf("提交事件应取勾选集首项: (%d, %v)", idx, ok)
	}
	other := &Interaction{DeliveryID: "tk", ActionIndex: -1, Selections: []InteractionSelection{
		{QuestionKey: "other", OptionIndexes: []int{3}},
	}}
	if _, ok := cardActionIndex(other); ok {
		t.Fatal("定位键不符不应取值（多题形态防御）")
	}
	if _, ok := cardActionIndex(&Interaction{DeliveryID: "tk", ActionIndex: -1}); ok {
		t.Fatal("无勾选集应 false")
	}
}
