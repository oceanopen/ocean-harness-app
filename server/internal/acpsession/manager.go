// Package acpsession ACP 会话域（T1.5）：sidecar 亲自持有 issue 级 ACP 会话生命周期——
// launch_settings 解析消费、绑定锚点落库（D7 受控反转：当初随 chat 视图删除
// claude_session_ref，本域以 t_issue_acp_sessions 重新落绑定——sidecar 亲自持有 ACP
// 会话后绑定语义才成立，这是受控反转而非回退）、事件流积累与会话视图、SSE hub 扇出。
// 跨入口回合锁（T2.2）：桌面 prompt（即时受理）与 bot 回合（排队受理 PromptQueued）
// 共用同一视图闸门，两入口抢跑由 per-entry 等位队列串行。
// 依赖方向：本包 → acp + agentcatalog + dal；由 main 装配为 global.AcpSessions，
// service/controller 层仅做受理转发与投影，不反向被本包依赖。
package acpsession

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	acpgo "github.com/BrokkAi/acp-go"
	"github.com/BrokkAi/acp-go/schema"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/agentcatalog"
)

// authRequiredHint claude 未登录的统一提示（握手步 -32000 特判；与 acpdoctor 的同款提示
// 语义一致，文案常量两域各自维护）。
const authRequiredHint = "claude 未登录：请先在终端运行 claude 完成登录，再重试"

// Dirs vendoring 目录对（config 的 AcpResourcesDir / AcpAdaptersDir，语义同 acpdoctor.Dirs；
// 未配置在 spawn 期经 EnsureVendored 显式报错，落 last_error 而非绕过）。
type Dirs struct {
	Resources string
	Adapters  string
}

// Manager ACP 会话域门面：per-issue 会话生命周期（ensure / prompt / cancel / respond /
// discard）、事件流订阅（SSE hub）与绑定锚点落库。进程模型 = per-issue 独立 AgentClient
// 进程（会话失败互不牵连；进程退出即会话终结，无 resume）。
type Manager struct {
	db       *gorm.DB
	dirs     Dirs
	port     int // sidecar 自身 HTTP 端口（spawn env 注入 OCEAN_HARNESS_PORT，会话内插件工具链用）
	log      *zap.Logger
	bindings *BindingStore
	hub      *hub

	mu      sync.Mutex
	entries map[string]*sessionEntry
}

// NewManager 装配会话域（main 步骤 5.5）：启动清扫运行时锚（无 resume 语义，重启后旧锚
// 全部失效归零）。port 为 sidecar 自身 HTTP 端口。
func NewManager(db *gorm.DB, dirs Dirs, port int, log *zap.Logger) (*Manager, error) {
	if log == nil {
		log = zap.NewNop()
	}
	m := &Manager{
		db:       db,
		dirs:     dirs,
		port:     port,
		log:      log,
		bindings: &BindingStore{DB: db},
		hub:      newHub(),
		entries:  map[string]*sessionEntry{},
	}
	if err := m.bindings.SweepSessionIDs(context.Background()); err != nil {
		return nil, fmt.Errorf("清扫 ACP 会话锚点失败: %w", err)
	}
	return m, nil
}

// sessionEntry 单 issue 的运行时会话宿主。mu 守护 state / client / session / discarded；
// 锁序：entry.mu → view 内部锁（绝不反向，view 不感知 entry）；turnQueue 的 q.mu 独立
// （约定见 turnqueue.go）。
type sessionEntry struct {
	issueID string
	mu      sync.Mutex

	state     SessionStatus
	client    *acp.AgentClient
	session   *acp.Session
	view      *sessionView
	turnQueue *turnQueue // 跨入口回合等位队列（T2.2）：PromptQueued 排队受理的槽位等待
	discarded bool       // Discard 已受理：后台 spawn 完成后发现即自清（不落锚、不注册 pump）
}

// spawnFunc 测试替换点（对齐 acpdoctor probeFunc 惯例）：catalog 条目 → vendored 拉起链
// → spawn env 注入 → 握手 → 建会话 → 下发权限模式。
var spawnFunc = spawnAgentSession

// Ensure 幂等受理会话创建：同步解析配置（校验失败立即反馈）→ starting/ready 态 join 返回
// 现状 → 否则建 entry + 落锚行 + 广播 starting → 后台 spawn 链（vendored 拉起 + 握手，
// 分钟级上限，不占 HTTP 请求）。pickedLaunchMode 为 LaunchModePicker 的临场启动声明
// （仅本次有效不落库，非空须为 acp）。前端经 SSE sessionStatus 帧或 getInfo 轮询跟进（对齐
// issueWorkspace init 的异步受理模型与 acpdoctor 的在跑单飞语义）。
func (m *Manager) Ensure(ctx context.Context, issueID, pickedLaunchMode string) (ViewSnapshot, error) {
	cfg, err := resolveSessionConfig(ctx, m.db, issueID, pickedLaunchMode)
	if err != nil {
		return ViewSnapshot{}, err
	}
	m.mu.Lock()
	if old, ok := m.entries[issueID]; ok {
		old.mu.Lock()
		joinable := old.state == StatusStarting || old.state == StatusReady
		old.mu.Unlock()
		if joinable {
			m.mu.Unlock()
			return old.view.snapshot(), nil
		}
		// failed/terminated：重建。旧 entry 已终态且无在途写者（spawn 失败路径在置态前
		// 已完整收尾；pump 终结路径置态后仅剩一次 hub 广播），直接替换。
		// 等位者随旧 entry 终结退出（close 广播）——旧等待不跨 entry 续接新会话。
		delete(m.entries, issueID)
		old.turnQueue.close()
	}
	entry := &sessionEntry{issueID: issueID, state: StatusStarting, view: newSessionView(), turnQueue: newTurnQueue()}
	m.entries[issueID] = entry
	m.mu.Unlock()

	if _, err := m.bindings.GetOrCreate(ctx, issueID); err != nil {
		m.mu.Lock()
		delete(m.entries, issueID)
		m.mu.Unlock()
		return ViewSnapshot{}, err
	}
	m.hub.broadcast(issueID, entry.view.setStarting(cfg))
	go m.runSpawn(entry, cfg)
	return entry.view.snapshot(), nil
}

// runSpawn 后台 spawn 链（Ensure 受理的后续）：拉起就绪 → 落锚 + 注册 pump；失败 → 落态
// 落因（last_error 跨重启可见）。不捕获请求 ctx（Ensure 已返回）；发现 discarded（受理后
// issue 被删）直接自清。
func (m *Manager) runSpawn(entry *sessionEntry, cfg SessionConfig) {
	client, session, err := spawnFunc(cfg, m.dirs, m.port, m.log)
	entry.mu.Lock()
	if entry.discarded {
		entry.mu.Unlock()
		if client != nil {
			client.Close()
		}
		return
	}
	if err != nil {
		entry.state = StatusFailed
		frames := entry.view.setFailed(err.Error())
		entry.mu.Unlock()
		m.log.Warn("ACP 会话创建失败", zap.String("issueId", entry.issueID), zap.String("agentCode", cfg.AgentCode), zap.Error(err))
		_ = m.bindings.SaveError(context.Background(), entry.issueID, cfg.AgentCode, err.Error())
		m.hub.broadcast(entry.issueID, frames)
		return
	}
	entry.client = client
	entry.session = session
	entry.state = StatusReady
	frames := entry.view.setReady(string(session.ID()), cfg, session.Modes())
	entry.mu.Unlock()
	if err := m.bindings.SaveReady(context.Background(), entry.issueID, cfg.AgentCode, string(session.ID())); err != nil {
		m.log.Warn("ACP 会话锚点落库失败", zap.String("issueId", entry.issueID), zap.Error(err))
	}
	m.hub.broadcast(entry.issueID, frames)
	go m.pump(entry)
}

// spawnAgentSession 生产拉起链（与 acpdoctor.CheckEntry 逐段同源：vendored 安装 → vendored
// claude 定位 → spawn env 注入 → Launch → 握手 → 建会话 → 下发权限模式；「探测通过」与
// 「生产拉起」同一条链，差别仅在探测后清理、本链长期持有）。node 预检不复制——那是
// doctor 的 stage 级定位面职责，本链 node 缺失在 EnsureVendored/VendoredSpawnConfig 显式
// 报错。任一后置步失败回收已拉起进程。
func spawnAgentSession(cfg SessionConfig, dirs Dirs, port int, log *zap.Logger) (*acp.AgentClient, *acp.Session, error) {
	entry, ok := agentcatalog.GetAgentCatalogInfoByCode(cfg.AgentCode)
	if !ok {
		return nil, nil, fmt.Errorf("agent catalog 无 %q 条目", cfg.AgentCode)
	}
	installDir, err := agentcatalog.EnsureVendored(entry, dirs.Resources, dirs.Adapters)
	if err != nil {
		return nil, nil, err
	}
	claudeBin, err := agentcatalog.VendoredClaudeBin(installDir)
	if err != nil {
		return nil, nil, err
	}
	spawn, err := entry.VendoredSpawnConfig(installDir, cfg.Cwd)
	if err != nil {
		return nil, nil, err
	}
	// OCEAN_HARNESS_PORT 显式注入（语义对齐 bot 驱动 driver_claude 的 turnEnv）：sidecar
	// 自身 env 无此变量（Rust 只注入 GO_SERVER_*），缺失会让会话内插件 .mcp.json 的
	// ${OCEAN_HARNESS_PORT:-9100} 回落默认端口，dev（9000）等场景错配。
	if spawn.Env == nil {
		spawn.Env = map[string]string{}
	}
	spawn.Env["OCEAN_HARNESS_PORT"] = strconv.Itoa(port)
	if spawn.Env, err = acp.ClaudeEnvOverrides(spawn.Env, claudeBin); err != nil {
		return nil, nil, err
	}
	// purpose=issue-session：生产会话流量标记——acp 包下游日志（握手/stderr/进程退出）
	// 随此 logger 自动携带，与 doctor 探测流量（doctor-probe）可辨识。
	client, err := acp.Launch(context.Background(), spawn, log.With(zap.String("purpose", "issue-session")))
	if err != nil {
		return nil, nil, fmt.Errorf("拉起 agent 进程失败: %w", err)
	}
	if _, err := client.Initialize(context.Background()); err != nil {
		client.Close()
		if hint, authed := authRequired(err); authed {
			return nil, nil, errors.New(hint)
		}
		return nil, nil, fmt.Errorf("ACP 握手失败: %w", err)
	}
	session, err := client.NewSession(context.Background(), acp.NewSessionParams{Cwd: cfg.Cwd})
	if err != nil {
		client.Close()
		if hint, authed := authRequired(err); authed {
			return nil, nil, errors.New(hint)
		}
		return nil, nil, fmt.Errorf("ACP 建会话失败: %w", err)
	}
	if err := client.SetPermissionMode(context.Background(), session, cfg.PermissionMode); err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("下发权限模式失败: %w", err)
	}
	return client, session, nil
}

// authRequired 判定握手步失败是否为 -32000（claude 未登录；判定与 acpdoctor.authRequired
// 同源同语义，纯函数便于测试对齐文案）。
func authRequired(err error) (string, bool) {
	if acpgo.IsAuthRequired(err) {
		return authRequiredHint, true
	}
	return "", false
}

// pump 单 entry 唯一的事件排空者（acp/events.go 背压契约：必须持续排空，慢消费会拖住
// 整条连接的读循环）：事件 → 视图积累 → hub 扇出；Terminated 哨兵 → 终结收尾后退出。
func (m *Manager) pump(entry *sessionEntry) {
	events := entry.session.Events()
	for {
		event := <-events
		if event.Terminated {
			m.finishTerminated(entry)
			return
		}
		m.hub.broadcast(entry.issueID, entry.view.applyEvent(&event))
	}
}

// finishTerminated 会话终结收尾：置态 + 终结帧 + 清锚落因（失败清锚语义对齐 bot 域）。
// 主动收尾（Discard/StopAll 的 Close）Err 为 nil，落「会话已终结」占位；意外死亡落
// Err() 证据（含 stderr 摘要）。
func (m *Manager) finishTerminated(entry *sessionEntry) {
	entry.mu.Lock()
	if entry.state == StatusTerminated {
		entry.mu.Unlock()
		return
	}
	reason := "会话已终结"
	if entry.client != nil {
		if err := entry.client.Err(); err != nil {
			reason = err.Error()
		}
	}
	entry.state = StatusTerminated
	frames := entry.view.setTerminated(reason)
	agentCode := entry.view.snapshot().AgentCode
	entry.mu.Unlock()
	// 等位者随会话终结全部退出（release 只弹队首，不关队列会让其余等位者无限滞留——
	// 堵死编排器会话队列并占住并发槽位）。与 Discard/StopAll/重建三处 close 同语义。
	entry.turnQueue.close()
	_ = m.bindings.SaveError(context.Background(), entry.issueID, agentCode, reason)
	m.hub.broadcast(entry.issueID, frames)
}

// Get 当前会话视图快照（无运行时 entry 返回 idle 空快照——「从未创建」与「有会话」由
// status 字段区分，不额外报错）。
func (m *Manager) Get(issueID string) ViewSnapshot {
	m.mu.Lock()
	entry, ok := m.entries[issueID]
	m.mu.Unlock()
	if !ok {
		return ViewSnapshot{Status: StatusIdle, Entries: []ConversationEntry{}, Pendings: []PendingView{}}
	}
	return entry.view.snapshot()
}

// Subscribe 订阅 issue 事件流：首帧即当前快照（无会话 = idle 空快照），返回帧通道与退订
// 函数（SSE handler defer 调用）。
func (m *Manager) Subscribe(issueID string) (<-chan Frame, func()) {
	return m.hub.subscribe(issueID, func() *ViewSnapshot {
		snap := m.Get(issueID)
		return &snap
	})
}

// Prompt 即时受理一轮回合（UI 入口语义）：视图侧串行闸门（活动回合拒绝，acp.ErrTurnActive
// 同语义）→ 立即返回 → 后台阻塞至 agent 终态并合成 turnEnded（StopReason 只在 Prompt
// 返回值可见，回合两端帧由本域合成补齐 gap）。bot 入口的排队受理（跨入口回合锁，T2.2）
// 走 PromptQueued。
func (m *Manager) Prompt(issueID, text string) error {
	entry, err := m.readyEntry(issueID)
	if err != nil {
		return err
	}
	frames, err := entry.view.beginTurn(text)
	if err != nil {
		return err
	}
	m.acceptTurn(entry, frames, text)
	return nil
}

// PromptQueued 排队受理一轮回合（T2.2 per-session 跨入口回合锁）：UI 与 bot 两入口抢同一
// ACP 会话时，活动回合中的后到者 FIFO 等位，当前回合结束后（release 唤醒）自动开跑；
// 受理成功（回合开始）即返回，回合异步跑到终态（受理式语义与 Prompt 一致）。等位无预算
// ——回合时长不可预估，由回合终态与 ctx 取消驱动（对齐 acp 层 prompt 不设超时的既有拍板）。
// 固定时序：每次循环先 join 再尝试闸门——「已在队列」与 turnActive 检查的竞态由先入队后
// 重试消解（回合结束的 release 不会漏掉已入队者；未等位即获槽位则 depart 出队）。公平性：
// UI 即时受理不经队列，回合空隙上桌面先到先得，等位者被插队后循环重试重新排队。
func (m *Manager) PromptQueued(ctx context.Context, issueID, text string) error {
	entry, err := m.readyEntry(issueID)
	if err != nil {
		return err
	}
	q := entry.turnQueue
	for {
		// 等位唤醒后对持有的 entry 复验就绪态：等位期间终结/重建的旧 entry 不得续接受理
		//（重建后的新 entry 自带新队列，旧等待不跨 entry 续接新会话）。
		if err := entry.readyError(); err != nil {
			return err
		}
		w := q.join()
		frames, err := entry.view.beginTurn(text)
		if err == nil {
			q.depart(w) // 未等位即获槽位（或被 release 弹出后闸门成功）：出队
			m.acceptTurn(entry, frames, text)
			return nil
		}
		select {
		case <-w: // 回合空隙唤醒（可能被 UI 即时受理插队）：重新排队重试
		case <-q.done():
			return errors.New("ACP 会话已终结，排队回合未受理")
		case <-ctx.Done():
			q.depart(w)
			return fmt.Errorf("排队等待回合结束被取消: %w", ctx.Err())
		}
	}
}

// acceptTurn 受理收口（Prompt 即时受理与 PromptQueued 排队受理共用）：广播受理帧 →
// 后台阻塞至 agent 终态 → 合成 turnEnded 广播 → 释放回合槽位（唤醒等位队首）。回合
// 终态先落视图再 release——被唤醒者重试闸门时槽位必然已释放。
func (m *Manager) acceptTurn(entry *sessionEntry, frames []Frame, text string) {
	m.hub.broadcast(entry.issueID, frames)
	entry.mu.Lock()
	client, session := entry.client, entry.session
	entry.mu.Unlock()
	go func() {
		stop, promptErr := client.PromptText(context.Background(), session, text)
		m.hub.broadcast(entry.issueID, entry.view.endTurn(string(stop), errorText(promptErr)))
		entry.turnQueue.release()
	}()
}

// Cancel 软取消当前回合（无活动回合幂等 no-op，语义见 acp.CancelTurn）。
func (m *Manager) Cancel(issueID string) error {
	entry, err := m.readyEntry(issueID)
	if err != nil {
		return err
	}
	entry.mu.Lock()
	client, session := entry.client, entry.session
	entry.mu.Unlock()
	client.CancelTurn(session)
	return nil
}

// RespondPermission 以 optionId 应答挂起权限审批（CAS once 语义透传）。source 标记
// 应答入口（PendingSourcePanel/Bot，T2.4 双入口收敛的 closed-history 登记维度）。
func (m *Manager) RespondPermission(issueID string, pendingID uint64, optionID, source string) error {
	entry, err := m.readyEntry(issueID)
	if err != nil {
		return err
	}
	frames, err := entry.view.respondPermission(pendingID, optionID, source)
	if err != nil {
		return err
	}
	m.hub.broadcast(issueID, frames)
	return nil
}

// RespondElicitation 应答挂起 elicitation（三态：accept 带 content / decline / cancel；
// content 键值对透传 ACP wire——schema.ElicitationContentValue 为 any 别名）。source
// 标记应答入口（同 RespondPermission）。
func (m *Manager) RespondElicitation(issueID string, pendingID uint64, action string, content map[string]any, source string) error {
	entry, err := m.readyEntry(issueID)
	if err != nil {
		return err
	}
	var response schema.CreateElicitationResponse
	switch action {
	case "accept":
		values := make(map[string]schema.ElicitationContentValue, len(content))
		for k, v := range content {
			values[k] = v
		}
		response = acpgo.AcceptElicitation(values)
	case "decline":
		response = acpgo.DeclineElicitation()
	case "cancel":
		response = acpgo.CancelElicitation()
	default:
		return fmt.Errorf("elicitation 应答 action %q 非法（可选 accept / decline / cancel）", action)
	}
	frames, err := entry.view.respondElicitation(pendingID, action, response, source)
	if err != nil {
		return err
	}
	m.hub.broadcast(issueID, frames)
	return nil
}

// Discard 会话收尾 + 绑定行删除（issue 删除级联入口，幂等）。不等待进程收敛（Close 有界
// 但不等它——issue 删除不被拖住）：ready 态终结帧由 pump 的哨兵路径广播；其余状态无
// pump，终结帧在此补发（订阅端收尾）。
func (m *Manager) Discard(issueID string) {
	m.mu.Lock()
	entry, ok := m.entries[issueID]
	delete(m.entries, issueID)
	m.mu.Unlock()
	_ = m.bindings.Delete(context.Background(), issueID)
	if !ok {
		return
	}
	entry.mu.Lock()
	entry.discarded = true
	state, client := entry.state, entry.client
	entry.mu.Unlock()
	entry.turnQueue.close() // 排队回合随会话删除退出（受理失败语义，回合未发生，不涉锚点清理）
	if client != nil && state == StatusReady {
		client.Close()
		return
	}
	if state != StatusTerminated {
		m.hub.broadcast(issueID, entry.view.setTerminated("会话已删除"))
	}
}

// StopAll 收敛全部会话（SIGTERM 步骤：先于 HTTP shutdown——先停事件源并关停订阅，
// SSE handler 随通道关闭返回，不拖 Shutdown）。
func (m *Manager) StopAll() {
	m.mu.Lock()
	entries := make([]*sessionEntry, 0, len(m.entries))
	for _, entry := range m.entries {
		entries = append(entries, entry)
	}
	m.mu.Unlock()
	for _, entry := range entries {
		entry.mu.Lock()
		client := entry.client
		entry.mu.Unlock()
		entry.turnQueue.close() // 排队回合随关停退出
		if client != nil {
			client.Close() // 有界（结算 + 连接关闭 + 进程组回收）
		}
	}
	m.hub.closeAll()
}

// readyEntry 取就绪态 entry（无 entry / 非就绪统一业务错误，错误文案带当前状态）。
func (m *Manager) readyEntry(issueID string) (*sessionEntry, error) {
	m.mu.Lock()
	entry, ok := m.entries[issueID]
	m.mu.Unlock()
	if !ok {
		return nil, errors.New("ACP 会话不存在，请先创建会话")
	}
	if err := entry.readyError(); err != nil {
		return nil, err
	}
	return entry, nil
}

// readyError 就绪态校验。readyEntry 之外，PromptQueued 在每次等位唤醒后复验持有的
// entry：等位期间终结/重建后，旧 entry 不得续接受理。
func (e *sessionEntry) readyError() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StatusReady {
		return fmt.Errorf("ACP 会话未就绪（当前 %s）", e.state)
	}
	return nil
}

// errorText 回合错误转文案（nil → 空串）。
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
