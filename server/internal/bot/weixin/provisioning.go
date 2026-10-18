package weixin

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

/*
 * 微信「扫码授权接入」进程级 attempt 状态机（对齐 dsh-im）：BeginLogin → 后台 goroutine 自驱
 * get_qrcode_status 长轮询（35s 长轮询不可惰性单飞——会卡前端 HTTP）→ confirmed 经
 * ProvisionActivator 落库拉连接。凭据仅落库不进日志不回前端，AttemptView 只出公开字段。
 */

const (
	// qrTTL 二维码本地有效期（dsh-im 同款推断值；协议 expired 状态兜底为准）。
	qrTTL = 5 * time.Minute
	// qrPollIntervalMs 前端轮询节奏（快照读取，后台自驱；dsh-im 同款 1s）。
	qrPollIntervalMs = 1000
)

// verifyCodePattern 配对码格式（dsh-im 同款 4-8 位数字）。
var verifyCodePattern = regexp.MustCompile(`^\d{4,8}$`)

// provisionState attempt 状态（微信特有 scanned / needs_verification 中间态）。
type provisionState string

const (
	provPending           provisionState = "pending"
	provScanned           provisionState = "scanned"
	provNeedsVerification provisionState = "needs_verification"
	provConnecting        provisionState = "connecting"
	provConnected         provisionState = "connected"
	provFailed            provisionState = "failed"
	provExpired           provisionState = "expired"
	provCancelled         provisionState = "cancelled"
)

func (s provisionState) terminal() bool {
	switch s {
	case provConnected, provFailed, provExpired, provCancelled:
		return true
	}
	return false
}

/*
 * ProvisionActivator 扫码确认后的激活回调：新建 bot 行或对 rebindBotID>0 换绑凭据，
 * 并拉起连接，返回 bot 行 id。由 main 装配注入（service 层实现）——weixin 包不反向依赖
 * service。实现方保证：token 仅落库，不进日志、不回传。
 */
type ProvisionActivator func(cred Credential, rebindBotID int) (botRowID int, err error)

// ErrAccountMismatch 换绑目标与扫码账号不一致（service 激活回调返回；attempt 映射具名错误码）。
var ErrAccountMismatch = errors.New("该微信账号与原绑定不一致：换绑仅限同账号重授权，更换账号请删除原机器人后重新扫码")

// AttemptView attempt 对外投影（剥除 qrcode/token；qrContent 为扫码链接，公开可回传）。
type AttemptView struct {
	AttemptID      string `json:"attemptId"`
	State          string `json:"state"`
	QRContent      string `json:"qrContent,omitempty"` // 扫码链接（pending 态非空，前端本地渲染二维码）
	ExpiresAt      int64  `json:"expiresAt"`           // 毫秒时间戳
	PollIntervalMs int    `json:"pollIntervalMs"`
	ErrCode        string `json:"errCode,omitempty"` // 终态原因（expired/verification-blocked/already-bound/activation-failed 等）
	BotID          int    `json:"botId,omitempty"`   // connected 后的 bot 行 id
}

/*
 * provisionAttempt 单个扫码会话。ctx 与 cancel 配对：cancel 终止在途长轮询与等待——
 * 用户取消后在途 poll 返回错误而非 confirmed，杜绝「取消后凭据照落库」。
 */
type provisionAttempt struct {
	mu         sync.Mutex
	id         string
	ctx        context.Context
	cancel     context.CancelFunc
	state      provisionState
	qrcode     string
	qrURL      string
	baseURL    string // 当前轮询 base（scaned_but_redirect 时切换）
	expiresAt  time.Time
	errCode    string
	botRowID   int
	rebindID   int
	verifyCode string
	verifyCh   chan string // SubmitVerifyCode → 后台循环（容量 1，非阻塞投递）
}

// ErrProvisionNotFound attempt 不存在（已过期清理/重启丢失——前端引导重新扫码）。
var ErrProvisionNotFound = errors.New("扫码会话不存在，请重新发起")

// ErrProvisionStateInvalid 当前状态不接受该操作（如非 needs_verification 态提交配对码）。
var ErrProvisionStateInvalid = errors.New("扫码会话当前状态不支持该操作")

// 进程级单会话（个人场景同一时刻至多一个进行中的扫码；新 begin 自动取消旧的）。
var (
	provisionMu   sync.Mutex
	activeAttempt *provisionAttempt
	activator     ProvisionActivator
)

// provisionClient 扫码会话协议客户端（apiClient 无状态；包级变量供测试注入伪端点）。
var provisionClient = newAPIClient()

// SetProvisionActivator 注入扫码成功激活回调（main 装配；幂等覆盖）。
func SetProvisionActivator(fn ProvisionActivator) {
	provisionMu.Lock()
	defer provisionMu.Unlock()
	activator = fn
}

// beginMu 串行化 begin 全程（含 BeginLogin 网络往返）：单会话槽的抢占与登记原子化。
var beginMu sync.Mutex

/*
 * StartProvisioning 发起新扫码会话（已有进行中会话先取消——同 dsh-im begin 内建取消）。
 * rebindBotID>0 = 对既有 bot 换绑（confirmed 后更新该行凭据而非新建）；localTokens 传
 * 既有 weixin bot token（服务端绑定感知）。
 */
func StartProvisioning(rebindBotID int, localTokens []string) (AttemptView, error) {
	beginMu.Lock()
	defer beginMu.Unlock()
	// 先摘旧会话占住单会话槽（网络往返期间也持占位，并发 begin 经 beginMu 排队后正确互踢）。
	provisionMu.Lock()
	prev := activeAttempt
	activeAttempt = nil
	provisionMu.Unlock()
	if prev != nil {
		abortAttempt(prev)
	}

	ctx, cancel := context.WithCancel(context.Background())
	login, err := provisionClient.BeginLogin(ctx, localTokens)
	if err != nil {
		cancel()
		return AttemptView{}, err
	}

	attempt := &provisionAttempt{
		id:        uuid.NewString(),
		ctx:       ctx,
		cancel:    cancel,
		state:     provPending,
		qrcode:    login.QRCode,
		qrURL:     login.QRCodeURL,
		baseURL:   qrBaseURL,
		expiresAt: time.Now().Add(qrTTL),
		rebindID:  rebindBotID,
		verifyCh:  make(chan string, 1),
	}
	provisionMu.Lock()
	activeAttempt = attempt
	provisionMu.Unlock()
	go attempt.loop()
	return attempt.view(), nil
}

// RegistrationStatus 读 attempt 当前快照（后台循环自驱轮询，本调用零协议请求）。
func RegistrationStatus(attemptID string) (AttemptView, error) {
	provisionMu.Lock()
	attempt := activeAttempt
	provisionMu.Unlock()
	if attempt == nil || attempt.id != attemptID {
		return AttemptView{}, ErrProvisionNotFound
	}
	return attempt.view(), nil
}

// CancelProvisioning 取消扫码会话（abort 在途长轮询与等待；幂等）。
func CancelProvisioning(attemptID string) error {
	provisionMu.Lock()
	attempt := activeAttempt
	if attempt == nil || attempt.id != attemptID {
		provisionMu.Unlock()
		return ErrProvisionNotFound
	}
	activeAttempt = nil
	provisionMu.Unlock()
	abortAttempt(attempt)
	return nil
}

// abortAttempt 取消并置态（幂等；终态不覆盖）。
func abortAttempt(a *provisionAttempt) {
	a.cancel()
	a.mu.Lock()
	if !a.state.terminal() {
		a.state = provCancelled
	}
	a.mu.Unlock()
}

// SubmitVerifyCode 提交配对码（仅 needs_verification 态受理；4-8 位数字）。
func SubmitVerifyCode(attemptID, code string) error {
	provisionMu.Lock()
	attempt := activeAttempt
	provisionMu.Unlock()
	if attempt == nil || attempt.id != attemptID {
		return ErrProvisionNotFound
	}
	trimmed := strings.TrimSpace(code)
	if !verifyCodePattern.MatchString(trimmed) {
		return errors.New("配对码须为 4-8 位数字")
	}
	attempt.mu.Lock()
	if attempt.state != provNeedsVerification {
		attempt.mu.Unlock()
		return ErrProvisionStateInvalid
	}
	attempt.mu.Unlock()
	select {
	case attempt.verifyCh <- trimmed:
	default: // 已有待消费配对码，本次覆盖语义由容量 1 通道的先后投递近似（取先到者）
	}
	return nil
}

// qrPollBackoff 轮询失败退避步长（失败近即时返回，不退避会在 TTL 内热旋打端点）。
const qrPollBackoff = 2 * time.Second

/*
 * loop 后台状态机主循环：长轮询直至终态/过期/取消。wait→pending；scaned→已扫码；
 * need_verifycode→挂起等配对码（verifyCh/取消/过期三路可中断，verify_code_blocked 为配对码
 * 错误终态）；scaned_but_redirect→切轮询 base；binded_redirect→已绑他处终态；confirmed→激活。
 */
func (a *provisionAttempt) loop() {
	for {
		if a.ctx.Err() != nil {
			a.finish(provCancelled, "")
			return
		}
		a.mu.Lock()
		expired := time.Now().After(a.expiresAt)
		a.mu.Unlock()
		if expired {
			a.finish(provExpired, "expired")
			return
		}

		if a.waitVerify() {
			return
		}

		poll, err := provisionClient.PollLogin(a.ctx, a.qrcodeSnap(), a.baseSnap(), a.verifySnap())
		if err != nil {
			if a.ctx.Err() != nil {
				return // 取消路径由 CancelProvisioning 置态
			}
			a.setErr("qr-poll-error") // 网络抖动不终结会话，退避后重试
			if a.sleepCtx(qrPollBackoff) {
				return
			}
			continue
		}

		switch poll.Status {
		case "wait":
			a.setStateErr(provPending, "")
		case "scaned":
			a.clearVerify()
			a.setStateErr(provScanned, "")
		case "need_verifycode":
			a.clearVerify()
			a.setStateErr(provNeedsVerification, "")
		case "verify_code_blocked":
			a.finish(provFailed, "verification-blocked")
			return
		case "expired":
			a.finish(provExpired, "expired")
			return
		case "scaned_but_redirect":
			a.switchBase(poll.RedirectHost)
			a.setStateErr(provScanned, "")
		case "binded_redirect":
			a.finish(provFailed, "already-bound")
			return
		case "confirmed":
			a.confirmed(poll)
			return
		}
	}
}

// waitVerify needs_verification 态挂起等待配对码；返回 true = 循环应退出（取消/过期）。
func (a *provisionAttempt) waitVerify() bool {
	a.mu.Lock()
	needs := a.state == provNeedsVerification && a.verifyCode == ""
	a.mu.Unlock()
	if !needs {
		return false
	}
	select {
	case code := <-a.verifyCh:
		a.mu.Lock()
		a.verifyCode = code
		a.state = provScanned
		a.mu.Unlock()
		return false
	case <-a.ctx.Done():
		return true
	case <-time.After(time.Until(a.expiresAt)):
		return false // 醒来后由主循环顶部统一判过期
	}
}

// confirmed confirmed 响应 → 凭据校验 → connecting → 激活回调 → 终态（激活不可逆）。
func (a *provisionAttempt) confirmed(poll *loginPoll) {
	cred := Credential{
		Token:       strings.TrimSpace(poll.BotToken),
		BaseURL:     serverBase(poll.BaseURL, a.baseSnap()),
		AccountID:   strings.TrimSpace(poll.IlinkBotID),
		OwnerUserID: strings.TrimSpace(poll.IlinkUserID),
	}
	if cred.Token == "" || cred.AccountID == "" || cred.OwnerUserID == "" {
		a.finish(provFailed, "qr-incomplete")
		return
	}
	a.setStateErr(provConnecting, "")
	if a.ctx.Err() != nil {
		return // 取消竞态：弃用凭据不落库（attempt 已被置 cancelled 终态）
	}

	provisionMu.Lock()
	fn := activator
	provisionMu.Unlock()
	if fn == nil {
		a.finish(provFailed, "activator-missing")
		return
	}
	rowID, err := fn(cred, a.rebindSnap())
	if err != nil {
		code := "activation-failed"
		if errors.Is(err, ErrAccountMismatch) {
			code = "account-mismatch"
		}
		a.finish(provFailed, code)
		return
	}
	// 激活已不可逆：即使取消先到也如实呈现 connected（bot 行已建并连接，cancelled 是谎报）。
	a.mu.Lock()
	a.botRowID = rowID
	a.state = provConnected
	a.errCode = ""
	a.qrcode, a.qrURL, a.verifyCode = "", "", ""
	a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

// finish 置终态并清理敏感字段与在途资源（幂等：终态不覆盖终态）。
func (a *provisionAttempt) finish(state provisionState, errCode string) {
	a.mu.Lock()
	if !a.state.terminal() {
		a.state = state
		a.errCode = errCode
		a.qrcode = ""
		a.qrURL = ""
		a.verifyCode = ""
	}
	a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *provisionAttempt) setStateErr(state provisionState, errCode string) {
	a.mu.Lock()
	a.state, a.errCode = state, errCode
	a.mu.Unlock()
}

func (a *provisionAttempt) setErr(errCode string) {
	a.mu.Lock()
	a.errCode = errCode
	a.mu.Unlock()
}

// clearVerify 清空已消费配对码（scaned/need_verifycode 响应到达即视为上枚已消费）。
func (a *provisionAttempt) clearVerify() {
	a.mu.Lock()
	a.verifyCode = ""
	a.mu.Unlock()
}

// switchBase 切换轮询 base（区域跳转；解析失败保持现值）。
func (a *provisionAttempt) switchBase(redirectHost string) {
	a.mu.Lock()
	a.baseURL = serverBase(redirectHost, a.baseURL)
	a.mu.Unlock()
}

func (a *provisionAttempt) qrcodeSnap() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.qrcode
}

func (a *provisionAttempt) baseSnap() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.baseURL
}

func (a *provisionAttempt) verifySnap() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.verifyCode
}

func (a *provisionAttempt) rebindSnap() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rebindID
}

// sleepCtx 可中断等待；返回 true = 因取消中止。
func (a *provisionAttempt) sleepCtx(d time.Duration) bool {
	select {
	case <-a.ctx.Done():
		return true
	case <-time.After(d):
		return false
	}
}

// view 公开投影（锁内快照）。
func (a *provisionAttempt) view() AttemptView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AttemptView{
		AttemptID:      a.id,
		State:          string(a.state),
		QRContent:      a.qrURL,
		ExpiresAt:      a.expiresAt.UnixMilli(),
		PollIntervalMs: qrPollIntervalMs,
		ErrCode:        a.errCode,
		BotID:          a.botRowID,
	}
}

/*
 * serverBase 服务端下发 base（redirect_host/baseurl 可能是裸域名）→ https URL 归一；
 * 空值或格式非法回落 fallback（区域跳转不应中断扫码流程）。
 */
func serverBase(value, fallback string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return fallback
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	base, err := normalizeAPIBaseURL(raw)
	if err != nil {
		return fallback
	}
	return base
}
