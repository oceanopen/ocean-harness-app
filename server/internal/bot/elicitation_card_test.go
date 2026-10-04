package bot

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/acpsession"
)

// radioField / checkboxField / otherField / textField 组装 requestedSchema 夹具（形态镜像
// Gold-Band PoC 捕获的 AskUserQuestion wire）。
func schemaOf(fields ...string) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` + strings.Join(fields, ",") + `}}`)
}

func radioField(key, title string, values ...string) string {
	variants := make([]string, len(values))
	for i, v := range values {
		variants[i] = `{"const":"` + v + `","title":"` + v + `选项"}`
	}
	return `"` + key + `":{"type":"string","title":"` + title + `","oneOf":[` + strings.Join(variants, ",") + `]}`
}

func checkboxField(key, title string, values ...string) string {
	variants := make([]string, len(values))
	for i, v := range values {
		variants[i] = `{"const":"` + v + `","title":"` + v + `选项"}`
	}
	return `"` + key + `":{"type":"array","title":"` + title + `","items":{"anyOf":[` + strings.Join(variants, ",") + `]}}`
}

func otherField(key, target string) string {
	return `"` + key + `":{"type":"string","title":"Other","_meta":{"_askUserQuestionCustomAnswer":{"questionId":"` + target + `","isCustomAnswer":true}}}`
}

func textField(key string) string {
	return `"` + key + `":{"type":"string","title":"自由输入"}`
}

// TaskID 编解码：-elicit- marker 往返 + 与 -perm- 域互斥（分流前提）。
func TestElicitCardTaskIDRoundTrip(t *testing.T) {
	taskID := elicitCardTaskID("i-1", testAcpSessionID, 9)
	if err := ValidateCardTaskID(taskID); err != nil {
		t.Fatalf("编码产物须过 TaskID 契约校验: %q: %v", taskID, err)
	}
	issue, token, pid, ok := parseElicitCardTaskID(taskID)
	if !ok || issue != "i-1" || token != "3f2a9c1e" || pid != 9 {
		t.Fatalf("往返不符: %q → (%q, %q, %d, %v)", taskID, issue, token, pid, ok)
	}
	// 域互斥：审批锚不被 elicitation 解码命中，反之亦然（marker 分流依据）。
	permID := permissionCardTaskID("i-1", testAcpSessionID, 9)
	if _, _, _, ok := parseElicitCardTaskID(permID); ok {
		t.Fatalf("审批锚不应被 elicitation 域解码: %q", permID)
	}
	if _, _, _, ok := parsePermissionCardTaskID(taskID); ok {
		t.Fatalf("elicitation 锚不应被审批域解码: %q", taskID)
	}
	for _, bad := range []string{"", "d-1", "acp-i@s1", "acp-i@s1-elicit-x", "acp-i@s1-elicit-0"} {
		if _, _, _, ok := parseElicitCardTaskID(bad); ok {
			t.Fatalf("非法锚 %q 应解码失败", bad)
		}
	}
}

// classifyElicitation 域：三卡型归类、Other 剔除与容忍、不可表示形态表、顺序保持。
func TestClassifyElicitation(t *testing.T) {
	// 单选（含 Other 容忍）：Other 字段剔除，仅剩预设选项。
	questions, ok := classifyElicitation(schemaOf(
		radioField("db", "数据库", "mysql", "postgres"),
		otherField("db_custom", "db"),
	))
	if !ok {
		t.Fatal("单选带 Other 应可表示")
	}
	if len(questions) != 1 || questions[0].Key != "db" || questions[0].Title != "数据库" || questions[0].Multi {
		t.Fatalf("单选题归类不符: %+v", questions)
	}
	if len(questions[0].Options) != 2 || questions[0].Options[0].ID != "mysql" || questions[0].Options[0].Text != "mysql选项" {
		t.Fatalf("选项归类不符: %+v", questions[0].Options)
	}

	// 多选：Multi=true。
	questions, ok = classifyElicitation(schemaOf(checkboxField("langs", "语言", "go", "rust", "ts")))
	if !ok || len(questions) != 1 || !questions[0].Multi || len(questions[0].Options) != 3 {
		t.Fatalf("多选题归类不符: %+v ok=%v", questions, ok)
	}

	// 多选带 Other：不可表示。
	if _, ok = classifyElicitation(schemaOf(
		checkboxField("langs", "语言", "go", "rust"),
		otherField("langs_custom", "langs"),
	)); ok {
		t.Fatal("多选带 Other 应不可表示")
	}

	// 多题（2..3 全单选，Other 逐题挂靠剔除）：顺序保持。
	questions, ok = classifyElicitation(schemaOf(
		radioField("env", "环境", "dev", "prod"),
		radioField("level", "级别", "p0", "p1"),
		otherField("env_custom", "env"),
	))
	if !ok || len(questions) != 2 || questions[0].Key != "env" || questions[1].Key != "level" {
		t.Fatalf("多题归类不符: %+v ok=%v", questions, ok)
	}

	// 不可表示形态表。
	for name, schema := range map[string]json.RawMessage{
		"自由文本字段":      schemaOf(radioField("a", "题", "x"), textField("note")),
		"纯文本表单":       schemaOf(textField("note")),
		"混合单多选":       schemaOf(radioField("a", "题", "x"), checkboxField("b", "题", "y")),
		"四题超限":        schemaOf(radioField("a", "题", "x"), radioField("b", "题", "x"), radioField("c", "题", "x"), radioField("d", "题", "x")),
		"单题选项超限":      schemaOf(radioField("a", "题", make([]string, 21)...)),
		"多题单题选项超限":    schemaOf(radioField("a", "题", make([]string, 11)...), radioField("b", "题", "x")),
		"无 oneOf 字符串": json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`),
		"const 非字符串":  json.RawMessage(`{"type":"object","properties":{"a":{"type":"string","oneOf":[{"const":1}]}}}`),
		"空 schema":    json.RawMessage(`{}`),
		"坏 JSON":      json.RawMessage(`{`),
	} {
		if _, ok := classifyElicitation(schema); ok {
			t.Fatalf("%s 应不可表示", name)
		}
	}

	// title 空白回落属性名（下拉无标题不可读）。
	questions, ok = classifyElicitation(json.RawMessage(
		`{"type":"object","properties":{"plain":{"type":"string","oneOf":[{"const":"v","title":"V"}]}}}`,
	))
	if !ok || questions[0].Title != "plain" {
		t.Fatalf("title 应回落属性名: %+v", questions)
	}
}

// elicitationCardSpec 域：三卡型 spec、TaskID marker、Description、不可表示回落。
func TestElicitationCardSpec(t *testing.T) {
	pending := func(schema json.RawMessage) acpsession.PendingView {
		return acpsession.PendingView{
			PendingID: 5, Kind: "elicitation",
			Request: &acp.ElicitationWire{Mode: "form", Message: "请选择数据库", RequestedSchema: schema},
		}
	}

	// 单选：Submit 语义 + vote 选项面。
	spec, ok := elicitationCardSpec("i-1", testAcpSessionID, pending(schemaOf(radioField("db", "数据库", "mysql", "postgres"))))
	if !ok {
		t.Fatal("单选表单应可出卡")
	}
	if !spec.Submit || spec.Multiple || len(spec.Questions) != 0 || len(spec.Options) != 2 {
		t.Fatalf("单选卡 spec 不符: %+v", spec)
	}
	if want := elicitCardTaskID("i-1", testAcpSessionID, 5); spec.TaskID != want {
		t.Fatalf("TaskID 不符: %q want %q", spec.TaskID, want)
	}
	if spec.Title != "Agent 请求输入" || spec.Description != "请选择数据库" {
		t.Fatalf("标题/描述不符: %+v", spec)
	}

	// 多选：Multiple=true。
	spec, ok = elicitationCardSpec("i-1", testAcpSessionID, pending(schemaOf(checkboxField("langs", "语言", "go", "rust"))))
	if !ok || !spec.Multiple {
		t.Fatalf("多选卡 spec 不符: %+v ok=%v", spec, ok)
	}

	// 多题：Questions 投影（title + 选项）。
	spec, ok = elicitationCardSpec("i-1", testAcpSessionID, pending(schemaOf(
		radioField("env", "环境", "dev", "prod"),
		radioField("level", "级别", "p0", "p1"),
	)))
	if !ok || len(spec.Questions) != 2 || spec.Questions[0].Title != "环境" || len(spec.Questions[1].Options) != 2 {
		t.Fatalf("多题卡 spec 不符: %+v ok=%v", spec, ok)
	}

	// 不可表示 / 挂起形态不符：ok=false。
	for name, p := range map[string]acpsession.PendingView{
		"自由文本":          pending(schemaOf(textField("note"))),
		"非 elicitation": {PendingID: 5, Kind: "permission"},
		"缺 Request":     {PendingID: 5, Kind: "elicitation"},
		"空 schema":      pending(nil),
	} {
		if _, ok := elicitationCardSpec("i-1", testAcpSessionID, p); ok {
			t.Fatalf("%s 应不可出卡", name)
		}
	}
}

// elicitationContent 域：勾选集 → accept content（单题 TaskID 锚 / 多题 q<序> 锚）。
func TestElicitationContent(t *testing.T) {
	const taskID = "acp-i@s1-elicit-5"

	// 单选：TaskID 定位，取首下标。
	questions := []elicitQuestion{{Key: "db", Options: []CardOption{{ID: "mysql"}, {ID: "postgres"}}}}
	content, ok := elicitationContent(questions, taskID, []InteractionSelection{
		{QuestionKey: taskID, OptionIndexes: []int{1}},
	})
	if !ok || len(content) != 1 || content["db"] != "postgres" {
		t.Fatalf("单选 content 不符: %+v ok=%v", content, ok)
	}

	// 多选：全部下标按卡选项序（输入乱序防御）。
	questions = []elicitQuestion{{Key: "langs", Multi: true, Options: []CardOption{{ID: "go"}, {ID: "rust"}, {ID: "ts"}}}}
	content, ok = elicitationContent(questions, taskID, []InteractionSelection{
		{QuestionKey: taskID, OptionIndexes: []int{2, 0}},
	})
	if !ok {
		t.Fatal("多选 content 应成功")
	}
	values, _ := content["langs"].([]string)
	if len(values) != 2 || values[0] != "go" || values[1] != "ts" {
		t.Fatalf("多选 values 不符（应按选项序）: %+v", content["langs"])
	}

	// 多题：q0/q1 定位，各自映射。
	questions = []elicitQuestion{
		{Key: "env", Options: []CardOption{{ID: "dev"}, {ID: "prod"}}},
		{Key: "level", Options: []CardOption{{ID: "p0"}, {ID: "p1"}}},
	}
	content, ok = elicitationContent(questions, taskID, []InteractionSelection{
		{QuestionKey: "q0", OptionIndexes: []int{1}},
		{QuestionKey: "q1", OptionIndexes: []int{0}},
	})
	if !ok || content["env"] != "prod" || content["level"] != "p0" {
		t.Fatalf("多题 content 不符: %+v ok=%v", content, ok)
	}

	// 不符形态：缺题 / 键错 / 下标越界 / 空勾选集。
	bad := []struct {
		name       string
		selections []InteractionSelection
	}{
		{"缺 q1", []InteractionSelection{{QuestionKey: "q0", OptionIndexes: []int{0}}}},
		{"键错", []InteractionSelection{{QuestionKey: "q9", OptionIndexes: []int{0}}}},
		{"越界", []InteractionSelection{{QuestionKey: "q0", OptionIndexes: []int{5}}}},
		{"空", nil},
	}
	for _, c := range bad {
		if _, ok := elicitationContent(questions, taskID, c.selections); ok {
			t.Fatalf("%s 应判定不符", c.name)
		}
	}
}

// elicitPendingView / elicitSubmitMsg 点击消费夹具（schema 夹具同文件上部共享）。
func elicitPendingView(pendingID uint64, schema json.RawMessage) acpsession.PendingView {
	return acpsession.PendingView{
		PendingID: pendingID, Kind: "elicitation",
		Request: &acp.ElicitationWire{Mode: "form", Message: "请选择", RequestedSchema: schema},
	}
}

func elicitSubmitMsg(taskID string, selections ...InteractionSelection) InboundMessage {
	return InboundMessage{
		MessageID: "ev-" + taskID, ConversationKey: "single:u1", SenderID: "u1",
		Interaction: &Interaction{
			DeliveryID: taskID, ActionIndex: -1, TaskID: taskID,
			RawKey: taskID + ":submit", Selections: selections,
		},
	}
}

// 点击消费矩阵·点选项 key（统一 submit 语义）：提示点提交、不置灰、不触达应答。
func TestElicitInteractionOptionClickHint(t *testing.T) {
	sessions := newFakeSessions(interactSnap(elicitPendingView(9, schemaOf(radioField("db", "数据库", "mysql", "postgres")))))
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), cardClickMsg(elicitCardTaskID("i-1", testAcpSessionID, 9), 0))
	if !handled || update != nil || !strings.Contains(text, "提交") {
		t.Fatalf("点选项应提示点提交且不置灰: (%q, %+v, %v)", text, update, handled)
	}
	if calls := sessions.elicitResponds(); len(calls) != 0 {
		t.Fatalf("点选项不得触达应答: %+v", calls)
	}
}

// 点击消费矩阵·submit 空勾选集：提示选择后提交、不置灰、不触达应答。
func TestElicitInteractionEmptySelection(t *testing.T) {
	sessions := newFakeSessions(interactSnap(elicitPendingView(9, schemaOf(radioField("db", "数据库", "mysql", "postgres")))))
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), elicitSubmitMsg(elicitCardTaskID("i-1", testAcpSessionID, 9)))
	if !handled || update != nil || !strings.Contains(text, "未选择") {
		t.Fatalf("空勾选集应提示重选且不置灰: (%q, %+v, %v)", text, update, handled)
	}
	if calls := sessions.elicitResponds(); len(calls) != 0 {
		t.Fatalf("空勾选集不得触达应答: %+v", calls)
	}
}

// 点击消费矩阵·命中（单选 submit）：accept 带勾选集归一的 content（source=bot）、确认文案
// 带选项摘要、置灰为「原卡完整形态 + Disabled=true」。
func TestElicitInteractionRespond(t *testing.T) {
	sessions := newFakeSessions(interactSnap(elicitPendingView(9, schemaOf(radioField("db", "数据库", "mysql", "postgres")))))
	route := gateTestRoute(acpResolve, sessions)
	taskID := elicitCardTaskID("i-1", testAcpSessionID, 9)

	text, update, handled := route.TryHandleInteraction(gateCfg(), elicitSubmitMsg(taskID,
		InteractionSelection{QuestionKey: taskID, OptionIndexes: []int{1}}))
	if !handled {
		t.Fatal("submit 命中挂起应消费点击")
	}
	if want := "✅ 已应答表单：postgres选项"; text != want {
		t.Fatalf("确认文案不符（带选项摘要）: %q want %q", text, want)
	}
	calls := sessions.elicitResponds()
	if len(calls) != 1 || calls[0].pendingID != 9 || calls[0].action != "accept" || calls[0].source != acpsession.PendingSourceBot {
		t.Fatalf("应答参数不符: %+v", calls)
	}
	if got, _ := calls[0].content["db"].(string); got != "postgres" {
		t.Fatalf("content 不符（勾选集 → 枚举 const）: %+v", calls[0].content)
	}
	if update == nil || !update.Disabled || !update.Submit || update.TaskID != taskID || len(update.Options) != 2 {
		t.Fatalf("置灰 spec 须为原卡完整形态 + Disabled: %+v", update)
	}
}

// 点击消费矩阵·命中（多题 submit）：q0/q1 勾选集 → 各键 content，摘要分号连接，置灰带
// Questions 投影。
func TestElicitInteractionRespondMultiQuestion(t *testing.T) {
	sessions := newFakeSessions(interactSnap(elicitPendingView(9, schemaOf(
		radioField("env", "环境", "dev", "prod"),
		radioField("level", "级别", "p0", "p1"),
	))))
	route := gateTestRoute(acpResolve, sessions)
	taskID := elicitCardTaskID("i-1", testAcpSessionID, 9)

	text, update, handled := route.TryHandleInteraction(gateCfg(), elicitSubmitMsg(taskID,
		InteractionSelection{QuestionKey: "q0", OptionIndexes: []int{1}},
		InteractionSelection{QuestionKey: "q1", OptionIndexes: []int{0}},
	))
	if !handled {
		t.Fatal("多题 submit 应消费点击")
	}
	if want := "✅ 已应答表单：prod选项；p0选项"; text != want {
		t.Fatalf("多题摘要不符: %q want %q", text, want)
	}
	calls := sessions.elicitResponds()
	if len(calls) != 1 || calls[0].action != "accept" {
		t.Fatalf("应答参数不符: %+v", calls)
	}
	if got, _ := calls[0].content["env"].(string); got != "prod" {
		t.Fatalf("env content 不符: %+v", calls[0].content)
	}
	if got, _ := calls[0].content["level"].(string); got != "p0" {
		t.Fatalf("level content 不符: %+v", calls[0].content)
	}
	if update == nil || !update.Disabled || len(update.Questions) != 2 {
		t.Fatalf("多题置灰 spec 不符: %+v", update)
	}
}

// 点击消费矩阵·勾选集不符（键错）：提示重试、不置灰、不触达应答。
func TestElicitInteractionSelectionMismatch(t *testing.T) {
	sessions := newFakeSessions(interactSnap(elicitPendingView(9, schemaOf(radioField("db", "数据库", "mysql", "postgres")))))
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), elicitSubmitMsg(elicitCardTaskID("i-1", testAcpSessionID, 9),
		InteractionSelection{QuestionKey: "q9", OptionIndexes: []int{0}}))
	if !handled || update != nil || !strings.Contains(text, "不符") {
		t.Fatalf("勾选集不符应提示重试且不置灰: (%q, %+v, %v)", text, update, handled)
	}
	if calls := sessions.elicitResponds(); len(calls) != 0 {
		t.Fatalf("勾选集不符不得触达应答: %+v", calls)
	}
}

// 点击消费矩阵·后到方（桌面先答的窄竞态：快照在手仍开放）：哨兵判别文案直出 + 置灰。
func TestElicitInteractionAlreadyHandled(t *testing.T) {
	sessions := newFakeSessions(interactSnap(elicitPendingView(9, schemaOf(radioField("db", "数据库", "mysql", "postgres")))))
	sessions.elicitRespondErr = &acpsession.PendingAlreadyHandledError{Kind: "表单", Source: acpsession.PendingSourcePanel, Label: "接受"}
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), elicitSubmitMsg(elicitCardTaskID("i-1", testAcpSessionID, 9),
		InteractionSelection{QuestionKey: elicitCardTaskID("i-1", testAcpSessionID, 9), OptionIndexes: []int{0}}))
	if !handled || text != "该表单已由桌面端应答（接受）" || update == nil || !update.Disabled {
		t.Fatalf("后到方应回判别文案并置灰: (%q, %+v, %v)", text, update, handled)
	}
}

// 点击消费矩阵·应答失败（非哨兵错误）：失败文案、不置灰（可重试）。
func TestElicitInteractionRespondFailure(t *testing.T) {
	sessions := newFakeSessions(interactSnap(elicitPendingView(9, schemaOf(radioField("db", "数据库", "mysql", "postgres")))))
	sessions.elicitRespondErr = errors.New("ACP 连接已关闭")
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), elicitSubmitMsg(elicitCardTaskID("i-1", testAcpSessionID, 9),
		InteractionSelection{QuestionKey: elicitCardTaskID("i-1", testAcpSessionID, 9), OptionIndexes: []int{0}}))
	if !handled || text != "❌ 表单应答失败：ACP 连接已关闭" || update != nil {
		t.Fatalf("失败应不置灰可重试: (%q, %+v, %v)", text, update, handled)
	}
}

// 点击消费矩阵·已关闭（桌面先答/回合结算，快照已摘挂起）：decline 探测取 closed-history
// 判别文案，不置灰。
func TestElicitInteractionAlreadyClosed(t *testing.T) {
	sessions := newFakeSessions(interactSnap()) // 快照无挂起 = 已关闭
	sessions.elicitRespondErr = &acpsession.PendingAlreadyHandledError{Kind: "表单", Source: acpsession.PendingSourcePanel, Label: "接受"}
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), elicitSubmitMsg(elicitCardTaskID("i-1", testAcpSessionID, 9),
		InteractionSelection{QuestionKey: elicitCardTaskID("i-1", testAcpSessionID, 9), OptionIndexes: []int{0}}))
	if !handled || update != nil || text != "该表单已由桌面端应答（接受）" {
		t.Fatalf("已关闭应经 decline 探测回判别文案: (%q, %+v, %v)", text, update, handled)
	}
	calls := sessions.elicitResponds()
	if len(calls) != 1 || calls[0].action != "decline" {
		t.Fatalf("已关闭探测应为 decline: %+v", calls)
	}
}

// 点击消费矩阵·会话重建撞号：代锚不符即旧代卡，失效文案收口、不触达应答。
func TestElicitInteractionSessionRebuilt(t *testing.T) {
	snap := interactSnap(elicitPendingView(9, schemaOf(radioField("db", "数据库", "mysql", "postgres"))))
	snap.AcpSessionID = "99999999-0000-4000-8000-abcdef012345" // 新代会话（撞号形态）
	sessions := newFakeSessions(snap)
	route := gateTestRoute(acpResolve, sessions)

	text, update, handled := route.TryHandleInteraction(gateCfg(), elicitSubmitMsg(elicitCardTaskID("i-1", testAcpSessionID, 9),
		InteractionSelection{QuestionKey: elicitCardTaskID("i-1", testAcpSessionID, 9), OptionIndexes: []int{0}}))
	if !handled || update != nil || !strings.Contains(text, "已失效") {
		t.Fatalf("旧代卡应失效收口: (%q, %+v, %v)", text, update, handled)
	}
	if calls := sessions.elicitResponds(); len(calls) != 0 {
		t.Fatalf("代锚不符不得触达应答: %+v", calls)
	}
}

// 快路径数字回吞两态（T3.3）：纯表单挂起可表示 → 不确定态文案（gate 无回合上下文，「审批
// 卡曾占位后残留纯表单态」与正常出卡态快照同形，确定态文案会指引不存在的卡；不可表示指回
// 桌面由 TestPendingGateOnlyElicitation 覆盖）。
func TestPendingGateOnlyElicitationRepresentable(t *testing.T) {
	sessions := newFakeSessions(gateSnap(elicitPendingView(21, schemaOf(radioField("db", "数据库", "mysql", "postgres")))))
	route := gateTestRoute(acpResolve, sessions)

	text, handled := route.TryRespondPending(gateCfg(), gateMsg("1"))
	if !handled || text != elicitPendingGateText {
		t.Fatalf("可表示表单挂起应回不确定态文案: (%q, %v)", text, handled)
	}
	if calls := sessions.elicitResponds(); len(calls) != 0 {
		t.Fatalf("数字非应答形态不得触达应答: %+v", calls)
	}
}
