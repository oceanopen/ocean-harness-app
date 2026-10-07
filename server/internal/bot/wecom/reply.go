package wecom

import (
	"encoding/json"
	"errors"
	"sync"

	aibot "github.com/oceanopen/wecom-aibot-go-sdk/aibot"
	aibottypes "github.com/oceanopen/wecom-aibot-go-sdk/aibot/types"

	"ocean-harness/server/internal/bot"
)

// replyClient 回复流出站面：replyStream（Flush/SendCard/UpdateCard）消费的 SDK 客户端
// 接口。生产实现 *aibot.WsClient；接缝为单测注入 fake——SendCard 拆帧顺序与失败语义
// （组合 msgtype 被企微静默丢卡事故的回归面）据此可测。
type replyClient interface {
	ReplyStream(frame aibottypes.WsFrameHeaders, streamId, content string, finish bool, msgItem []aibottypes.ReplyMsgItem, feedback *aibottypes.ReplyFeedback) (*aibottypes.WsFrame[json.RawMessage], error)
	ReplyStreamNonBlocking(frame aibottypes.WsFrameHeaders, streamId, content string, finish bool, msgItem []aibottypes.ReplyMsgItem, feedback *aibottypes.ReplyFeedback) (*aibottypes.WsFrame[json.RawMessage], error)
	ReplyTemplateCard(frame aibottypes.WsFrameHeaders, card aibottypes.TemplateCard, feedback *aibottypes.ReplyFeedback) (*aibottypes.WsFrame[json.RawMessage], error)
	UpdateTemplateCard(frame aibottypes.WsFrameHeaders, card aibottypes.TemplateCard, userIds []string) (*aibottypes.WsFrame[json.RawMessage], error)
	SendMessage(chatid string, body any) (*aibottypes.WsFrame[json.RawMessage], error)
}

// OpenReply 实现 bot.ChannelRuntime：由入站帧路由开一条覆写式流（同 streamId 多次
// ReplyStream = 服务端整条覆盖刷新）。streamId 每消息独立（aibot.GenerateReqId）。载荷
// 统一为 wsRoute（消息流/事件流均带推送面，卡解耦推送共用；事件流终帧另走推送——见
// wsRoute 注释）；旧 headers 载荷保留兼容（无推送面，卡与终帧全回落 respond_msg）。
func (r *channelRuntime) OpenReply(route bot.RouteInfo) (bot.ReplyStream, error) {
	switch p := route.Payload.(type) {
	case wsRoute:
		return &replyStream{rt: r, headers: p.headers, streamId: aibot.GenerateReqId("stream"),
			push: &pushTarget{chatID: p.chatID, chatType: p.chatType}, viaEvent: p.event}, nil
	case aibottypes.WsFrameHeaders:
		return &replyStream{rt: r, headers: p, streamId: aibot.GenerateReqId("stream")}, nil
	default:
		return nil, errors.New("wecom 路由信息类型异常（须为 wsRoute 或 WsFrameHeaders）")
	}
}

// replyStream bot.ReplyStream 的企微落地。终帧恰好一次由核心回复泵保证；本层只做
// 客户端快照与 finished 幂等防御。cardAttached 标记本流已发过卡（一回合一卡，重复调用
// 报错，见 card.go 的 SendCard）。push 非 nil 即带推送面（wsRoute 开出）：卡一律在流收尾
// 后走 aibot_send_msg 独立推送（不占回复流）；终帧通道由 viaEvent 分流——事件流（事件
// req_id 不可用于 respond_msg，真机 errcode=846605 实证）走推送，消息流仍走
// aibot_respond_msg 覆写流（保流式）。
type replyStream struct {
	rt       *channelRuntime
	headers  aibottypes.WsFrameHeaders
	streamId string
	push     *pushTarget // 主动推送目标（事件流终帧 / 卡解耦推送共用）；nil = 无推送面（旧 headers 载荷）
	viaEvent bool        // 交互事件流：终帧走主动推送；消息流终帧走 respond_msg 覆写流

	mu           sync.Mutex
	finished     bool
	cardAttached bool
}

// pushTarget aibot_send_msg 主动推送目标（事件流终帧 / 卡解耦推送共用）。
type pushTarget struct {
	chatID   string
	chatType int
}

// pushMarkdownBody aibot_send_msg 的 markdown 消息体（官方字段规范；markdown 渲染与流式
// 文本一致）。SDK 的 SendMarkdownMsgBody 缺 chat_type 字段（不填时企微优先按群聊解析，
// 单聊 userid 会被误解析），SendMessage(chatid, body any) 接口开放，适配器自构造零 SDK
// 改动。
type pushMarkdownBody struct {
	ChatType int    `json:"chat_type"`
	MsgType  string `json:"msgtype"` // 固定 markdown
	Markdown struct {
		Content string `json:"content"`
	} `json:"markdown"`
}

// pushCardBody aibot_send_msg 的模板卡消息体（官方字段规范；chat_type 缺省陷阱同上）。
type pushCardBody struct {
	ChatType     int                     `json:"chat_type"`
	MsgType      string                  `json:"msgtype"` // 固定 template_card
	TemplateCard aibottypes.TemplateCard `json:"template_card"`
}

// Flush 实现 bot.ReplyStream。final=false 走 NonBlocking（上一帧未 ack 时跳过——协议固有
// ~5s ack 延迟下这是官方背压姿势）；final=true 阻塞保证送达。事件流（viaEvent）final 恒
// 走主动推送（编排器交互步对点击恰发一次终帧，无中间帧面）；消息流（含带推送面的）走
// aibot_respond_msg 保流式。
func (s *replyStream) Flush(content string, final bool) error {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return nil // 终帧后的迟到 Flush 幂等忽略
	}
	client := s.rt.clientSnapshot()
	s.mu.Unlock()
	if client == nil {
		return errors.New("企微连接已关闭，无法发送")
	}

	if final {
		var err error
		if s.viaEvent && s.push != nil {
			body := pushMarkdownBody{ChatType: s.push.chatType, MsgType: "markdown"}
			body.Markdown.Content = content
			_, err = client.SendMessage(s.push.chatID, body)
		} else {
			_, err = client.ReplyStream(s.headers, s.streamId, content, true, nil, nil)
		}
		s.mu.Lock()
		s.finished = true
		s.mu.Unlock()
		return err
	}
	_, err := client.ReplyStreamNonBlocking(s.headers, s.streamId, content, false, nil, nil)
	return err
}

// ByteLimit 实现 bot.ReplyStream。
func (s *replyStream) ByteLimit() int { return byteLimit }
