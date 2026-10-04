// Package wecom 企业微信「智能机器人」渠道适配器：实现 bot 包的 ChannelRuntime/Factory 契约。
// 职责边界：连接生命周期（WsClient 封装 + 状态投影）、入站帧规范化（含附件同步下载落盘）、
// 覆写式回复流落地。编排/幂等/白名单/prompt 全在 bot 核心，本包不做业务决策。
package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	aibot "github.com/oceanopen/wecom-aibot-go-sdk/aibot"
	aibottypes "github.com/oceanopen/wecom-aibot-go-sdk/aibot/types"
	"go.uber.org/zap"

	"ocean-harness/server/internal/bot"
	"ocean-harness/server/internal/dal/enums"
)

// byteLimit 企微流式单条内容字节上限（SDK types/api.go 校验值）。
const byteLimit = 20480

// inboundBufferCapacity 入站处理通道容量：ws readLoop 回调必须快进快出，下载/入队串行在本
// 通道的消费 goroutine（保消息顺序）。满即丢（企微 WS 无补投，fail closed 不积压）。
const inboundBufferCapacity = 32

// Factory 渠道工厂（main 装配注册）。
type Factory struct{}

// Channel 实现 bot.Factory。
func (Factory) Channel() enums.Channel { return enums.CHANNEL_WECOM }

// Create 实现 bot.Factory。
func (Factory) Create() (bot.ChannelRuntime, error) { return &channelRuntime{}, nil }

// wecomCredential credential JSON 的 shape（bot 核心不解引用，仅适配器解释）。
type wecomCredential struct {
	BotId  string `json:"botId"`
	Secret string `json:"secret"`
}

// channelRuntime 单个企微 bot 的连接运行时。state/lastErr 由 SDK 回调驱动写入，Status 投影读取。
type channelRuntime struct {
	mu        sync.Mutex
	cfg       bot.BotRuntimeConfig
	client    *aibot.WsClient
	cancel    context.CancelFunc
	state     string // connecting | connected | disconnected | stopped
	lastErr   string
	log       *zap.Logger
	onInbound func(bot.InboundMessage) // supervisor 装配注入的入站出口

	inbound  chan func(*bot.InboundMessage) // 入站构建任务（保序串行消费）
	stopProc chan struct{}
}

// Start 实现 bot.ChannelRuntime：构造 WsClient、装配回调、后台连接（不阻塞）。
func (r *channelRuntime) Start(cfg bot.BotRuntimeConfig, onInbound func(bot.InboundMessage)) error {
	var cred wecomCredential
	if err := json.Unmarshal([]byte(cfg.Credential), &cred); err != nil {
		return fmt.Errorf("credential 解析失败: %w", err)
	}
	if cred.BotId == "" || cred.Secret == "" {
		return errors.New("企微凭据缺失：credential JSON 须含 botId 与 secret")
	}

	log := zap.L().With(zap.String("component", "bot.wecom"), zap.Int("botID", cfg.BotID))
	ctx, cancel := context.WithCancel(context.Background())

	// 幂等防御：先停旧代连接（supervisor 常规路径已保证先 Stop，这里兜底）；断连在锁外完成
	// （同 Stop，见 stopAndRelease 注释），终态窗口内旧代迟到回调经代际守卫全部忽略。
	r.stopAndRelease()

	r.mu.Lock()
	r.cfg = cfg
	r.cancel = cancel
	r.log = log
	r.onInbound = onInbound
	r.state = "connecting"
	r.lastErr = ""
	// 入站通道按 Start 代际创建：processLoop 以参数持有本代通道（见下），旧代 loop 只能
	// 收到旧代 stop 关闭信号退出，绝不消费新代消息——同实例误重入也不会双消费者抢食。
	ch := make(chan func(*bot.InboundMessage), inboundBufferCapacity)
	stop := make(chan struct{})
	r.inbound = ch
	r.stopProc = stop
	client := aibot.NewWsClient(aibottypes.WsClientOptions{
		BotId:  cred.BotId,
		Secret: cred.Secret,
		// 网络断开无限重连（指数退避）；认证失败由 SDK 内置 5 次上限终止（坏 secret 不死循环）。
		MaxReconnectAttempts: -1,
		Logger:               sdkLogger{log: log},
	})
	r.client = client
	r.mu.Unlock()

	// 连接生命周期 → 状态投影（闭包捕获本代 client，经代际守卫写入；旧代迟到的 SDK 回调
	// 不污染新代状态）。
	client.OnAuthenticated = func() { r.setStateForClient(client, "connected", "") }
	client.OnReconnecting = func(int) { r.setStateForClient(client, "connecting", "") }
	client.OnDisconnected = func(reason string) { r.setStateForClient(client, "disconnected", reason) }
	// 被新连接踢下线：终态不重连（SDK 已置 started=false），恢复手段是 UI 重启连接。
	client.OnDisconnectedEvent = func(*aibottypes.WsFrame[aibottypes.EventMessage]) {
		r.setStateForClient(client, "disconnected", "连接被其他会话接管（同一 botId/Secret 在别处建立了新连接）；如需在本机恢复请重启连接")
	}
	client.OnError = func(err error) { log.Warn("企微连接错误", zap.Error(err)) }

	// 消息回调：一律轻投递到处理通道（readLoop 上不做重活）。
	client.OnText = func(frame *aibottypes.WsFrame[aibottypes.TextMessage]) {
		r.submitMsg(frame.Headers, frame.Body.BaseMessage, func(in *bot.InboundMessage) {
			in.Text = frame.Body.Text.Content
		})
	}
	client.OnImage = func(frame *aibottypes.WsFrame[aibottypes.ImageMessage]) {
		r.submitMsg(frame.Headers, frame.Body.BaseMessage, func(in *bot.InboundMessage) {
			r.buildWithAttachment(in, frame.Body.Image.Url, frame.Body.Image.AesKey, "image", "image.png")
		})
	}
	client.OnFile = func(frame *aibottypes.WsFrame[aibottypes.FileMessage]) {
		r.submitMsg(frame.Headers, frame.Body.BaseMessage, func(in *bot.InboundMessage) {
			r.buildWithAttachment(in, frame.Body.File.Url, frame.Body.File.AesKey, "file", "file.bin")
		})
	}
	client.OnVoice = func(frame *aibottypes.WsFrame[aibottypes.VoiceMessage]) {
		r.submitMsg(frame.Headers, frame.Body.BaseMessage, func(in *bot.InboundMessage) {
			in.Text = frame.Body.Voice.Content // 语音转写文本直接作为正文
		})
	}
	client.OnMixed = func(frame *aibottypes.WsFrame[aibottypes.MixedMessage]) {
		r.submitMsg(frame.Headers, frame.Body.BaseMessage, func(in *bot.InboundMessage) {
			r.buildMixed(frame.Body.Mixed, in)
		})
	}
	// 模板卡片点击事件（T3.1）：SDK dispatch 已预调 DecodeEvent，回调内 Body.Event 即具体
	// 事件类型。归一为合成 BaseMessage + Interaction 后轻投递（与消息回调同款），重活
	// （拦截步判定/置灰更新）在编排器侧完成；解码失败丢弃并告警（无 msgid 不可幂等）。
	client.OnTemplateCardEvent = func(frame *aibottypes.WsFrame[aibottypes.EventMessage]) {
		base, interaction, ok := cardEventPayload(frame.Body)
		if !ok {
			log.Warn("企微模板卡片事件解码失败，已丢弃", zap.String("msgid", frame.Body.MsgId))
			return
		}
		r.submitMsg(frame.Headers, base, func(in *bot.InboundMessage) {
			in.Interaction = &interaction
		})
	}

	// 入站处理 goroutine：保序串行消费（下载在 5 分钟有效 URL 约束下同步完成）。
	go r.processLoop(ch, stop)

	go func() {
		if err := client.Connect(ctx); err != nil {
			// Connect 返回 = 认证失败重试耗尽或 ctx 取消；网络中断由回调驱动状态。
			r.setStateForClient(client, "disconnected", err.Error())
			log.Error("企微连接终止", zap.Error(err))
		}
	}()
	return nil
}

// Stop 实现 bot.ChannelRuntime：断开连接 + 停处理通道（幂等）。
func (r *channelRuntime) Stop() { r.stopAndRelease() }

// stopAndRelease 停止当前代连接并释放资源（Stop / Start 防御路径共用）。锁内只做快照、
// 字段置空与置终态，真正的断连必须在锁外：SDK 的 Disconnect 会在调用者 goroutine 上内联
// 触发 OnDisconnected → setStateForClient，若此时仍持 r.mu 即不可重入自死锁——且死锁
// goroutine 持有的 supervisor s.mu 会毒化全部 imBot 接口（getList 也随之永久挂起）。
func (r *channelRuntime) stopAndRelease() {
	r.mu.Lock()
	cancel, client, stop := r.cancel, r.client, r.stopProc
	r.cancel, r.client, r.stopProc = nil, nil, nil
	r.state = "stopped"
	r.mu.Unlock()
	releaseGeneration(cancel, client, stop)
}

// releaseGeneration 锁外释放一代连接资源：取消 ctx → 断开 SDK 连接（内联回调经代际守卫
// 看到 r.client 已置空/换代而忽略）→ 停入站消费 loop。
func releaseGeneration(cancel context.CancelFunc, client *aibot.WsClient, stop chan struct{}) {
	if cancel != nil {
		cancel()
	}
	if client != nil {
		client.Disconnect()
	}
	if stop != nil {
		close(stop)
	}
}

// setStateForClient 回调驱动的状态写入，代际守卫：仅当 client 仍是当前代连接（r.client ==
// client）时生效。停止/换代后 r.client 已置空，迟到的 SDK 回调（含 Disconnect 内联触发的
// OnDisconnected）在此自然忽略——stopped 终态不被覆盖，且写入方永远拿得到锁。
func (r *channelRuntime) setStateForClient(client *aibot.WsClient, state, lastErr string) {
	r.mu.Lock()
	if r.client == client {
		r.state = state
		r.lastErr = lastErr
	}
	r.mu.Unlock()
}

// Status 实现 bot.ChannelRuntime。
func (r *channelRuntime) Status() bot.ChannelStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return bot.ChannelStatus{State: r.state, LastError: r.lastErr}
}

// clientSnapshot 回复流用的客户端快照（nil = 已停止/未启动）。
func (r *channelRuntime) clientSnapshot() *aibot.WsClient {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client
}

// submit 入站构建任务轻投递；通道满即丢并告警。
func (r *channelRuntime) submit(build func(*bot.InboundMessage)) {
	r.mu.Lock()
	ch, stop := r.inbound, r.stopProc
	r.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- build:
	case <-stop:
	default:
		zap.L().Warn("bot 入站通道已满，消息丢弃", zap.String("channel", "wecom"))
	}
}

// processLoop 保序串行消费：构建（含同步下载）→ 交核心编排器。通道与停止信号按 Start
// 代际以参数固定——本 loop 只消费本代通道，Stop 关旧代 stop 后本 loop 自然退出。
func (r *channelRuntime) processLoop(ch chan func(*bot.InboundMessage), stop chan struct{}) {
	for {
		select {
		case build := <-ch:
			msg := &bot.InboundMessage{}
			build(msg)
			if msg.MessageID != "" && r.onInbound != nil {
				r.onInbound(*msg)
			}
		case <-stop:
			return
		}
	}
}

// sdkLogger 适配 SDK types.Logger 到 zap（SDK 调用形如 Info(msg, args...)）。
type sdkLogger struct {
	log *zap.Logger
}

func (l sdkLogger) Debug(message string, args ...any) { l.log.Debug(joinLogArgs(message, args)) }
func (l sdkLogger) Info(message string, args ...any)  { l.log.Info(joinLogArgs(message, args)) }
func (l sdkLogger) Warn(message string, args ...any)  { l.log.Warn(joinLogArgs(message, args)) }
func (l sdkLogger) Error(message string, args ...any) { l.log.Error(joinLogArgs(message, args)) }

func joinLogArgs(message string, args []any) string {
	if len(args) == 0 {
		return message
	}
	return message + " " + fmt.Sprint(args...)
}
