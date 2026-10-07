// card.go 企微模板卡片通道落地（T3.1）：CardSpec ⇄ 企微模板卡（vote_interaction 单题投票 /
// multiple_interaction 多题下拉）的双向翻译 + replyStream 的 bot.CardReplyStream 实现。出站
// key 编码（`TaskID:<下标>` / `TaskID:submit`）、题目定位键（单题 = TaskID、多题 = q<序>）与
// 入站反解（last-colon 切分 / selected_items 勾选集归一）双向同源，TaskID 字符集不含冒号
// 保证切分无歧义。
package wecom

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	aibot "github.com/oceanopen/wecom-aibot-go-sdk/aibot"
	aibottypes "github.com/oceanopen/wecom-aibot-go-sdk/aibot/types"

	"ocean-harness/server/internal/bot"
)

// 企微 vote_interaction 卡文案上限（rune）：主标题 26 字、标题辅助 30 字、选项 11 字；
// multiple_interaction 卡：题目标题 13 字、选项 10 字。
const (
	cardTitleMaxRunes        = 26
	cardDescMaxRunes         = 30
	cardOptionMaxRunes       = 11
	cardSelectTitleMaxRunes  = 13
	cardSelectOptionMaxRunes = 10
)

// 企微卡选项数上限：vote checkbox [1,20]（SDK 类型注释 [1,20]）；multiple_interaction
// 题数 ≤3、每题选项 [1,10]。
const (
	cardVoteOptionMax     = 20
	cardSelectQuestionMax = 3
	cardSelectOptionMax   = 10
)

// 提交按钮文案（≤10 字）：首发「提交」；置灰更新帧改「已处理」——submit_button 无 disable
// 字段且更新帧不可省略（errcode=42049 真机实证），文案是传达消费态的唯一口子（按钮仍可
// 点，误点由拦截步兜底「该消息已处理过」）。
const (
	cardSubmitText     = "提交"
	cardSubmitTextDone = "已处理"
)

// cardSubmitTextFor 按帧形态取提交按钮文案（首发「提交」/置灰帧「已处理」，key 恒不变）。
func cardSubmitTextFor(disabled bool) string {
	if disabled {
		return cardSubmitTextDone
	}
	return cardSubmitText
}

// templateCardOption SDK Checkbox.OptionList 匿名结构体的本地别名（字段名/类型/tag 须与
// SDK 声明逐项一致才可赋值；别名只为字面量构造可写）。
type templateCardOption = struct {
	Id        string `json:"id"`
	Text      string `json:"text"`
	IsChecked bool   `json:"is_checked,omitempty"`
}

// templateSelectOption SDK SelectionItem.OptionList 匿名结构体的本地别名（同上）。
type templateSelectOption = struct {
	Id   string `json:"id"`
	Text string `json:"text"`
}

// encodeActionKey 选项点击的 event_key：`TaskID:<下标>`。
func encodeActionKey(taskID string, index int) string {
	return taskID + ":" + strconv.Itoa(index)
}

// encodeSubmitKey 提交按钮的 event_key：`TaskID:submit`。
func encodeSubmitKey(taskID string) string {
	return taskID + ":submit"
}

// cardQuestionKey 多题卡的题目定位键：`q<题序>`（0 基）。字段名字符集不可控（schema 属性
// 名），位置序号在首发与置灰重建间确定性一致；Gold-Band PoC 同款形态。
func cardQuestionKey(index int) string {
	return "q" + strconv.Itoa(index)
}

// buildTemplateCard CardSpec → 企微模板卡（Questions 非空走 multiple_interaction 多题下拉，
// 否则 vote_interaction 单题投票）。统一用于首发与置灰更新（更新帧须与原卡保持 card_type/
// task_id/submit_button.key 一致，同一 builder 天然保证；submit_button 更新帧不可省略——
// errcode=42049 真机实证，置灰置 checkbox.disable / select_list[].disable 并改按钮文案）。
func buildTemplateCard(spec bot.CardSpec) (aibottypes.TemplateCard, error) {
	if err := bot.ValidateCardTaskID(spec.TaskID); err != nil {
		return aibottypes.TemplateCard{}, err
	}
	if len(spec.Questions) > 0 {
		return buildMultipleInteractionCard(spec)
	}
	return buildVoteCard(spec)
}

// buildVoteCard 单题投票卡（vote_interaction checkbox）。
func buildVoteCard(spec bot.CardSpec) (aibottypes.TemplateCard, error) {
	if len(spec.Options) == 0 {
		return aibottypes.TemplateCard{}, errors.New("卡片选项列表为空（企微 checkbox 须至少一项）")
	}
	if len(spec.Options) > cardVoteOptionMax {
		return aibottypes.TemplateCard{}, fmt.Errorf("卡片选项超过上限 %d（企微 checkbox option_list [1,%d]）", cardVoteOptionMax, cardVoteOptionMax)
	}
	options := make([]templateCardOption, 0, len(spec.Options))
	for i, opt := range spec.Options {
		options = append(options, templateCardOption{
			Id:   encodeActionKey(spec.TaskID, i),
			Text: truncateRunes(opt.Text, cardOptionMaxRunes),
			// 单选默认选中第一项（Gold-Band PoC 校准：单选卡须有选中项，方案阶段 3 场景
			// 映射拍板）；多选不预选。
			IsChecked: !spec.Multiple && i == 0,
		})
	}
	card := aibottypes.TemplateCard{
		CardType:  aibottypes.TemplateCardType.VoteInteraction,
		MainTitle: &aibottypes.TemplateCardMainTitle{Title: truncateRunes(spec.Title, cardTitleMaxRunes), Desc: truncateRunes(spec.Description, cardDescMaxRunes)},
		Checkbox: &aibottypes.TemplateCardCheckbox{
			QuestionKey: spec.TaskID,
			Mode:        boolToInt(spec.Multiple), // 0 单选，1 多选
			Disable:     spec.Disabled,
			OptionList:  options,
		},
		TaskId: spec.TaskID,
	}
	// 提交按钮恒有（协议必填，首发与更新帧同规）：官方模板卡片类型规格中 vote_interaction
	// 的 submit_button 为必须字段——长连接真机校验 errcode=42049「submit_button.text
	// Missing or Invalid」实证，且对 aibot_respond_update_msg 置灰帧同样成立（曾试置灰帧
	// 省略提交按钮以去可点元素，被同码拒绝后回退）。text≤10 字、key 与 TaskID 同源可反解；
	// spec.Submit 退化为纯应答语义标记（应答取自提交回传勾选集），不再决定按钮有无。置灰帧
	// 文案改「已处理」传达消费态（见 cardSubmitTextFor）。
	card.SubmitButton = &aibottypes.TemplateCardSubmitButton{Text: cardSubmitTextFor(spec.Disabled), Key: encodeSubmitKey(spec.TaskID)}
	return card, nil
}

// buildMultipleInteractionCard 多题下拉卡（multiple_interaction select_list）：题目定位键
// 按位置序生成、选项 id 用纯下标（各题 option_list 独立命名空间，卡内唯一即满足协议；下拉
// 选择不触发独立点击事件，无需 event_key 反解）。多题下拉必选——默认选中首项（不填或错填
// 企微也回落第一个，显式给值保证首发与置灰重建一致）。
func buildMultipleInteractionCard(spec bot.CardSpec) (aibottypes.TemplateCard, error) {
	if len(spec.Questions) > cardSelectQuestionMax {
		return aibottypes.TemplateCard{}, fmt.Errorf("卡片题目超过上限 %d（企微 select_list ≤%d 题）", cardSelectQuestionMax, cardSelectQuestionMax)
	}
	selectList := make([]aibottypes.TemplateCardSelectionItem, 0, len(spec.Questions))
	for qi, question := range spec.Questions {
		if len(question.Options) == 0 {
			return aibottypes.TemplateCard{}, errors.New("卡片题目选项列表为空（企微 select_list 须至少一项）")
		}
		if len(question.Options) > cardSelectOptionMax {
			return aibottypes.TemplateCard{}, fmt.Errorf("卡片题目选项超过上限 %d（企微 select_list option_list [1,%d]）", cardSelectOptionMax, cardSelectOptionMax)
		}
		options := make([]templateSelectOption, 0, len(question.Options))
		for oi, opt := range question.Options {
			options = append(options, templateSelectOption{
				Id:   strconv.Itoa(oi),
				Text: truncateRunes(opt.Text, cardSelectOptionMaxRunes),
			})
		}
		selectList = append(selectList, aibottypes.TemplateCardSelectionItem{
			QuestionKey: cardQuestionKey(qi),
			Title:       truncateRunes(question.Title, cardSelectTitleMaxRunes),
			Disable:     spec.Disabled,
			SelectedId:  options[0].Id,
			OptionList:  options,
		})
	}
	return aibottypes.TemplateCard{
		CardType:     aibottypes.TemplateCardType.MultipleInteraction,
		MainTitle:    &aibottypes.TemplateCardMainTitle{Title: truncateRunes(spec.Title, cardTitleMaxRunes), Desc: truncateRunes(spec.Description, cardDescMaxRunes)},
		SelectList:   selectList,
		SubmitButton: &aibottypes.TemplateCardSubmitButton{Text: cardSubmitTextFor(spec.Disabled), Key: encodeSubmitKey(spec.TaskID)},
		TaskId:       spec.TaskID,
	}, nil
}

// parseEventKey 企微 event_key → 中立 Interaction。last-colon 切分（TaskID 字符集无冒号，
// 生产 key 的 TaskID 段必无歧义；外来 key 含多个冒号时取最右冒号，多冒号段整体作
// DeliveryID——与生产编码的反解语义一致）。taskID 为渠道回传的 task_id 原样（置灰对照锚）。
func parseEventKey(eventKey, taskID string) bot.Interaction {
	in := bot.Interaction{TaskID: taskID, RawKey: eventKey, ActionIndex: -1}
	i := strings.LastIndex(eventKey, ":")
	if i < 0 {
		return in
	}
	in.DeliveryID = eventKey[:i]
	if n, err := strconv.Atoi(eventKey[i+1:]); err == nil && n >= 0 {
		in.ActionIndex = n
	}
	return in
}

// normalizeSelections 提交事件的勾选集归一（SDK selected_items → 中立 Selections）：选项 id
// 剥 `<TaskID>:` 前缀得下标（vote 选项 id 形态），无前缀时按纯下标解析（多题下拉选项 id
// 形态）；无法解析的选项丢弃，全部无效的题整题跳过。无勾选集（纯点击型事件）返回 nil。
func normalizeSelections(items *aibottypes.TemplateCardSelectedItems, deliveryID string) []bot.InteractionSelection {
	if items == nil || len(items.SelectedItem) == 0 {
		return nil
	}
	prefix := ""
	if deliveryID != "" {
		prefix = deliveryID + ":"
	}
	var selections []bot.InteractionSelection
	for _, item := range items.SelectedItem {
		if item.OptionIds == nil || len(item.OptionIds.OptionId) == 0 {
			continue // 无选中项的题不参与（防御）
		}
		var indexes []int
		for _, id := range item.OptionIds.OptionId {
			if n, ok := parseOptionIndex(id, prefix); ok {
				indexes = append(indexes, n)
			}
		}
		if len(indexes) == 0 {
			continue
		}
		selections = append(selections, bot.InteractionSelection{QuestionKey: item.QuestionKey, OptionIndexes: indexes})
	}
	return selections
}

// parseOptionIndex 渠道选项 id → 下标：vote 形态 `<TaskID>:<i>`（剥前缀）、多题下拉形态
// `<i>`（纯下标）。负数/非数字返回 false。
func parseOptionIndex(id, prefix string) (int, bool) {
	s := id
	if prefix != "" && strings.HasPrefix(s, prefix) {
		s = s[len(prefix):]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// wsRoute 长连接帧路由载荷（消息流/交互事件流共用）：headers 供 respond_msg 系回复透传——
// 消息流文本走 aibot_respond_msg 覆写流、事件流置灰更新走 aibot_respond_update_msg（须用
// 事件 req_id，5 秒窗口内）；chatID/chatType 供 aibot_send_msg 主动推送——事件流终帧（事件
// req_id 不可用于 respond_msg，真机 errcode=846605 实证；官方对点击事件只文档化了更新卡片
// 一条应答通道）与消息流的卡片解耦推送（卡帧夹在本条回复流里会被企微与亚秒级占位/终帧
// 合并投递，「正在思考…」占位被吞，真机实证——卡走独立气泡，见 reply.go 的 SendCard）。
// 消息流也带推送面：文本终帧仍走 respond_msg 保流式，仅卡走推送。
type wsRoute struct {
	headers  aibottypes.WsFrameHeaders
	chatID   string // 推送目标：单聊 = 点击者/发送者 userid，群聊 = 会话 chatid
	chatType int    // 官方 chat_type 枚举：1 单聊 / 2 群聊
	event    bool   // 交互事件流：终帧走主动推送；消息流终帧仍走 aibot_respond_msg 覆写流
}

// wsRouteFrom 帧 → 路由载荷。chat_type 显式指定：官方规范不填时优先按群聊解析，单聊
// userid 会被误解析。
func wsRouteFrom(headers aibottypes.WsFrameHeaders, base aibottypes.BaseMessage, event bool) wsRoute {
	if base.ChatType == "group" {
		return wsRoute{headers: headers, chatID: base.ChatId, chatType: 2, event: event}
	}
	return wsRoute{headers: headers, chatID: base.From.UserId, chatType: 1, event: event}
}

// cardEventPayload 模板卡片事件帧 → (合成 BaseMessage, Interaction)。SDK dispatch 已预调
// DecodeEvent，回调内 Body.Event 即 TemplateCardEventData；事件帧无 Quote/Text，合成
// BaseMessage 供 submitMsg 复用 fillCommon（会话键/发送者/路由归一）。false = 非
// 模板卡事件（解码失败/形态异常），调用方丢弃。
func cardEventPayload(body aibottypes.EventMessage) (aibottypes.BaseMessage, bot.Interaction, bool) {
	ev, ok := body.Event.(aibottypes.TemplateCardEventData)
	if !ok {
		return aibottypes.BaseMessage{}, bot.Interaction{}, false
	}
	base := aibottypes.BaseMessage{
		MsgId:      body.MsgId,
		AibotId:    body.AibotId,
		ChatId:     body.ChatId,
		ChatType:   body.ChatType,
		From:       aibottypes.MessageFrom{UserId: body.From.UserId},
		MsgType:    body.MsgType,    // 事件回调固定 "event"
		CreateTime: body.CreateTime, // 事件产生时间戳透传（fillCommon 入站延迟定标消费）
	}
	interaction := parseEventKey(ev.EventKey, ev.TaskId)
	interaction.Selections = normalizeSelections(ev.SelectedItems, interaction.DeliveryID)
	return base, interaction, true
}

// truncateRunes rune 级截断（超限截到 max，防中文按字节截半）。
func truncateRunes(s string, max int) string {
	if rs := []rune(s); len(rs) > max {
		return string(rs[:max])
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// 编译期断言：replyStream 具备卡能力（无卡渠道的流不实现本接口，编排器置灰步类型探测跳过）。
var _ bot.CardReplyStream = (*replyStream)(nil)

// SendCard 实现 bot.CardReplyStream：先文本后卡——文本帧照旧走 aibot_respond_msg 覆写流
// （final=true 即终流，流式回归纯文本原始逻辑），卡在流收尾后作为独立跟进消息走
// aibot_send_msg 推送（不占回复流、不受「同消息一卡」限制）。时序拍板（真机定标：服务端
// 全通道亚秒级 ack、企微客户端对快速连发的多通道消息合并渲染，服务端排序改变不了客户端
// 节奏）只对齐「流答复完、卡片跟进」的自然消息序。失败语义：文本帧失败即返回（卡未发，
// 调用方回落 Flush 重试文本）；卡推送失败时文本已终流送达，返回错误仅供上游感知（调用方
// 回落 Flush 被幂等吞掉，无重复文本）。无推送面（旧 headers 载荷/测试装配）卡回落
// respond_msg 独立卡帧。
func (s *replyStream) SendCard(spec bot.CardSpec, content string, final bool) error {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return nil // 终流后的迟到调用幂等忽略（与 Flush 同款防御）
	}
	if s.cardAttached {
		s.mu.Unlock()
		return errors.New("本条消息的卡片已发送（企微同一消息只能回复一次卡片）")
	}
	client := s.rt.clientSnapshot()
	s.mu.Unlock()
	if client == nil {
		return errors.New("企微连接已关闭，无法发送卡片")
	}
	card, err := buildTemplateCard(spec)
	if err != nil {
		return err
	}
	// 文本帧先行（不经 s.Flush：其 final 失败也会置 finished，随后调用方回落 Flush 将被幂等
	// 防御吞掉——此处仅成功时置位，失败交调用方回落重试，卡未发）。非终帧与 Flush 同款非
	// 阻塞背压，ErrReplySkipped 视作送达。
	if final {
		_, err = client.ReplyStream(s.headers, s.streamId, content, true, nil, nil)
	} else {
		_, err = client.ReplyStreamNonBlocking(s.headers, s.streamId, content, false, nil, nil)
		if errors.Is(err, aibot.ErrReplySkipped) {
			err = nil
		}
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	if final {
		s.finished = true
	}
	s.mu.Unlock()
	if s.push != nil {
		_, err = client.SendMessage(s.push.chatID, pushCardBody{ChatType: s.push.chatType, MsgType: "template_card", TemplateCard: card})
	} else {
		_, err = client.ReplyTemplateCard(s.headers, card, nil)
	}
	s.mu.Lock()
	if err == nil {
		s.cardAttached = true
	}
	s.mu.Unlock()
	return err
}

// UpdateCard 实现 bot.CardReplyStream：置灰更新（TaskId 须与回调 task_id 一致，须在点击
// 事件 5 秒窗口内以事件帧 headers 发起——本流即由该事件帧开出，天然满足）。与流终态正交：
// 编排器保证置灰先于终帧，此处不做 finished 防御。
func (s *replyStream) UpdateCard(spec bot.CardSpec) error {
	client := s.rt.clientSnapshot()
	if client == nil {
		return errors.New("企微连接已关闭，无法更新卡片")
	}
	card, err := buildTemplateCard(spec)
	if err != nil {
		return err
	}
	_, err = client.UpdateTemplateCard(s.headers, card, nil)
	return err
}
