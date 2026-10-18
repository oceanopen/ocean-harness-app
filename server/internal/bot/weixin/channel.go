package weixin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"ocean-harness/server/internal/bot"
	"ocean-harness/server/internal/dal/enums"
)

/*
 * inboundBufferCapacity 入站处理通道容量：长轮询回调必须快进快出，附件同步下载/入队串行在
 * 消费 goroutine（保消息顺序）。批投递中途通道满则放弃剩余且不推游标——重启重放，核心幂等去重兜底。
 */
const inboundBufferCapacity = 32

// startRetryDelays notifystart 重试节奏；retryableStart 网络族错误可重试（超时/网络/408/425/429/5xx）。
var startRetryDelays = []time.Duration{250 * time.Millisecond, time.Second, 3 * time.Second}

func retryableStart(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	switch ae.Code {
	case errNetwork, errTimeout:
		return true
	case errHTTP:
		return ae.Status == 408 || ae.Status == 425 || ae.Status == 429 || ae.Status >= 500
	}
	return false
}

// Factory 渠道工厂（main 装配注册）。
type Factory struct{}

// Channel 实现 bot.Factory。
func (Factory) Channel() enums.Channel { return enums.CHANNEL_WEIXIN }

// Create 实现 bot.Factory。
func (Factory) Create() (bot.ChannelRuntime, error) { return &channelRuntime{}, nil }

// Credential credential JSON 的 shape（核心不解引用，仅适配器解释；激活时落库）。
type Credential struct {
	Token       string `json:"token"`
	BaseURL     string `json:"baseUrl"`
	AccountID   string `json:"accountId"`
	OwnerUserID string `json:"ownerUserId"`
}

// channelState channel_state 列的 JSON 形态（适配器自管；新字段向后兼容追加）。
type channelState struct {
	Cursor string `json:"cursor"`
}

/*
 * channelRuntime 单个微信 bot 的连接运行时：自管长轮询循环（无 SDK，协议层直连）。
 * gen 为连接代际——Start/Stop 递增，后台 goroutine 捕获本代值，状态/游标写入前校验，
 * 迟到的旧代写入自然忽略（对齐 wecom 的 client 指针代际守卫）。
 */
type channelRuntime struct {
	mu      sync.Mutex
	cred    Credential
	baseURL string
	client  *apiClient
	cancel  context.CancelFunc
	gen     int
	state   string // connecting | connected | disconnected | stopped
	lastErr string
	cursor  channelState

	inbound  chan *inboundMessage
	stopProc chan struct{}

	sendMu   sync.Mutex // 出站串行 + 间隔节奏（文本与产物共用，见 reply.go paceSend）
	lastSend time.Time
}

/*
 * connGen 单代连接的参数快照（Start 锁内装配）：后台 goroutine 一律消费本代值而非读
 * r.cfg/r.log——Stop→Start 交错时旧代 goroutine 带旧快照自然沉寂，无数据竞争。
 */
type connGen struct {
	cfg     bot.BotRuntimeConfig
	log     *zap.Logger
	client  *apiClient
	token   string
	baseURL string
	gen     int
}

// Start 实现 bot.ChannelRuntime：解析凭据 → 后台 notifyStart + 长轮询（不阻塞）。
func (r *channelRuntime) Start(cfg bot.BotRuntimeConfig, onInbound func(bot.InboundMessage)) error {
	var cred Credential
	if err := json.Unmarshal([]byte(cfg.Credential), &cred); err != nil {
		return fmt.Errorf("credential 解析失败: %w", err)
	}
	if cred.Token == "" || cred.BaseURL == "" {
		return errors.New("微信凭据缺失：credential JSON 须含 token 与 baseUrl")
	}
	baseURL, err := normalizeAPIBaseURL(cred.BaseURL)
	if err != nil {
		return fmt.Errorf("baseUrl 不受信任: %w", err)
	}

	log := zap.L().With(zap.String("component", "bot.weixin"), zap.Int("botID", cfg.BotID))
	ctx, cancel := context.WithCancel(context.Background())

	// 幂等防御：先停旧代连接（supervisor 常规路径已保证先 Stop，这里兜底）。
	r.stopAndRelease()

	r.mu.Lock()
	r.cred = cred
	r.baseURL = baseURL
	r.cancel = cancel
	r.gen++
	r.state = "connecting"
	r.lastErr = ""
	r.cursor = loadChannelState(cfg.ChannelState)
	ch := make(chan *inboundMessage, inboundBufferCapacity)
	stop := make(chan struct{})
	r.inbound = ch
	r.stopProc = stop
	client := newAPIClient()
	r.client = client
	cg := connGen{cfg: cfg, log: log, client: client, token: cred.Token, baseURL: baseURL, gen: r.gen}
	r.mu.Unlock()

	go r.consumeLoop(ch, stop, cg, onInbound)
	go r.run(ctx, cg)
	return nil
}

/*
 * run 连接主循环：notifyStart（重试 3 档）→ 长轮询循环 → 收尾 notifyStop。全程消费 connGen
 * 快照（含收尾下线通知的 token/baseURL），收尾与新 Start 交错也不会误用新代凭据。
 * -14（token 失效）为终态：置 disconnected + 换绑指引，不再自动重试（token 无法自愈）。
 */
func (r *channelRuntime) run(ctx context.Context, cg connGen) {
	if err := r.notifyStartWithRetry(ctx, cg); err != nil {
		if ctx.Err() == nil {
			var ae *APIError
			if errors.As(err, &ae) && ae.Code == errStaleToken {
				r.setStateForGen(cg.gen, "disconnected", "微信登录凭据已失效，请在机器人列表对该 bot 执行「重新扫码授权」")
			} else {
				r.setStateForGen(cg.gen, "disconnected", err.Error())
			}
			cg.log.Error("微信连接启动失败", zap.Error(err))
		}
		return
	}
	r.setStateForGen(cg.gen, "connected", "")
	r.pollLoop(ctx, cg)

	if ctx.Err() == nil {
		// 非取消退出（终态断连等）：补发下线通知，尽力而为（本代快照，不误伤新连接）。
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = cg.client.NotifyStop(stopCtx, cg.token, cg.baseURL)
	}
}

// pollBackoffCap 网络族退避封顶（自愈节奏上限）；起步步长 2s。
const pollBackoffCap = 60 * time.Second

// transientPollErr 网络族（可自愈）：网络/超时/HTTP 408/425/429/5xx，语义对齐 retryableStart。
func transientPollErr(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	switch ae.Code {
	case errNetwork, errTimeout:
		return true
	case errHTTP:
		return ae.Status == 408 || ae.Status == 425 || ae.Status == 429 || ae.Status >= 500
	}
	return false
}

/*
 * pollLoop 长轮询主循环：失败分两族——网络族无限退避自愈（2s 翻倍封顶 60s，不计惩罚，
 * 对齐 wecom 断网自愈）；服务端明确拒绝（凭据/协议异常无法自愈）三振终止须人工介入。
 * Stop 取消即退。
 */
func (r *channelRuntime) pollLoop(ctx context.Context, cg connGen) {
	consecutiveRejects := 0
	backoff := 2 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		resp, err := cg.client.GetUpdates(ctx, cg.token, cg.baseURL, r.cursorSnapshot())
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var ae *APIError
			if errors.As(err, &ae) && ae.Code == errStaleToken {
				r.setStateForGen(cg.gen, "disconnected", "微信登录凭据已失效，请在机器人列表对该 bot 执行「重新扫码授权」")
				cg.log.Error("微信 token 已失效，连接终止", zap.String("providerCode", ae.ProviderCode))
				return
			}
			if !transientPollErr(err) {
				consecutiveRejects++
				r.setStateForGen(cg.gen, "connecting", err.Error())
				if consecutiveRejects >= 3 {
					r.setStateForGen(cg.gen, "disconnected", fmt.Sprintf("连续 %d 次拉取消息被拒绝: %v", consecutiveRejects, err))
					cg.log.Error("微信长轮询连续被拒，连接终止", zap.Error(err))
					return
				}
				backoff = 2 * time.Second
				if r.sleepCtx(ctx, time.Duration(1<<(consecutiveRejects-1))*2*time.Second) {
					return
				}
				continue
			}
			r.setStateForGen(cg.gen, "connecting", err.Error())
			if r.sleepCtx(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, pollBackoffCap)
			continue
		}
		consecutiveRejects = 0
		backoff = 2 * time.Second
		r.setStateForGen(cg.gen, "connected", "")
		if r.dispatchBatch(cg, resp) && resp.GetUpdatesBuf != "" {
			r.saveCursor(cg, resp.GetUpdatesBuf)
		}
	}
}

// sleepCtx 可中断等待；返回 true = 因取消中止。
func (r *channelRuntime) sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return true
	case <-time.After(d):
		return false
	}
}

/*
 * dispatchBatch 整批轻投递到消费通道（下载在消费侧）。返回是否全部投递成功——中途通道满
 * 则放弃剩余且不推游标：宁可重启重放（核心幂等去重），不静默丢消息。
 */
func (r *channelRuntime) dispatchBatch(cg connGen, resp *updatesResponse) bool {
	r.mu.Lock()
	ch, stop := r.inbound, r.stopProc
	r.mu.Unlock()
	if ch == nil {
		return false
	}
	for i := range resp.Msgs {
		select {
		case ch <- &resp.Msgs[i]:
		case <-stop:
			return false
		default:
			cg.log.Warn("微信入站通道已满，剩余消息留待重放", zap.Int("droppedFrom", i))
			return false
		}
	}
	return true
}

/*
 * consumeLoop 保序串行消费：入站规范化（含附件同步下载，CDN 参数有时效）→ 交核心编排器。
 * 通道、停止信号与配置/日志按 Start 代际以参数固定——本 loop 只消费本代值。
 */
func (r *channelRuntime) consumeLoop(ch chan *inboundMessage, stop chan struct{}, cg connGen, onInbound func(bot.InboundMessage)) {
	for {
		select {
		case m := <-ch:
			msg := r.normalizeInbound(cg, m)
			if msg.MessageID != "" && onInbound != nil {
				onInbound(msg)
			}
		case <-stop:
			return
		}
	}
}

// notifyStartWithRetry 上线通知重试（节奏 startRetryDelays；不可重试错误立即上抛）。
func (r *channelRuntime) notifyStartWithRetry(ctx context.Context, cg connGen) error {
	for attempt := 0; ; attempt++ {
		err := cg.client.NotifyStart(ctx, cg.token, cg.baseURL)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt >= len(startRetryDelays) || !retryableStart(err) {
			return err
		}
		cg.log.Warn("notifystart 失败重试", zap.Error(err), zap.Duration("wait", startRetryDelays[attempt]))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(startRetryDelays[attempt]):
		}
	}
}

// Stop 实现 bot.ChannelRuntime：取消轮询 + 停消费通道（幂等；notifyStop 由 run goroutine 收尾）。
func (r *channelRuntime) Stop() { r.stopAndRelease() }

// stopAndRelease 停止当前代连接（锁内只做快照、置空与置终态，通道关闭在锁外）。
func (r *channelRuntime) stopAndRelease() {
	r.mu.Lock()
	cancel, stop := r.cancel, r.stopProc
	r.cancel, r.stopProc, r.inbound, r.client = nil, nil, nil, nil
	r.gen++
	r.state = "stopped"
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if stop != nil {
		close(stop)
	}
}

// setStateForGen 状态写入的代际守卫：仅当前代生效（stopped 终态不被旧代覆盖）。
func (r *channelRuntime) setStateForGen(gen int, state, lastErr string) {
	r.mu.Lock()
	if r.gen == gen {
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

// clientSnapshot 当前协议客户端（nil = 已停止/未启动；apiClient 无状态可跨代共享）。
func (r *channelRuntime) clientSnapshot() *apiClient {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client
}

func (r *channelRuntime) token() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cred.Token
}

func (r *channelRuntime) baseURLValue() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.baseURL
}

// cursorSnapshot 当前长轮询游标。
func (r *channelRuntime) cursorSnapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cursor.Cursor
}

// saveCursor 游标推进：内存即时生效 + 落库尽力而为（代际校验防旧代覆盖新代；回写通道取本代快照）。
func (r *channelRuntime) saveCursor(cg connGen, raw string) {
	r.mu.Lock()
	if r.gen != cg.gen {
		r.mu.Unlock()
		return
	}
	r.cursor.Cursor = raw
	r.mu.Unlock()
	if cg.cfg.SaveChannelState != nil {
		payload, err := json.Marshal(channelState{Cursor: raw})
		if err == nil {
			_ = cg.cfg.SaveChannelState(cg.cfg.BotID, string(payload))
		}
	}
}

// loadChannelState 解析 channel_state 快照；损坏按空游标兜底（历史重放由核心幂等去重兜住）。
func loadChannelState(raw string) channelState {
	var st channelState
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &st)
	}
	return st
}
