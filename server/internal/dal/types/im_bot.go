package types

import (
	"encoding/json"
	"time"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
)

/*
 * ImBotAccessPolicy 访问白名单（t_im_bots.access_policy JSON 的请求/响应形态）：单 scope，单聊/群聊共用一套（按发送者 userid 判定）；
 * mode oneof 校验在 Bind 链完成。
 */
type ImBotAccessPolicy struct {
	Mode       string   `json:"mode" binding:"required,oneof=open allowlist"` // open | allowlist
	AllowUsers []string `json:"allowUsers"`                                   // allowlist 模式生效
}

/*
 * ImBotCredential credential JSON 列的 shape SSOT（service 序列化 / FromModel 反序列化共用）；
 * wecom 适配器内的同构结构属渠道边界解释层，刻意不共享（核心/DTO 不依赖渠道包）。
 */
type ImBotCredential struct {
	BotId  string `json:"botId"`
	Secret string `json:"secret"`
}

/*
 * 每 action 一个独立 Request 类型（对齐项目既有 DTO 风格）；secret 更新时留空 = 沿用原值。
 * 渠道差异：wecom 凭据手工录入（botId/secret），weixin 凭据唯一来源是扫码授权（Create 拒绝、
 * Update 不触凭据列、换绑走 provision 链路）。
 */

// ImBotGetListRequest 是 POST /api/imBot/getList 的入参（无筛选，列全部）。
type ImBotGetListRequest struct{}

// ImBotGetInfoRequest 是 POST /api/imBot/getInfo 的入参。
type ImBotGetInfoRequest struct {
	ID string `json:"id" binding:"required"`
}

// ImBotCreateRequest 是 POST /api/imBot/create 的入参（仅 wecom 可手工创建；weixin 由 service 拒绝并引导扫码）。
// 归属四字段空串 = 未配置哨兵（conversationType ∈ {workspace,project}，两档必填校验随 T4.1 抽屉改造落地）。
type ImBotCreateRequest struct {
	Channel          enums.Channel          `json:"channel" binding:"required,oneof=wecom weixin"`
	Name             string                 `json:"name" binding:"required,max=100"`
	BotId            string                 `json:"botId" binding:"required"`
	Secret           string                 `json:"secret" binding:"required"`
	ConversationType enums.ConversationType `json:"conversationType"`
	WorkspaceId      string                 `json:"workspaceId"`
	ProjectId        string                 `json:"projectId"`
	TaskId           string                 `json:"taskId"`
	AccessPolicy     ImBotAccessPolicy      `json:"accessPolicy" binding:"required"`
	Enabled          bool                   `json:"enabled"`
}

// ImBotUpdateRequest 是 POST /api/imBot/update 的入参（secret 留空 = 沿用原值；weixin 行仅更新非凭据字段）。
type ImBotUpdateRequest struct {
	ID               string                 `json:"id" binding:"required"`
	Name             string                 `json:"name" binding:"required,max=100"`
	BotId            string                 `json:"botId" binding:"omitempty"`
	Secret           string                 `json:"secret" binding:"omitempty"`
	ConversationType enums.ConversationType `json:"conversationType"`
	WorkspaceId      string                 `json:"workspaceId"`
	ProjectId        string                 `json:"projectId"`
	TaskId           string                 `json:"taskId"`
	AccessPolicy     ImBotAccessPolicy      `json:"accessPolicy" binding:"required"`
	Enabled          bool                   `json:"enabled"`
}

// ImBotDeleteRequest 是 POST /api/imBot/delete 的入参（物理删除 + 级联删会话映射）。
type ImBotDeleteRequest struct {
	ID string `json:"id" binding:"required"`
}

// ImBotRestartRequest 是 POST /api/imBot/restart 的入参（以当前配置重连，被踢/改配置后恢复用）。
type ImBotRestartRequest struct {
	ID string `json:"id" binding:"required"`
}

// ImBotGetConversationsRequest 是 POST /api/imBot/getConversations 的入参（bot 的会话列表）。
type ImBotGetConversationsRequest struct {
	BotId string `json:"botId" binding:"required"`
}

/*
 * ImBotConversationData bot 会话行投影（工具栏会话明细数据链）：ChatType 由会话键前缀派生（group: → 群聊，single: → 私聊，key 前缀自描述不落表）；
 * ConversationId 为归属会话锚（指向 t_conversations.id，空 = 未关联）。
 */
type ImBotConversationData struct {
	ConversationKey string     `json:"conversationKey"`
	ChatType        string     `json:"chatType"`        // single | group
	ConversationId  string     `json:"conversationId"`  // 空 = 未关联
	LastMessageAt   *time.Time `json:"lastMessageAt"`   // null = 从未活跃
}

/*
 * ImBotProvisionBeginRequest 是 POST /api/imBot/provisionBegin 的入参（开新扫码会话，已有进行中
 * 会话自动取消；weixin 传非空 botId = 对既有 bot 换绑凭据，空 = 新建；wecom 忽略 botId）。
 */
type ImBotProvisionBeginRequest struct {
	Channel enums.Channel `json:"channel" binding:"required,oneof=wecom weixin"`
	BotID   string        `json:"botId"`
}

// ImBotProvisionPollRequest 是 POST /api/imBot/provisionPoll 的入参（轮询一次扫码结果）。
type ImBotProvisionPollRequest struct {
	Channel   enums.Channel `json:"channel" binding:"required,oneof=wecom weixin"`
	AttemptID string        `json:"attemptId" binding:"required"`
}

// ImBotProvisionCancelRequest 是 POST /api/imBot/provisionCancel 的入参。
type ImBotProvisionCancelRequest struct {
	Channel   enums.Channel `json:"channel" binding:"required,oneof=wecom weixin"`
	AttemptID string        `json:"attemptId" binding:"required"`
}

/*
 * ImBotProvisionVerifyCodeRequest 是 POST /api/imBot/provisionVerifyCode 的入参（微信扫码的
 * 配对码提交，仅 needs_verification 态受理；wecom 无此环节）。
 */
type ImBotProvisionVerifyCodeRequest struct {
	Channel    enums.Channel `json:"channel" binding:"required,oneof=wecom weixin"`
	AttemptID  string        `json:"attemptId" binding:"required"`
	VerifyCode string        `json:"verifyCode" binding:"required"`
}

/*
 * ImBotProvisionView 扫码会话投影：剥除 qrcode 令牌与凭据（凭据仅在服务端落库，provision 扫码
 * 链路不回传；落库后经 getList/getInfo 明文回显，见 ImBotResponseData）；
 * qrContent 为二维码图片 URL（公开可回传，前端本地渲染）。
 */
type ImBotProvisionView struct {
	AttemptID      string `json:"attemptId"`
	State          string `json:"state"` // pending | scanned | needs_verification | connecting | connected | failed | expired | cancelled（wecom 无中间两态）
	QRContent      string `json:"qrContent,omitempty"`
	ExpiresAt      int64  `json:"expiresAt"` // 毫秒时间戳
	PollIntervalMs int    `json:"pollIntervalMs"`
	ErrCode        string `json:"errCode,omitempty"`
	BotID          string `json:"botId,omitempty"` // connected 后的 bot 行 id uuid（含换绑目标）
}

/*
 * ImBotResponseData bot 响应：JSON 列反序列化为结构 + credential 明文回显 + 运行态合并；
 * 不嵌入 *model.ImBot（JSON 列为 string、enabled 为 YesNo），扁平呈现形态由 FromModel 装配。
 */
type ImBotResponseData struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Channel          enums.Channel          `json:"channel"`
	ConversationType enums.ConversationType `json:"conversationType"` // '' = 未配置哨兵
	WorkspaceId      string                 `json:"workspaceId"`      // '' = 未选择（扫码接入待补选）
	WorkspaceName    string                 `json:"workspaceName"`    // 关联工作空间名（service 合并装配）
	ProjectId        string                 `json:"projectId"`        // '' = 未配置
	TaskId           string                 `json:"taskId"`           // '' = 未配置
	AccessPolicy     ImBotAccessPolicy      `json:"accessPolicy"`
	Enabled          bool                   `json:"enabled"`
	// credential 明文 secret：桌面应用数据全本地，编辑抽屉直接回显（前端默认掩码 + 显式切换明文）。
	Secret string `json:"secret"`
	// credential 内的 botId（企微后台颁发），表单回显用；weixin 行为空。
	BotId string `json:"botId"`
	// credential 内的 accountId（微信 iLink 账号 id），列表回显用；wecom 行为空。
	AccountId string `json:"accountId"`
	// 运行态（supervisor 投影合并；未运行 = stopped，LastError 回落 DB 行 last_error）。
	ConnState string `json:"connState"`
	LastError string `json:"lastError"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

/*
 * FromModel DO → 响应：JSON 列解析失败兜底零值（不阻塞列表加载）；secret 明文回传（本地数据语义，编辑抽屉回显，前端自行掩码展示）。
 */
func (ImBotResponseData) FromModel(r *model.ImBot) ImBotResponseData {
	out := ImBotResponseData{
		ID:               r.ID,
		Name:             r.Name,
		Channel:          r.Channel,
		ConversationType: r.ConversationType,
		WorkspaceId:      r.WorkspaceID,
		ProjectId:        r.ProjectID,
		TaskId:           r.TaskID,
		Enabled:          r.Enabled.IsYes(),
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
	_ = json.Unmarshal([]byte(r.AccessPolicy), &out.AccessPolicy)
	if r.Credential != "" {
		var cred ImBotCredential
		if err := json.Unmarshal([]byte(r.Credential), &cred); err == nil {
			out.BotId = cred.BotId
			out.Secret = cred.Secret
		}
		// weixin 凭据 shape 与渠道边界解耦（就地匿名结构投影 accountId，不依赖渠道包）。
		var wxCred struct {
			AccountID string `json:"accountId"`
		}
		if err := json.Unmarshal([]byte(r.Credential), &wxCred); err == nil {
			out.AccountId = wxCred.AccountID
		}
	}
	return out
}
