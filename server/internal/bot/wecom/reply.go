package wecom

import (
	"errors"
	"sync"

	aibot "github.com/oceanopen/wecom-aibot-go-sdk/aibot"
	aibottypes "github.com/oceanopen/wecom-aibot-go-sdk/aibot/types"

	"ocean-harness/server/internal/bot"
)

// OpenReply 实现 bot.ChannelRuntime：由入站帧 headers 开一条覆写式流（同 streamId 多次
// ReplyStream = 服务端整条覆盖刷新）。streamId 每消息独立（aibot.GenerateReqId）。
func (r *channelRuntime) OpenReply(route bot.RouteInfo) (bot.ReplyStream, error) {
	headers, ok := route.Payload.(aibottypes.WsFrameHeaders)
	if !ok {
		return nil, errors.New("wecom 路由信息类型异常（须为 WsFrameHeaders）")
	}
	return &replyStream{rt: r, headers: headers, streamId: aibot.GenerateReqId("stream")}, nil
}

// replyStream bot.ReplyStream 的企微落地。终帧恰好一次由核心回复泵保证；本层只做
// 客户端快照与 finished 幂等防御。
type replyStream struct {
	rt       *channelRuntime
	headers  aibottypes.WsFrameHeaders
	streamId string

	mu       sync.Mutex
	finished bool
}

// Flush 实现 bot.ReplyStream。final=false 走 NonBlocking（上一帧未 ack 时跳过——协议固有
// ~5s ack 延迟下这是官方背压姿势）；final=true 阻塞保证送达。
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
		_, err := client.ReplyStream(s.headers, s.streamId, content, true, nil, nil)
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
