// card.go 企微模板卡片通道落地（T3.1）：CardSpec ⇄ 企微 vote_interaction 卡的双向翻译 +
// replyStream 的 bot.CardReplyStream 实现。出站 key 编码（`TaskID:<下标>` / `TaskID:submit`）
// 与入站反解（last-colon 切分）双向同源，TaskID 字符集不含冒号保证切分无歧义。
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

// 企微 vote_interaction 卡文案上限（rune）：主标题 26 字、标题辅助 30 字、选项 11 字。
const (
	cardTitleMaxRunes  = 26
	cardDescMaxRunes   = 30
	cardOptionMaxRunes = 11
)

// cardSubmitText 多选提交按钮文案（≤10 字）。
const cardSubmitText = "提交"

// templateCardOption SDK Checkbox.OptionList 匿名结构体的本地别名（字段名/类型/tag 须与
// SDK 声明逐项一致才可赋值；别名只为字面量构造可写）。
type templateCardOption = struct {
	Id        string `json:"id"`
	Text      string `json:"text"`
	IsChecked bool   `json:"is_checked,omitempty"`
}

// encodeActionKey 选项点击的 event_key：`TaskID:<下标>`。
func encodeActionKey(taskID string, index int) string {
	return taskID + ":" + strconv.Itoa(index)
}

// encodeSubmitKey 多选提交按钮的 event_key：`TaskID:submit`。
func encodeSubmitKey(taskID string) string {
	return taskID + ":submit"
}

// buildTemplateCard CardSpec → 企微 vote_interaction 卡。统一用于首发与置灰更新
// （更新帧须与原卡保持 card_type/task_id/submit_button.key 一致，同一 builder 天然保证；
// submit_button 无 disable 字段，置灰只置 checkbox.disable）。
func buildTemplateCard(spec bot.CardSpec) (aibottypes.TemplateCard, error) {
	if err := validateCardTaskID(spec.TaskID); err != nil {
		return aibottypes.TemplateCard{}, err
	}
	if len(spec.Options) == 0 {
		return aibottypes.TemplateCard{}, errors.New("卡片选项列表为空（企微 checkbox 须至少一项）")
	}
	options := make([]templateCardOption, 0, len(spec.Options))
	for i, opt := range spec.Options {
		options = append(options, templateCardOption{
			Id:   encodeActionKey(spec.TaskID, i),
			Text: truncateRunes(opt.Text, cardOptionMaxRunes),
		})
	}
	card := aibottypes.TemplateCard{
		CardType:  aibottypes.TemplateCardType.VoteInteraction,
		MainTitle: &aibottypes.TemplateCardMainTitle{Title: truncateRunes(spec.Title, cardTitleMaxRunes), Desc: truncateRunes(spec.Description, cardDescMaxRunes)},
		Checkbox: &aibottypes.TemplateCardCheckbox{
			QuestionKey: spec.TaskID,
			Mode:        boolToInt(spec.Multiple), // 0 单选（点击即答），1 多选
			Disable:     spec.Disabled,
			OptionList:  options,
		},
		TaskId: spec.TaskID,
	}
	if spec.Multiple {
		// 仅多选需要提交按钮（单选点击即回调）；text≤10 字、key 与 TaskID 同源可反解。
		card.SubmitButton = &aibottypes.TemplateCardSubmitButton{Text: cardSubmitText, Key: encodeSubmitKey(spec.TaskID)}
	}
	return card, nil
}

// validateCardTaskID 企微 task_id 契约：非空、≤128 字节、字符集 [0-9A-Za-z_-@]（不含冒号
// 是本通道 key 编码无歧义的前提）。
func validateCardTaskID(taskID string) error {
	if taskID == "" {
		return errors.New("卡片 TaskID 不能为空")
	}
	if len(taskID) > 128 {
		return errors.New("卡片 TaskID 超过 128 字节上限")
	}
	for _, c := range taskID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '-' || c == '@') {
			return fmt.Errorf("卡片 TaskID 含非法字符 %q（仅允许数字/字母/_-@）", c)
		}
	}
	return nil
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
	return base, parseEventKey(ev.EventKey, ev.TaskId), true
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
