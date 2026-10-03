package acp

import (
	"context"
	"encoding/json"
	"fmt"

	acpgo "github.com/BrokkAi/acp-go"
	"github.com/BrokkAi/acp-go/schema"
	"go.uber.org/zap"
)

// ElicitationWire elicitation/create 的全保真 wire 投影。acp-go 生成模型把 spec 的
// requestedSchema/url 丢弃（ElicitationFormMode/URLMode 只建模了 scope 变体，v0.11/v0.12
// 均如此，上游生成缺陷），本结构在原始 JSON-RPC 参数进入 typed 解码前自行解析补齐——
// 会话域投影（PendingView.Request）以此为准，typed 模型仅作路由回退与应答通道。
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

// parseElicitationWire 从原始参数解析全保真 wire（宽松解码：未知字段忽略，缺字段留零值
// ——投影按 omitempty 收敛，路由以 SessionID 有无为准）。
func parseElicitationWire(raw json.RawMessage) (*ElicitationWire, error) {
	var wire ElicitationWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("elicitation 参数解析失败: %w", err)
	}
	return &wire, nil
}

// WireFromRequest 从 typed 模型降级构造 wire（拦截未命中/解析失败路径的兜底投影：只能
// 还原 message 与归属，requestedSchema/url 已被上游丢弃不可恢复）。
func WireFromRequest(request schema.CreateElicitationRequest) *ElicitationWire {
	wire := &ElicitationWire{Message: request.Message}
	switch {
	case request.Form != nil:
		wire.Mode = string(schema.CreateElicitationRequestKindForm)
		wire.SessionID, wire.ToolCallID, wire.RequestID = scopeOf(request.Form.Session, request.Form.Request)
	case request.URL != nil:
		wire.Mode = string(schema.CreateElicitationRequestKindUrl)
		wire.SessionID, wire.ToolCallID, wire.RequestID = scopeOf(request.URL.Session, request.URL.Request)
	}
	return wire
}

// scopeOf 提取 mode 内二选一 scope 的归属字段（至多一个非 nil）。
func scopeOf(session *schema.ElicitationSessionScope, request *schema.ElicitationRequestScope) (sessionID, toolCallID, requestID string) {
	if session != nil {
		sessionID = string(session.SessionID)
		if session.ToolCallID != nil {
			toolCallID = string(*session.ToolCallID)
		}
		return sessionID, toolCallID, ""
	}
	if request != nil {
		return "", "", wireRequestID(request.RequestID)
	}
	return "", "", ""
}

// wireRequestID RequestId（wire 上字符串/数值皆可）尽力字符串化：字符串直取，其余
// fmt 化兜底（请求级 elicitation 一期不路由，投影完整性优先）。
func wireRequestID(id schema.RequestId) string {
	if s, ok := id.(string); ok {
		return s
	}
	return fmt.Sprint(id)
}

// elicitationWireCtxKey ctx 携带键（私有类型防跨域碰撞）。
type elicitationWireCtxKey struct{}

func withElicitationWire(ctx context.Context, wire *ElicitationWire) context.Context {
	return context.WithValue(ctx, elicitationWireCtxKey{}, wire)
}

func elicitationWireFromContext(ctx context.Context) *ElicitationWire {
	wire, _ := ctx.Value(elicitationWireCtxKey{}).(*ElicitationWire)
	return wire
}

// elicitationWireInterceptor 入站 Handler 链拦截器：elicitation/create 的原始参数在
// acp-go typed 解码（有损）前自行全保真解析，经 ctx 透传给 CreateElicitation。解析失败
// 只降级投影（warn 落因），不阻断交互本身——表单渲染缺失可容忍，交互挂死不可容忍。
func elicitationWireInterceptor(log *zap.Logger, next acpgo.Handler) acpgo.Handler {
	return func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method == schema.ElicitationCreateMethodName {
			if wire, err := parseElicitationWire(raw); err != nil {
				log.Warn("ACP elicitation wire 解析失败，投影降级 typed 模型", zap.Error(err))
			} else {
				ctx = withElicitationWire(ctx, wire)
			}
		}
		return next(ctx, method, raw)
	}
}
