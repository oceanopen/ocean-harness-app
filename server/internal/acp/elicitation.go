package acp

import (
	"encoding/json"

	"github.com/ironpark/acp-go/acp1"
)

/*
 * ElicitationWire elicitation/create 的全保真 wire 投影（会话域渲染的事实源）。
 * 从原始参数 JSON 直接解析——typed 变体虽全字段在位，但 requestedSchema 等 JSON
 * Schema 载荷经 typed 解码再编码存在键序/未知字段漂移风险，投影以原始 wire 为准。
 */
type ElicitationWire struct {
	Mode    string `json:"mode,omitempty"` // form | url | 其他/缺省（未知形态）
	Message string `json:"message,omitempty"`
	// 会话/请求归属（二选一，会话归属是挂起路由的前提）。
	SessionID  string `json:"sessionId,omitempty"`
	ToolCallID string `json:"toolCallId,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	// form 模式载荷：表单 JSON Schema（前端据此渲染表单）。
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
	// url 模式载荷（一期握手未广告 url 能力，到达即兜底渲染，字段保留投影完整性）。
	URL           string `json:"url,omitempty"`
	ElicitationID string `json:"elicitationId,omitempty"`
}

/*
 * wireFromElicitationParams 从原始参数解析全保真 wire（宽松解码：未知字段忽略，缺字段
 * 留零值——投影按 omitempty 收敛，路由以 SessionID 有无为准；参数为库校验过的合法
 * wire，解析恒成功）。
 */
func wireFromElicitationParams(params *acp1.CreateElicitationRequest) *ElicitationWire {
	var wire ElicitationWire
	_ = json.Unmarshal(params.RawJSON(), &wire)
	return &wire
}

/*
 * elicitationSessionID 解析 elicitation 的会话归属（typed 变体判定，库已校验合法性）：
 * form/url/custom 的 session scope 变体有归属；request 级变体无会话归属（false），
 * 路由层拒绝。
 */
func elicitationSessionID(params *acp1.CreateElicitationRequest) (acp1.SessionID, bool) {
	if form, err := params.As[acp1.CreateElicitationRequestFormSession](); err == nil {
		return form.SessionID, true
	}
	if url, err := params.As[acp1.CreateElicitationRequestURLSession](); err == nil {
		return url.SessionID, true
	}
	if custom, err := params.As[acp1.CreateElicitationRequestCustomSession](); err == nil {
		return custom.SessionID, true
	}
	return "", false
}
