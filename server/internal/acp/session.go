package acp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	acpgo "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
)

const (
	/*
	 * turnCancelBudget 软取消收敛预算：session/cancel 发出后等 agent 回 prompt
	 * 终态的时限，超时放弃等待（以错误返回）。取消总时限同款 10s。
	 */
	turnCancelBudget = 10 * time.Second

	/*
	 * terminateGrace 终结哨兵投递上限：消费方持续不排空时放弃哨兵（消费方以
	 * AgentClient.Done()/Err() 为兜底终结信号），保证 Close 有界。
	 */
	terminateGrace = 5 * time.Second
)

// ErrTurnActive 单会话回合互斥：上一回合未收敛时再发 Prompt。
var ErrTurnActive = errors.New("ACP 会话已有进行中的回合")

// Session 一个 ACP 会话的客户端视图：事件流消费 + 模式状态读取。
type Session struct {
	runtime *sessionRuntime
}

// ID 会话 ID（agent 分配）。
func (s *Session) ID() acp1.SessionID { return s.runtime.id }

// Events 事件流（有界，背压契约见 events.go；Terminated 哨兵 = 停止消费）。
func (s *Session) Events() <-chan SessionEvent { return s.runtime.events }

/*
 * Modes 模式目录合并视图（创建响应 + current_mode_update 回写 + SetPermissionMode 确认的并集）。
 * 返回快照私有副本：CurrentModeID 由通知回写持锁就地改，消费方读副本无共享可变态。
 */
func (s *Session) Modes() *acp1.SessionModeState {
	return s.runtime.modesSnapshot()
}

/*
 * sessionRuntime 一个 ACP session 的路由与回合状态（client 内部）。handle 由库派生
 * （Prompt/Cancel 入口）；modes 为本地维护的模式状态（库不回写，current_mode_update
 * 通知与 SetPermissionMode 确认为唯一回写通道）。
 */
type sessionRuntime struct {
	id     acp1.SessionID
	client *AgentClient
	handle *acp1.ClientSession

	events chan SessionEvent

	// dispatchMu 串行化入队与终结标记（终结后一切事件丢弃）。
	dispatchMu sync.Mutex
	terminated bool

	// turnMu 串行化回合状态与未决交互清单（Prompt/CancelTurn/注册/结算入口）。
	turnGen      uint64             // 回合代际：看门狗与收尾据此不误伤后续回合
	turnCancel   context.CancelFunc // 活动回合的回合 ctx 取消（nil = 无活动回合；取消即触发库发 session/cancel）
	turnGiveup   chan struct{}      // 软取消超预算的放弃信号（关闭 = prompt 放弃等待，以错误返回）
	permPendings []*PendingPermission
	elicPendings []*PendingElicitation
	turnMu       sync.Mutex

	// stateMu 守护 modes 的就地维护（通知回写 + SetPermissionMode 确认合并）。
	stateMu sync.Mutex
	modes   *acp1.SessionModeState
}

func newSessionRuntime(id acp1.SessionID, client *AgentClient, modes *acp1.SessionModeState) *sessionRuntime {
	return &sessionRuntime{
		id:     id,
		client: client,
		handle: client.agent.Session(id),
		events: make(chan SessionEvent, sessionEventQueueSize),
		modes:  modes,
	}
}

/*
 * dispatchUpdate 通知回调路径：回写本地模式状态后入队。运行在连接层通知分发
 * goroutine——串行有序，回调内禁止再等下一个通知。
 */
func (r *sessionRuntime) dispatchUpdate(n acp1.SessionNotification) {
	r.applyStateUpdate(n.Update)
	r.enqueue(SessionEvent{Update: &n})
}

/*
 * applyStateUpdate 本地模式状态回写（SetPermissionMode 成功合并之外唯一回写通道），
 * 多入口（T1.5/T2 bot）读同一份视图。
 */
func (r *sessionRuntime) applyStateUpdate(update acp1.SessionUpdate) {
	mode, ok := update.Variant().(acp1.SessionUpdateCurrentModeUpdate)
	if !ok {
		return
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.modes != nil {
		r.modes.CurrentModeID = mode.CurrentModeID
	}
}

/*
 * enqueue 事件入队（终结后丢弃）。通道满则阻塞直至消费或进程/连接收敛——背压契约见
 * events.go；收敛期弃投递防拖死连接层通知分发。
 */
func (r *sessionRuntime) enqueue(event SessionEvent) {
	r.dispatchMu.Lock()
	defer r.dispatchMu.Unlock()
	if r.terminated {
		return
	}
	select {
	case r.events <- event:
	case <-r.client.closing:
		// 进程/连接收敛中：事件失去消费意义，丢弃防死锁
	}
}

/*
 * terminate 追加终结哨兵（幂等）。只在连接关闭且全部入站 handler join 之后调用；
 * 消费方持续不排空时超时放弃（terminateGrace）。
 */
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

/*
 * modesSnapshot 模式状态快照：值拷贝出私有副本（CurrentModeID 是唯一会被就地回写的
 * 字段），隔离后回写不再触及共享态；AvailableModes 目录创建后只读，共享安全。
 */
func (r *sessionRuntime) modesSnapshot() *acp1.SessionModeState {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.modes == nil {
		return nil
	}
	modes := *r.modes
	return &modes
}

// setModeID 确认后合并回写（SetPermissionMode 成功路径；通知回写的先行收敛）。
func (r *sessionRuntime) setModeID(modeID acp1.SessionModeID) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.modes != nil {
		r.modes.CurrentModeID = modeID
	}
}

/*
 * prompt 发起一轮回合：单会话互斥（ErrTurnActive）；阻塞至 agent 终态。软取消映射到
 * 库内建链：回合 ctx 取消 → 库发 session/cancel 且请求不灭，回合以代理取消应答收敛；
 * 调用方 ctx 取消走桥接，超预算放弃等待（以错误返回）。
 */
func (r *sessionRuntime) prompt(ctx context.Context, content []acp1.ContentBlock) (acp1.StopReason, error) {
	r.turnMu.Lock()
	if r.turnCancel != nil {
		r.turnMu.Unlock()
		return "", ErrTurnActive
	}
	/*
	 * WithoutCancel：软取消期间调用方 ctx 已死，但 prompt 调用要活着等 agent 终态；
	 * 放弃等待是唯一终止手段。
	 */
	turnCtx, hardCancel := context.WithCancel(context.WithoutCancel(ctx))
	r.turnGen++
	gen := r.turnGen
	r.turnCancel = hardCancel
	giveup := make(chan struct{})
	r.turnGiveup = giveup
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
			r.turnGiveup = nil
		}
		r.turnMu.Unlock()
		r.settlePendings()
		close(bridgeDone)
	}()

	turn, err := r.handle.Prompt(turnCtx, content...)
	if err != nil {
		if errors.Is(err, acpgo.ErrTurnInProgress) {
			// 库侧互斥：上一回合（含放弃等待的残局）尚未被代理应答收敛
			return "", ErrTurnActive
		}
		return "", fmt.Errorf("ACP 回合受理失败: %w", err)
	}
	results := make(chan promptWaitResult, 1)
	go func() {
		resp, err := turn.Wait()
		results <- promptWaitResult{resp: resp, err: err}
	}()
	select {
	case result := <-results:
		if result.err != nil {
			if acpgo.IsCode(result.err, acpgo.ErrorCodeRequestCancelled) {
				/*
				 * session/cancel 后请求被以 -32800 终止（JSON-RPC 取消语义）：语义即回合取消，
				 * 归一为干净的 cancelled 终态而非错误。
				 */
				return acp1.StopReasonCancelled, nil
			}
			if turnCtx.Err() != nil {
				// 软取消已发出后的错误：归一为可识别的回合取消错误
				return "", fmt.Errorf("ACP 回合已取消（软取消超预算 %s 后硬中断）: %w", turnCancelBudget, turnCtx.Err())
			}
			return "", fmt.Errorf("ACP 回合失败: %w", result.err)
		}
		return result.resp.StopReason, nil
	case <-giveup:
		return "", fmt.Errorf("ACP 回合已取消（软取消超预算 %s 后硬中断）", turnCancelBudget)
	}
}

// promptWaitResult 回合等待结果（prompt 内部投递）。
type promptWaitResult struct {
	resp *acp1.PromptResponse
	err  error
}

// CancelTurn 软取消当前回合（无活动回合为幂等 no-op）。
func (r *sessionRuntime) cancelCurrentTurn() {
	r.turnMu.Lock()
	gen := r.turnGen
	r.turnMu.Unlock()
	r.cancelTurn(gen)
}

/*
 * cancelTurn 软取消：结算未决交互（取消赢过用户迟到的应答）+ 取消回合 ctx（库据此发
 * session/cancel）；预算内等 agent 终态，超时关闭放弃信号（代际校验防误伤后续回合）。
 */
func (r *sessionRuntime) cancelTurn(gen uint64) {
	r.turnMu.Lock()
	hardCancel := r.turnCancel
	giveup := r.turnGiveup
	current := r.turnGen
	r.turnMu.Unlock()
	if hardCancel == nil || gen != current {
		return
	}
	/*
	 * 先同步直发 cancel（保 wire 序：cancel 通知先于取消应答落管道——agent 侧先观测
	 * 取消再收应答，收敛语义不依赖两者竞速），再结算未决交互（取消赢过用户迟到应答）；
	 * 回合 ctx 收口由库 AfterFunc 兜底再发（幂等）。
	 */
	_ = r.handle.Cancel(context.Background())
	r.settlePendings()
	hardCancel()
	time.AfterFunc(turnCancelBudget, func() {
		/*
		 * CAS-once 武装放弃信号：同 gen 二次 cancelTurn 会排布第二个定时器，状态翻转
		 * 滞后于 close 本身，仅校验 stillActive 会对已关闭通道二次 close（panic）。
		 */
		r.turnMu.Lock()
		armed := r.turnCancel != nil && r.turnGen == gen && r.turnGiveup == giveup
		if armed {
			r.turnGiveup = nil
		}
		r.turnMu.Unlock()
		if armed {
			close(giveup)
		}
	})
}

/*
 * settlePendings 结算全部未决交互（权限→cancelled、elicitation→decline）。
 * 幂等；在回合收尾、取消、会话关闭、连接收敛各路径都会调用。
 */
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

/*
 * handlePermission 连接层请求分支的权限路由。无活动回合直接 cancelled（agent 状态
 * 错乱或迟到请求不挂死）；有则注册挂起 + 事件入队 + 等待决策。运行在连接层每请求
 * 独立 goroutine——允许阻塞等 UI 决策。
 */
func (r *sessionRuntime) handlePermission(ctx context.Context, request *acp1.RequestPermissionRequest) (*acp1.RequestPermissionResponse, error) {
	pending := r.registerPermission(request)
	if pending == nil {
		return acp1.PermissionCancelled(), nil
	}
	r.enqueue(SessionEvent{Permission: pending})
	return &acp1.RequestPermissionResponse{Outcome: pending.wait(ctx)}, nil
}

func (r *sessionRuntime) registerPermission(request *acp1.RequestPermissionRequest) *PendingPermission {
	r.turnMu.Lock()
	defer r.turnMu.Unlock()
	if r.turnCancel == nil {
		return nil
	}
	p := newPendingPermission(r.client.nextPendingID(), request)
	r.permPendings = append(r.permPendings, p)
	return p
}

/*
 * handleElicitation 连接层请求分支的 elicitation 路由（与权限同构；wire 为原始参数
 * 全保真解析产物，投影侧唯一事实源）。
 */
func (r *sessionRuntime) handleElicitation(ctx context.Context, params *acp1.CreateElicitationRequest) (*acp1.CreateElicitationResponse, error) {
	pending := r.registerElicitation(params)
	if pending == nil {
		resp := acp1.NewCreateElicitationResponse(acp1.CreateElicitationResponseDecline{})
		return &resp, nil
	}
	r.enqueue(SessionEvent{Elicitation: pending})
	response := pending.wait(ctx)
	return &response, nil
}

func (r *sessionRuntime) registerElicitation(params *acp1.CreateElicitationRequest) *PendingElicitation {
	r.turnMu.Lock()
	defer r.turnMu.Unlock()
	if r.turnCancel == nil {
		return nil
	}
	p := newPendingElicitation(r.client.nextPendingID(), params)
	r.elicPendings = append(r.elicPendings, p)
	return p
}
