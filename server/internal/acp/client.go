// Package acp 是 ACP（Agent Client Protocol）客户端域：拉起 agent 子进程（进程组 +
// stderr 分类）→ acp-go 连接装配 → per-session 回合/交互运行时，供会话域与 bot 域消费。
package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	acpgo "github.com/BrokkAi/acp-go"
	"github.com/BrokkAi/acp-go/agent"
	"github.com/BrokkAi/acp-go/schema"
	"go.uber.org/zap"

	"ocean-harness/server/internal/buildinfo"
)

// 握手与会话方法限时（Gold-Band 同款）：初始化/建会话 60s、关会话 10s。prompt 与
// 交互应答不设超时——回合时长与用户决策时长不可预估，由 ctx 与取消语义驱动。
const (
	initializeTimeout   = 60 * time.Second
	newSessionTimeout   = 60 * time.Second
	closeSessionTimeout = 10 * time.Second

	// 早到帧缓冲：session/new 响应前 agent 抢跑推送的 update 按 sessionId 暂存，
	// NewSession 注册会话后按序回放（available_commands 等早期推送不再被丢弃）。
	// TTL 内未注册即整体过期；单会话容量上限丢最旧（正常 agent 早期推送量级极小，
	// 上限仅防异常 agent 撑内存）。
	earlyFrameTTL = 5 * time.Second
	earlyFrameCap = 64
)

// AgentClient 一个 ACP agent 子进程 + 其协议连接的宿主句柄。一个进程可承载多个
// ACP session（per issue 一个，T1.5 会话域管理分配）。并发安全。
type AgentClient struct {
	proc *AgentProcess
	conn *acpgo.Connection
	log  *zap.Logger

	closing   chan struct{} // 收敛信号：入队逃生口（主动 Close 与意外死亡路径都关闭）
	closeOnce sync.Once
	// closedByCaller 仅 Close() 置位：closing 在意外死亡路径也必须关闭（enqueue
	// 逃生口不能卡 join），主动收尾判别单独走此标记，Err() 据此区分两种收敛。
	closedByCaller atomic.Bool

	initMu sync.Mutex
	init   acpgo.Initialization

	sessMu   sync.Mutex
	sessions map[acpgo.SessionID]*sessionRuntime
	early    map[acpgo.SessionID][]earlyFrame // 早到帧缓冲（sessMu 内读写；注册时摘除回放）

	pendingSeq atomic.Uint64
}

// earlyFrame 早于会话注册到达的 update（带到达时刻供 TTL 过期判定）。
type earlyFrame struct {
	update acpgo.Update
	at     time.Time
}

// Launch 拉起 agent 子进程并装配 ACP 连接（进程组 + stderr 分类 + 入站路由）。
// TODO(T1.4): doctor 握手探测复用本入口前段（Launch → Initialize → 清理）。
func Launch(ctx context.Context, cfg SpawnConfig, log *zap.Logger) (*AgentClient, error) {
	if log == nil {
		log = zap.NewNop()
	}
	proc, err := spawnAgent(cfg, log)
	if err != nil {
		return nil, err
	}
	c := &AgentClient{
		proc:     proc,
		log:      log,
		closing:  make(chan struct{}),
		sessions: make(map[acpgo.SessionID]*sessionRuntime),
		early:    make(map[acpgo.SessionID][]earlyFrame),
	}

	// 入站装配（必须先于 Connect——agent 在任何流量前就要能应答）：
	// onRequest = 权限/elicitation 路由（fs/terminal 未广告不会来，其余方法库回 -32601）；
	// onNotify = 容错 session/update 分发（单帧解码失败只记日志——库契约：Notifications
	// 返回 error 会终止整条连接）。
	sessionUpdates := acpgo.SessionUpdates(c.onUpdate)
	notifications := acpgo.Notifications(func(method string, raw json.RawMessage) error {
		// TODO(T1.7): elicitation/complete（表单流式更新通知）在此补充分发。
		if err := sessionUpdates(method, raw); err != nil {
			c.log.Warn("ACP session/update 解码失败，已丢弃", zap.String("method", method), zap.Error(err))
		}
		return nil
	})
	c.conn = acpgo.Connect(proc.stdout, proc.stdin, agent.HandleClientHost(nil, nil, c, c), notifications)
	go c.awaitProcessExit()
	return c, nil
}

// Initialize ACP 握手（连接级一次，由库保证；本层缓存结果幂等）。能力面：不广告
// fs/terminal（agent 不会发起，无需兜底面）；广告 configOptions（set_mode 新轨）与
// elicitation form（T1.7 表单交互，url 暂不广告）。
func (c *AgentClient) Initialize(ctx context.Context) (acpgo.Initialization, error) {
	c.initMu.Lock()
	defer c.initMu.Unlock()
	if c.init.ProtocolVersion != 0 {
		return c.init, nil
	}
	tctx, cancel := context.WithTimeout(ctx, initializeTimeout)
	defer cancel()
	caps := acpgo.Capabilities{
		Session:     acpgo.ConfigOptionsClientCapabilities(true),
		Elicitation: acpgo.ElicitationClientCapabilities(true, false),
	}
	init, err := c.conn.InitializeWithInfo(tctx, caps, acpgo.ClientInfo{
		Name:    "ocean-harness",
		Version: buildinfo.Version,
	})
	if err != nil {
		return init, err
	}
	c.init = init
	// 协商结果日志锚点：生产面协议版本与 agent 身份的第一证据。版本判定由库承担
	//（不支持的版本握手即失败），走到这里即已锚定稳定面。
	fields := []zap.Field{zap.Uint16("protocolVersion", uint16(init.ProtocolVersion))}
	if init.AgentInfo != nil {
		fields = append(fields,
			zap.String("agent", init.AgentInfo.Name),
			zap.String("agentVersion", init.AgentInfo.Version))
	}
	c.log.Info("ACP 握手完成", fields...)
	return init, nil
}

func (c *AgentClient) initialization() acpgo.Initialization {
	c.initMu.Lock()
	defer c.initMu.Unlock()
	return c.init
}

// HasClaudeCodeExtension 探测 agent 是否上报 claudeCode _meta 扩展（initialize 响应
// 顶层 _meta，claude-code-acp 的识别标记）。
// TODO(T1.3/T1.5): claude 专属启动参数（权限旁路等）经此分支启用。
func HasClaudeCodeExtension(init acpgo.Initialization) bool {
	if init.Meta == nil {
		return false
	}
	_, ok := init.Meta["claudeCode"]
	return ok
}

// NewSessionParams 建会话参数。
type NewSessionParams struct {
	// Cwd 会话工作目录；缺省回落进程 Cwd（均须绝对路径，库校验）。
	Cwd string
	// Meta _meta 透传（扩展通道；走通用 Call 序列化）。
	// TODO(T1.3/T1.5): claudeCode options 注入策略待权限策略扩展后启用。
	Meta schema.Meta
}

// NewSession 建会话并注册事件路由（未握手则先握手，幂等）。
func (c *AgentClient) NewSession(ctx context.Context, params NewSessionParams) (*Session, error) {
	init, err := c.Initialize(ctx)
	if err != nil {
		return nil, fmt.Errorf("ACP 握手失败: %w", err)
	}
	tctx, cancel := context.WithTimeout(ctx, newSessionTimeout)
	defer cancel()

	directory := params.Cwd
	if directory == "" {
		directory = c.proc.cwd
	}
	var session acpgo.Session
	if params.Meta == nil {
		session, err = c.conn.NewSessionWithOptions(tctx, init, directory, acpgo.NewSessionOptions{
			MCPServers: []schema.McpServer{},
		})
	} else {
		// 库 NewSessionWithOptions 不透传 _meta：带 Meta 走通用 Call（库 README 明示用法）。
		err = c.conn.Call(tctx, schema.SessionNewMethodName, schema.NewSessionRequest{
			Cwd:        directory,
			MCPServers: []schema.McpServer{},
			Meta:       params.Meta,
		}, &session)
	}
	if err != nil {
		return nil, fmt.Errorf("ACP 建会话失败: %w", err)
	}
	if session.SessionID == "" {
		return nil, errors.New("ACP agent 返回空 sessionId")
	}

	runtime := newSessionRuntime(session.SessionID, c, session)
	c.sessMu.Lock()
	if _, exists := c.sessions[session.SessionID]; exists {
		c.sessMu.Unlock()
		return nil, fmt.Errorf("ACP sessionId 冲突: %s", session.SessionID)
	}
	c.sessions[session.SessionID] = runtime
	early := c.early[session.SessionID]
	delete(c.early, session.SessionID)
	c.sessMu.Unlock()
	// 回放早到帧（锁外：dispatchUpdate 入队可能因通道满阻塞，不得拖住 sessMu 的路由）。
	for _, frame := range early {
		runtime.dispatchUpdate(frame.update)
	}
	return &Session{runtime: runtime}, nil
}

// PromptText 以纯文本发起一轮回合（内容块构造的便捷封装）。
func (c *AgentClient) PromptText(ctx context.Context, s *Session, text string) (schema.StopReason, error) {
	return c.Prompt(ctx, s, []acpgo.Content{acpgo.NewTextContent(text)})
}

// Prompt 发起一轮回合：阻塞至 agent 终态。互斥见 ErrTurnActive；调用方 ctx 取消走
// 软取消（session/cancel + 结算未决交互），超预算硬取消（以错误返回）。
func (c *AgentClient) Prompt(ctx context.Context, s *Session, content []acpgo.Content) (schema.StopReason, error) {
	return s.runtime.prompt(ctx, content)
}

// CancelTurn 软取消会话当前回合（无活动回合为幂等 no-op）。
func (c *AgentClient) CancelTurn(s *Session) {
	s.runtime.cancelCurrentTurn()
}

// CloseSession 结束会话：先结算未决交互（Gold-Band 顺序：结算先于 session/close），
// 再协议关闭，最后移除注册 + 终结哨兵。
func (c *AgentClient) CloseSession(ctx context.Context, s *Session) error {
	s.runtime.settlePendings()
	tctx, cancel := context.WithTimeout(ctx, closeSessionTimeout)
	defer cancel()
	err := c.conn.CloseSession(tctx, c.initialization(), s.runtime.id)
	c.removeSession(s.runtime.id)
	return err
}

func (c *AgentClient) removeSession(id acpgo.SessionID) {
	c.sessMu.Lock()
	runtime, ok := c.sessions[id]
	delete(c.sessions, id)
	c.sessMu.Unlock()
	if ok {
		runtime.terminate()
	}
}

// Done 进程/连接终结信号。
func (c *AgentClient) Done() <-chan struct{} { return c.proc.Done() }

// Err 进程/连接收敛原因：进程存活返回 nil；主动收尾（Close）后恒为 nil（终结不是
// 异常）；意外死亡返回带 stderr 摘要的包装错误（「为什么挂」的第一证据面）。判别
// 走 closedByCaller 而非 closing——后者在意外死亡路径也关闭（入队逃生口）。
func (c *AgentClient) Err() error {
	select {
	case <-c.proc.Done():
	default:
		return nil
	}
	if c.closedByCaller.Load() {
		return nil
	}
	if waitErr := c.proc.WaitErr(); waitErr != nil && !errors.Is(waitErr, os.ErrProcessDone) {
		if detail := c.proc.exitDetail(); detail != "" {
			return fmt.Errorf("ACP agent 进程异常退出: %w\nstderr 摘要: %s", waitErr, detail)
		}
		return fmt.Errorf("ACP agent 进程异常退出: %w", waitErr)
	}
	return c.conn.Err()
}

// Close 全量收尾（幂等）：置主动收尾标记 → 关账 → 收敛序。
func (c *AgentClient) Close() {
	// 先置位再收敛：Err() 的「主动收尾」判别先于 Done() 可见，不被收敛序反超。
	c.closedByCaller.Store(true)
	c.markClosing()
	c.converge()
}

// converge 收敛序（Close 与进程死亡监听共用；并发调用各步均幂等——conn.Close 走
// sync.Once，proc.Close 的 Kill/cancel/<-done 各自幂等）：逐会话结算未决交互 →
// 连接关闭（join 全部入站 handler 与读循环）→ 进程组回收 → 逐会话终结哨兵。
func (c *AgentClient) converge() {
	for _, runtime := range c.snapshotSessions() {
		runtime.settlePendings()
	}
	_ = c.conn.Close()
	c.proc.Close()
	for _, runtime := range c.snapshotSessions() {
		runtime.terminate()
	}
}

// awaitProcessExit 进程死亡监听：EOF 后读循环自停，此处补齐与 Close 相同的收敛序
// （不含主动收尾标记——这不是调用方收的账），并落因日志。
func (c *AgentClient) awaitProcessExit() {
	<-c.proc.Done()
	c.markClosing()
	c.converge()
	fields := []zap.Field{zap.Error(c.proc.WaitErr())}
	if detail := c.proc.exitDetail(); detail != "" {
		fields = append(fields, zap.String("stderr", detail))
	}
	c.log.Warn("ACP agent 进程退出", fields...)
}

func (c *AgentClient) markClosing() {
	c.closeOnce.Do(func() { close(c.closing) })
}

func (c *AgentClient) snapshotSessions() []*sessionRuntime {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	runtimes := make([]*sessionRuntime, 0, len(c.sessions))
	for _, runtime := range c.sessions {
		runtimes = append(runtimes, runtime)
	}
	return runtimes
}

// onUpdate session/update 分发：按 update.SessionID 路由到会话事件通道（锁内完成
// 查找 + 入队，与 removeSession 的「先摘除后终结」顺序配对，杜绝已终结会话的迟到入队）；
// 未注册会话的早到帧（session/new 响应前 agent 抢跑推送）进缓冲，注册后回放。
func (c *AgentClient) onUpdate(update acpgo.Update) error {
	c.sessMu.Lock()
	runtime, ok := c.sessions[update.SessionID]
	if ok {
		runtime.dispatchUpdate(update)
	} else {
		c.bufferEarlyFrame(update)
	}
	c.sessMu.Unlock()
	if !ok {
		c.log.Debug("ACP update 早于会话注册，已入早到帧缓冲", zap.String("sessionId", string(update.SessionID)))
	}
	return nil
}

// bufferEarlyFrame 早到帧入缓冲（须持 sessMu 调用）：惰性过期清理（整会话最后帧超
// TTL 即丢弃）+ 单会话容量上限丢最旧。
func (c *AgentClient) bufferEarlyFrame(update acpgo.Update) {
	now := time.Now()
	for id, frames := range c.early {
		if now.Sub(frames[len(frames)-1].at) > earlyFrameTTL {
			delete(c.early, id)
		}
	}
	frames := append(c.early[update.SessionID], earlyFrame{update: update, at: now})
	if len(frames) > earlyFrameCap {
		frames = frames[len(frames)-earlyFrameCap:]
	}
	c.early[update.SessionID] = frames
}

// RequestPermission 实现 agent.Permissions：按 sessionId 路由到会话运行时。
func (c *AgentClient) RequestPermission(ctx context.Context, request schema.RequestPermissionRequest) (schema.RequestPermissionResponse, error) {
	runtime, ok := c.lookupSession(request.SessionID)
	if !ok {
		return schema.RequestPermissionResponse{}, &acpgo.RPCError{Code: -32602, Message: "unknown sessionId"}
	}
	return runtime.handlePermission(ctx, request)
}

// CreateElicitation 实现 agent.Elicitation：按会话路由（elicitation 顶层无 sessionId，
// 会话归属在 form 的 session scope 内，经 elicitationSessionID 解析）。
func (c *AgentClient) CreateElicitation(ctx context.Context, request schema.CreateElicitationRequest) (schema.CreateElicitationResponse, error) {
	sessionID, ok := elicitationSessionID(request)
	if !ok {
		// TODO(T1.7): url 模式 / 请求级（无会话归属）elicitation 的路由与 UI 呈现。
		return schema.CreateElicitationResponse{}, &acpgo.RPCError{Code: -32602, Message: "unknown sessionId"}
	}
	runtime, ok := c.lookupSession(sessionID)
	if !ok {
		return schema.CreateElicitationResponse{}, &acpgo.RPCError{Code: -32602, Message: "unknown sessionId"}
	}
	return runtime.handleElicitation(ctx, request)
}

func (c *AgentClient) lookupSession(id acpgo.SessionID) (*sessionRuntime, bool) {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	runtime, ok := c.sessions[id]
	return runtime, ok
}

func (c *AgentClient) nextPendingID() pendingID { return c.pendingSeq.Add(1) }
