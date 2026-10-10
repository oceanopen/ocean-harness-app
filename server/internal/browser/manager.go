package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"ocean-harness/server/internal/acp"
)

// 会话管理（T1.3，D4 用完即释放）：per-profile 单引擎进程的状态机（idle/starting/
// ready/failed）、互斥与并发上限、per-call 超时与连续超时自愈、主层 idle 计时释放、
// 孤儿回收与 SSE 投影。
//
// 锁三分纪律（对齐 acpsession 锁序）：
//   - m.mu：注册表 CRUD 与 activeCount（持锁只做 map 操作与计数，绝不调 entry 方法）
//   - entry.mu：状态守护（置态 + view 投影 + 产帧数据；帧广播恒在锁外）
//   - view 锁：投影读写（hub 建连 snapshot 只碰 view 锁——长调用期间面板 SSE 建连不卡死）
//
// 锁序：callMu → m.mu → entry.mu → view，绝不反向；hub 锁独立（broadcast 恒在其余锁外
// 调用，锁内只经 subscribe 的 snapshot 闭包碰 view 读锁）。

// 时序与容量常量（var 化供测试覆写；编码规则 1——时序缺陷不引入防抖掩盖）。
var (
	// spawnHandshakeTimeout 引擎拉起握手预算：LaunchEngine 无内建预算，挂死引擎会无限
	// 占住 PumpStderr goroutine（acpsession 分步超时同款纪律的 browser 单点总预算形态）。
	spawnHandshakeTimeout = 60 * time.Second
	// ensureWaitTimeout starting 等就绪上限。
	ensureWaitTimeout = 30 * time.Second
	// forwardCallTimeout per-call 超时上限（防 evaluate 死循环把 profile 永久挂死）。
	forwardCallTimeout = 120 * time.Second
	// pagesRefreshTimeout pages 投影刷新（tab 列表拉取）超时。
	pagesRefreshTimeout = 30 * time.Second
	// idleReleaseTimeout 主层 idle 释放计时（D4 双层的主层；每次成功调用即重置。兜底层
	// 为引擎侧 --idle-timeout 15 分钟，见 pin.go——到期只关浏览器窗口，entry 状态无需
	// 迁移。登录配方 wait_for 轮询与面板截图轮询天然保活，属预期）。
	idleReleaseTimeout = 10 * time.Minute
)

const (
	// maxConcurrentProfiles 并发 profile 上限（manager 单飞互斥之外的总闸）。
	maxConcurrentProfiles = 3
	// consecutiveTimeoutLimit 连续调用超时阈值：置 failed 自动重建。
	consecutiveTimeoutLimit = 3
)

// 显式中文错误（调用方按 errors.Is 判别）。
var (
	ErrSessionBusy    = errors.New("浏览器会话忙：已有操作正在执行，请稍后重试")
	ErrSessionClosing = errors.New("浏览器会话正在关闭，请稍后重试")
	errSessionClosed  = errors.New("浏览器会话已关闭（引擎已释放），请重试")
)

// Manager 浏览器会话域门面：per-profile 引擎生命周期（ensure/forward/close）、SSE
// 投影订阅与孤儿回收。进程模型 = per-profile 单引擎进程，用完即释放（D4）。
type Manager struct {
	dirs Dirs
	log  *zap.Logger
	hub  *hub
	view *browserView

	mu      sync.Mutex
	entries map[string]*sessionEntry
	// activeCount starting/ready 条目数（并发上限判据）。仅在 m.mu → entry.mu 双锁
	// 临界区内增减（锁序见文件头；m.mu 永不反向嵌套 entry.mu 之外的路径）。
	activeCount int
}

// NewManager 装配会话域（T2.1 由 main 懒启动调用，构造即返回）：启动即清扫残留引擎
// 孤儿（recoverOrphans——sidecar 重启后引擎/浏览器残留的 pid 锚点回收）。
func NewManager(dirs Dirs, log *zap.Logger) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
	m := &Manager{
		dirs:    dirs,
		log:     log,
		hub:     newHub(),
		view:    newBrowserView(),
		entries: map[string]*sessionEntry{},
	}
	m.recoverOrphans()
	return m
}

// sessionEntry 单 profile 运行时会话宿主。mu 守护 state/handle/closing/consecutive
// Timeouts/idleTimer/spawnDone/callCancel；callMu 为调用互斥（长 Forward 持有期间不碰
// m.mu/entry.mu 之外的慢操作）。idle = 已释放（条目驻留供投影，下次调用 ensure 重拉）。
type sessionEntry struct {
	profile string
	mu      sync.Mutex

	state    SessionStatus
	headless bool
	handle   *EngineHandle
	// closing Close 已受理——仅覆盖 starting 窗口（spawn 期间 Close 无法直接回收，
	// 由 runSpawn 终结时消费复位；生命周期 = 一次 spawn），在途调用据此归类。
	closing             bool
	consecutiveTimeouts int
	idleTimer           *time.Timer
	spawnDone           chan struct{} // 本次 spawn 终结信号（close 广播，Ensure 等就绪用）
	// callCancel 在途调用的取消函数（go-sdk 连接关闭不会使在途 CallTool 失败——悬挂至
	// per-call 超时；Close/headless 切换时显式取消，在途调用得显式错误而非悬挂）。
	callCancel context.CancelFunc

	callMu sync.Mutex
}

// entryOf 取或建 entry（注册表 CRUD，只碰 m.mu）。
func (m *Manager) entryOf(profile string) *sessionEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.entries[profile]; ok {
		return entry
	}
	entry := &sessionEntry{profile: profile, state: StatusIdle}
	m.entries[profile] = entry
	return entry
}

// resetIdleTimerLocked 重置主层 idle 计时（成功调用后；到期走 Close 释放路径）。
func (e *sessionEntry) resetIdleTimerLocked(m *Manager) {
	if e.idleTimer != nil {
		e.idleTimer.Stop()
	}
	e.idleTimer = time.AfterFunc(idleReleaseTimeout, func() { _ = m.Close(e.profile) })
}

// stopIdleTimerLocked 停止 idle 计时（释放/失败路径）。
func (e *sessionEntry) stopIdleTimerLocked() {
	if e.idleTimer != nil {
		e.idleTimer.Stop()
		e.idleTimer = nil
	}
}

// Ensure 幂等受理引擎会话：ready 复用（headless 一致时）；starting 等就绪（上限
// ensureWaitTimeout）；failed/idle 自动（重）拉；ready 且显式 headless 与当前不同 =
// close + reopen（D10 切换，登录态在 profile 目录续存，重开成本 1-3s）。headless 缺省
// 读 profile 偏好文件，显式传入覆写回存。
func (m *Manager) Ensure(ctx context.Context, profile string, headless *bool) (SessionView, error) {
	if err := ValidateProfileName(profile); err != nil {
		return SessionView{}, err
	}
	profileDir, err := ProfileDir(profile)
	if err != nil {
		return SessionView{}, err
	}
	wantHeadless, err := m.resolveHeadless(profileDir, headless)
	if err != nil {
		return SessionView{}, err
	}
	err = m.ensureSpawned(ctx, m.entryOf(profile), profile, wantHeadless)
	return m.view.row(profile), err
}

// resolveHeadless D10 偏好解析：缺省读 prefs.json（缺失 = 有头），显式传入覆写回存。
func (m *Manager) resolveHeadless(profileDir string, headless *bool) (bool, error) {
	if headless == nil {
		prefs, err := ReadProfilePrefs(profileDir)
		if err != nil {
			return false, err
		}
		return prefs.Headless, nil
	}
	if err := WriteProfilePrefs(profileDir, ProfilePrefs{Headless: *headless}); err != nil {
		return false, err
	}
	return *headless, nil
}

// ensureSpawned 就绪保证：join 快路径 → starting 等待 → idle/failed（重）拉 /
// ready-headless-切换重建。join 的一方在 spawn 终结后重评估（headless 与诉求不一致时
// 走重建——D10 切换契约对 joining 方同样成立）。spawn 链 ctx 预算由调用方 ctx +
// ensureWaitTimeout 双重约束，引擎握手自身预算见 runSpawn。
func (m *Manager) ensureSpawned(ctx context.Context, entry *sessionEntry, profile string, wantHeadless bool) error {
	for {
		m.mu.Lock()
		entry.mu.Lock()
		if entry.state == StatusReady && !entry.closing && entry.headless == wantHeadless {
			entry.mu.Unlock()
			m.mu.Unlock()
			return nil
		}
		if entry.state == StatusStarting {
			// join 在途 spawn：等终结后重评估（不自建 spawn——同 profile 单飞）。
			spawnDone := entry.spawnDone
			entry.mu.Unlock()
			m.mu.Unlock()
			if err := waitSpawn(ctx, spawnDone); err != nil {
				return err
			}
			if err := m.settleSpawn(entry, profile); err != nil {
				return err
			}
			// 诉求以 prefs 为仲裁（并发期显式覆写已回存 SSOT）：与 entry 不一致才走
			// 重建——多方并发诉求收敛到 prefs，不互相追逐。
			profileDir, err := ProfileDir(profile)
			if err != nil {
				return err
			}
			wantHeadless, err = m.resolveHeadless(profileDir, nil)
			if err != nil {
				return err
			}
			continue
		}
		if entry.closing {
			entry.mu.Unlock()
			m.mu.Unlock()
			return ErrSessionClosing
		}

		var old *EngineHandle
		var oldCancel context.CancelFunc
		switch entry.state {
		case StatusReady: // headless 切换 = close + reopen（D10）
			old = entry.handle
			oldCancel = entry.callCancel
			entry.callCancel = nil
			entry.stopIdleTimerLocked()
		case StatusIdle, StatusFailed:
			if m.activeCount >= maxConcurrentProfiles {
				entry.mu.Unlock()
				m.mu.Unlock()
				return fmt.Errorf("并发浏览器 profile 达上限（%d），请先关闭其它会话", maxConcurrentProfiles)
			}
			m.activeCount++
		default:
			entry.mu.Unlock()
			m.mu.Unlock()
			return fmt.Errorf("浏览器会话状态异常: %s", entry.state)
		}
		entry.handle = nil
		entry.state = StatusStarting
		entry.headless = wantHeadless
		entry.closing = false
		entry.consecutiveTimeouts = 0
		entry.spawnDone = make(chan struct{})
		spawnDone := entry.spawnDone
		sv := m.view.apply(profile, func(row *SessionView) {
			row.Status = StatusStarting
			row.Headless = wantHeadless
			row.Error = ""
		})
		entry.mu.Unlock()
		m.mu.Unlock()

		if oldCancel != nil {
			oldCancel() // 先取消旧引擎在途调用（同 Close 语义：session.Close 等在途请求结算）
		}
		if old != nil {
			_ = old.Close() // 旧引擎优雅关停（锁外慢操作）
		}
		m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
		go m.runSpawn(entry)

		if err := waitSpawn(ctx, spawnDone); err != nil {
			return err
		}
		if err := m.settleSpawn(entry, profile); err != nil {
			return err
		}
		// 自建 spawn 期间可能被他人显式切换——continue 后按 prefs 重评估统一出口。
		continue
	}
}

// settleSpawn spawn 终态检查：ready 即就绪；failed 显式报错（重试 Ensure 即重建）；
// starting = join 的一方已抢先重建（显式 headless 覆写已回写 prefs，缺省方跟随当前
// prefs 即可）——让路返回 nil，调用方后续操作经 Forward 的 Ensure join 新 spawn；
// 关闭/释放路径给关闭语义错误。
func (m *Manager) settleSpawn(entry *sessionEntry, profile string) error {
	m.mu.Lock()
	entry.mu.Lock()
	state, closing := entry.state, entry.closing
	entry.mu.Unlock()
	m.mu.Unlock()
	switch {
	case closing:
		return ErrSessionClosing
	case state == StatusReady, state == StatusStarting:
		return nil
	case state == StatusFailed:
		return fmt.Errorf("浏览器引擎启动失败: %s", m.view.row(profile).Error)
	default: // idle：spawn 期间被 Close
		return errSessionClosed
	}
}

// waitSpawn 等待 spawn 终结（调用方 ctx + 等就绪上限双约束）。
func waitSpawn(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(ensureWaitTimeout):
		return fmt.Errorf("浏览器引擎准备超时（上限 %s），请稍后重试", ensureWaitTimeout)
	}
}

// runSpawn 后台拉起链（ensureSpawned 受理的后续）：握手预算 spawnHandshakeTimeout；
// 成功置 ready + 退出守护，失败置 failed（均锁外广播）。不捕获请求 ctx（调用方等待
// 已由 waitSpawn 约束，引擎拉起不因请求返回而中止）。
func (m *Manager) runSpawn(entry *sessionEntry) {
	m.mu.Lock()
	entry.mu.Lock()
	profile, headless := entry.profile, entry.headless
	spawnDone := entry.spawnDone
	entry.mu.Unlock()
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), spawnHandshakeTimeout)
	defer cancel()
	handle, err := LaunchEngine(ctx, m.dirs, profile, headless, m.log)

	m.mu.Lock()
	entry.mu.Lock()
	if entry.state != StatusStarting || entry.spawnDone != spawnDone {
		// 防御：条目已被并发替换（正常流程不可达——替换仅发生在终态）。回收新引擎。
		entry.mu.Unlock()
		m.mu.Unlock()
		if handle != nil {
			_ = handle.Close()
		}
		return
	}
	if err != nil && entry.closing {
		// spawn 期间被 Close 且拉起失败：按关闭收尾（用户已关闭，不再报失败态）。
		entry.state = StatusIdle
		m.activeCount--
		entry.closing = false
		entry.mu.Unlock()
		m.mu.Unlock()
		close(spawnDone)
		return
	}
	if err != nil {
		entry.state = StatusFailed
		m.activeCount--
		sv := m.view.apply(profile, func(row *SessionView) {
			row.Status = StatusFailed
			row.Error = err.Error()
		})
		entry.mu.Unlock()
		m.mu.Unlock()
		m.log.Warn("浏览器引擎启动失败", zap.String("profile", profile), zap.Error(err))
		m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
		close(spawnDone)
		return
	}
	if entry.closing { // spawn 期间被 Close：直接回收（view 已由 Close 置 idle）
		entry.state = StatusIdle
		m.activeCount--
		entry.closing = false
		entry.mu.Unlock()
		m.mu.Unlock()
		_ = handle.Close()
		close(spawnDone)
		return
	}
	entry.state = StatusReady
	entry.handle = handle
	entry.resetIdleTimerLocked(m)
	sv := m.view.apply(profile, func(row *SessionView) {
		row.Status = StatusReady
		row.Error = ""
	})
	entry.mu.Unlock()
	m.mu.Unlock()
	m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
	go m.watchExit(entry, handle)
	close(spawnDone)
}

// watchExit 退出守护：引擎意外死亡（非调用方主动 Close）→ 置 failed 并广播（下次调用
// ensure 自动重建）。主动收尾路径（Close/idle 释放/超时回收）由调用方自理。
func (m *Manager) watchExit(entry *sessionEntry, handle *EngineHandle) {
	<-handle.Exited()
	m.mu.Lock()
	entry.mu.Lock()
	if entry.handle != handle || handle.ClosedByCaller() {
		entry.mu.Unlock()
		m.mu.Unlock()
		return
	}
	entry.state = StatusFailed
	entry.handle = nil
	m.activeCount--
	entry.stopIdleTimerLocked()
	errText := "引擎进程意外退出"
	if err := handle.ExitErr(); err != nil {
		errText += ": " + err.Error()
	}
	if tail := strings.TrimSpace(handle.StderrTail().Text()); tail != "" {
		errText += "（stderr: " + tail + "）"
	}
	profile := entry.profile
	sv := m.view.apply(profile, func(row *SessionView) {
		row.Status = StatusFailed
		row.Error = errText
	})
	entry.mu.Unlock()
	m.mu.Unlock()
	m.log.Warn("浏览器引擎意外退出", zap.String("profile", profile), zap.String("reason", errText))
	m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
}

// Forward 引擎调用统一入口（mcp_tool 转发与 controller navigate/screenshot 均经此）：
// ensure 就绪 → callMu 互斥（busy 即拒，中文文案）→ per-call 超时 → 成功重置 idle 计时
// + 页面投影刷新（截图轮询路径豁免）。转发结果按 isError:true 标志判错（引擎错误一律
// isError 文本，非 JSON-RPC error）——结果原样返回，包装归调用方（T2.2 手工透传
// Content 数组）。issueTag 为归因展示标签（lastIssueTag/recentCalls，非过滤键）。
func (m *Manager) Forward(ctx context.Context, profile, tool string, args map[string]any, issueTag string) (*mcp.CallToolResult, error) {
	if err := ValidateProfileName(profile); err != nil {
		return nil, err
	}
	if _, err := m.Ensure(ctx, profile, nil); err != nil {
		return nil, err
	}
	entry := m.entryOf(profile)
	if !entry.callMu.TryLock() {
		return nil, ErrSessionBusy
	}
	defer entry.callMu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, forwardCallTimeout)
	defer cancel()

	m.mu.Lock()
	entry.mu.Lock()
	if entry.closing || entry.state != StatusReady || entry.handle == nil {
		entry.mu.Unlock()
		m.mu.Unlock()
		m.recordCall(profile, tool, issueTag, CallClosed, "")
		return nil, errSessionClosed
	}
	handle := entry.handle
	entry.callCancel = cancel
	entry.mu.Unlock()
	m.mu.Unlock()
	// 在途登记清除（调用终结后 Close 不再持失效 cancel；callMu 仍持有，无并发登记）。
	defer func() {
		m.mu.Lock()
		entry.mu.Lock()
		entry.callCancel = nil
		entry.mu.Unlock()
		m.mu.Unlock()
	}()

	res, err := handle.Session().CallTool(callCtx, &mcp.CallToolParams{Name: tool, Arguments: args})

	m.mu.Lock()
	entry.mu.Lock()
	released := entry.handle != handle // 会话已释放/替换（close/idle 释放/超时回收）——在途调用据此归类
	if err != nil {
		timedOut := errors.Is(err, context.DeadlineExceeded)
		// released 时不做超时计数与置 failed：会话已被并发释放（计数已结清），
		// 归类走 CallClosed。
		if timedOut && !released {
			entry.consecutiveTimeouts++
			if entry.consecutiveTimeouts >= consecutiveTimeoutLimit {
				// 连续超时 → 置 failed 自动重建：引擎可能挂死，就地杀组回收。
				stale := entry.handle
				entry.state = StatusFailed
				entry.handle = nil
				m.activeCount--
				entry.stopIdleTimerLocked()
				failText := fmt.Sprintf("连续 %d 次调用超时，会话已标记失败，下次调用将自动重建", consecutiveTimeoutLimit)
				sv := m.view.apply(profile, func(row *SessionView) {
					row.Status = StatusFailed
					row.Error = failText
				})
				entry.mu.Unlock()
				m.mu.Unlock()
				if stale != nil {
					// 异步回收：引擎可能挂死，session.Close 等在途请求结算可能长阻塞，
					// 不得拖住 Forward 返回（callMu 已随 defer 释放，不影响后续调用）。
					go func(h *EngineHandle) { _ = h.Close() }(stale)
				}
				m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
				m.recordCall(profile, tool, issueTag, CallTimeout, err.Error())
				return nil, fmt.Errorf("%s: %w", failText, err)
			}
		}
		entry.mu.Unlock()
		m.mu.Unlock()
		outcome := CallError
		callErr := err
		switch {
		case timedOut:
			outcome = CallTimeout
		case released:
			outcome = CallClosed
			callErr = fmt.Errorf("%w: %w", errSessionClosed, err)
		}
		m.recordCall(profile, tool, issueTag, outcome, callErr.Error())
		return nil, callErr
	}
	entry.consecutiveTimeouts = 0
	entry.resetIdleTimerLocked(m)
	entry.mu.Unlock()
	m.mu.Unlock()

	outcome, errText := CallOK, ""
	if res.IsError {
		outcome, errText = CallError, resultText(res)
		if strings.Contains(strings.ToLower(errText), "stale") {
			outcome = CallStaleRef
		}
	}
	m.recordCall(profile, tool, issueTag, outcome, errText)
	if !pagesRefreshExempt[tool] {
		m.refreshPages(handle, profile)
	}
	return res, nil
}

// recordCall 操作流水记录（view 行 recentCalls 环形缓冲 + lastIssueTag）并广播
// sessions 帧（锁外）。
func (m *Manager) recordCall(profile, tool, issueTag, outcome, errText string) {
	// rune 安全截断（字节截断会切断 UTF-8 序列，序列化端得 U+FFFD）。
	if runes := []rune(errText); len(runes) > 200 {
		errText = string(runes[:200]) + "…"
	}
	sv := m.view.apply(profile, func(row *SessionView) {
		if issueTag != "" {
			row.LastIssueTag = issueTag
		}
		row.RecentCalls = append(row.RecentCalls, RecentCall{
			Tool:     tool,
			IssueTag: issueTag,
			Outcome:  outcome,
			Error:    errText,
		})
		if len(row.RecentCalls) > recentCallsCap {
			row.RecentCalls = row.RecentCalls[len(row.RecentCalls)-recentCallsCap:]
		}
	})
	m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
}

// Close 释放会话（D4 主层释放：idle 计时到期/面板切换/用户明示）：走 EngineHandle.Close
// 收敛序（session.Close 优雅关停 → 杀组 → 删 pid 文件）。entry 回 idle，下次调用 ensure
// 重拉（登录态在 profile 目录续存，重拉 1-3s）。starting 态受理 closing 由 runSpawn
// 收尾；在途调用因连接终结得到显式错误而非悬挂。
func (m *Manager) Close(profile string) error {
	m.mu.Lock()
	entry, ok := m.entries[profile]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	entry.mu.Lock()
	switch entry.state {
	case StatusIdle:
		entry.mu.Unlock()
		m.mu.Unlock()
		return nil
	case StatusStarting:
		entry.closing = true
		sv := m.view.apply(profile, func(row *SessionView) { row.Status = StatusIdle })
		entry.mu.Unlock()
		m.mu.Unlock()
		m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
		return nil
	case StatusFailed:
		// failed 的 activeCount 已在置 failed 的迁移点结清（handle 恒 nil），只归位
		// idle 供投影与重拉，不落通用递减路径（防计数下穿、上限闸漂移）。
		entry.state = StatusIdle
		sv := m.view.apply(profile, func(row *SessionView) { row.Status = StatusIdle })
		entry.mu.Unlock()
		m.mu.Unlock()
		m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
		return nil
	}
	handle := entry.handle
	cancelCall := entry.callCancel
	entry.callCancel = nil
	entry.state = StatusIdle
	m.activeCount--
	entry.handle = nil
	entry.stopIdleTimerLocked()
	sv := m.view.apply(profile, func(row *SessionView) { row.Status = StatusIdle })
	entry.mu.Unlock()
	m.mu.Unlock()
	if cancelCall != nil {
		// 先取消在途调用：go-sdk 的 session.Close 会等在途请求结算，不先取消会阻塞至
		// per-call 超时（最长 120s）。
		cancelCall()
	}
	if handle != nil {
		_ = handle.Close() // 锁外慢操作
	}
	m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
	return nil
}

// StopAll 全量释放（SIGTERM 收敛序列：Browser.StopAll() 在 AcpSessions.StopAll() 前，
// T2.1 装配）——先逐 entry 释放再关停订阅。
func (m *Manager) StopAll() {
	m.mu.Lock()
	entries := make([]*sessionEntry, 0, len(m.entries))
	for _, entry := range m.entries {
		entries = append(entries, entry)
	}
	m.mu.Unlock()
	for _, entry := range entries {
		_ = m.Close(entry.profile)
	}
	m.hub.closeAll()
}

// Snapshot 全量投影（REST getInfo 消费）。
func (m *Manager) Snapshot() *BrowserViewSnapshot {
	return m.view.snapshot()
}

// Subscribe SSE 订阅（controller GET events 消费）：建连首帧全量 snapshot。
func (m *Manager) Subscribe() (<-chan Frame, func()) {
	return m.hub.subscribe(func() *BrowserViewSnapshot { return m.view.snapshot() })
}

// recoverOrphans sidecar 启动清扫：扫全部 profile 的 .engine.pid 残留锚点，杀组前
// 校验 cmdline 含 vendored 安装目录路径（PID 复用防护——引擎自发崩溃路径不删 pid
// 文件，残留锚点同样经此校验防误杀）；校验不过或进程已消亡仅清 pid 文件。
func (m *Manager) recoverOrphans() {
	entry, err := EngineEntry()
	if err != nil {
		m.log.Warn("浏览器引擎 pin 读取失败，跳过孤儿清扫", zap.Error(err))
		return
	}
	// vendored 安装目录锚点（EnsureVendored 布局 <dstRoot>/<id>/<version>；Adapters
	// 未配置时无法锚定——不杀，仅清 pid 文件）。
	anchor := ""
	if m.dirs.Adapters != "" {
		anchor = filepath.Join(m.dirs.Adapters, entry.ID, entry.Version)
	}
	profilesDir := filepath.Join(Root(), "profiles")
	dirs, err := os.ReadDir(profilesDir)
	if err != nil {
		return // 无 profiles 目录 = 无孤儿
	}
	for _, d := range dirs {
		if d.IsDir() {
			m.recoverOrphanProfile(filepath.Join(profilesDir, d.Name()), anchor)
		}
	}
}

// recoverOrphanProfile 单 profile 的孤儿锚点回收。
func (m *Manager) recoverOrphanProfile(profileDir, anchor string) {
	pidFile := filepath.Join(profileDir, pidFileName)
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return // 无锚点
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		_ = os.Remove(pidFile) // 锚点损坏：清之
		return
	}
	if !orphanIsEngine(pid, anchor) {
		// 进程已消亡、PID 易主或锚点不可得：不杀，仅清 pid 文件。
		_ = os.Remove(pidFile)
		return
	}
	m.log.Warn("检测到残留浏览器引擎会话，回收进程组", zap.Int("pid", pid), zap.String("profileDir", profileDir))
	if err := killOrphanPid(pid); err != nil {
		m.log.Warn("残留引擎进程组回收失败（保留 pid 文件待下次清扫）", zap.Int("pid", pid), zap.Error(err))
		return
	}
	_ = os.Remove(pidFile)
}

// orphanIsEngine PID 复用防护：校验目标 cmdline 属于 vendored 引擎（unix 含安装目录
// 锚点；Windows 无便捷 cmdline 通道，tasklist 映像名 node.exe 弱校验——已知局限，
// 引擎本身是 node 进程）。
func orphanIsEngine(pid int, anchor string) bool {
	cmdline, alive := pidCmdline(pid)
	if !alive {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.Contains(strings.ToLower(cmdline), "node.exe")
	}
	return anchor != "" && strings.Contains(cmdline, anchor)
}

// pidCmdline 读进程命令行（unix：/proc 优先、ps 兜底——macOS 无 /proc；Windows：
// tasklist CSV）。第二返回值 = 进程是否存活。
func pidCmdline(pid int) (string, bool) {
	pidStr := strconv.Itoa(pid)
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", "PID eq "+pidStr, "/FO", "CSV", "/NH").Output()
		if err != nil {
			return "", false
		}
		text := string(out)
		// 无匹配行时 tasklist 输出 INFO 提示（本地化为系统语言，按引号特征判存活）。
		if !strings.Contains(text, `"`) {
			return "", false
		}
		return text, true
	}
	if data, err := os.ReadFile(filepath.Join("/proc", pidStr, "cmdline")); err == nil {
		return strings.ReplaceAll(string(data), "\x00", " "), true
	}
	out, err := exec.Command("ps", "-o", "command=", "-p", pidStr).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// killOrphanPid 以 pid 垫板 exec.Cmd 复用 acp.KillProcessGroup（单一 SSOT：unix 杀
// 进程组、Windows taskkill 树杀）。
func killOrphanPid(pid int) error {
	return acp.KillProcessGroup(&exec.Cmd{Process: &os.Process{Pid: pid}})
}
