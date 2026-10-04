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

// cardSubmitText 提交按钮文案（≤10 字）。
const cardSubmitText = "提交"

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
// 否则 vote_interaction 单题投票）。统一用于首发与置灰更新（更新帧须与原卡保持
// card_type/task_id/submit_button.key 一致，同一 builder 天然保证；submit_button 无 disable
// 字段，置灰只置 checkbox.disable / select_list[].disable）。
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
	if spec.Multiple || spec.Submit {
		// 多选与提交型卡需要提交按钮（T3.3 统一 submit 语义：单选点击只是改选，应答取自
		// 提交回传的勾选集）；text≤10 字、key 与 TaskID 同源可反解。
		card.SubmitButton = &aibottypes.TemplateCardSubmitButton{Text: cardSubmitText, Key: encodeSubmitKey(spec.TaskID)}
	}
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
		SubmitButton: &aibottypes.TemplateCardSubmitButton{Text: cardSubmitText, Key: encodeSubmitKey(spec.TaskID)},
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
		MsgId:    body.MsgId,
		AibotId:  body.AibotId,
		ChatId:   body.ChatId,
		ChatType: body.ChatType,
		From:     aibottypes.MessageFrom{UserId: body.From.UserId},
		MsgType:  body.MsgType, // 事件回调固定 "event"
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

// SendCard 实现 bot.CardReplyStream：流文本与卡同帧下发（ReplyStreamWithCard 双渲染单
// 事件，非卡渠道的降级文案由上层双参签名保证不丢失）。final=true 时一并终流；协议限
// 同一消息只回一次卡，重复调用报错。
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
	_, err = client.ReplyStreamWithCard(s.headers, s.streamId, content, final, aibot.ReplyStreamWithCardOptions{TemplateCard: &card})
	s.mu.Lock()
	if err == nil {
		s.cardAttached = true
		if final {
			s.finished = true
		}
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
