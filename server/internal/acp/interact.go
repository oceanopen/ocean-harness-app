package acp

import (
	"context"
	"errors"
	"fmt"
	"sync"

	acpgo "github.com/BrokkAi/acp-go"
	"github.com/BrokkAi/acp-go/schema"
)

// pendingID 挂起交互的本地序号（client 内单调；T1.5 投影 HTTP/SSE 时用作关联键）。
type pendingID = uint64

// elicitationSessionID 解析 elicitation 的会话归属：顶层无 sessionId 字段，归属在
// form 的 session scope 内；url 模式 / 请求级 elicitation 无会话归属（返回 false）。
func elicitationSessionID(request schema.CreateElicitationRequest) (schema.SessionId, bool) {
	if request.Form != nil && request.Form.Session != nil {
		return request.Form.Session.SessionID, true
	}
	return "", false
}

// ErrAlreadyResolved 交互已被应答或已结算后的重复应答（CAS once 拒绝；
// TODO(T2.4): 桌面/Terminal 双入口审批收敛在其上扩展——first-writer-wins 即本 CAS
// 的跨入口投影）。
var ErrAlreadyResolved = errors.New("ACP 交互已应答或已结算")

// ErrUnknownOption 应答的 optionId 不在请求自带的 Options 里（协议约束：只能从
// 请求选项中选）。不消耗 CAS 名额。
var ErrUnknownOption = errors.New("ACP 应答 optionId 不在该请求的选项中")

// PendingPermission 一次 session/request_permission 的挂起态。注册后经订阅通道投递，
// 消费方 Respond 决策；等待方（连接层 handler goroutine）经 wait 取决策，回合取消 /
// 连接收敛时以 cancelled 收敛。
type PendingPermission struct {
	id        pendingID
	sessionID acpgo.SessionID
	options   []schema.PermissionOption
	toolCall  schema.ToolCallUpdate

	mu       sync.Mutex
	resolved bool
	decided  chan struct{} // 关闭 = 已有最终 outcome（outcome 字段就绪）
	outcome  schema.RequestPermissionOutcome
}

func newPendingPermission(id pendingID, request schema.RequestPermissionRequest) *PendingPermission {
	return &PendingPermission{
		id:        id,
		sessionID: request.SessionID,
		options:   request.Options,
		toolCall:  request.ToolCall,
		decided:   make(chan struct{}),
	}
}

// ID 本地序号。
func (p *PendingPermission) ID() pendingID { return p.id }

// SessionID 归属会话。
func (p *PendingPermission) SessionID() acpgo.SessionID { return p.sessionID }

// Options 请求自带的选项（消费方展示 + 应答取值域）。
func (p *PendingPermission) Options() []schema.PermissionOption { return p.options }

// ToolCall 触发审批的工具调用详情（UI 展示用）。
func (p *PendingPermission) ToolCall() schema.ToolCallUpdate { return p.toolCall }

// Respond 以 optionId 应答（CAS once）。optionId 不在 Options 返回 ErrUnknownOption；
// 已应答/已结算返回 ErrAlreadyResolved。
func (p *PendingPermission) Respond(optionID string) error {
	for _, option := range p.options {
		if string(option.OptionID) == optionID {
			return p.settle(schema.RequestPermissionOutcome{
				Selected: &schema.SelectedPermissionOutcome{OptionID: option.OptionID},
			})
		}
	}
	return fmt.Errorf("%w: %q", ErrUnknownOption, optionID)
}

// cancel 以 cancelled 结算（无 selected）。已 resolved 返回 ErrAlreadyResolved。
func (p *PendingPermission) cancel() error {
	return p.settle(cancelledPermissionOutcome())
}

// Cancel 导出的取消途径：消费方（真握手测试的防御收敛、T1.7 桌面审批弹窗的关闭动作）
// 放弃决策时以 cancelled 结算。已 resolved 返回 ErrAlreadyResolved。
func (p *PendingPermission) Cancel() error { return p.cancel() }

func (p *PendingPermission) settle(outcome schema.RequestPermissionOutcome) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.resolved {
		return ErrAlreadyResolved
	}
	p.resolved = true
	p.outcome = outcome
	close(p.decided)
	return nil
}

// wait 阻塞取最终 outcome。ctx 取消不产生错误：以 cancelled outcome 收敛（若用户
// 决策先到 CAS 则用户决策保留——取消赢在「未决」上，不覆写已落袋的用户应答）。
// 返回 nil error 是刻意的：handler 返回 error 会被库转成 -32800 拆连接。
func (p *PendingPermission) wait(ctx context.Context) schema.RequestPermissionOutcome {
	select {
	case <-p.decided:
		return p.outcome
	default:
	}
	select {
	case <-p.decided:
	case <-ctx.Done():
	}
	_ = p.cancel()
	return p.outcome
}

// PendingElicitation 一次 elicitation/create 的挂起态。与权限同构，应答为三态
// （accept 带内容 / decline / cancel），由库构造器生成（acpgo.AcceptElicitation /
// DeclineElicitation / CancelElicitation）。
type PendingElicitation struct {
	id        pendingID
	sessionID acpgo.SessionID
	request   schema.CreateElicitationRequest
	wire      *ElicitationWire // 全保真 wire 投影（nil = 拦截未命中，视图侧降级构造）

	mu       sync.Mutex
	resolved bool
	decided  chan struct{}
	response schema.CreateElicitationResponse
}

func newPendingElicitation(id pendingID, request schema.CreateElicitationRequest, wire *ElicitationWire) *PendingElicitation {
	sessionID, _ := elicitationSessionID(request)
	if sessionID == "" && wire != nil {
		sessionID = schema.SessionId(wire.SessionID)
	}
	return &PendingElicitation{
		id:        id,
		sessionID: acpgo.SessionID(sessionID),
		request:   request,
		wire:      wire,
		decided:   make(chan struct{}),
	}
}

// ID 本地序号。
func (p *PendingElicitation) ID() pendingID { return p.id }

// SessionID 归属会话。
func (p *PendingElicitation) SessionID() acpgo.SessionID { return p.sessionID }

// Request 原始 typed 请求（应答通道不经过它——Respond 直达；UI 投影用 Wire()，
// typed 模型已被上游丢弃 requestedSchema/url）。
func (p *PendingElicitation) Request() schema.CreateElicitationRequest { return p.request }

// Wire 全保真 wire 投影（表单 schema / URL / 消息文案，nil 时视图侧以 WireFromRequest 降级）。
func (p *PendingElicitation) Wire() *ElicitationWire { return p.wire }

// Respond 以完整应答结算（CAS once；构造见 acpgo.AcceptElicitation 等）。
func (p *PendingElicitation) Respond(response schema.CreateElicitationResponse) error {
	return p.settle(response)
}

// cancel 结算未决 elicitation。自动收敛（回合取消/连接关闭）走 DECLINE 而非 cancel：
// agent 侧把 decline 当作用户拒绝继续回合，语义上等价于审批未给出（Gold-Band
// 超时/取消一律 decline 的同款选择）。
func (p *PendingElicitation) cancel() error {
	return p.settle(acpgo.DeclineElicitation())
}

func (p *PendingElicitation) settle(response schema.CreateElicitationResponse) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.resolved {
		return ErrAlreadyResolved
	}
	p.resolved = true
	p.response = response
	close(p.decided)
	return nil
}

// wait 阻塞取最终应答（语义同 PendingPermission.wait）。
func (p *PendingElicitation) wait(ctx context.Context) schema.CreateElicitationResponse {
	select {
	case <-p.decided:
		return p.response
	default:
	}
	select {
	case <-p.decided:
	case <-ctx.Done():
	}
	_ = p.cancel()
	return p.response
}
