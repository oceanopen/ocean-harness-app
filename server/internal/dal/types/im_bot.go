package types

import (
	"encoding/json"
	"fmt"
	"time"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
)

// ImBotAccessPolicy 访问白名单（t_im_bots.access_policy JSON 的请求/响应形态）。
// 单 scope：单聊/群聊共用一套（按发送者 userid 判定）；mode oneof 校验在 Bind 链完成。
type ImBotAccessPolicy struct {
	Mode       string   `json:"mode" binding:"required,oneof=open allowlist"` // open | allowlist
	AllowUsers []string `json:"allowUsers"`                                   // allowlist 模式生效
}

// ImBotCredential credential JSON 列的 shape SSOT（service 序列化 / FromModel 反序列化共用）。
// wecom 适配器内的同构结构属渠道边界解释层，刻意不共享（核心/DTO 不依赖渠道包）。
type ImBotCredential struct {
	BotId  string `json:"botId"`
	Secret string `json:"secret"`
}

// 每 action 一个独立 Request 类型（对齐项目既有 DTO 风格）。
// channel 本期固定 wecom（service 赋值，不入参）；secret 更新时留空 = 沿用原值。

// ImBotGetListRequest 是 POST /api/imBot/getList 的入参（无筛选，列全部）。
type ImBotGetListRequest struct{}

// ImBotGetInfoRequest 是 POST /api/imBot/getInfo 的入参。
type ImBotGetInfoRequest struct {
	ID int `json:"id" binding:"required"`
}

// ImBotCreateRequest 是 POST /api/imBot/create 的入参。
type ImBotCreateRequest struct {
	Name         string            `json:"name" binding:"required,max=100"`
	BotId        string            `json:"botId" binding:"required"`
	Secret       string            `json:"secret" binding:"required"`
	WorkspaceDir string            `json:"workspaceDir"` // 可空 = 未配置（对话时提醒补配）
	Model        string            `json:"model" binding:"omitempty"`
	SystemPrompt string            `json:"systemPrompt" binding:"omitempty,max=8000"`
	AllowedTools []string          `json:"allowedTools"`
	AccessPolicy ImBotAccessPolicy `json:"accessPolicy" binding:"required"`
	Enabled      bool              `json:"enabled"`
}

// ImBotUpdateRequest 是 POST /api/imBot/update 的入参（secret 留空 = 沿用原值）。
type ImBotUpdateRequest struct {
	ID           int               `json:"id" binding:"required"`
	Name         string            `json:"name" binding:"required,max=100"`
	BotId        string            `json:"botId" binding:"required"`
	Secret       string            `json:"secret" binding:"omitempty"`
	WorkspaceDir string            `json:"workspaceDir"` // 可空 = 未配置
	Model        string            `json:"model" binding:"omitempty"`
	SystemPrompt string            `json:"systemPrompt" binding:"omitempty,max=8000"`
	AllowedTools []string          `json:"allowedTools"`
	AccessPolicy ImBotAccessPolicy `json:"accessPolicy" binding:"required"`
	Enabled      bool              `json:"enabled"`
}

// ImBotDeleteRequest 是 POST /api/imBot/delete 的入参（物理删除 + 级联删会话映射）。
type ImBotDeleteRequest struct {
	ID int `json:"id" binding:"required"`
}

// ImBotRestartRequest 是 POST /api/imBot/restart 的入参（以当前配置重连，被踢/改配置后恢复用）。
type ImBotRestartRequest struct {
	ID int `json:"id" binding:"required"`
}

// ImBotProvisionBeginRequest 是 POST /api/imBot/provisionBegin 的入参（无入参，开新扫码会话；
// 已有进行中会话自动取消）。
type ImBotProvisionBeginRequest struct{}

// ImBotProvisionPollRequest 是 POST /api/imBot/provisionPoll 的入参（惰性单飞轮询一次）。
type ImBotProvisionPollRequest struct {
	AttemptID string `json:"attemptId" binding:"required"`
}

// ImBotProvisionCancelRequest 是 POST /api/imBot/provisionCancel 的入参。
type ImBotProvisionCancelRequest struct {
	AttemptID string `json:"attemptId" binding:"required"`
}

// ImBotProvisionView 扫码会话投影。剥除 scode 与 secret（凭据仅在服务端落库，绝不回传前端）；
// qrContent 为腾讯授权页 URL（二维码内容，公开可回传，前端本地渲染二维码）。
type ImBotProvisionView struct {
	AttemptID      string `json:"attemptId"`
	State          string `json:"state"` // pending | connecting | connected | failed | expired | cancelled
	QRContent      string `json:"qrContent,omitempty"`
	ExpiresAt      int64  `json:"expiresAt"` // 毫秒时间戳（0 = 无 QR 语境）
	PollIntervalMs int    `json:"pollIntervalMs"`
	ErrCode        string `json:"errCode,omitempty"`
	BotID          int    `json:"botId,omitempty"` // connected 后的新 bot 行 id
}

// ImBotResponseData bot 响应：JSON 列反序列化为结构 + credential 脱敏 + 运行态合并。
// 不嵌入 *model.ImBot（JSON 列为 string、enabled 为 YesNo），扁平呈现形态由 FromModel 装配。
type ImBotResponseData struct {
	ID           int               `json:"id"`
	Name         string            `json:"name"`
	Channel      enums.Channel     `json:"channel"`
	WorkspaceDir string            `json:"workspaceDir"`
	Model        string            `json:"model"`
	SystemPrompt string            `json:"systemPrompt"`
	AllowedTools []string          `json:"allowedTools"`
	AccessPolicy ImBotAccessPolicy `json:"accessPolicy"`
	Enabled      bool              `json:"enabled"`
	// credential 脱敏投影：永不回传明文（尾 4 位 + 是否已设置；前端编辑留空 = 沿用原值）。
	SecretMasked string `json:"secretMasked"`
	HasSecret    bool   `json:"hasSecret"`
	// credential 内的 botId（企微后台颁发），表单回显用。
	BotId string `json:"botId"`
	// 运行态（supervisor 投影合并；未运行 = stopped，LastError 回落 DB 行 last_error）。
	ConnState string `json:"connState"`
	LastError string `json:"lastError"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// FromModel DO → 响应：JSON 列解析失败兜底零值（不阻塞列表加载）；secret 只出掩码。
func (ImBotResponseData) FromModel(r *model.ImBot) ImBotResponseData {
	out := ImBotResponseData{
		ID:           r.ID,
		Name:         r.Name,
		Channel:      r.Channel,
		WorkspaceDir: r.WorkspaceDir,
		Model:        r.Model,
		SystemPrompt: r.SystemPrompt,
		AllowedTools: []string{},
		Enabled:      r.Enabled.IsYes(),
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
	_ = json.Unmarshal([]byte(r.AllowedTools), &out.AllowedTools) // 失败保留空数组
	if out.AllowedTools == nil {
		out.AllowedTools = []string{}
	}
	_ = json.Unmarshal([]byte(r.AccessPolicy), &out.AccessPolicy)
	if r.Credential != "" {
		var cred ImBotCredential
		if err := json.Unmarshal([]byte(r.Credential), &cred); err == nil {
			out.BotId = cred.BotId
			out.HasSecret = cred.Secret != ""
			if n := len(cred.Secret); n > 0 {
				if n <= 4 {
					out.SecretMasked = "****"
				} else {
					out.SecretMasked = fmt.Sprintf("****%s", cred.Secret[n-4:])
				}
			}
		}
	}
	return out
}
