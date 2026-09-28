package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// 企微智能机器人「扫码授权接入」协议客户端 + 进程级 attempt 状态机。
//
// 协议（腾讯官方端点，无需任何中转服务；同 dsh-im 的 wecom qr-auth 实现）：
//  1. GET  {qrGenerateURL}?source=<qrSource>&plat=<1|2|3> → data.scode（一次性连接码）+ data.auth_url
//     （授权页 URL，即二维码内容；必须强校验为 work.weixin.qq.com 域——防钓鱼）
//  2. 用户用企业微信 App 扫码并在授权页确认创建智能机器人
//  3. GET  {qrQueryURL}?scode=<scode> → data.status: success（携带 data.bot_info{botid,secret}）
//     / expired|timeout / fail|failed|error / 其余一律视为 waiting
//
// 凭据走轮询不走回调：无需公网入口。secret 仅经 ProvisionActivator 回调落库（sqlite，项目
// 既定口径），不进日志、不回前端——AttemptView 只出 qrContent/expiresAt 等公开字段。

const (
	qrGenerateURL = "https://work.weixin.qq.com/ai/qc/generate"
	qrQueryURL    = "https://work.weixin.qq.com/ai/qc/query_result"

	// qrSource 扫码来源标识（腾讯侧语义未知，dsh-im 传自身标识可用；实测不可用时降级手动接入）。
	qrSource = "ocean-harness"

	// qrTTL scode 本地有效期（腾讯未下发 TTL，dsh-im 同款推断值；以协议 expired 状态兜底为准）。
	qrTTL = 5 * time.Minute

	// qrPollIntervalMs 前端轮询节奏（宿主下发，前端不自造节奏）。
	qrPollIntervalMs = 3000

	// qrHTTPTimeout 单次腾讯请求超时。
	qrHTTPTimeout = 10 * time.Second
)

// defaultPlat 操作系统 → 腾讯 plat 参数（win32→2、linux→3、其他→1，dsh-im 同款映射）。
func defaultPlat() string {
	switch os.Getenv("GOOS") {
	case "windows":
		return "2"
	case "linux":
		return "3"
	default:
		return "1"
	}
}

// ---------- 协议响应（最小解析面） ----------

type qrGenerateResponse struct {
	Data struct {
		Scode   string `json:"scode"`
		AuthURL string `json:"auth_url"`
	} `json:"data"`
}

type qrQueryResponse struct {
	Data struct {
		Status  string `json:"status"`
		BotInfo *struct {
			BotID  string `json:"botid"`
			Secret string `json:"secret"`
		} `json:"bot_info"`
	} `json:"data"`
}

// qrPollStatus 腾讯轮询状态归一（四态，未知值一律 waiting——协议宽松兜底）。
type qrPollStatus string

const (
	qrPollWaiting qrPollStatus = "waiting"
	qrPollExpired qrPollStatus = "expired"
	qrPollFailed  qrPollStatus = "failed"
	qrPollSuccess qrPollStatus = "success"
)

// normalizeTencentStatus 腾讯原始 status → 四态归一。
func normalizeTencentStatus(raw string) qrPollStatus {
	switch raw {
	case "success":
		return qrPollSuccess
	case "expired", "timeout":
		return qrPollExpired
	case "fail", "failed", "error":
		return qrPollFailed
	default:
		return qrPollWaiting
	}
}

// safeAuthURL 授权页 URL 强校验：scheme/host（含端口）必须与协议端点完全一致（生产即
// https+work.weixin.qq.com，防钓鱼+防异端口仿冒，同 dsh-im）；测试注入伪端点时随之对照。
func safeAuthURL(raw string) (string, error) {
	base, err := url.Parse(qrBase())
	if err != nil {
		return "", fmt.Errorf("协议端点解析失败: %w", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("授权链接解析失败: %w", err)
	}
	if u.Scheme != base.Scheme || u.Host != base.Host {
		return "", fmt.Errorf("授权链接域名异常（须为 %s）: %s", base.Host, raw)
	}
	return raw, nil
}

// qrHTTPGet 腾讯协议 GET：禁跟随重定向（授权端点被劫持重定向时直接暴露为错误）+ 超时兜底。
func qrHTTPGet(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{
		Timeout: qrHTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("腾讯端点返回 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("腾讯端点响应解析失败: %w", err)
	}
	return nil
}

// qrBaseOverride 仅供测试注入伪端点 base（如 httptest server）；生产恒空。
var qrBaseOverride string

func qrBase() string {
	if qrBaseOverride != "" {
		return qrBaseOverride
	}
	return "https://work.weixin.qq.com"
}

// qrGenerate 申请扫码会话：返回 scode + 授权页 URL（二维码内容）。
func qrGenerate(ctx context.Context) (scode, authURL string, err error) {
	var resp qrGenerateResponse
	err = qrHTTPGet(ctx, fmt.Sprintf("%s/ai/qc/generate?source=%s&plat=%s", qrBase(), qrSource, defaultPlat()), &resp)
	if err != nil {
		return "", "", fmt.Errorf("扫码会话申请失败: %w", err)
	}
	if resp.Data.Scode == "" {
		return "", "", errors.New("扫码会话申请失败：腾讯未返回 scode")
	}
	authURL, err = safeAuthURL(resp.Data.AuthURL)
	if err != nil {
		return "", "", err
	}
	return resp.Data.Scode, authURL, nil
}

// qrPollResult 单次轮询结果。
type qrPollResult struct {
	Status qrPollStatus
	BotID  string // 仅 success
	Secret string // 仅 success（调用方直落库，禁止日志/回传）
}

// qrPoll 轮询一次扫码结果。
func qrPoll(ctx context.Context, scode string) (qrPollResult, error) {
	var resp qrQueryResponse
	if err := qrHTTPGet(ctx, qrBase()+"/ai/qc/query_result?scode="+url.QueryEscape(scode), &resp); err != nil {
		return qrPollResult{}, fmt.Errorf("扫码结果查询失败: %w", err)
	}
	status := normalizeTencentStatus(resp.Data.Status)
	result := qrPollResult{Status: status}
	if status == qrPollSuccess {
		if resp.Data.BotInfo == nil || resp.Data.BotInfo.BotID == "" || resp.Data.BotInfo.Secret == "" {
			return qrPollResult{}, errors.New("扫码授权返回缺少机器人凭据")
		}
		result.BotID = resp.Data.BotInfo.BotID
		result.Secret = resp.Data.BotInfo.Secret
	}
	return result, nil
}

// ---------- attempt 状态机 ----------

// provisionState attempt 状态（同 dsh-im 状态集；无 scanned 中间态——腾讯协议不区分已扫码未确认）。
type provisionState string

const (
	provPending    provisionState = "pending"
	provConnecting provisionState = "connecting"
	provConnected  provisionState = "connected"
	provFailed     provisionState = "failed"
	provExpired    provisionState = "expired"
	provCancelled  provisionState = "cancelled"
)

// ProvisionActivator 扫码成功后的激活回调：落库新 bot 并拉起连接，返回 bot 行 id。
// 由 main 装配注入（service 层实现）——wecom 包不反向依赖 service。
// 实现方保证：secret 仅落库，不进日志、不回传。
type ProvisionActivator func(remoteBotID, secret string) (botRowID int, err error)

// AttemptView attempt 对外投影（剥除 scode/secret；qrContent 为授权页 URL=二维码内容，公开可回传）。
type AttemptView struct {
	AttemptID      string `json:"attemptId"`
	State          string `json:"state"`
	QRContent      string `json:"qrContent,omitempty"` // 授权页 URL（pending 态非空）
	ExpiresAt      int64  `json:"expiresAt"`           // 毫秒时间戳（0 = 无 QR 语境）
	PollIntervalMs int    `json:"pollIntervalMs"`
	ErrCode        string `json:"errCode,omitempty"` // failed 终态原因（qr-start-failed/qr-connect-failed/activation-failed/expired）
	BotID          int    `json:"botId,omitempty"`   // connected 后的新 bot 行 id
}

// provisionAttempt 单个扫码会话。ctx 与 cancel 配对：cancel 终止在途腾讯请求
// （含轮询）——用户取消后，在途 poll 返回错误而非 success，杜绝「取消后凭据照落库」。
type provisionAttempt struct {
	mu             sync.Mutex
	id             string
	ctx            context.Context
	state          provisionState
	scode          string
	authURL        string
	expiresAt      time.Time
	pollIntervalMs int
	errCode        string
	botRowID       int
	cancel         context.CancelFunc
	polling        bool // 惰性单飞标记：并发 RegistrationStatus 只有一轮真实腾讯请求
}

// ErrProvisionNotFound attempt 不存在（已过期清理/重启丢失——前端引导重新扫码）。
var ErrProvisionNotFound = errors.New("扫码会话不存在，请重新发起")

// 进程级单会话（个人场景同一时刻至多一个进行中的扫码；新 begin 自动取消旧的）。
var (
	provisionMu   sync.Mutex
	activeAttempt *provisionAttempt
	activator     ProvisionActivator
)

// SetProvisionActivator 注入扫码成功激活回调（main 装配；幂等覆盖）。
func SetProvisionActivator(fn ProvisionActivator) {
	provisionMu.Lock()
	defer provisionMu.Unlock()
	activator = fn
}

// StartProvisioning 发起新扫码会话（已有进行中会话则先取消——同 dsh-im begin 内建取消）。
func StartProvisioning() (AttemptView, error) {
	provisionMu.Lock()
	prev := activeAttempt
	provisionMu.Unlock()
	if prev != nil {
		CancelProvisioning(prev.view().AttemptID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	scode, authURL, err := qrGenerate(ctx)
	if err != nil {
		cancel()
		return AttemptView{}, err
	}

	attempt := &provisionAttempt{
		id:             uuid.NewString(),
		ctx:            ctx,
		state:          provPending,
		scode:          scode,
		authURL:        authURL,
		expiresAt:      time.Now().Add(qrTTL),
		pollIntervalMs: qrPollIntervalMs,
		cancel:         cancel,
	}
	provisionMu.Lock()
	activeAttempt = attempt
	provisionMu.Unlock()
	return attempt.view(), nil
}

// RegistrationStatus 惰性单飞轮询：每次调用至多触发一轮真实腾讯请求（polling 标记去重），
// pending 本地过期直接置 expired；success → connecting（激活回调完成建 bot+连接）→ connected/failed。
func RegistrationStatus(attemptID string) (AttemptView, error) {
	provisionMu.Lock()
	attempt := activeAttempt
	provisionMu.Unlock()
	if attempt == nil || attempt.id != attemptID {
		return AttemptView{}, ErrProvisionNotFound
	}
	return attempt.pollOnce(), nil
}

// CancelProvisioning 取消扫码会话（abort 在途腾讯请求；幂等）。
func CancelProvisioning(attemptID string) error {
	provisionMu.Lock()
	attempt := activeAttempt
	if attempt == nil || attempt.id != attemptID {
		provisionMu.Unlock()
		return ErrProvisionNotFound
	}
	activeAttempt = nil
	provisionMu.Unlock()
	attempt.cancel()
	attempt.mu.Lock()
	if attempt.state == provPending || attempt.state == provConnecting {
		attempt.state = provCancelled
	}
	attempt.mu.Unlock()
	return nil
}

// pollOnce 单飞轮询入口（见 RegistrationStatus）。
func (a *provisionAttempt) pollOnce() AttemptView {
	a.mu.Lock()
	// 本地 TTL 过期兜底（腾讯 expired 之外的第二道防线）。
	if a.state == provPending && time.Now().After(a.expiresAt) {
		a.finishLocked(provExpired, "expired")
	}
	if a.state != provPending {
		view := a.viewLocked()
		a.mu.Unlock()
		return view
	}
	if a.polling {
		// 上一轮腾讯请求仍在途：直接回当前快照（前端按 pollIntervalMs 会再来）。
		view := a.viewLocked()
		a.mu.Unlock()
		return view
	}
	a.polling = true
	scode := a.scode
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.polling = false
		a.mu.Unlock()
	}()

	result, err := qrPoll(a.ctx, scode)
	if err != nil {
		// 网络抖动不终结会话：回 pending 快照（含 errCode 供前端展示），等下一轮。
		a.mu.Lock()
		a.errCode = "qr-poll-error"
		view := a.viewLocked()
		a.mu.Unlock()
		return view
	}

	a.mu.Lock()
	switch result.Status {
	case qrPollWaiting:
		// 保持 pending，清除上轮临时错误。
		a.errCode = ""
	case qrPollExpired:
		a.finishLocked(provExpired, "expired")
	case qrPollFailed:
		a.finishLocked(provFailed, "qr-failed")
	case qrPollSuccess:
		// success → connecting → 激活（建 bot + 连接）→ connected/failed。
		a.state = provConnecting
		a.mu.Unlock()
		a.activate(result.BotID, result.Secret)
		return a.view()
	}
	view := a.viewLocked()
	a.mu.Unlock()
	return view
}

// activate 执行扫码成功后的激活回调（connecting 态内完成）。任何失败落 failed 终态。
func (a *provisionAttempt) activate(remoteBotID, secret string) {
	provisionMu.Lock()
	fn := activator
	provisionMu.Unlock()

	a.mu.Lock()
	if fn == nil {
		a.finishLocked(provFailed, "activator-missing")
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	botRowID, err := fn(remoteBotID, secret)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.finishLocked(provFailed, "activation-failed")
		return
	}
	a.botRowID = botRowID
	a.finishLocked(provConnected, "")
}

// finishLocked 置终态并清理敏感/连接资源（须持 a.mu）。
func (a *provisionAttempt) finishLocked(state provisionState, errCode string) {
	a.state = state
	a.errCode = errCode
	a.scode = ""
	a.authURL = ""
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
}

// view 公开投影（锁内快照）。
func (a *provisionAttempt) view() AttemptView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.viewLocked()
}

func (a *provisionAttempt) viewLocked() AttemptView {
	return AttemptView{
		AttemptID:      a.id,
		State:          string(a.state),
		QRContent:      a.authURL,
		ExpiresAt:      a.expiresAt.UnixMilli(),
		PollIntervalMs: a.pollIntervalMs,
		ErrCode:        a.errCode,
		BotID:          a.botRowID,
	}
}
