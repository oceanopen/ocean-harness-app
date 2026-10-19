/*
 * Package acp 是 ACP（Agent Client Protocol）客户端域：拉起 agent 子进程（进程组 +
 * stderr 分类）→ ironpark/acp-go acp1 连接装配 → per-session 回合/交互运行时，
 * 供会话域与 bot 域消费。
 */
package acp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	acpgo "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"go.uber.org/zap"

	"ocean-harness/server/internal/buildinfo"
)

/*
 * 握手与会话方法限时（Gold-Band 同款）：初始化/建会话 60s、关会话 10s。prompt 与
 * 交互应答不设超时——回合时长与用户决策时长不可预估，由 ctx 与取消语义驱动。
 */
const (
	initializeTimeout   = 60 * time.Second
	newSessionTimeout   = 60 * time.Second
	closeSessionTimeout = 10 * time.Second

	/*
	 * 早到帧缓冲：session/new 响应前 agent 抢跑推送的 update 按 sessionId 暂存，NewSession 注册会话后按序回放（available_commands 等早期推送不再被丢弃）。
	 * TTL 内未注册即整体过期；单会话容量上限丢最旧（正常 agent 早期推送量级极小，
	 * 上限仅防异常 agent 撑内存）。
	 */
	earlyFrameTTL = 5 * time.Second
	earlyFrameCap = 64
)

/*
 * AgentClient 一个 ACP agent 子进程 + 其协议连接的宿主句柄。一个进程可承载多个
 * ACP session（per issue 一个，T1.5 会话域管理分配）。并发安全；实现 acp1.Client
 * （SessionUpdate 分发 + RequestPermission 路由）与 acp1.ElicitationHandler。
 */
type AgentClient struct {
	proc  *AgentProcess
	agent *acp1.RemoteAgent
	log   *zap.Logger

	closing   chan struct{} // 收敛信号：入队逃生口（主动 Close 与意外死亡路径都关闭）
	closeOnce sync.Once
	/*
	 * closedByCaller 仅 Close() 置位：closing 在意外死亡路径也必须关闭（enqueue
	 * 逃生口不能卡 join），主动收尾判别单独走此标记，Err() 据此区分两种收敛。
	 */
	closedByCaller atomic.Bool

	initMu sync.Mutex
	init   *acp1.InitializeResponse

	sessMu   sync.Mutex
	sessions map[acp1.SessionID]*sessionRuntime
	early    map[acp1.SessionID][]earlyFrame // 早到帧缓冲（sessMu 内读写；注册时摘除回放）

	pendingSeq atomic.Uint64
}

// earlyFrame 早于会话注册到达的 update（带到达时刻供 TTL 过期判定）。
type earlyFrame struct {
	update acp1.SessionNotification
	at     time.Time
}

/*
 * Launch 拉起 agent 子进程并装配 ACP 连接（进程组 + stderr 分类 + 入站路由）。
 */
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
		sessions: make(map[acp1.SessionID]*sessionRuntime),
		early:    make(map[acp1.SessionID][]earlyFrame),
	}
	/*
	 * 入站装配（Client 接口承载 update 分发与权限/elicitation 路由；通知串行有序、
	 * 请求独立 goroutine）。fs/terminal 未广告不会来，其余方法库回 method not found。
	 */
	c.agent = acp1.ConnectAgent(context.Background(), acpgo.NewStdioTransport(proc.stdout, proc.stdin),
		func(*acp1.ClientSideConnection) acp1.Client { return c })
	go c.awaitProcessExit()
	return c, nil
}

/*
 * Initialize ACP 握手（连接级一次，本层缓存结果幂等）。能力面：不广告 fs/terminal
 * （agent 不会发起，无需兜底面）；广告 configOptions（set_mode 新轨）与 elicitation
 * form（表单交互，url 暂不广告）。
 */
func (c *AgentClient) Initialize(ctx context.Context) (*acp1.InitializeResponse, error) {
	c.initMu.Lock()
	defer c.initMu.Unlock()
	if c.init != nil {
		return c.init, nil
	}
	tctx, cancel := context.WithTimeout(ctx, initializeTimeout)
	defer cancel()
	caps := acp1.ClientCapabilitiesOf(c)
	caps.Elicitation = &acp1.ElicitationCapabilities{Form: &acp1.ElicitationFormCapabilities{}}
	caps.Session = &acp1.ClientSessionCapabilities{
		ConfigOptions: &acp1.SessionConfigOptionsCapabilities{Boolean: &acp1.BooleanConfigOptionCapabilities{}},
	}
	init, err := c.agent.Initialize(tctx, &acp1.InitializeRequest{
		ClientCapabilities: caps,
		ClientInfo:         &acp1.Implementation{Name: "ocean-harness", Version: buildinfo.Version},
	})
	if err != nil {
		return nil, err
	}
	c.init = init
	/*
	 * 协商结果日志锚点：生产面协议版本与 agent 身份的第一证据。
	 */
	fields := []zap.Field{zap.Uint16("protocolVersion", uint16(init.ProtocolVersion))}
	if init.AgentInfo != nil {
		fields = append(fields,
			zap.String("agent", init.AgentInfo.Name),
			zap.String("agentVersion", init.AgentInfo.Version))
	}
	c.log.Info("ACP 握手完成", fields...)
	return init, nil
}

/*
 * HasClaudeCodeExtension 探测 agent 是否上报 claudeCode _meta 扩展（实际在
 * agentCapabilities._meta，claude-code-acp 的识别标记）。
 */
func HasClaudeCodeExtension(init *acp1.InitializeResponse) bool {
	if init == nil || init.AgentCapabilities == nil {
		return false
	}
	_, ok, err := init.AgentCapabilities.Meta.Get[map[string]any]("claudeCode")
	return err == nil && ok
}

// NewSessionParams 建会话参数。
type NewSessionParams struct {
	// Cwd 会话工作目录；缺省回落进程 Cwd（均须绝对路径，库校验）。
	Cwd string
	// Meta _meta 透传（扩展通道；claudeCode options 注入由此进入）。
	Meta acp1.Meta
}

// NewSession 建会话并注册事件路由（未握手则先握手，幂等）。
func (c *AgentClient) NewSession(ctx context.Context, params NewSessionParams) (*Session, error) {
	if _, err := c.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("ACP 握手失败: %w", err)
	}
	tctx, cancel := context.WithTimeout(ctx, newSessionTimeout)
	defer cancel()

	directory := params.Cwd
	if directory == "" {
		directory = c.proc.cwd
	}
	created, err := c.agent.NewSession(tctx, &acp1.NewSessionRequest{
		Cwd:        directory,
		MCPServers: []acp1.MCPServer{},
		Meta:       params.Meta,
	})
	if err != nil {
		return nil, fmt.Errorf("ACP 建会话失败: %w", err)
	}
	return c.registerSession(created.SessionID, created.Modes)
}

// LoadSessionParams 恢复既有会话参数（锚点 resume 的 wire 入口）。
type LoadSessionParams struct {
	// Cwd 会话工作目录；缺省回落进程 Cwd（均须绝对路径，库校验）。
	Cwd string
	/*
	 * SessionID 待恢复的 ACP 会话 id（claude 侧会话文件按此定位；load 响应不含 id，
	 * 会话以请求 id 注册）。
	 */
	SessionID string
}

/*
 * LoadSession 恢复既有会话（ACP session/load；agent 未声明 load 能力即请求报错，由
 * 调用方降级 NewSession）。agent 侧从其 SDK 会话文件读回上下文，历史以 session/update
 * 重放（响应前的重放推送经早到帧缓冲承接，注册后回放，与 session/new 抢跑同轨）。
 */
func (c *AgentClient) LoadSession(ctx context.Context, params LoadSessionParams) (*Session, error) {
	if _, err := c.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("ACP 握手失败: %w", err)
	}
	tctx, cancel := context.WithTimeout(ctx, newSessionTimeout)
	defer cancel()

	directory := params.Cwd
	if directory == "" {
		directory = c.proc.cwd
	}
	loaded, err := c.agent.LoadSession(tctx, &acp1.LoadSessionRequest{
		Cwd:        directory,
		SessionID:  acp1.SessionID(params.SessionID),
		MCPServers: []acp1.MCPServer{},
	})
	if err != nil {
		return nil, fmt.Errorf("ACP 恢复会话失败: %w", err)
	}
	return c.registerSession(acp1.SessionID(params.SessionID), loaded.Modes)
}

/*
 * registerSession 会话注册收口（NewSession/LoadSession 共用）：运行时装配 + 会话表登记
 * + 早到帧回放（锁外：dispatchUpdate 入队可能因通道满阻塞，不得拖住 sessMu 的路由）。
 */
func (c *AgentClient) registerSession(id acp1.SessionID, modes *acp1.SessionModeState) (*Session, error) {
	if id == "" {
		return nil, errors.New("ACP agent 返回空 sessionId")
	}
	runtime := newSessionRuntime(id, c, modes)
	c.sessMu.Lock()
	if _, exists := c.sessions[id]; exists {
		c.sessMu.Unlock()
		return nil, fmt.Errorf("ACP sessionId 冲突: %s", id)
	}
	c.sessions[id] = runtime
	early := c.early[id]
	delete(c.early, id)
	c.sessMu.Unlock()
	for i := range early {
		runtime.dispatchUpdate(early[i].update)
	}
	return &Session{runtime: runtime}, nil
}

// PromptText 以纯文本发起一轮回合（内容块构造的便捷封装）。
func (c *AgentClient) PromptText(ctx context.Context, s *Session, text string) (acp1.StopReason, error) {
	return c.Prompt(ctx, s, []acp1.ContentBlock{acp1.NewContentBlock(acp1.ContentBlockText{Text: text})})
}

/*
 * Prompt 发起一轮回合：阻塞至 agent 终态。互斥见 ErrTurnActive；调用方 ctx 取消走
 * 软取消（session/cancel + 结算未决交互），超预算放弃等待（以错误返回）。
 */
func (c *AgentClient) Prompt(ctx context.Context, s *Session, content []acp1.ContentBlock) (acp1.StopReason, error) {
	return s.runtime.prompt(ctx, content)
}

// CancelTurn 软取消会话当前回合（无活动回合为幂等 no-op）。
func (c *AgentClient) CancelTurn(s *Session) {
	s.runtime.cancelCurrentTurn()
}

/*
 * CloseSession 结束会话：先结算未决交互（结算先于 session/close），再协议关闭，
 * 最后移除注册 + 终结哨兵。
 */
func (c *AgentClient) CloseSession(ctx context.Context, s *Session) error {
	s.runtime.settlePendings()
	tctx, cancel := context.WithTimeout(ctx, closeSessionTimeout)
	defer cancel()
	_, err := c.agent.CloseSession(tctx, &acp1.CloseSessionRequest{SessionID: s.runtime.id})
	c.removeSession(s.runtime.id)
	return err
}

func (c *AgentClient) removeSession(id acp1.SessionID) {
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

/*
 * Err 进程/连接收敛原因：进程存活返回 nil；主动收尾后恒为 nil（终结不是异常）；
 * 意外死亡返回带 stderr 摘要的包装错误，进程死因缺失时回落连接层死因。
 */
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
	select {
	case <-c.agent.Done():
		return c.agent.Wait()
	default:
		return nil
	}
}

// Close 全量收尾（幂等）：置主动收尾标记 → 关账 → 收敛序。
func (c *AgentClient) Close() {
	// 先置位再收敛：Err() 的「主动收尾」判别先于 Done() 可见，不被收敛序反超。
	c.closedByCaller.Store(true)
	c.markClosing()
	c.converge()
}

/*
 * converge 收敛序（Close 与进程死亡监听共用；并发调用各步均幂等）：逐会话结算未决
 * 交互 → 连接关闭（transport 关闭即管道收口）→ 进程组回收 → 逐会话终结哨兵。
 */
func (c *AgentClient) converge() {
	for _, runtime := range c.snapshotSessions() {
		runtime.settlePendings()
	}
	_ = c.agent.Close()
	c.proc.Close()
	for _, runtime := range c.snapshotSessions() {
		runtime.terminate()
	}
}

/*
 * awaitProcessExit 进程死亡监听：EOF 后连接读循环自停，此处补齐与 Close 相同的收敛序
 * （不含主动收尾标记）并落因日志：主动收尾降为 DEBUG，意外死亡保持 WARN。
 */
func (c *AgentClient) awaitProcessExit() {
	<-c.proc.Done()
	c.markClosing()
	c.converge()
	fields := []zap.Field{zap.Error(c.proc.WaitErr())}
	if detail := c.proc.exitDetail(); detail != "" {
		fields = append(fields, zap.String("stderr", detail))
	}
	if c.closedByCaller.Load() {
		fields = append(fields, zap.Bool("closedByCaller", true))
		c.log.Debug("ACP agent 进程退出", fields...)
		return
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

/*
 * SessionUpdate 实现 acp1.Client：session/update 按会话路由（锁内只查表、锁外分发
 * ——入队可阻塞，不得拖住 sessMu 的跨会话路由；终结后迟到事件由 enqueue 丢弃）；
 * 未注册会话的早到帧进缓冲注册后回放；恒返 nil（通知失败不终止连接）。
 */
func (c *AgentClient) SessionUpdate(ctx context.Context, n *acp1.SessionNotification) error {
	if n == nil {
		return nil
	}
	c.sessMu.Lock()
	runtime, ok := c.sessions[n.SessionID]
	if !ok {
		c.bufferEarlyFrame(*n)
	}
	c.sessMu.Unlock()
	if ok {
		runtime.dispatchUpdate(*n)
	} else {
		c.log.Debug("ACP update 早于会话注册，已入早到帧缓冲", zap.String("sessionId", string(n.SessionID)))
	}
	return nil
}

/*
 * bufferEarlyFrame 早到帧入缓冲（须持 sessMu 调用）：惰性过期清理（整会话最后帧超
 * TTL 即丢弃）+ 单会话容量上限丢最旧。
 */
func (c *AgentClient) bufferEarlyFrame(update acp1.SessionNotification) {
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

// RequestPermission 实现 acp1.Client：按 sessionId 路由到会话运行时。
func (c *AgentClient) RequestPermission(ctx context.Context, request *acp1.RequestPermissionRequest) (*acp1.RequestPermissionResponse, error) {
	runtime, ok := c.lookupSession(request.SessionID)
	if !ok {
		return nil, acpgo.InvalidParams("unknown sessionId")
	}
	return runtime.handlePermission(ctx, request)
}

/*
 * CreateElicitation 实现 acp1.ElicitationHandler：按会话路由（typed 变体全保真，归属
 * 解析不再有降级路径）。请求级 elicitation（request scope 变体）不支持，拒绝 -32602。
 */
func (c *AgentClient) CreateElicitation(ctx context.Context, params *acp1.CreateElicitationRequest) (*acp1.CreateElicitationResponse, error) {
	sessionID, ok := elicitationSessionID(params)
	if !ok {
		return nil, acpgo.InvalidParams("unknown sessionId")
	}
	runtime, ok := c.lookupSession(sessionID)
	if !ok {
		return nil, acpgo.InvalidParams("unknown sessionId")
	}
	return runtime.handleElicitation(ctx, params)
}

// CompleteElicitation 实现 acp1.ElicitationHandler：url 模式完成通知一期不消费。
func (c *AgentClient) CompleteElicitation(ctx context.Context, params *acp1.CompleteElicitationNotification) error {
	return nil
}

func (c *AgentClient) lookupSession(id acp1.SessionID) (*sessionRuntime, bool) {
	c.sessMu.Lock()
	defer c.sessMu.Unlock()
	runtime, ok := c.sessions[id]
	return runtime, ok
}

func (c *AgentClient) nextPendingID() pendingID { return c.pendingSeq.Add(1) }
