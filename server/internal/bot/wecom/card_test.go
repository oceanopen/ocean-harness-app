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
	// 单选默认选中第一项（T3.2 拍板：单选卡须有选中项），其余不选。
	if !card.Checkbox.OptionList[0].IsChecked || card.Checkbox.OptionList[1].IsChecked {
		t.Fatalf("单选应仅首项选中: %+v", card.Checkbox.OptionList)
	}
	if card.SubmitButton != nil {
		t.Fatalf("单选不应有提交按钮: %+v", card.SubmitButton)
	}

	// 多选：mode 1 + 提交按钮（文案「提交」、key `TaskID:submit`），不预选。
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
	for i, opt := range card.Checkbox.OptionList {
		if opt.IsChecked {
			t.Fatalf("多选不应预选（选项 %d）: %+v", i, opt)
		}
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

// Submit 语义（T3.3）：单选 + Submit=true 也带提交按钮（点击只是改选，应答取提交回传的
// 勾选集）；默认选中仍为首项。
func TestBuildVoteCardSubmit(t *testing.T) {
	spec := bot.CardSpec{
		Title:   "选择方案",
		Options: []bot.CardOption{{ID: "a", Text: "方案一"}, {ID: "b", Text: "方案二"}},
		Submit:  true,
		TaskID:  "elicit_task@1",
	}
	card, err := buildTemplateCard(spec)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if card.Checkbox == nil || card.Checkbox.Mode != 0 {
		t.Fatalf("Submit 单选应为 mode=0: %+v", card.Checkbox)
	}
	if card.SubmitButton == nil || card.SubmitButton.Key != "elicit_task@1:submit" {
		t.Fatalf("Submit=true 应带提交按钮: %+v", card.SubmitButton)
	}
	if !card.Checkbox.OptionList[0].IsChecked {
		t.Fatal("Submit 单选仍默认选中首项")
	}
}

// 多题下拉卡（multiple_interaction select_list，T3.3）：卡型分派、题目定位键 q<序>、选项 id
// 纯下标、selected_id 首项、截断（题 13 字/选项 10 字）、置灰、上限校验拒绝。
func TestBuildMultipleInteractionCard(t *testing.T) {
	spec := bot.CardSpec{
		Title: "补充信息",
		Questions: []bot.CardQuestion{
			{Title: "环境", Options: []bot.CardOption{{ID: "dev", Text: "开发"}, {ID: "prod", Text: "生产"}}},
			{Title: "级别", Options: []bot.CardOption{{ID: "p0", Text: "紧急"}, {ID: "p1", Text: "普通"}}},
		},
		TaskID: "elicit_task@2",
	}
	card, err := buildTemplateCard(spec)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if card.CardType != aibottypes.TemplateCardType.MultipleInteraction {
		t.Fatalf("卡型应为 multiple_interaction: %q", card.CardType)
	}
	if card.Checkbox != nil {
		t.Fatal("多题卡不应有 checkbox")
	}
	if len(card.SelectList) != 2 {
		t.Fatalf("应两道题: %+v", card.SelectList)
	}
	first := card.SelectList[0]
	if first.QuestionKey != "q0" || first.Title != "环境" {
		t.Fatalf("首题定位键/标题不符: %+v", first)
	}
	if len(first.OptionList) != 2 || first.OptionList[0].Id != "0" || first.OptionList[0].Text != "开发" || first.OptionList[1].Id != "1" {
		t.Fatalf("首题选项 id 应为纯下标: %+v", first.OptionList)
	}
	if first.SelectedId != "0" {
		t.Fatalf("默认选中应为首项: %q", first.SelectedId)
	}
	if card.SelectList[1].QuestionKey != "q1" {
		t.Fatalf("次题定位键不符: %q", card.SelectList[1].QuestionKey)
	}
	if card.SubmitButton == nil || card.SubmitButton.Key != "elicit_task@2:submit" {
		t.Fatalf("多题卡恒带提交按钮: %+v", card.SubmitButton)
	}
	if first.Disable {
		t.Fatal("未置灰帧 disable 应为 false")
	}

	// 置灰：select_list[].disable=true（更新帧同 builder，task_id/submit key 不变）。
	gray := spec
	gray.Disabled = true
	card, err = buildTemplateCard(gray)
	if err != nil {
		t.Fatalf("置灰构建失败: %v", err)
	}
	if !card.SelectList[0].Disable || !card.SelectList[1].Disable || card.TaskId != "elicit_task@2" {
		t.Fatalf("置灰帧应整卡 disable 且 TaskId 不变: %+v", card.SelectList)
	}

	// 截断：题目标题 14→13、选项 11→10（rune 级）。
	trunc := spec
	trunc.Questions = []bot.CardQuestion{{Title: rep("题", 14), Options: []bot.CardOption{{ID: "a", Text: rep("项", 11)}}}}
	card, err = buildTemplateCard(trunc)
	if err != nil {
		t.Fatalf("截断构建失败: %v", err)
	}
	if got := len([]rune(card.SelectList[0].Title)); got != 13 {
		t.Fatalf("题目标题应截到 13: %d", got)
	}
	if got := len([]rune(card.SelectList[0].OptionList[0].Text)); got != 10 {
		t.Fatalf("选项应截到 10: %d", got)
	}

	// 校验拒绝：题数 >3、单题选项空、单题选项 >10。
	bad := []bot.CardSpec{
		{Title: "t", TaskID: "ok_task", Questions: []bot.CardQuestion{
			{Title: "a", Options: spec.Questions[0].Options},
			{Title: "b", Options: spec.Questions[0].Options},
			{Title: "c", Options: spec.Questions[0].Options},
			{Title: "d", Options: spec.Questions[0].Options},
		}},
		{Title: "t", TaskID: "ok_task", Questions: []bot.CardQuestion{{Title: "a"}}},
		{Title: "t", TaskID: "ok_task", Questions: []bot.CardQuestion{
			{Title: "a", Options: make([]bot.CardOption, 11)},
		}},
	}
	for i, s := range bad {
		if _, err := buildTemplateCard(s); err == nil {
			t.Fatalf("非法多题 spec[%d] 应报错: %+v", i, s.Questions)
		}
	}
}

// normalizeSelections 勾选集归一（T3.3）：vote 前缀形态、多题纯下标形态、无效 id 丢弃、
// 整题无效跳过、nil/缺 OptionIds 容错。
func TestNormalizeSelections(t *testing.T) {
	// vote 形态：`<TaskID>:<i>` 剥前缀得下标。
	items := &aibottypes.TemplateCardSelectedItems{SelectedItem: []aibottypes.TemplateCardSelectedItem{
		{QuestionKey: "task@1", OptionIds: &aibottypes.TemplateCardOptionIds{OptionId: []string{"task@1:0", "task@1:2"}}},
	}}
	got := normalizeSelections(items, "task@1")
	if len(got) != 1 || got[0].QuestionKey != "task@1" || len(got[0].OptionIndexes) != 2 || got[0].OptionIndexes[0] != 0 || got[0].OptionIndexes[1] != 2 {
		t.Fatalf("vote 勾选集归一不符: %+v", got)
	}

	// 多题形态：选项 id 纯下标（无 TaskID 前缀），多题各自成条。
	items = &aibottypes.TemplateCardSelectedItems{SelectedItem: []aibottypes.TemplateCardSelectedItem{
		{QuestionKey: "q0", OptionIds: &aibottypes.TemplateCardOptionIds{OptionId: []string{"1"}}},
		{QuestionKey: "q1", OptionIds: &aibottypes.TemplateCardOptionIds{OptionId: []string{"0", "2"}}},
	}}
	got = normalizeSelections(items, "task@1")
	if len(got) != 2 || got[0].QuestionKey != "q0" || len(got[0].OptionIndexes) != 1 || got[0].OptionIndexes[0] != 1 ||
		got[1].QuestionKey != "q1" || len(got[1].OptionIndexes) != 2 || got[1].OptionIndexes[1] != 2 {
		t.Fatalf("多题勾选集归一不符: %+v", got)
	}

	// 无效 id 丢弃；全部无效/缺 OptionIds 的题整题跳过。
	items = &aibottypes.TemplateCardSelectedItems{SelectedItem: []aibottypes.TemplateCardSelectedItem{
		{QuestionKey: "q0", OptionIds: &aibottypes.TemplateCardOptionIds{OptionId: []string{"task@1:1", "task@1:x", "other:9"}}},
		{QuestionKey: "q1", OptionIds: &aibottypes.TemplateCardOptionIds{OptionId: []string{"x", "-1"}}},
		{QuestionKey: "q2"},
	}}
	got = normalizeSelections(items, "task@1")
	if len(got) != 1 || got[0].QuestionKey != "q0" || len(got[0].OptionIndexes) != 1 || got[0].OptionIndexes[0] != 1 {
		t.Fatalf("无效项应丢弃、空题跳过: %+v", got)
	}

	// nil 勾选集（纯点击型事件）：nil。
	if got := normalizeSelections(nil, "task@1"); got != nil {
		t.Fatalf("nil 应归一 nil: %+v", got)
	}
	// DeliveryID 为空（无冒号 key）：仅纯下标可解析。
	items = &aibottypes.TemplateCardSelectedItems{SelectedItem: []aibottypes.TemplateCardSelectedItem{
		{QuestionKey: "q0", OptionIds: &aibottypes.TemplateCardOptionIds{OptionId: []string{"3"}}},
	}}
	got = normalizeSelections(items, "")
	if len(got) != 1 || got[0].OptionIndexes[0] != 3 {
		t.Fatalf("空 DeliveryID 应按纯下标解析: %+v", got)
	}
}

// cardEventPayload 勾选集链路（T3.3）：selected_items 归一进 Interaction.Selections，与
// event_key 反解同帧产出。
func TestCardEventSelectionsPipeline(t *testing.T) {
	body := aibottypes.EventMessage{
		MsgId: "ev-10", AibotId: "bot-1", ChatType: "single",
		From:    aibottypes.EventFrom{UserId: "u1", CorpId: "corp"},
		MsgType: "event",
		Event: aibottypes.TemplateCardEventData{
			EventType: "template_card_event", EventKey: "elicit_task@3:submit", TaskId: "elicit_task@3",
			SelectedItems: &aibottypes.TemplateCardSelectedItems{SelectedItem: []aibottypes.TemplateCardSelectedItem{
				{QuestionKey: "elicit_task@3", OptionIds: &aibottypes.TemplateCardOptionIds{OptionId: []string{"elicit_task@3:1"}}},
			}},
		},
	}
	_, interaction, ok := cardEventPayload(body)
	if !ok {
		t.Fatal("模板卡事件应解码成功")
	}
	if interaction.DeliveryID != "elicit_task@3" || interaction.ActionIndex != -1 {
		t.Fatalf("提交事件反解不符: %+v", interaction)
	}
	if len(interaction.Selections) != 1 || interaction.Selections[0].QuestionKey != "elicit_task@3" ||
		len(interaction.Selections[0].OptionIndexes) != 1 || interaction.Selections[0].OptionIndexes[0] != 1 {
		t.Fatalf("勾选集应随帧归一: %+v", interaction.Selections)
	}
}
