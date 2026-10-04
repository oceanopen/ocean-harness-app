// 本文件是 elicitation 卡域（T3.3）：elicitation 挂起的 requestedSchema（JSON Schema）
// → 卡题集分类 → 三卡型 spec 构造与勾选集应答映射。分类规则镜像前端 SSOT（
// AcpSessionView/elicitationForm.ts：string+oneOf→单选 / array+items.anyOf→多选 / 其余
// 形态 IM 不可表示），Other 自由输入字段（AskUserQuestion 的 _meta._askUserQuestionCustomAnswer
// 标记，Gold-Band PoC 校准的 wire 形态）从卡题集剔除。TaskID 与审批卡同族编码，marker
// 为 -elicit-（permissionCardMarker 留位兑现）；首发与置灰共用同一构造函数（企微整卡替换，
// 同 CardReplyStream.UpdateCard 契约）。
package bot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"ocean-harness/server/internal/acpsession"
)

// elicit 卡 TaskID marker（与 -perm- 同族：acp-<issueID>@<会话代锚>-elicit-<pendingID>）。
const elicitCardMarker = "-elicit-"

// elicitCardTaskID 编码 elicitation 卡投递锚（复用审批卡的前缀/代锚/切分语义）。
func elicitCardTaskID(issueID, acpSessionID string, pendingID uint64) string {
	return permissionCardPrefix + issueID + permissionCardSessionSep +
		permissionCardSessionToken(acpSessionID) + elicitCardMarker + strconv.FormatUint(pendingID, 10)
}

// parseElicitCardTaskID 解码 elicitation 卡投递锚；非本域形态返回 ok=false（与
// parsePermissionCardTaskID 并列，TryHandleInteraction 按 marker 分流）。
func parseElicitCardTaskID(taskID string) (issueID, sessionToken string, pendingID uint64, ok bool) {
	if !strings.HasPrefix(taskID, permissionCardPrefix) {
		return "", "", 0, false
	}
	rest := taskID[len(permissionCardPrefix):]
	i := strings.LastIndex(rest, elicitCardMarker)
	if i <= 0 {
		return "", "", 0, false
	}
	left := rest[:i]
	pid, err := strconv.ParseUint(rest[i+len(elicitCardMarker):], 10, 64)
	if err != nil || pid == 0 {
		return "", "", 0, false
	}
	at := strings.Index(left, permissionCardSessionSep)
	if at <= 0 || at == len(left)-1 {
		return "", "", 0, false
	}
	return left[:at], left[at+1:], pid, true
}

// elicitQuestion 分类后的卡题（单题卡恰一题、多题卡 2..3 题）。Key 是 schema 属性名（应答
// content 的键）；勾选集下标按 Options 序对齐。
type elicitQuestion struct {
	Key     string
	Title   string
	Multi   bool         // true = 多选（checkbox），false = 单选（radio）
	Options []CardOption // ID = 枚举 const（应答 wire 值），Text = title 兜底 const
}

// elicitFieldSchema requestedSchema 属性子集（与前端 FieldSchema 镜像；越界字段归类后
// 令整卡不可表示，不进 elicitQuestion）。
type elicitFieldSchema struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	OneOf []struct {
		Const *string `json:"const"`
		Title string  `json:"title"`
	} `json:"oneOf"`
	Items *struct {
		AnyOf []struct {
			Const *string `json:"const"`
			Title string  `json:"title"`
		} `json:"anyOf"`
	} `json:"items"`
	Meta *struct {
		CustomAnswer *struct {
			IsCustomAnswer bool   `json:"isCustomAnswer"`
			QuestionID     string `json:"questionId"`
		} `json:"_askUserQuestionCustomAnswer"`
	} `json:"_meta"`
}

// isCustomAnswer Other 自由输入字段判定：_meta 标记 isCustomAnswer=true（questionId 挂靠
// 目标题）。这类字段从卡题集剔除——IM 卡无法承载自由文本。
func (f *elicitFieldSchema) isCustomAnswer() bool {
	return f.Meta != nil && f.Meta.CustomAnswer != nil && f.Meta.CustomAnswer.IsCustomAnswer
}

// classifyElicitation requestedSchema → 卡题集与可表示性。不可表示（返回 false）：非对象/
// 无 properties、存在自由文本/数值/布尔/未知形态字段（卡承载不了）、多选题带 Other 挂靠
// （自定义答案是多选语义的一部分，缺它即语义残缺；单选带 Other 容忍——预设选项独立成立）、
// 题数不在 {1} ∪ [2,3] 全单选、混合单多选、选项数超卡上限（单题 20 / 多题每题 10）。
func classifyElicitation(schema json.RawMessage) ([]elicitQuestion, bool) {
	var top struct {
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &top); err != nil || len(top.Properties) == 0 {
		return nil, false
	}
	props, err := orderedProperties(top.Properties)
	if err != nil || len(props) == 0 {
		return nil, false
	}

	// 第一遍：全量解码，收集 Other 字段挂靠的题集与逐字段形态。
	fields := make([]elicitFieldSchema, len(props))
	otherTargets := make(map[string]bool, len(props))
	for i, prop := range props {
		if err := json.Unmarshal(prop.Raw, &fields[i]); err != nil {
			return nil, false
		}
		if fields[i].isCustomAnswer() {
			if id := fields[i].Meta.CustomAnswer.QuestionID; id != "" {
				otherTargets[id] = true
			}
		}
	}

	// 第二遍：非 Other 字段归类进卡题集；不可承载形态直接判负。
	var questions []elicitQuestion
	for i, prop := range props {
		field := fields[i]
		if field.isCustomAnswer() {
			continue
		}
		question, ok := elicitQuestionOf(prop.Key, field)
		if !ok {
			return nil, false // text/number/switch/unknown：自由输入或越界形态，指回桌面
		}
		questions = append(questions, question)
	}

	switch {
	case len(questions) == 1:
		q := questions[0]
		if q.Multi && otherTargets[q.Key] {
			return nil, false // 多选带 Other：不可表示（自定义答案是多选语义的一部分）
		}
		if len(q.Options) == 0 || len(q.Options) > cardQuestionOptionLimit(false) {
			return nil, false
		}
	case len(questions) >= 2 && len(questions) <= 3:
		for _, q := range questions {
			if q.Multi {
				return nil, false // 多题卡（下拉）只承载单选
			}
			if len(q.Options) == 0 || len(q.Options) > cardQuestionOptionLimit(true) {
				return nil, false
			}
		}
	default:
		return nil, false // 0 题（纯 Other/空表单）或 >3 题
	}
	return questions, true
}

// cardQuestionOptionLimit 卡型选项上限（企微协议）：单题 checkbox [1,20]、多题卡每题
// select_list [1,10]。
func cardQuestionOptionLimit(multiQuestion bool) int {
	if multiQuestion {
		return 10
	}
	return 20
}

// elicitQuestionOf 单字段归类（镜像前端 planField）：string+oneOf→单选、array+items.anyOf→
// 多选；枚举成立需列表非空且每项 const 为字符串（任一缺失整体降级，降级即不可表示）。
// title 空白回落属性名（Gold-Band 同款，下拉无标题不可读）。
func elicitQuestionOf(key string, field elicitFieldSchema) (elicitQuestion, bool) {
	title := strings.TrimSpace(field.Title)
	if title == "" {
		title = key
	}
	switch field.Type {
	case "string":
		if len(field.OneOf) == 0 {
			return elicitQuestion{}, false
		}
		options := make([]CardOption, 0, len(field.OneOf))
		for _, variant := range field.OneOf {
			if variant.Const == nil {
				return elicitQuestion{}, false
			}
			options = append(options, CardOption{ID: *variant.Const, Text: optionLabel(variant.Title, *variant.Const)})
		}
		return elicitQuestion{Key: key, Title: title, Options: options}, true
	case "array":
		if field.Items == nil || len(field.Items.AnyOf) == 0 {
			return elicitQuestion{}, false
		}
		options := make([]CardOption, 0, len(field.Items.AnyOf))
		for _, variant := range field.Items.AnyOf {
			if variant.Const == nil {
				return elicitQuestion{}, false
			}
			options = append(options, CardOption{ID: *variant.Const, Text: optionLabel(variant.Title, *variant.Const)})
		}
		return elicitQuestion{Key: key, Title: title, Multi: true, Options: options}, true
	}
	return elicitQuestion{}, false
}

// optionLabel 枚举项展示文案：title 兜底 const 值（前端 label 语义）。
func optionLabel(title, fallback string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	return fallback
}

// orderedProperties 解码 JSON 对象为有序键值列表。Go map 解码丢插入序，而 AskUserQuestion
// 的多题顺序、Other 挂靠顺序都依赖 properties 序（前端 Object.entries 保序，此处必须同序）。
func orderedProperties(raw json.RawMessage) ([]struct {
	Key string
	Raw json.RawMessage
}, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	open, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := open.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("properties 非对象: %v", open)
	}
	var props []struct {
		Key string
		Raw json.RawMessage
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("属性名非字符串: %v", keyTok)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		props = append(props, struct {
			Key string
			Raw json.RawMessage
		}{key, value})
	}
	if _, err := dec.Token(); err != nil { // '}'
		return nil, err
	}
	return props, nil
}

// elicitationCardSpec elicitation 挂起 → 卡 spec（首发与置灰重建的 SSOT，同
// permissionCardSpec）。不可表示（ok=false）由调用方回落桌面指引文案。
func elicitationCardSpec(issueID, acpSessionID string, pending acpsession.PendingView) (CardSpec, bool) {
	if pending.Kind != "elicitation" || pending.Request == nil || len(pending.Request.RequestedSchema) == 0 {
		return CardSpec{}, false
	}
	questions, ok := classifyElicitation(pending.Request.RequestedSchema)
	if !ok {
		return CardSpec{}, false
	}
	spec := CardSpec{
		Title:  "Agent 请求输入",
		Submit: true, // 统一 submit 语义：应答取提交回传的勾选集（方案阶段拍板③）
		TaskID: elicitCardTaskID(issueID, acpSessionID, pending.PendingID),
	}
	if msg := strings.TrimSpace(pending.Request.Message); msg != "" {
		spec.Description = msg
	}
	if len(questions) == 1 {
		// 单题卡：vote checkbox（单选 mode 0 / 多选 mode 1），选项即卡题选项。
		spec.Options = questions[0].Options
		spec.Multiple = questions[0].Multi
		return spec, true
	}
	// 多题卡（2..3 全单选）：multiple_interaction select_list。
	spec.Questions = make([]CardQuestion, len(questions))
	for i, q := range questions {
		spec.Questions[i] = CardQuestion{Title: q.Title, Options: q.Options}
	}
	return spec, true
}

// handleElicitationInteraction 表单卡点击消费（-elicit- 域，TryHandleInteraction 分流入口，
// T3.3 统一 submit 语义）：点选项 key 只是改选（提示点提交，不置灰不触达应答）；submit key
// 才承载应答——勾选集归一 content 后 accept（成功与后到方判别均置灰；其他失败不置灰可
// 重试）。快照未就绪/代锚不符出失效文案不置灰；挂起已关闭（桌面先答/回合结算）经 decline
// 探测取 closed-history 判别文案（decline 只对开放挂起生效，快照 miss 后挂起已摘除不会被
// 调用——探测安全，同审批域空 optionID 探测形态）。
func (r *driverRoute) handleElicitationInteraction(issueID, sessionToken string, pendingID uint64, in *Interaction) (string, *CardSpec, bool) {
	if in.ActionIndex >= 0 {
		return "该表单为提交型：请在卡片中选择后点「提交」。", nil, true
	}
	snap := r.sessions.Get(issueID)
	if snap.Status != acpsession.StatusReady {
		return "该表单卡已失效（ACP 会话已重启或未就绪），请重新发起对话后再试。", nil, true
	}
	if snap.AcpSessionID == "" || permissionCardSessionToken(snap.AcpSessionID) != sessionToken {
		return "该表单卡已失效（ACP 会话已重启或未就绪），请重新发起对话后再试。", nil, true
	}
	pending, found := findElicitationPending(snap.Pendings, pendingID)
	if !found {
		// 已关闭：decline 探测的错误即收敛判别文案（closed-history：「该表单已由桌面端/IM
		// 应答（…）」/「该表单已随回合结束自动结算」；未命中历史则「不存在或已关闭」）。
		err := r.sessions.RespondElicitation(issueID, pendingID, "decline", nil, acpsession.PendingSourceBot)
		if err == nil {
			return "该表单已处理。", nil, true // 已关闭挂起的 decline 不应成功，防御收口
		}
		return firstLine(err.Error()), nil, true
	}
	if pending.Request == nil || len(pending.Request.RequestedSchema) == 0 {
		return "该表单形态已变化，请在桌面端处理。", nil, true // 出卡时 schema 在场，理论不可达的防御
	}
	questions, ok := classifyElicitation(pending.Request.RequestedSchema)
	if !ok {
		return "该表单形态已变化，请在桌面端处理。", nil, true // 同上（出卡可表示 → 点击时可分类）
	}
	if len(in.Selections) == 0 {
		return "未选择任何选项：请在卡片中选择后点「提交」。", nil, true
	}
	content, ok := elicitationContent(questions, in.DeliveryID, in.Selections)
	if !ok {
		return "勾选内容与卡片不符，请重新在卡片中选择后点「提交」。", nil, true
	}
	gray, ok := elicitationCardSpec(issueID, snap.AcpSessionID, pending)
	if !ok {
		return "该表单形态已变化，请在桌面端处理。", nil, true // classify 已过，此处恒 ok——防御收口
	}
	gray.Disabled = true
	err := r.sessions.RespondElicitation(issueID, pendingID, "accept", content, acpsession.PendingSourceBot)
	switch {
	case err == nil:
		return "✅ 已应答表单：" + elicitAnswerSummary(questions, content), &gray, true
	case errors.Is(err, acpsession.ErrPendingAlreadyHandled):
		// 后到方：文案自带「谁、以何动作处理」判别（同审批域窄竞态窗口置灰）。
		return firstLine(err.Error()), &gray, true
	default:
		return "❌ 表单应答失败：" + firstLine(err.Error()), nil, true
	}
}

// findElicitationPending 挂起目录中定位指定 id 的 elicitation 挂起（kind 收窄是防御——
// 本域 TaskID 只为 elicitation 挂起生成）。
func findElicitationPending(pendings []acpsession.PendingView, pendingID uint64) (acpsession.PendingView, bool) {
	for i := range pendings {
		if pendings[i].PendingID == pendingID && pendings[i].Kind == "elicitation" {
			return pendings[i], true
		}
	}
	return acpsession.PendingView{}, false
}

// elicitAnswerSummary 应答摘要（应答确认文案用）：按卡题序列出所选选项文案（title 兜底
// const 值），多选顿号连接、多题分号连接。
func elicitAnswerSummary(questions []elicitQuestion, content map[string]any) string {
	var parts []string
	for _, q := range questions {
		values, _ := content[q.Key].([]string)
		labels := make([]string, 0, len(values))
		for _, id := range values {
			labels = append(labels, elicitOptionLabel(q.Options, id))
		}
		if single, ok := content[q.Key].(string); ok {
			labels = append(labels, elicitOptionLabel(q.Options, single))
		}
		if len(labels) > 0 {
			parts = append(parts, strings.Join(labels, "、"))
		}
	}
	return strings.Join(parts, "；")
}

// elicitOptionLabel 按 const 值查选项展示文案（未命中回原值）。
func elicitOptionLabel(options []CardOption, id string) string {
	for _, opt := range options {
		if opt.ID == id {
			return opt.Text
		}
	}
	return id
}

// elicitationContent 卡题集 + 提交勾选集 → accept content（键 = schema 属性名，值 = 枚举
// const；多选为数组、按卡选项序）。ok=false = 勾选集与卡题集不符（缺题/键错/下标越界），
// 调用方提示重试不置灰。单题卡勾选集以 TaskID 定位，多题卡以 q<题序> 定位（适配器出卡
// 编码，见 wecom/card.go cardQuestionKey）。
func elicitationContent(questions []elicitQuestion, taskID string, selections []InteractionSelection) (map[string]any, bool) {
	byKey := make(map[string]InteractionSelection, len(selections))
	for _, selection := range selections {
		byKey[selection.QuestionKey] = selection
	}
	content := make(map[string]any, len(questions))
	for i, q := range questions {
		key := taskID
		if len(questions) > 1 {
			key = "q" + strconv.Itoa(i)
		}
		selection, ok := byKey[key]
		if !ok || len(selection.OptionIndexes) == 0 {
			return nil, false
		}
		if q.Multi {
			indexes := append([]int(nil), selection.OptionIndexes...)
			sort.Ints(indexes) // 应答序按卡选项序（与前端勾选序语义一致），防御渠道回序抖动
			values := make([]string, 0, len(indexes))
			for _, idx := range indexes {
				if idx < 0 || idx >= len(q.Options) {
					return nil, false
				}
				values = append(values, q.Options[idx].ID)
			}
			content[q.Key] = values
		} else {
			idx := selection.OptionIndexes[0] // 单选取首下标（客户端保证唯一，冗余项容忍）
			if idx < 0 || idx >= len(q.Options) {
				return nil, false
			}
			content[q.Key] = q.Options[idx].ID
		}
	}
	return content, true
}
