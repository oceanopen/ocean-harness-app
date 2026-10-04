package wecom

import (
	"strings"
	"testing"

	aibottypes "github.com/oceanopen/wecom-aibot-go-sdk/aibot/types"

	"ocean-harness/server/internal/bot"
	"ocean-harness/server/internal/dal/enums"
)

// rep 重复 n 次拼接超限文案（避免字面量里数字符）。
func rep(s string, n int) string { return strings.Repeat(s, n) }

// buildTemplateCard 域：单/多选形态、截断、key 编码、Disabled、参数校验拒绝。
func TestBuildTemplateCard(t *testing.T) {
	spec := bot.CardSpec{
		Title:       "审批请求",
		Description: "允许 claude 执行 Bash 命令",
		Options:     []bot.CardOption{{ID: "allow", Text: "允许"}, {ID: "reject", Text: "拒绝"}},
		TaskID:      "task_2026@01",
	}

	// 单选：mode 0、无提交按钮、选项 key 逐项 `TaskID:<i>`。
	card, err := buildTemplateCard(spec)
	if err != nil {
		t.Fatalf("合法 spec 构建失败: %v", err)
	}
	if card.CardType != aibottypes.TemplateCardType.VoteInteraction {
		t.Fatalf("卡型应为 vote_interaction: %q", card.CardType)
	}
	if card.TaskId != "task_2026@01" {
		t.Fatalf("TaskId 应透传: %q", card.TaskId)
	}
	if card.MainTitle == nil || card.MainTitle.Title != "审批请求" || card.MainTitle.Desc != "允许 claude 执行 Bash 命令" {
		t.Fatalf("主标题不符: %+v", card.MainTitle)
	}
	if card.Checkbox == nil {
		t.Fatal("checkbox 缺失")
	}
	if card.Checkbox.Mode != 0 || card.Checkbox.Disable {
		t.Fatalf("单选应为 mode=0 未置灰: %+v", card.Checkbox)
	}
	if len(card.Checkbox.OptionList) != 2 {
		t.Fatalf("应两个选项: %+v", card.Checkbox.OptionList)
	}
	if card.Checkbox.OptionList[0].Id != "task_2026@01:0" || card.Checkbox.OptionList[0].Text != "允许" {
		t.Fatalf("选项 0 编码不符: %+v", card.Checkbox.OptionList[0])
	}
	if card.Checkbox.OptionList[1].Id != "task_2026@01:1" || card.Checkbox.OptionList[1].Text != "拒绝" {
		t.Fatalf("选项 1 编码不符: %+v", card.Checkbox.OptionList[1])
	}
	if card.SubmitButton != nil {
		t.Fatalf("单选不应有提交按钮: %+v", card.SubmitButton)
	}

	// 多选：mode 1 + 提交按钮（文案「提交」、key `TaskID:submit`）。
	multi := spec
	multi.Multiple = true
	card, err = buildTemplateCard(multi)
	if err != nil {
		t.Fatalf("多选构建失败: %v", err)
	}
	if card.Checkbox.Mode != 1 {
		t.Fatalf("多选应为 mode=1: %d", card.Checkbox.Mode)
	}
	if card.SubmitButton == nil || card.SubmitButton.Text != "提交" || card.SubmitButton.Key != "task_2026@01:submit" {
		t.Fatalf("提交按钮不符: %+v", card.SubmitButton)
	}

	// 置灰：checkbox.disable=true（更新帧统一走本 builder，task_id/keys 与原卡一致）。
	gray := multi
	gray.Disabled = true
	card, err = buildTemplateCard(gray)
	if err != nil {
		t.Fatalf("置灰构建失败: %v", err)
	}
	if !card.Checkbox.Disable || card.TaskId != "task_2026@01" {
		t.Fatalf("置灰帧应 disable=true 且 TaskId 不变: %+v", card.Checkbox)
	}

	// 截断：标题 27→26、描述 31→30、选项 12→11（rune 级）。
	trunc := spec
	trunc.Title = rep("标", 27)
	trunc.Description = rep("描", 31)
	trunc.Options = []bot.CardOption{{ID: "a", Text: rep("选", 12)}}
	card, err = buildTemplateCard(trunc)
	if err != nil {
		t.Fatalf("截断构建失败: %v", err)
	}
	if got := len([]rune(card.MainTitle.Title)); got != 26 {
		t.Fatalf("标题应截到 26: %d", got)
	}
	if got := len([]rune(card.MainTitle.Desc)); got != 30 {
		t.Fatalf("描述应截到 30: %d", got)
	}
	if got := len([]rune(card.Checkbox.OptionList[0].Text)); got != 11 {
		t.Fatalf("选项应截到 11: %d", got)
	}

	// 校验拒绝：空 TaskID / 超长 / 非法字符（含冒号）/ 空 Options。
	bad := []bot.CardSpec{
		{Title: "t", Options: spec.Options, TaskID: ""},
		{Title: "t", Options: spec.Options, TaskID: "x" + rep("a", 128)},
		{Title: "t", Options: spec.Options, TaskID: "bad:id"},
		{Title: "t", Options: nil, TaskID: "ok_task"},
	}
	for i, s := range bad {
		if _, err := buildTemplateCard(s); err == nil {
			t.Fatalf("非法 spec[%d] 应报错: %+v", i, s)
		}
	}
}

// parseEventKey 域：last-colon 切分、数字后缀、非数字后缀、无冒号、多冒号。
func TestParseEventKey(t *testing.T) {
	cases := []struct {
		eventKey    string
		deliveryID  string
		actionIndex int
	}{
		{"dlv:0", "dlv", 0},
		{"dlv:12", "dlv", 12},
		{"dlv:submit", "dlv", -1},
		{"a:b:3", "a:b", 3},   // last-colon：多冒号取最右
		{"nocolon", "", -1},   // 无冒号：DeliveryID 空
		{"", "", -1},          // 空 key
		{"dlv:-1", "dlv", -1}, // 负数非合法下标，保持 -1
	}
	for _, c := range cases {
		in := parseEventKey(c.eventKey, "tk")
		if in.DeliveryID != c.deliveryID || in.ActionIndex != c.actionIndex {
			t.Fatalf("parseEventKey(%q) = (%q,%d), want (%q,%d)", c.eventKey, in.DeliveryID, in.ActionIndex, c.deliveryID, c.actionIndex)
		}
		if in.TaskID != "tk" || in.RawKey != c.eventKey {
			t.Fatalf("TaskID/RawKey 应原样保全: %+v", in)
		}
	}
}

// key 往返：build 产出的每个选项/提交 key 经 parseEventKey 反解回 (TaskID, 下标/-1)。
func TestCardKeyRoundTrip(t *testing.T) {
	const taskID = "rt_task@1"
	spec := bot.CardSpec{
		Title:    "往返",
		Options:  []bot.CardOption{{ID: "a", Text: "一"}, {ID: "b", Text: "二"}, {ID: "c", Text: "三"}},
		TaskID:   taskID,
		Multiple: true,
	}
	card, err := buildTemplateCard(spec)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	for i, opt := range card.Checkbox.OptionList {
		in := parseEventKey(opt.Id, taskID)
		if in.DeliveryID != taskID || in.ActionIndex != i {
			t.Fatalf("选项 key %q 反解不符: %+v", opt.Id, in)
		}
	}
	in := parseEventKey(card.SubmitButton.Key, taskID)
	if in.DeliveryID != taskID || in.ActionIndex != -1 {
		t.Fatalf("提交 key %q 反解不符: %+v", card.SubmitButton.Key, in)
	}
}

// cardEventPayload + submitMsg 归一链：单聊/群聊会话键、Interaction 注入、路由与 headers
// 透传、Text 恒空；非模板卡事件 false。
func TestCardEventNormalize(t *testing.T) {
	headers := aibottypes.WsFrameHeaders{ReqId: "req-1"}
	mkBody := func(chatType, chatID string) aibottypes.EventMessage {
		return aibottypes.EventMessage{
			MsgId: "ev-9", AibotId: "bot-1", ChatId: chatID, ChatType: chatType,
			From:    aibottypes.EventFrom{UserId: "u1", CorpId: "corp"},
			MsgType: "event",
			Event:   aibottypes.TemplateCardEventData{EventType: "template_card_event", EventKey: "dlv:1", TaskId: "dlv"},
		}
	}

	// 单聊：会话键 single:u1。
	rt := &channelRuntime{inbound: make(chan func(*bot.InboundMessage), 1), stopProc: make(chan struct{})}
	base, interaction, ok := cardEventPayload(mkBody("single", ""))
	if !ok {
		t.Fatal("模板卡事件应解码成功")
	}
	rt.submitMsg(headers, base, func(in *bot.InboundMessage) { in.Interaction = &interaction })
	build := <-rt.inbound
	msg := &bot.InboundMessage{}
	build(msg)

	if msg.MessageID != "ev-9" || msg.ConversationKey != "single:u1" || msg.ChatType != bot.ChatDirect || msg.SenderID != "u1" {
		t.Fatalf("单聊归一字段不符: %+v", msg)
	}
	if msg.Text != "" || msg.Quote != nil {
		t.Fatalf("交互消息 Text/Quote 应恒空: %+v", msg)
	}
	if msg.Route.Channel != enums.CHANNEL_WECOM || msg.Route.Payload.(aibottypes.WsFrameHeaders) != headers {
		t.Fatalf("路由应透传点击事件帧 headers: %+v", msg.Route)
	}
	if msg.Interaction == nil || msg.Interaction.DeliveryID != "dlv" || msg.Interaction.ActionIndex != 1 ||
		msg.Interaction.TaskID != "dlv" || msg.Interaction.RawKey != "dlv:1" {
		t.Fatalf("Interaction 注入不符: %+v", msg.Interaction)
	}

	// 群聊：会话键 group:chat1（ChatId 粒度）。
	base, interaction, ok = cardEventPayload(mkBody("group", "chat1"))
	if !ok {
		t.Fatal("群聊事件应解码成功")
	}
	rt2 := &channelRuntime{inbound: make(chan func(*bot.InboundMessage), 1), stopProc: make(chan struct{})}
	rt2.submitMsg(headers, base, func(in *bot.InboundMessage) { in.Interaction = &interaction })
	build = <-rt2.inbound
	msg = &bot.InboundMessage{}
	build(msg)
	if msg.ConversationKey != "group:chat1" || msg.ChatType != bot.ChatGroup || msg.SenderID != "u1" {
		t.Fatalf("群聊归一不符（会话键应取 ChatId、发送者保留点击者）: %+v", msg)
	}

	// 非模板卡事件：false（调用方丢弃）。
	other := mkBody("single", "")
	other.Event = aibottypes.EnterChatEvent{EventType: "enter_chat"}
	if _, _, ok := cardEventPayload(other); ok {
		t.Fatal("非模板卡事件应返回 false")
	}
}
