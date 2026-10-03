package acp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	acpgo "github.com/BrokkAi/acp-go"
	"github.com/BrokkAi/acp-go/schema"
)

const (
	// turnCancelBudget 软取消收敛预算：session/cancel 通知发出后等 agent 回 prompt
	// 终态的时限，超时转硬取消（拆回合 ctx）。Gold-Band 取消总时限同款 10s。
	turnCancelBudget = 10 * time.Second

	// terminateGrace 终结哨兵投递上限：消费方持续不排空时放弃哨兵（消费方以
	// AgentClient.Done()/Err() 为兜底终结信号），保证 Close 有界。
	terminateGrace = 5 * time.Second
)

// ErrTurnActive 单会话回合互斥：上一回合未收敛时再发 Prompt。
var ErrTurnActive = errors.New("ACP 会话已有进行中的回合")

// Session 一个 ACP 会话的客户端视图：事件流消费 + 模式状态读取。
type Session struct {
	runtime *sessionRuntime
}

// ID 会话 ID（agent 分配）。
func (s *Session) ID() acpgo.SessionID { return s.runtime.id }

// Events 事件流（有界，背压契约见 events.go；Terminated 哨兵 = 停止消费）。
func (s *Session) Events() <-chan SessionEvent { return s.runtime.events }

// Modes 模式目录合并视图（创建响应 + current_mode_update 回写 + SetMode 确认的并集）。
// 返回快照私有副本：CurrentModeID 由通知回写持锁就地改，消费方读副本无共享可变态。
func (s *Session) Modes() *schema.SessionModeState {
	return s.runtime.sessionValue().Modes
}

// sessionRuntime 一个 ACP session 的路由与回合状态（client 内部）。
type sessionRuntime struct {
	id     acpgo.SessionID
	client *AgentClient

	events chan SessionEvent

	// dispatchMu 串行化入队与终结标记（终结后一切事件丢弃）。
	dispatchMu sync.Mutex
	terminated bool

	// turnMu 串行化回合状态与未决交互清单（Prompt/CancelTurn/注册/结算入口）。
	turnGen      uint64             // 回合代际：看门狗与收尾据此不误伤后续回合
	turnCancel   context.CancelFunc // 活动回合的硬取消（nil = 无活动回合）
	permPendings []*PendingPermission
	elicPendings []*PendingElicitation
	turnMu       sync.Mutex

	// stateMu 守护 session 值的就地维护（通知回写 + SetMode 由库就地改）。
	stateMu sync.Mutex
	session acpgo.Session
}

func newSessionRuntime(id acpgo.SessionID, client *AgentClient, session acpgo.Session) *sessionRuntime {
	return &sessionRuntime{
		id:      id,
		client:  client,
		events:  make(chan SessionEvent, sessionEventQueueSize),
		session: session,
	}
}

// dispatchUpdate 通知回调路径：回写本地模式状态后入队。运行在连接层读循环
// goroutine——库契约要求快速返回，禁止回调内再调 Connection 方法。
func (r *sessionRuntime) dispatchUpdate(update acpgo.Update) {
	r.applyStateUpdate(update)
	r.enqueue(SessionEvent{Update: &update})
}

// applyStateUpdate 库不回写本地 Session.Modes（SetMode 成功才就地改）；模式状态由
// 本层从 current_mode_update 通知维护，多入口（T1.5/T2 bot）读同一份视图。
func (r *sessionRuntime) applyStateUpdate(update acpgo.Update) {
	if update.Update.CurrentModeUpdate == nil {
		return
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.session.Modes != nil {
		r.session.Modes.CurrentModeID = update.Update.CurrentModeUpdate.CurrentModeID
	}
	// TODO(T1.5): config_option_update / session_info 等变体按消费需要补充本地回写。
}

// enqueue 事件入队（终结后丢弃）。通道满则阻塞直至消费或进程/连接收敛——背压契约见
// events.go；收敛期弃投递防拖死连接层读循环的 join。
func (r *sessionRuntime) enqueue(event SessionEvent) {
	r.dispatchMu.Lock()
	defer r.dispatchMu.Unlock()
	if r.terminated {
		return
	}
	select {
	case r.events <- event:
	case <-r.client.closing:
		// 进程/连接收敛中：事件失去消费意义，丢弃防 join 死锁
	}
}

// terminate 追加终结哨兵（幂等）。只在连接关闭且全部入站 handler join 之后调用；
// 消费方持续不排空时超时放弃（terminateGrace）。
func (r *sessionRuntime) terminate() {
	r.dispatchMu.Lock()
	defer r.dispatchMu.Unlock()
	if r.terminated {
		return
	}
	r.terminated = true
	select {
	case r.events <- SessionEvent{Terminated: true}:
	case <-time.After(terminateGrace):
	}
}

// sessionValue 会话状态快照（PromptContent 按值消费 + SetMode 快照送验的输入）。
// Modes 浅拷贝出私有值副本：CurrentModeID 是唯一会被回写的字段（通知回写 + SetMode
// 确认合并），隔离后库经快照指针的就地写不再触及共享态；AvailableModes 目录创建后
// 只读，共享安全。
func (r *sessionRuntime) sessionValue() acpgo.Session {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	snapshot := r.session
	if snapshot.Modes != nil {
		modes := *snapshot.Modes
		snapshot.Modes = &modes
	}
	return snapshot
}

// prompt 发起一轮回合：单会话互斥（ErrTurnActive）；阻塞至 agent 终态。回合 ctx 不随
// 调用方 ctx 立即消亡——调用方 ctx 取消走软取消（session/cancel + 结算未决交互），
// 等 agent 回终态（stopReason=cancelled）后干净返回；超预算才硬取消（以错误返回）。
func (r *sessionRuntime) prompt(ctx context.Context, content []acpgo.Content) (schema.StopReason, error) {
	r.turnMu.Lock()
	if r.turnCancel != nil {
		r.turnMu.Unlock()
		return "", ErrTurnActive
	}
	// WithoutCancel：软取消期间调用方 ctx 已死，但 prompt 调用要活着等 agent 终态；
	// 硬取消是唯一终止手段。
	turnCtx, hardCancel := context.WithCancel(context.WithoutCancel(ctx))
	r.turnGen++
	gen := r.turnGen
	r.turnCancel = hardCancel
	r.turnMu.Unlock()

	bridgeDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			r.cancelTurn(gen)
		case <-bridgeDone:
		}
	}()
	defer func() {
		r.turnMu.Lock()
		if r.turnGen == gen {
			r.turnCancel = nil
		}
		r.turnMu.Unlock()
		r.settlePendings()
		close(bridgeDone)
	}()

	stop, err := r.client.conn.PromptContent(turnCtx, r.client.initialization(), r.sessionValue(), content)
	if err != nil {
		var rpcErr *acpgo.RPCError
		if errors.As(err, &rpcErr) && rpcErr.Code == -32800 {
			// session/cancel 后请求被以 -32800 终止（JSON-RPC 取消语义）：语义即回合取消，
			// 归一为干净的 cancelled 终态而非错误。
			return schema.StopReasonCancelled, nil
		}
		if turnCtx.Err() != nil {
			// 硬取消：transport 以 ctx.Err 终止调用，归一为可识别的回合取消错误
			return "", fmt.Errorf("ACP 回合已取消（软取消超预算 %s 后硬中断）: %w", turnCancelBudget, turnCtx.Err())
		}
		return stop, fmt.Errorf("ACP 回合失败: %w", err)
	}
	return stop, nil
}

// CancelTurn 软取消当前回合（无活动回合为幂等 no-op）。
func (r *sessionRuntime) cancelCurrentTurn() {
	r.turnMu.Lock()
	gen := r.turnGen
	r.turnMu.Unlock()
	r.cancelTurn(gen)
}

// cancelTurn 软取消：session/cancel 通知 + 立即结算未决交互（取消赢过用户迟到的
// 应答）；预算内等 agent 终态，超时硬取消（代际校验防误伤后续回合）。
func (r *sessionRuntime) cancelTurn(gen uint64) {
	r.turnMu.Lock()
	hardCancel := r.turnCancel
	current := r.turnGen
	r.turnMu.Unlock()
	if hardCancel == nil || gen != current {
		return
	}
	_ = r.client.conn.CancelSession(context.Background(), r.id)
	r.settlePendings()
	time.AfterFunc(turnCancelBudget, func() {
		r.turnMu.Lock()
		stillActive := r.turnCancel != nil && r.turnGen == gen
		r.turnMu.Unlock()
		if stillActive {
			hardCancel()
		}
	})
}

// settlePendings 结算全部未决交互（权限→cancelled、elicitation→decline）。
// 幂等；在回合收尾、取消、会话关闭、连接收敛各路径都会调用。
func (r *sessionRuntime) settlePendings() {
	r.turnMu.Lock()
	perms := r.permPendings
	elics := r.elicPendings
	r.permPendings = nil
	r.elicPendings = nil
	r.turnMu.Unlock()
	for _, p := range perms {
		_ = p.cancel()
	}
	for _, e := range elics {
		_ = e.cancel()
	}
}

// handlePermission 连接层 onRequest 的权限分支。无活动回合直接 cancelled（Gold-Band
// 语义：agent 状态错乱或迟到请求不挂死）；有则注册挂起 + 事件入队 + 等待决策。
// 运行在连接层每请求独立 goroutine——允许阻塞等 UI 决策。
func (r *sessionRuntime) handlePermission(ctx context.Context, request schema.RequestPermissionRequest) (schema.RequestPermissionResponse, error) {
	pending := r.registerPermission(request)
	if pending == nil {
		return schema.RequestPermissionResponse{Outcome: cancelledPermissionOutcome()}, nil
	}
	r.enqueue(SessionEvent{Permission: pending})
	outcome := pending.wait(ctx)
	return schema.RequestPermissionResponse{Outcome: outcome}, nil
}

func (r *sessionRuntime) registerPermission(request schema.RequestPermissionRequest) *PendingPermission {
	r.turnMu.Lock()
	defer r.turnMu.Unlock()
	if r.turnCancel == nil {
		return nil
	}
	p := newPendingPermission(r.client.nextPendingID(), request)
	r.permPendings = append(r.permPendings, p)
	return p
}

// handleElicitation 连接层 onRequest 的 elicitation 分支（与权限同构；wire 为拦截层
// 全保真解码产物，可为 nil——投影侧降级）。
func (r *sessionRuntime) handleElicitation(ctx context.Context, request schema.CreateElicitationRequest, wire *ElicitationWire) (schema.CreateElicitationResponse, error) {
	pending := r.registerElicitation(request, wire)
	if pending == nil {
		return acpgo.DeclineElicitation(), nil
	}
	r.enqueue(SessionEvent{Elicitation: pending})
	response := pending.wait(ctx)
	return response, nil
}

func (r *sessionRuntime) registerElicitation(request schema.CreateElicitationRequest, wire *ElicitationWire) *PendingElicitation {
	r.turnMu.Lock()
	defer r.turnMu.Unlock()
	if r.turnCancel == nil {
		return nil
	}
	p := newPendingElicitation(r.client.nextPendingID(), request, wire)
	r.elicPendings = append(r.elicPendings, p)
	return p
}

// cancelledPermissionOutcome 无活动回合/自动收敛路径的 cancelled 应答。
func cancelledPermissionOutcome() schema.RequestPermissionOutcome {
	return schema.RequestPermissionOutcome{Cancelled: &schema.RequestPermissionOutcomeCancelled{}}
}
