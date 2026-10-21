/*
 * Package acpsession ACP 会话域（T1.5）：sidecar 亲自持有 issue 级 ACP 会话生命周期——launch_settings 解析消费、绑定锚点落库（D7 受控反转：随 chat 视图删除 claude_session_ref 后，本域以 t_issue_acp_sessions 重新落绑定，sidecar 亲自持有 ACP 会话后绑定语义才成立）、事件流积累与会话视图、SSE hub 扇出；跨入口回合锁（T2.2）：桌面 prompt（即时受理）与 bot 回合（排队受理 PromptQueued）共用同一视图闸门，抢跑由 per-entry 等位队列串行；依赖方向：本包 → acp + agentcatalog + dal，由 main 装配为 global.AcpSessions，service/controller 层仅做受理转发与投影，不反向被本包依赖。
 */
package acpsession

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	acpgo "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/agentcatalog"
	"ocean-harness/server/internal/appdb"
)

/*
 * authRequiredHint claude 未登录的统一提示（握手步 -32000 特判；与 acpdoctor 的同款提示
 * 语义一致，文案常量两域各自维护）。
 */
const authRequiredHint = "claude 未登录：请先在终端运行 claude 完成登录，再重试"

/*
 * Dirs vendoring 目录对（config 的 AcpResourcesDir / AcpAdaptersDir，语义同 acpdoctor.Dirs；
 * 未配置在 spawn 期经 EnsureVendored 显式报错，落 last_error 而非绕过）。
 */
type Dirs struct {
	Resources string
	Adapters  string
}

/*
 * Manager ACP 会话域门面：per-issue 会话生命周期（ensure / prompt / cancel / respond /
 * discard）、事件流订阅（SSE hub）与绑定锚点落库。进程模型 = per-issue 独立 AgentClient
 * 进程（会话失败互不牵连；idle 回收后重开经锚点 resume 恢复，见 Ensure）。
 */
type Manager struct {
	db        *gorm.DB
	dirs      Dirs
	port      int    // sidecar 自身 HTTP 端口（spawn env 注入 OCEAN_HARNESS_PORT，会话内插件工具链用）
	appDbPath string // Rust 共享 app.db 路径（闲置回收阈值读取；空 = 未注入，配置读失败回落默认）
	log       *zap.Logger
	bindings  *BindingStore
	hub       *hub

	mu      sync.Mutex
	entries map[string]*sessionEntry
}

/*
 * NewManager 装配会话域（main 步骤 5.5）：启动清扫运行时锚——重启后运行时进程已不存在，锚归零即重开走全新会话（会话文件在 claude 侧仍可 resume，重启恢复留待后续评审，本域现阶段只在进程内 idle 回收场景 resume）；port 为 sidecar 自身 HTTP 端口，appDbPath 为 Rust 共享 app.db 路径（可空：闲置回收阈值读失败回落默认）。
 */
func NewManager(db *gorm.DB, dirs Dirs, port int, appDbPath string, log *zap.Logger) (*Manager, error) {
	if log == nil {
		log = zap.NewNop()
	}
	m := &Manager{
		db:        db,
		dirs:      dirs,
		port:      port,
		appDbPath: appDbPath,
		log:       log,
		bindings:  &BindingStore{DB: db},
		hub:       newHub(),
		entries:   map[string]*sessionEntry{},
	}
	if err := m.bindings.SweepSessionIDs(context.Background()); err != nil {
		return nil, fmt.Errorf("清扫 ACP 会话锚点失败: %w", err)
	}
	return m, nil
}

/*
 * sessionEntry 单 issue 的运行时会话宿主；mu 守护 state / client / session / discarded / idle 计时字段。锁序：entry.mu → view 内部锁（绝不反向，view 不感知 entry）；turnQueue 的 q.mu 独立（约定见 turnqueue.go）；entry.mu → hub.mu / q.mu 均单向（hub、队列绝不反拿 entry.mu）。
 */
type sessionEntry struct {
	issueID string
	mu      sync.Mutex

	state     SessionStatus
	client    *acp.AgentClient
	session   *acp.Session
	view      *sessionView
	turnQueue *turnQueue // 跨入口回合等位队列（T2.2）：PromptQueued 排队受理的槽位等待
	discarded bool       // Discard 已受理：后台 spawn 完成后发现即自清（不落锚、不注册 pump）

	idleTimer *time.Timer // 闲置回收倒计时（完全空闲判据通过时起表；离开空闲停表）
	idleSince time.Time   // 本次空闲起点（到期回调按当前配置重算剩余时长，改阈值即时生效）
	released  bool        // 闲置回收已受理：pump 终结哨兵见此早退（不落态、不清锚、不广播）

	// terminateReason Terminate 预设的终结原因（优先于 client.Err() 证据落因）。
	terminateReason string
}

/*
 * spawnFunc 测试替换点（对齐 acpdoctor probeFunc 惯例）：catalog 条目 → vendored 拉起链
 * → spawn env 注入 → 握手 → 建/恢复会话（resumeSessionID 非空走 LoadSession）→ 下发权限模式。
 */
var spawnFunc = spawnAgentSession

/*
 * Ensure 幂等受理会话创建：同步解析配置（校验失败立即反馈）→ starting/ready 态 join 返回现状 → 否则建 entry + 落锚行 + 广播 starting → 后台 spawn 链（vendored 拉起 + 握手，分钟级上限，不占 HTTP 请求）；pickedLaunchMode 为 LaunchModePicker 的临场启动声明（仅本次有效不落库，非空须为 acp）；前端经 SSE sessionStatus 帧或 getInfo 轮询跟进（对齐 issueWorkspace init 的异步受理模型与 acpdoctor 的在跑单飞语义）。
 */
func (m *Manager) Ensure(ctx context.Context, issueID, pickedLaunchMode string) (ViewSnapshot, error) {
	cfg, err := resolveSessionConfig(ctx, m.db, issueID, pickedLaunchMode)
	if err != nil {
		return ViewSnapshot{}, err
	}
	m.mu.Lock()
	if old, ok := m.entries[issueID]; ok {
		old.mu.Lock()
		// released（闲置回收已受理）不可 join——join 返回的 ready 快照会随即进程被回收。
		joinable := !old.released && (old.state == StatusStarting || old.state == StatusReady)
		old.mu.Unlock()
		if joinable {
			m.mu.Unlock()
			return old.view.snapshot(), nil
		}
		/*
		 * failed/terminated：重建。旧 entry 已终态且无在途写者（spawn 失败路径在置态前已完整收尾；pump 终结路径置态后仅剩一次 hub 广播），直接替换；released（回收在途）同走此路：reclaim 的 map 身份检查防误摘新 entry，旧 client 由其 Close；等位者随旧 entry 终结退出（close 广播）——旧等待不跨 entry 续接新会话。
		 */
		delete(m.entries, issueID)
		old.turnQueue.close()
	}
	entry := &sessionEntry{issueID: issueID, state: StatusStarting, view: newSessionView(), turnQueue: newTurnQueue()}
	m.entries[issueID] = entry
	m.mu.Unlock()

	if err := m.bindings.GetOrCreate(ctx, issueID); err != nil {
		m.mu.Lock()
		delete(m.entries, issueID)
		m.mu.Unlock()
		return ViewSnapshot{}, err
	}
	/*
	 * 锚点只读预取：非空即可恢复（idle 回收后的重建路径）；读取异常按全新会话受理（恢复
	 * 尽力而为，不因锚点读失败阻塞受理）。
	 */
	resumeID, err := m.bindings.Peek(ctx, issueID)
	if err != nil {
		m.log.Warn("读取会话锚点失败，按全新会话受理", zap.String("issueId", issueID), zap.Error(err))
		resumeID = ""
	}
	m.hub.broadcast(issueID, entry.view.setStarting(cfg))
	go m.runSpawn(entry, cfg, resumeID)
	return entry.view.snapshot(), nil
}

/*
 * runSpawn 后台 spawn 链（Ensure 受理的后续）：拉起就绪 → 落锚 + 注册 pump；失败 → 落态落因（last_error 跨重启可见）；不捕获请求 ctx（Ensure 已返回），发现 discarded（受理后 issue 被删）直接自清；resumeID 非空时拉起链以 LoadSession 恢复既有会话（失败内部降级全新会话，不阻塞受理）。
 */
func (m *Manager) runSpawn(entry *sessionEntry, cfg SessionConfig, resumeID string) {
	client, session, err := spawnFunc(cfg, m.dirs, m.port, m.log, resumeID)
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
	m.resetIdleCheck(entry) // 就绪即重估：无人订阅且无回合则开始闲置倒计时
	go m.pump(entry)
}

/*
 * spawnAgentSession 生产拉起链（与 acpdoctor.CheckEntry 逐段同源：vendored 安装 → vendored claude 定位 → spawn env 注入 → Launch → 握手 → 建/恢复会话 → 下发权限模式；「探测通过」与「生产拉起」同一条链，差别仅在探测后清理、本链长期持有）；node 预检不复制——那是 doctor 的 stage 级定位面职责，本链 node 缺失在 EnsureVendored/VendoredSpawnConfig 显式报错；任一后置步失败回收已拉起进程。
 */
func spawnAgentSession(cfg SessionConfig, dirs Dirs, port int, log *zap.Logger, resumeSessionID string) (*acp.AgentClient, *acp.Session, error) {
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
	env, err := acp.ClaudeEnvOverrides(entry.Env, claudeBin)
	if err != nil {
		return nil, nil, err
	}
	spawn, err := entry.VendoredSpawnConfig(dirs.Resources, installDir, cfg.Cwd, env)
	if err != nil {
		return nil, nil, err
	}
	/*
	 * OCEAN_HARNESS_PORT 显式注入（语义对齐 bot 驱动 driver_claude 的 turnEnv）：sidecar
	 * 自身 env 无此变量（Rust 只注入 GO_SERVER_*），缺失会让会话内插件 .mcp.json 的
	 * ${OCEAN_HARNESS_PORT:-9100} 回落默认端口，dev（9000）等场景错配。
	 */
	spawn.Env["OCEAN_HARNESS_PORT"] = strconv.Itoa(port)
	/*
	 * purpose=issue-session：生产会话流量标记——acp 包下游日志（握手/stderr/进程退出）
	 * 随此 logger 自动携带，与 doctor 探测流量（doctor-probe）可辨识。
	 */
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
	session, err := newOrResumedSession(context.Background(), client, cfg, resumeSessionID, log)
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

/*
 * newOrResumedSession 建/恢复会话收口：锚点非空走 LoadSession（适配器读回 claude 会话
 * 文件并重放历史）；恢复失败（能力未声明/会话文件丢失等）降级全新会话——恢复尽力而为，
 * 不因锚点失效阻塞受理，降级后 SaveReady 以新 id 覆盖锚点。
 */
func newOrResumedSession(ctx context.Context, client *acp.AgentClient, cfg SessionConfig, resumeSessionID string, log *zap.Logger) (*acp.Session, error) {
	if resumeSessionID != "" {
		session, err := client.LoadSession(ctx, acp.LoadSessionParams{Cwd: cfg.Cwd, SessionID: resumeSessionID})
		if err == nil {
			return session, nil
		}
		log.Warn("ACP 会话恢复失败，降级全新会话", zap.String("resumeSessionId", resumeSessionID), zap.Error(err))
	}
	return client.NewSession(ctx, acp.NewSessionParams{Cwd: cfg.Cwd})
}

/*
 * authRequired 判定握手步失败是否为 -32000（claude 未登录；判定与 acpdoctor.authRequired
 * 同源同语义，纯函数便于测试对齐文案）。
 */
func authRequired(err error) (string, bool) {
	if acpgo.IsCode(err, acpgo.ErrorCodeAuthRequired) {
		return authRequiredHint, true
	}
	return "", false
}

/*
 * pump 单 entry 唯一的事件排空者（acp/events.go 背压契约：必须持续排空，慢消费会拖住
 * 整条连接的读循环）：事件 → 视图积累 → hub 扇出；Terminated 哨兵 → 终结收尾后退出。
 */
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

/*
 * finishTerminated 会话终结收尾：置态 + 终结帧 + 清锚落因（失败清锚语义对齐 bot 域）。
 * 主动收尾（Discard/StopAll 的 Close）Err 为 nil，落「会话已终结」占位；意外死亡落
 * Err() 证据（含 stderr 摘要）。
 */
func (m *Manager) finishTerminated(entry *sessionEntry) {
	entry.mu.Lock()
	if entry.state == StatusTerminated || entry.released {
		/*
		 * released（闲置回收已摘 entry）：主动收尾不是会话终结——不落态、不清锚（锚点
		 * 供重开 resume）、不广播（无订户）。
		 */
		entry.mu.Unlock()
		return
	}
	reason := entry.terminateReason
	if reason == "" {
		reason = "会话已终结"
		if entry.client != nil {
			if err := entry.client.Err(); err != nil {
				reason = err.Error()
			}
		}
	}
	entry.state = StatusTerminated
	frames := entry.view.setTerminated(reason)
	agentCode := entry.view.snapshot().AgentCode
	entry.mu.Unlock()
	/*
	 * 等位者随会话终结全部退出（release 只弹队首，不关队列会让其余等位者无限滞留——
	 * 堵死编排器会话队列并占住并发槽位）。与 Discard/StopAll/重建三处 close 同语义。
	 */
	entry.turnQueue.close()
	_ = m.bindings.SaveError(context.Background(), entry.issueID, agentCode, reason)
	m.hub.broadcast(entry.issueID, frames)
}

/*
 * Terminate 主动终结会话（回合失败收尾/活性判死的入口，幂等）：Close 进程组后收敛全部
 * 复用既有链——连接停止使在途 prompt 失败（endTurn(error) 释放回合槽），pump 哨兵走
 * finishTerminated（置态、清锚落因、广播终结帧）。与 Discard 的差异：保留绑定行（下条
 * 消息 Ensure 全新会话重建，绑定关系不受影响）。
 */
func (m *Manager) Terminate(issueID, reason string) {
	m.mu.Lock()
	entry, ok := m.entries[issueID]
	m.mu.Unlock()
	if !ok {
		return
	}
	entry.mu.Lock()
	if entry.state != StatusReady || entry.released || entry.client == nil {
		entry.mu.Unlock()
		return
	}
	entry.terminateReason = reason
	client := entry.client
	entry.mu.Unlock()
	client.Close()
}

/*
 * Get 当前会话视图快照（无运行时 entry 返回 idle 空快照——「从未创建」与「有会话」由
 * status 字段区分，不额外报错）。
 */
func (m *Manager) Get(issueID string) ViewSnapshot {
	m.mu.Lock()
	entry, ok := m.entries[issueID]
	m.mu.Unlock()
	if !ok {
		return ViewSnapshot{Status: StatusIdle, Entries: []ConversationEntry{}, Pendings: []PendingView{}}
	}
	return entry.view.snapshot()
}

/*
 * Subscribe 订阅 issue 事件流：首帧即当前快照（无会话 = idle 空快照），返回帧通道与退订
 * 函数（SSE handler defer 调用）。订户在场即离开空闲（停表）；退订后重估闲置倒计时。
 */
func (m *Manager) Subscribe(issueID string) (<-chan Frame, func()) {
	m.mu.Lock()
	entry, ok := m.entries[issueID]
	m.mu.Unlock()
	frames, cancel := m.hub.subscribe(issueID, func() *ViewSnapshot {
		snap := m.Get(issueID)
		return &snap
	})
	if ok {
		m.stopIdleTimer(entry)
	}
	return frames, func() {
		cancel()
		if ok {
			m.resetIdleCheck(entry)
		}
	}
}

/*
 * Prompt 即时受理一轮回合（UI 入口语义）：视图侧串行闸门（活动回合拒绝，acp.ErrTurnActive 同语义）→ 立即返回 → 后台阻塞至 agent 终态并合成 turnEnded（StopReason 只在 Prompt 返回值可见，回合两端帧由本域合成补齐 gap）；来源恒 panel（HTTP 面零改动，T1.1 的 Source 装配收口在此），bot 入口的排队受理（跨入口回合锁，T2.2）走 PromptQueued。
 */
func (m *Manager) Prompt(issueID, text string) error {
	entry, err := m.readyEntry(issueID)
	if err != nil {
		return err
	}
	frames, err := entry.view.beginTurn(text, PromptMeta{Source: EntrySourcePanel})
	if err != nil {
		return err
	}
	m.acceptTurn(entry, frames, text)
	return nil
}

/*
 * PromptQueued 排队受理一轮回合（T2.2 per-session 跨入口回合锁）：UI 与 bot 两入口抢同一 ACP 会话时，活动回合中的后到者 FIFO 等位、当前回合结束后（release 唤醒）自动开跑，受理成功（回合开始）即返回、回合异步跑到终态（受理式语义与 Prompt 一致）；meta 为 bot 侧来源与展示元数据（T1.1），随受理落 user 条目。等位无预算——回合时长不可预估，由回合终态与 ctx 取消驱动（对齐 acp 层 prompt 不设超时的既有拍板）。固定时序：每次循环先 join 再尝试闸门——「已在队列」与 turnActive 检查的竞态由先入队后重试消解（回合结束的 release 不会漏掉已入队者；未等位即获槽位则 depart 出队）；公平性：UI 即时受理不经队列，回合空隙上桌面先到先得，等位者被插队后循环重试重新排队。
 */
func (m *Manager) PromptQueued(ctx context.Context, issueID, text string, meta PromptMeta) error {
	entry, err := m.readyEntry(issueID)
	if err != nil {
		return err
	}
	q := entry.turnQueue
	for {
		/*
		 * 等位唤醒后对持有的 entry 复验就绪态：等位期间终结/重建的旧 entry 不得续接受理
		 * （重建后的新 entry 自带新队列，旧等待不跨 entry 续接新会话）。
		 */
		if err := entry.readyError(); err != nil {
			return err
		}
		w := q.join()
		frames, err := entry.view.beginTurn(text, meta)
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

/*
 * acceptTurn 受理收口（Prompt 即时受理与 PromptQueued 排队受理共用）：广播受理帧 →
 * 后台阻塞至 agent 终态 → 合成 turnEnded 广播 → 释放回合槽位（唤醒等位队首）。回合
 * 终态先落视图再 release——被唤醒者重试闸门时槽位必然已释放。
 */
func (m *Manager) acceptTurn(entry *sessionEntry, frames []Frame, text string) {
	m.stopIdleTimer(entry) // 回合受理即离开空闲（判据复查兜底停表前的窗口）
	m.hub.broadcast(entry.issueID, frames)
	entry.mu.Lock()
	client, session := entry.client, entry.session
	entry.mu.Unlock()
	go func() {
		stop, promptErr := client.PromptText(context.Background(), session, text)
		m.hub.broadcast(entry.issueID, entry.view.endTurn(string(stop), errorText(promptErr)))
		entry.turnQueue.release()
		m.resetIdleCheck(entry) // 回合终态：重新评估闲置倒计时
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

/*
 * RespondPermission 以 optionId 应答挂起权限审批（CAS once 语义透传）。source 标记
 * 应答入口（PendingSourcePanel/Bot，T2.4 双入口收敛的 closed-history 登记维度）。
 */
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

/*
 * RespondElicitation 应答挂起 elicitation（三态：accept 带 content / decline / cancel；
 * content 键值对经 elicitationContentValue 收敛透传 ACP wire）。source 标记应答入口
 * （同 RespondPermission）。
 */
func (m *Manager) RespondElicitation(issueID string, pendingID uint64, action string, content map[string]any, source string) error {
	entry, err := m.readyEntry(issueID)
	if err != nil {
		return err
	}
	var response acp1.CreateElicitationResponse
	switch action {
	case "accept":
		values := make(map[string]acp1.ElicitationContentValue, len(content))
		for k, v := range content {
			value, err := elicitationContentValue(v)
			if err != nil {
				return err
			}
			values[k] = value
		}
		response = acp1.NewCreateElicitationResponse(acp1.CreateElicitationResponseAccept{Content: values})
	case "decline":
		response = acp1.NewCreateElicitationResponse(acp1.CreateElicitationResponseDecline{})
	case "cancel":
		response = acp1.NewCreateElicitationResponse(acp1.CreateElicitationResponseCancel{})
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

/*
 * elicitationContentValue HTTP 入参 any → ElicitationContentValue 收敛（wire 值域
 * string/float64/bool/[]string 四型）；其余类型显式拒绝，不静默丢内容。
 */
func elicitationContentValue(v any) (acp1.ElicitationContentValue, error) {
	switch value := v.(type) {
	case string:
		return acp1.NewElicitationContentValue(value)
	case float64:
		return acp1.NewElicitationContentValue(value)
	case bool:
		return acp1.NewElicitationContentValue(value)
	case []string:
		return acp1.NewElicitationContentValue(value)
	}
	return acp1.ElicitationContentValue{}, fmt.Errorf("elicitation 应答值类型不支持: %T", v)
}

/*
 * Discard 会话收尾 + 绑定行删除（issue 删除级联入口，幂等）。不等待进程收敛（Close 有界
 * 但不等它——issue 删除不被拖住）：ready 态终结帧由 pump 的哨兵路径广播；其余状态无
 * pump，终结帧在此补发（订阅端收尾）。
 */
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

/*
 * StopAll 收敛全部会话（SIGTERM 步骤：先于 HTTP shutdown——先停事件源并关停订阅，
 * SSE handler 随通道关闭返回，不拖 Shutdown）。
 */
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
		m.stopIdleTimerLocked(entry) // 关停不再回收计时
		entry.mu.Unlock()
		entry.turnQueue.close() // 排队回合随关停退出
		if client != nil {
			client.Close() // 有界（结算 + 连接关闭 + 进程组回收）
		}
	}
	m.hub.closeAll()
}

/*
 * —— 闲置回收（T4）——
 * 完全空闲判据：就绪态 + 无活动回合 + 无等位回合 + 无订户（等位者不持订户，漏判会杀掉排队回合）；判据通过起表倒计时、离开空闲停表，到期回调以空闲起点 + 当前配置重算剩余时长（改阈值即时生效）后复查判据，仍满足才回收：置 released + 摘 entry + Close（进程退出，node 内存释放）。回收路径与意外死亡分流——pump 终结哨兵见 released 直接退出，不落态、不清锚（acpSessionId 保留供重开 resume）、不广播（无订户无人感知）。
 */

/*
 * acpSessionIdleTimeoutOptions 与前端 ACP_SESSION_IDLE_TIMEOUT_OPTIONS（appConfig.ts）
 * 对齐的值域；脏值回落默认（Rust DEFAULT_ACP_SESSION_IDLE_TIMEOUT_MINUTES 单源）。
 */
var acpSessionIdleTimeoutOptions = map[int]bool{5: true, 15: true, 30: true, 60: true}

const defaultAcpSessionIdleTimeoutMinutes = 30

/*
 * parseIdleTimeoutMinutes 配置值解析（与前端 parseAcpSessionIdleTimeoutMinutes 同口径：
 * 非法或不在值域回落默认 30）。
 */
func parseIdleTimeoutMinutes(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || !acpSessionIdleTimeoutOptions[parsed] {
		return defaultAcpSessionIdleTimeoutMinutes
	}
	return parsed
}

/*
 * idleTimeoutMinutes 现读配置（无缓存：改设置下一次起表/到期即生效；读失败回落默认——
 * air 自测等无 app.db 场景不受阻）。
 */
func (m *Manager) idleTimeoutMinutes() int {
	value, err := appdb.ReadAppConfigValue(m.appDbPath, appdb.KeyAcpSessionIdleTimeoutMinutes)
	if err != nil {
		return defaultAcpSessionIdleTimeoutMinutes
	}
	return parseIdleTimeoutMinutes(value)
}

// idleTimeout 阈值时长（测试替换点：短阈值驱动回收用例；生产 = 配置分钟转时长）。
var idleTimeout = (*Manager).idleTimeoutDuration

// idleTimeoutDuration 配置分钟 → 时长。
func (m *Manager) idleTimeoutDuration() time.Duration {
	return time.Duration(m.idleTimeoutMinutes()) * time.Minute
}

/*
 * fullyIdleLocked 完全空闲判据（须持 entry.mu）：未回收未删除 + 就绪态 + 无活动回合 +
 * 无等位回合 + 无订户（等位者不持订户，漏判会杀掉排队回合）。idleSubs 为锁外预读的订户数。
 */
func (m *Manager) fullyIdleLocked(entry *sessionEntry, idleSubs int) bool {
	return !entry.released && !entry.discarded && entry.state == StatusReady &&
		!entry.view.snapshot().TurnActive && entry.turnQueue.pending() == 0 && idleSubs == 0
}

/*
 * resetIdleTimerLocked 重估空闲计时（须持 entry.mu）：先停表；判据通过则以当前配置起表
 * 并记空闲起点，离开空闲清起点（起点只归属当前空闲段，是到期重算的依据）。判据复查与
 * 起表同锁完成，到期回调的判据复查兜底两者之间的竞态。
 */
func (m *Manager) resetIdleTimerLocked(entry *sessionEntry, idleSubs int) {
	m.stopIdleTimerLocked(entry)
	if !m.fullyIdleLocked(entry, idleSubs) {
		entry.idleSince = time.Time{}
		return
	}
	entry.idleSince = time.Now()
	entry.idleTimer = time.AfterFunc(idleTimeout(m), func() {
		m.reclaimIdle(entry)
	})
}

/*
 * stopIdleTimerLocked 停表（须持 entry.mu；nil 幂等）。不清空闲起点——到期回调以其重算
 * 剩余时长；清零由 resetIdleTimerLocked 的离开空闲分支负责。
 */
func (m *Manager) stopIdleTimerLocked(entry *sessionEntry) {
	if entry.idleTimer != nil {
		entry.idleTimer.Stop()
		entry.idleTimer = nil
	}
}

/*
 * resetIdleCheck 空闲状态迁移点的重估入口（回合终态/退订/就绪落态后调用）。订户数须在
 * entry.mu 外预读——锁内嵌套 hub.mu 会经快照闭包（hub.mu → m.mu）与 Ensure 的
 * m.mu → entry.mu 构成环；预读到起表之间的新订户由 Subscribe 的停表兜底。
 */
func (m *Manager) resetIdleCheck(entry *sessionEntry) {
	idleSubs := m.hub.subscriberCount(entry.issueID)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	m.resetIdleTimerLocked(entry, idleSubs)
}

// stopIdleTimer 离开空闲（回合受理/新订户）的停表入口；released/已终结 entry 幂等。
func (m *Manager) stopIdleTimer(entry *sessionEntry) {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	m.stopIdleTimerLocked(entry)
}

/*
 * reclaimIdle 倒计时到期：先按当前配置重算剩余时长（改阈值即时生效——阈值改长则按空闲
 * 起点顺延），判据仍满足才回收。订户数锁外预读（锁序约定，同 resetIdleCheck）。摘 entry
 * 先于 Close——pump 的终结哨兵经 released 早退，不会再走 finishTerminated。
 */
func (m *Manager) reclaimIdle(entry *sessionEntry) {
	idleSubs := m.hub.subscriberCount(entry.issueID)
	entry.mu.Lock()
	m.stopIdleTimerLocked(entry)
	if !m.fullyIdleLocked(entry, idleSubs) {
		entry.mu.Unlock()
		return
	}
	remaining := idleTimeout(m)
	if idle := time.Since(entry.idleSince); idle < remaining {
		// 阈值改长：按空闲起点顺延剩余时长继续等（起点不变，下个到期继续重算）。
		entry.idleTimer = time.AfterFunc(remaining-idle, func() { m.reclaimIdle(entry) })
		entry.mu.Unlock()
		return
	}
	entry.released = true
	client := entry.client
	entry.mu.Unlock()
	m.mu.Lock()
	if m.entries[entry.issueID] == entry {
		delete(m.entries, entry.issueID)
	}
	m.mu.Unlock()
	m.log.Info("ACP 会话闲置回收", zap.String("issueId", entry.issueID), zap.String("acpSessionId", entry.view.snapshot().AcpSessionID))
	if client != nil {
		client.Close() // 有界（结算 + 连接关闭 + 进程组回收）；pump 哨兵经 released 早退
	}
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

/*
 * readyError 就绪态校验。readyEntry 之外，PromptQueued 在每次等位唤醒后复验持有的
 * entry：等位期间终结/重建/回收后，旧 entry 不得续接受理。
 */
func (e *sessionEntry) readyError() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.released {
		return errors.New("ACP 会话已闲置回收，重新打开后自动恢复")
	}
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
