/*
 * Package weixin 微信个人号渠道适配器：腾讯 iLink 开放 bot 协议（HTTPS REST + 35s 长轮询，
 * 对齐开源 openclaw-weixin v2.4.6；仅私聊）。本文件为纯协议层，连接生命周期/入站/回复/扫码
 * 编排见其余文件。TODO(主动推送): 发送受会话额度硬限，首版仅被动回复；TODO(群聊): 协议未开放。
 */
package weixin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	qrBaseURL       = "https://ilinkai.weixin.qq.com/"
	cdnBaseURL      = "https://novac2c.cdn.weixin.qq.com/c2c"
	cdnHost         = "novac2c.cdn.weixin.qq.com"
	protocolVersion = "2.4.6"
	defaultBotType  = "3"
	// maxMessageChars 单条文本消息上限（协议约束 1800 字符，超长由调用方按 splitText 分段）。
	maxMessageChars = 1800
	defaultTimeout  = 15 * time.Second
	longPollTimeout = 35 * time.Second
	cdnUploadRetry  = 3
	cdnUploadExpiry = 120 * time.Second
	// maxMediaBytes CDN 单文件上下行硬上限（防御异常大文件占满内存）。
	maxMediaBytes = 100 << 20
	botAgent      = "OceanHarness/1.0.0"
)

// 错误分类码：timeout/network/http 为可重试网络族；stale-token 为终态（须重新扫码）；
// delivery-uncertain 表示出站结果不确定（可能已投出，禁止盲目重试造成重复投递）。
const (
	errNetwork    = "network-error"
	errTimeout    = "timeout"
	errHTTP       = "http-error"
	errInvalid    = "invalid-response"
	errStaleToken = "stale-token"
	errRejected   = "rejected"
	errUncertain  = "delivery-uncertain"
)

/*
 * APIError 微信服务交互失败的分类错误：Code 判定可重试/终态，Status 为 HTTP 状态码，
 * ProviderCode 为业务码（ret/errcode）原文。
 */
type APIError struct {
	Code         string
	Status       int
	ProviderCode string
	msg          string
}

func (e *APIError) Error() string { return e.msg }

func apiErr(code, format string, args ...any) *APIError {
	return &APIError{Code: code, msg: fmt.Sprintf(format, args...)}
}

/*
 * numString 数值/字符串双形态兼容字段：保留 JSON 原文字节不做数值化。iLink 的 id 类字段
 * （message_id 等）为 16-20 位大整数，超 float64/int64 安全范围，数值化会静默丢精度。
 */
type numString string

func (n *numString) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		*n = ""
		return nil
	}
	if len(trimmed) > 1 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*n = numString(s)
		return nil
	}
	*n = numString(trimmed)
	return nil
}

func (n numString) String() string { return string(n) }

// isZero 空/0 归一（ret==0 = 成功；数字 0 与字符串 "0" 两形态等价）。
func (n numString) isZero() bool {
	s := strings.TrimSpace(string(n))
	if s == "" {
		return true
	}
	v, err := strconv.ParseFloat(s, 64)
	return err == nil && v == 0
}

// providerEnvelope 业务码信封：ret/errcode 任一非 0 即拒绝（两字段均可缺席）。
type providerEnvelope struct {
	Ret     numString `json:"ret"`
	ErrCode numString `json:"errcode"`
}

func (e providerEnvelope) rejected() string {
	if !e.Ret.isZero() {
		return e.Ret.String()
	}
	if !e.ErrCode.isZero() {
		return e.ErrCode.String()
	}
	return ""
}

// checkEnvelope 拒绝码归一：-14 = token 失效终态，其余为一般拒绝。
func checkEnvelope(env providerEnvelope, staleMsg, rejectMsg string) error {
	code := env.rejected()
	if code == "" {
		return nil
	}
	if code == "-14" {
		return &APIError{Code: errStaleToken, ProviderCode: code, msg: staleMsg}
	}
	return &APIError{Code: errRejected, ProviderCode: code, msg: rejectMsg}
}

// apiClient iLink 协议客户端（无状态，共享连接池；hostAllowed 仅测试注入，nil = 腾讯域白名单）。
type apiClient struct {
	http        *http.Client
	hostAllowed func(*url.URL) bool
}

func newAPIClient() *apiClient {
	return &apiClient{http: &http.Client{}}
}

type requestSpec struct {
	method   string
	baseURL  string
	endpoint string
	token    string
	body     any
	timeout  time.Duration
}

// requestJSON 规范化 baseUrl → 域白名单校验 → 认证头 → 请求；非 2xx/解析失败归一 APIError。
func (c *apiClient) requestJSON(ctx context.Context, spec requestSpec, out any) error {
	base, err := normalizeBaseURLFormat(spec.baseURL)
	if err != nil {
		return err
	}
	ref, err := url.Parse(spec.endpoint)
	if err != nil {
		return apiErr(errInvalid, "微信服务地址无效。")
	}
	full := base.ResolveReference(ref)
	allowed := trustedWeixinAPIHost
	if c.hostAllowed != nil {
		allowed = c.hostAllowed
	}
	if !allowed(full) {
		return apiErr("untrusted-endpoint", "拒绝访问不受信任的微信服务地址。")
	}
	timeout := spec.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var payload io.Reader
	if spec.body != nil {
		encoded, err := json.Marshal(spec.body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, spec.method, full.String(), payload)
	if err != nil {
		return err
	}
	req.Header.Set("iLink-App-Id", "bot")
	req.Header.Set("iLink-App-ClientVersion", ilinkClientVersion)
	if spec.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if spec.token != "" {
		req.Header.Set("AuthorizationType", "ilink_bot_token")
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(spec.token))
		req.Header.Set("X-WECHAT-UIN", wechatUIN())
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return apiErr(errTimeout, "微信服务请求超时。")
		}
		return apiErr(errNetwork, "暂时无法访问微信服务。")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Code: errHTTP, Status: resp.StatusCode,
			msg: fmt.Sprintf("微信服务请求失败（HTTP %d）。", resp.StatusCode)}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return apiErr(errInvalid, "微信服务返回了无法解析的响应。")
	}
	return nil
}

// baseInfo 协议要求每个业务请求体携带的版本自报字段。
type baseInfo struct {
	ChannelVersion string `json:"channel_version"`
	BotAgent       string `json:"bot_agent"`
}

func newBaseInfo() baseInfo {
	return baseInfo{ChannelVersion: protocolVersion, BotAgent: botAgent}
}

// ---------- 扫码登录 ----------

type loginQR struct {
	QRCode    string // 轮询令牌（get_qrcode_status 凭证）
	QRCodeURL string // 编码进二维码的扫码链接（非图片地址，前端本地渲染）
}

/*
 * BeginLogin 申请登录二维码（bot_type=3 个人微信号，免认证）；localTokens 传既有 bot token
 * （服务端绑定感知，dsh-im 同款，去重取末 10 个）。
 */
func (c *apiClient) BeginLogin(ctx context.Context, localTokens []string) (*loginQR, error) {
	tokens := make([]string, 0, len(localTokens))
	seen := make(map[string]bool, len(localTokens))
	for _, t := range localTokens {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			tokens = append(tokens, t)
		}
	}
	if len(tokens) > 10 {
		tokens = tokens[len(tokens)-10:]
	}
	var resp struct {
		providerEnvelope
		QRCode    string `json:"qrcode"`
		QRCodeURL string `json:"qrcode_img_content"`
	}
	if err := c.requestJSON(ctx, requestSpec{
		method:   http.MethodPost,
		baseURL:  qrBaseURL,
		endpoint: "ilink/bot/get_bot_qrcode?bot_type=" + defaultBotType,
		body:     map[string]any{"local_token_list": tokens},
		timeout:  10 * time.Second,
	}, &resp); err != nil {
		return nil, err
	}
	if code := resp.rejected(); code != "" {
		return nil, &APIError{Code: errRejected, ProviderCode: code, msg: "微信服务拒绝了二维码申请。"}
	}
	qrURL, err := normalizeQRURL(resp.QRCodeURL)
	if err != nil {
		return nil, err
	}
	if resp.QRCode == "" {
		return nil, apiErr(errInvalid, "微信服务没有返回二维码令牌。")
	}
	return &loginQR{QRCode: resp.QRCode, QRCodeURL: qrURL}, nil
}

// loginStatusSet 扫码轮询合法状态全集（协议契约，未知状态视为协议破坏）。
var loginStatusSet = map[string]bool{
	"wait": true, "scaned": true, "confirmed": true, "expired": true,
	"scaned_but_redirect": true, "need_verifycode": true,
	"verify_code_blocked": true, "binded_redirect": true,
}

type loginPoll struct {
	providerEnvelope
	Status       string `json:"status"`
	BotToken     string `json:"bot_token"`
	IlinkBotID   string `json:"ilink_bot_id"`
	IlinkUserID  string `json:"ilink_user_id"`
	BaseURL      string `json:"baseurl"`
	RedirectHost string `json:"redirect_host"`
}

// PollLogin 长轮询一次扫码状态（confirmed 携带激活凭据；scaned_but_redirect 携带区域跳转 host）。
func (c *apiClient) PollLogin(ctx context.Context, qrcode, baseURL, verifyCode string) (*loginPoll, error) {
	if qrcode == "" {
		return nil, errors.New("qrcode is required")
	}
	endpoint := "ilink/bot/get_qrcode_status?qrcode=" + url.QueryEscape(qrcode)
	if verifyCode != "" {
		endpoint += "&verify_code=" + url.QueryEscape(verifyCode)
	}
	var resp loginPoll
	if err := c.requestJSON(ctx, requestSpec{
		method:   http.MethodGet,
		baseURL:  baseURL,
		endpoint: endpoint,
		timeout:  longPollTimeout,
	}, &resp); err != nil {
		return nil, err
	}
	if !loginStatusSet[resp.Status] {
		return nil, apiErr(errInvalid, "微信服务返回了无法识别的扫码状态。")
	}
	return &resp, nil
}

// ---------- 消息收发 ----------

type inboundMessage struct {
	MessageID    numString     `json:"message_id"`
	ClientID     string        `json:"client_id"`
	FromUserID   string        `json:"from_user_id"`
	ToUserID     string        `json:"to_user_id"`
	MessageType  int           `json:"message_type"`
	MessageState int           `json:"message_state"`
	CreateTimeMs numString     `json:"create_time_ms"`
	Seq          numString     `json:"seq"`
	ContextToken string        `json:"context_token"`
	ItemList     []inboundItem `json:"item_list"`
}

// item 类型：1=文本 2=图片 3=语音 4=文件 5=视频（入站判型按 type 与具名字段双保险）。
type inboundItem struct {
	Type      int               `json:"type"`
	TextItem  *inboundTextItem  `json:"text_item"`
	VoiceItem *inboundVoiceItem `json:"voice_item"`
	ImageItem *inboundMediaItem `json:"image_item"`
	FileItem  *inboundFileItem  `json:"file_item"`
	VideoItem *inboundMediaItem `json:"video_item"`
	RefMsg    *inboundRefMsg    `json:"ref_msg"`
}

type inboundTextItem struct {
	Text string `json:"text"`
}

// inboundVoiceItem 语音项：Text 为服务端转写文本（有值即可作文本消息处理）。
type inboundVoiceItem struct {
	Text string `json:"text"`
}

// inboundMedia 媒体下载参数（query param 优先，full_url 兜底；aes_key 为 base64 编码密钥）。
type inboundMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param"`
	FullURL           string `json:"full_url"`
	AESKey            string `json:"aes_key"`
}

// inboundMediaItem 图片/视频共用媒体项（视频无独立字段，结构与图片一致）。
type inboundMediaItem struct {
	AESKey string       `json:"aeskey"`
	Media  inboundMedia `json:"media"`
}

type inboundFileItem struct {
	AESKey   string       `json:"aeskey"`
	Media    inboundMedia `json:"media"`
	FileName string       `json:"file_name"`
	Len      numString    `json:"len"`
}

// inboundRefMessage 引用消息摘要（msg_id 为被引消息 id，type/text 同 item 语义）。
type inboundRefMessage struct {
	MessageID numString         `json:"msg_id"`
	Type      int               `json:"type"`
	TextItem  *inboundTextItem  `json:"text_item"`
	VoiceItem *inboundVoiceItem `json:"voice_item"`
	ImageItem *inboundMediaItem `json:"image_item"`
	FileItem  *inboundFileItem  `json:"file_item"`
}

// inboundRefMsg 一级引用快照（ref_msg）：message_item 为被引消息摘要，title 为文本兜底。
type inboundRefMsg struct {
	MessageItem  *inboundRefMessage `json:"message_item"`
	Title        string             `json:"title"`
	CreateTimeMs numString          `json:"create_time_ms"`
	UpdateTimeMs numString          `json:"update_time_ms"`
}

type updatesResponse struct {
	providerEnvelope
	Msgs          []inboundMessage `json:"msgs"`
	GetUpdatesBuf string           `json:"get_updates_buf"`
}

/*
 * GetUpdates 长轮询一批新消息：超时归一空批（正常节拍），取消（Stop）原样上抛；
 * ret=-14 = token 失效终态。
 */
func (c *apiClient) GetUpdates(ctx context.Context, token, baseURL, buf string) (*updatesResponse, error) {
	var resp updatesResponse
	err := c.requestJSON(ctx, requestSpec{
		method:   http.MethodPost,
		baseURL:  baseURL,
		endpoint: "ilink/bot/getupdates",
		token:    token,
		timeout:  longPollTimeout,
		body:     map[string]any{"get_updates_buf": buf, "base_info": newBaseInfo()},
	}, &resp)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Code == errTimeout {
			return &updatesResponse{GetUpdatesBuf: buf}, nil
		}
		return nil, err
	}
	if err := checkEnvelope(resp.providerEnvelope, "微信登录凭据已失效，请重新扫码授权。", "微信消息同步请求被拒绝。"); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ---------- 出站发送 ----------

type sendItem struct {
	Type      int            `json:"type"`
	TextItem  *sendTextItem  `json:"text_item,omitempty"`
	ImageItem *sendImageItem `json:"image_item,omitempty"`
	FileItem  *sendFileItem  `json:"file_item,omitempty"`
}

type sendTextItem struct {
	Text string `json:"text"`
}

// sendMedia 出站媒体描述（CDN 下载参数 + 上传密钥回传，密钥编码 base64(32-hex)）。
type sendMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param"`
	AESKey            string `json:"aes_key"`
	EncryptType       int    `json:"encrypt_type"`
}

type sendImageItem struct {
	Media   sendMedia `json:"media"`
	MidSize int       `json:"mid_size"`
}

type sendFileItem struct {
	Media    sendMedia `json:"media"`
	FileName string    `json:"file_name"`
	Len      string    `json:"len"`
}

type sendMessageBody struct {
	FromUserID   string     `json:"from_user_id"`
	ToUserID     string     `json:"to_user_id"`
	ClientID     string     `json:"client_id"`
	MessageType  int        `json:"message_type"`
	MessageState int        `json:"message_state"`
	ItemList     []sendItem `json:"item_list"`
	ContextToken string     `json:"context_token,omitempty"`
}

// SendText 发送单条文本（≤1800 字符，分段由调用方完成），返回幂等 client_id。
func (c *apiClient) SendText(ctx context.Context, token, baseURL, to, text, contextToken string) (string, error) {
	clientID := "ocean-weixin-" + newRequestID()
	var resp providerEnvelope
	err := c.requestJSON(ctx, requestSpec{
		method:   http.MethodPost,
		baseURL:  baseURL,
		endpoint: "ilink/bot/sendmessage",
		token:    token,
		body: map[string]any{
			"msg": sendMessageBody{
				FromUserID: "", ToUserID: to, ClientID: clientID,
				MessageType: 2, MessageState: 2,
				ItemList:     []sendItem{{Type: 1, TextItem: &sendTextItem{Text: text}}},
				ContextToken: contextToken,
			},
			"base_info": newBaseInfo(),
		},
	}, &resp)
	if err != nil {
		return "", err
	}
	if err := checkEnvelope(resp, "", "微信服务拒绝了回复消息。"); err != nil {
		return "", err
	}
	return clientID, nil
}

// Artifact 出站媒体产物（DeliveryKey 为唯一投递键，决定幂等 client_id）。
type Artifact struct {
	FileName    string
	Bytes       []byte
	DeliveryKey string
}

// uncertainDelivery 出站媒体结果不确定（网络中断/超时/5xx——可能已投出，上层禁止降级重发）。
func uncertainDelivery(err error) error {
	var ae *APIError
	if errors.As(err, &ae) && (ae.Code == errNetwork || ae.Code == errTimeout || ae.Status >= 500) {
		return &APIError{Code: errUncertain, Status: ae.Status, msg: "微信文件投递结果不确定。"}
	}
	return err
}

// artifactClientID 幂等投递键派生 client_id：文件直用 DeliveryKey，图片追加 NUL+mediaType
// （同键异型不同 id；同键同型重试不重复投递）。
func artifactClientID(deliveryKey string, mediaType int) string {
	seed := deliveryKey
	if mediaType != 3 {
		seed = fmt.Sprintf("%s\x00%d", deliveryKey, mediaType)
	}
	return "ocean-weixin-" + hexEncode(sha256Sum([]byte(seed))[:16])
}

/*
 * SendArtifact 三步投递：getuploadurl 申请 → CDN AES-128-ECB 密文上传 → sendmessage 发媒体消息。
 * mediaType 1=图片 2 类型项 / 3=文件 4 类型项；client_id 由 DeliveryKey 派生（文件直用原文，
 * 图片追加 NUL+mediaType，同键异型不同 id）。
 */
func (c *apiClient) SendArtifact(ctx context.Context, token, baseURL, to string, art *Artifact, mediaType int, contextToken string) (string, error) {
	if to == "" || art == nil || art.FileName == "" || len(art.Bytes) == 0 {
		return "", errors.New("toUserId and file are required")
	}
	fileKey := newRequestID()
	aesKey := newAESKey()
	rawMD5 := md5Sum(art.Bytes)

	var up struct {
		providerEnvelope
		UploadFullURL string `json:"upload_full_url"`
		UploadParam   string `json:"upload_param"`
	}
	if err := c.requestJSON(ctx, requestSpec{
		method:   http.MethodPost,
		baseURL:  baseURL,
		endpoint: "ilink/bot/getuploadurl",
		token:    token,
		body: map[string]any{
			"filekey": fileKey, "media_type": mediaType, "to_user_id": to,
			"rawsize": len(art.Bytes), "rawfilemd5": hexEncode(rawMD5),
			"filesize": paddedSize(len(art.Bytes)), "no_need_thumb": true,
			"aeskey": hexEncode(aesKey), "base_info": newBaseInfo(),
		},
	}, &up); err != nil {
		return "", err
	}
	if code := up.rejected(); code != "" {
		return "", &APIError{Code: errRejected, ProviderCode: code, msg: "微信服务拒绝了文件上传请求。"}
	}
	uploadURL, err := cdnUploadURL(up.UploadFullURL, up.UploadParam, fileKey)
	if err != nil {
		return "", err
	}
	ciphertext, err := aesECBEncrypt(art.Bytes, aesKey)
	if err != nil {
		return "", err
	}
	downloadParam, err := c.uploadCDN(ctx, uploadURL, ciphertext)
	if err != nil {
		return "", err
	}
	clientID := artifactClientID(art.DeliveryKey, mediaType)
	media := sendMedia{
		EncryptQueryParam: downloadParam,
		AESKey:            base64StdEncode([]byte(hexEncode(aesKey))),
		EncryptType:       1,
	}
	item := sendItem{Type: 2, ImageItem: &sendImageItem{Media: media, MidSize: len(ciphertext)}}
	if mediaType == 3 {
		item = sendItem{Type: 4, FileItem: &sendFileItem{Media: media,
			FileName: art.FileName, Len: strconv.Itoa(len(art.Bytes))}}
	}
	var resp providerEnvelope
	sendErr := c.requestJSON(ctx, requestSpec{
		method:   http.MethodPost,
		baseURL:  baseURL,
		endpoint: "ilink/bot/sendmessage",
		token:    token,
		body: map[string]any{
			"msg": sendMessageBody{
				FromUserID: "", ToUserID: to, ClientID: clientID,
				MessageType: 2, MessageState: 2,
				ItemList:     []sendItem{item},
				ContextToken: contextToken,
			},
			"base_info": newBaseInfo(),
		},
	}, &resp)
	if sendErr != nil {
		return "", uncertainDelivery(sendErr)
	}
	if err := checkEnvelope(resp, "", "微信服务拒绝了文件消息。"); err != nil {
		return "", err
	}
	return clientID, nil
}

// uploadCDN CDN 密文上传（重试 3 次；4xx 明确拒绝不重试；成功取响应头 x-encrypted-param）。
func (c *apiClient) uploadCDN(ctx context.Context, uploadURL string, ciphertext []byte) (string, error) {
	var lastErr error
	for attempt := range cdnUploadRetry {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		param, err := c.uploadCDNOnce(ctx, uploadURL, ciphertext)
		if err == nil {
			return param, nil
		}
		var ae *APIError
		if errors.As(err, &ae) && ae.Code == "upload-rejected" {
			return "", err
		}
		lastErr = err
	}
	return "", lastErr
}

func (c *apiClient) uploadCDNOnce(ctx context.Context, uploadURL string, ciphertext []byte) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, cdnUploadExpiry)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, uploadURL, bytes.NewReader(ciphertext))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(ciphertext))
	resp, err := c.http.Do(req)
	if err != nil {
		if cctx.Err() != nil && errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return "", apiErr("upload-timeout", "微信文件上传长时间没有进展，已超时。")
		}
		return "", apiErr(errNetwork, "微信文件上传网络失败。")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return "", &APIError{Code: "upload-rejected", Status: resp.StatusCode,
			msg: fmt.Sprintf("微信文件上传被拒绝（HTTP %d）。", resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Code: errHTTP, Status: resp.StatusCode,
			msg: fmt.Sprintf("微信文件上传失败（HTTP %d）。", resp.StatusCode)}
	}
	param := resp.Header.Get("x-encrypted-param")
	if param == "" {
		return "", apiErr(errInvalid, "微信文件上传响应缺少下载参数。")
	}
	return param, nil
}

// ---------- 输入状态与会话生命周期通知 ----------

// GetConfig 拉取 typing 会话票据。
func (c *apiClient) GetConfig(ctx context.Context, token, baseURL, to, contextToken string) (string, error) {
	var resp struct {
		providerEnvelope
		TypingTicket string `json:"typing_ticket"`
	}
	body := map[string]any{"ilink_user_id": to, "base_info": newBaseInfo()}
	if contextToken != "" {
		body["context_token"] = contextToken
	}
	if err := c.requestJSON(ctx, requestSpec{
		method:   http.MethodPost,
		baseURL:  baseURL,
		endpoint: "ilink/bot/getconfig",
		token:    token,
		timeout:  10 * time.Second,
		body:     body,
	}, &resp); err != nil {
		return "", err
	}
	if err := checkEnvelope(resp.providerEnvelope, "", "微信服务拒绝了机器人配置请求。"); err != nil {
		return "", err
	}
	return resp.TypingTicket, nil
}

// SendTyping 上报输入状态（status 1=开始 2=结束）。
func (c *apiClient) SendTyping(ctx context.Context, token, baseURL, to, ticket string, status int) error {
	var resp providerEnvelope
	err := c.requestJSON(ctx, requestSpec{
		method:   http.MethodPost,
		baseURL:  baseURL,
		endpoint: "ilink/bot/sendtyping",
		token:    token,
		timeout:  10 * time.Second,
		body: map[string]any{
			"ilink_user_id": to, "typing_ticket": ticket, "status": status,
			"base_info": newBaseInfo(),
		},
	}, &resp)
	if err != nil {
		return err
	}
	return checkEnvelope(resp, "", "微信服务拒绝了输入状态请求。")
}

// NotifyStart 上线通知（服务端随后向本连接投递新消息；-14 = token 失效终态）。
func (c *apiClient) NotifyStart(ctx context.Context, token, baseURL string) error {
	return c.notify(ctx, token, baseURL, "ilink/bot/msg/notifystart", "微信账号连接启动失败。")
}

// NotifyStop 下线通知（尽力而为，失败不影响停止）。
func (c *apiClient) NotifyStop(ctx context.Context, token, baseURL string) error {
	return c.notify(ctx, token, baseURL, "ilink/bot/msg/notifystop", "微信服务未确认停止通知。")
}

func (c *apiClient) notify(ctx context.Context, token, baseURL, endpoint, rejectMsg string) error {
	var resp providerEnvelope
	err := c.requestJSON(ctx, requestSpec{
		method:   http.MethodPost,
		baseURL:  baseURL,
		endpoint: endpoint,
		token:    token,
		timeout:  10 * time.Second,
		body:     map[string]any{"base_info": newBaseInfo()},
	}, &resp)
	if err != nil {
		return err
	}
	return checkEnvelope(resp, "微信登录凭据已失效，请重新扫码授权。", rejectMsg)
}

// ---------- CDN 下载 ----------

// DownloadCdn 下载 CDN 密文（受信域校验 + 大小上限）。
func (c *apiClient) DownloadCdn(ctx context.Context, downloadURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, cdnUploadExpiry)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, apiErr(errNetwork, "微信媒体下载网络失败。")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Code: errHTTP, Status: resp.StatusCode,
			msg: fmt.Sprintf("微信媒体下载失败（HTTP %d）。", resp.StatusCode)}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		return nil, apiErr(errNetwork, "微信媒体下载读取失败。")
	}
	if len(data) > maxMediaBytes {
		return nil, apiErr("media-too-large", "微信媒体超过大小上限。")
	}
	return data, nil
}
