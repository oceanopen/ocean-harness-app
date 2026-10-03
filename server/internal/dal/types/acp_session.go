package types

// ACP 会话域（/api/acpSession）的请求 DTO。响应 shape SSOT 是 acpsession.ViewSnapshot
//（嵌套 ACP wire 结构原样透传，镜像到 types 只会双份维护，见 view.go 的 SSOT 注释）。

// AcpSessionEnsureRequest 是 POST /api/acpSession/ensure 的入参（幂等受理，异步建模）。
type AcpSessionEnsureRequest struct {
	IssueID string `json:"issueId" binding:"required"`
}

// AcpSessionGetInfoRequest 是 POST /api/acpSession/getInfo 的入参。
type AcpSessionGetInfoRequest struct {
	IssueID string `json:"issueId" binding:"required"`
}

// AcpSessionPromptRequest 是 POST /api/acpSession/prompt 的入参（受理即返回，回合终态经 SSE）。
type AcpSessionPromptRequest struct {
	IssueID string `json:"issueId" binding:"required"`
	Text    string `json:"text" binding:"required"`
}

// AcpSessionCancelRequest 是 POST /api/acpSession/cancel 的入参（软取消当前回合，幂等）。
type AcpSessionCancelRequest struct {
	IssueID string `json:"issueId" binding:"required"`
}

// AcpSessionRespondPermissionRequest 是 POST /api/acpSession/respondPermission 的入参
// （pendingId 为会话域本地序号，自 1 起）。
type AcpSessionRespondPermissionRequest struct {
	IssueID   string `json:"issueId" binding:"required"`
	PendingID uint64 `json:"pendingId" binding:"required"`
	OptionID  string `json:"optionId" binding:"required"`
}

// AcpSessionRespondElicitationRequest 是 POST /api/acpSession/respondElicitation 的入参
// （action 三态；content 仅 accept 时有意义，键值对透传 ACP wire）。
type AcpSessionRespondElicitationRequest struct {
	IssueID   string         `json:"issueId" binding:"required"`
	PendingID uint64         `json:"pendingId" binding:"required"`
	Action    string         `json:"action" binding:"required,oneof=accept decline cancel"`
	Content   map[string]any `json:"content"`
}

// AcpSessionEventsRequest 是 GET /api/acpSession/events 的入参（SSE 长连，issueId 走 query）。
type AcpSessionEventsRequest struct {
	IssueID string `form:"issueId" binding:"required"`
}
