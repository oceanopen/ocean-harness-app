package weixin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ocean-harness/server/internal/bot"
)

/*
 * 出站节奏与打字态参数：sendInterval 相邻出站间隔（防触发频控）；打字态 5s 一 poke、
 * 票据 60s 强制换新；replyByteLimit 交核心回复泵截断的上限（协议 1800 字符/条由分段消化）。
 */
const (
	sendInterval       = 300 * time.Millisecond
	typingPokeEvery    = 5 * time.Second
	typingMaxTicketAge = 60 * time.Second
	replyByteLimit     = 200000
	partialFailNotice  = "部分内容发送失败，请稍后重试或查看机器人日志。"
)

// imageExts 按扩展名判图（命中走图片通道，失败降级文件通道）。
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true,
}

// 编译期断言：replyStream 同时满足核心流式回复与产物投递契约。
var (
	_ bot.ReplyStream         = (*replyStream)(nil)
	_ bot.ArtifactReplyStream = (*replyStream)(nil)
)

// OpenReply 实现 bot.ChannelRuntime：校验路由载荷与连接存活性，开出回复流。
func (r *channelRuntime) OpenReply(route bot.RouteInfo) (bot.ReplyStream, error) {
	p, ok := route.Payload.(replyRoute)
	if !ok || p.ToUserID == "" {
		return nil, errors.New("微信路由信息类型异常")
	}
	client := r.clientSnapshot()
	if client == nil {
		return nil, errors.New("微信连接已关闭，无法回复")
	}
	return &replyStream{rt: r, route: p, client: client}, nil
}

/*
 * replyStream bot.ReplyStream 的微信落地：iLink 无覆写流语义——中间帧仅驱动「对方正在输入」
 * 打字态，终帧一次性分段发送；产物经 SendArtifact 独立通道。终帧恰好一次由核心回复泵保证，
 * 本层只做 finished 幂等防御。
 */
type replyStream struct {
	rt     *channelRuntime
	route  replyRoute
	client *apiClient

	mu       sync.Mutex
	finished bool
	ticket   string
	ticketAt time.Time
	lastPoke time.Time
}

// Flush 实现 bot.ReplyStream：中间帧推进打字态（尽力而为，失败静默）；终帧分段发送，
// 部分失败时补发一条交代（全部失败返回末次错误交泵处理）。
func (s *replyStream) Flush(content string, final bool) error {
	if !final {
		s.pokeTyping()
		return nil
	}
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return nil // 终帧后的迟到 Flush 幂等忽略
	}
	s.finished = true
	s.mu.Unlock()
	s.stopTyping()
	return s.sendFinal(content)
}

// sendFinal 终帧分段节流发送；failed>0 且有成功时补发交代文案。
func (s *replyStream) sendFinal(content string) error {
	chunks := splitText(strings.TrimSpace(content), maxMessageChars)
	failed := 0
	var lastErr error
	for _, chunk := range chunks {
		if err := s.sendTextWithRetry(chunk); err != nil {
			failed++
			lastErr = err
		}
	}
	if failed > 0 && failed < len(chunks) {
		_ = s.sendTextWithRetry(partialFailNotice) // 交代尽力而为
	}
	if failed == len(chunks) && len(chunks) > 0 {
		return lastErr
	}
	return nil
}

/*
 * retryableSendErr 收到 HTTP 响应的失败可重试一次（429/5xx 瞬态，4xx 确定性拒绝重试无益
 * 但无害）；网络/超时无服务端回执 = 结果不确定，禁止重试（防重复消息）；token 失效为终态。
 */
func retryableSendErr(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	return ae.Code == errHTTP || ae.Code == errRejected
}

func (s *replyStream) sendTextWithRetry(text string) error {
	err := s.rt.sendTextThrottled(s.client, s.route, text)
	if err == nil || !retryableSendErr(err) {
		return err
	}
	return s.rt.sendTextThrottled(s.client, s.route, text)
}

/*
 * pokeTyping 推进「对方正在输入」：5s 节流 poke，票据超 60s 先换新。全部尽力而为，
 * 失败静默（打字态只是装饰，不阻塞回复主链路）。
 */
func (s *replyStream) pokeTyping() {
	now := time.Now()
	s.mu.Lock()
	if s.finished || now.Sub(s.lastPoke) < typingPokeEvery {
		s.mu.Unlock()
		return
	}
	ticket, needFetch := s.ticket, s.ticket == "" || now.Sub(s.ticketAt) > typingMaxTicketAge
	s.mu.Unlock()

	ctx := context.Background()
	token, baseURL := s.rt.token(), s.rt.baseURLValue()
	if needFetch {
		fresh, err := s.client.GetConfig(ctx, token, baseURL, s.route.ToUserID, s.route.ContextToken)
		if err != nil {
			return
		}
		ticket = fresh
		s.mu.Lock()
		s.ticket, s.ticketAt = ticket, now
		s.mu.Unlock()
	}
	if err := s.client.SendTyping(ctx, token, baseURL, s.route.ToUserID, ticket, 1); err != nil {
		return
	}
	s.mu.Lock()
	s.lastPoke = now
	s.mu.Unlock()
}

// stopTyping 结束打字态（尽力而为；票据缺席 = 从未 poke，无需结束）。
func (s *replyStream) stopTyping() {
	s.mu.Lock()
	ticket := s.ticket
	s.ticket = ""
	s.mu.Unlock()
	if ticket == "" {
		return
	}
	_ = s.client.SendTyping(context.Background(), s.rt.token(), s.rt.baseURLValue(),
		s.route.ToUserID, ticket, 2)
}

/*
 * SendArtifact 实现 bot.ArtifactReplyStream：图片扩展名先走图片通道，确定性失败降级文件
 * 通道重投（结果不确定禁止换通道——可能已投出，换通道必然重复）。
 */
func (s *replyStream) SendArtifact(deliveryKey string, name string, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) > maxMediaBytes {
		return errors.New("微信产物超过 100MB 投递上限")
	}
	art := &Artifact{FileName: filepath.Base(name), Bytes: data, DeliveryKey: deliveryKey}
	if imageExts[strings.ToLower(filepath.Ext(name))] {
		err = s.rt.sendArtifactThrottled(s.client, s.route, art, 1)
		if err == nil {
			return nil
		}
		var ae *APIError
		if errors.As(err, &ae) && ae.Code == errUncertain {
			return err
		}
	}
	return s.rt.sendArtifactThrottled(s.client, s.route, art, 3)
}

// SendNotice 实现 bot.ArtifactReplyStream：复用文本通道（节流 + 一次重试）。
func (s *replyStream) SendNotice(text string) error { return s.sendTextWithRetry(text) }

// ByteLimit 实现 bot.ReplyStream。
func (s *replyStream) ByteLimit() int { return replyByteLimit }

// paceSend 出站节奏：与上一次出站启动点至少间隔 sendInterval（须持 sendMu）。
func (r *channelRuntime) paceSend() {
	if wait := sendInterval - time.Since(r.lastSend); wait > 0 {
		time.Sleep(wait)
	}
	r.lastSend = time.Now()
}

// sendTextThrottled 单条文本出站（全局串行 + 节流）。
func (r *channelRuntime) sendTextThrottled(client *apiClient, route replyRoute, text string) error {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()
	r.paceSend()
	_, err := client.SendText(context.Background(), r.token(), r.baseURLValue(),
		route.ToUserID, text, route.ContextToken)
	return err
}

// sendArtifactThrottled 媒体产物出站（与文本共用节奏；CDN 上传内部已含重试）。
func (r *channelRuntime) sendArtifactThrottled(client *apiClient, route replyRoute, art *Artifact, mediaType int) error {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()
	r.paceSend()
	_, err := client.SendArtifact(context.Background(), r.token(), r.baseURLValue(),
		route.ToUserID, art, mediaType, route.ContextToken)
	return err
}
