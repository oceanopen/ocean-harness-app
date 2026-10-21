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
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"ocean-harness/server/internal/acp"
)

/*
 * 会话管理（T1.3，D4 用完即释放）：per-profile 单引擎进程的状态机（idle/starting/ready/failed）、互斥与并发上限、per-call 超时与连续超时自愈、主层 idle 计时释放、孤儿回收与 SSE 投影。
 * 锁三分纪律（对齐 acpsession 锁序）：m.mu = 注册表 CRUD 与 activeCount（持锁只做 map 操作与计数，绝不调 entry 方法）；entry.mu = 状态守护（置态 + view 投影 + 产帧数据，帧广播恒在锁外）；view 锁 = 投影读写（hub 建连 snapshot 只碰 view 锁——长调用期间面板 SSE 建连不卡死）。
 * 锁序：callMu → m.mu → entry.mu → view，绝不反向；hub 锁独立（broadcast 恒在其余锁外调用，锁内只经 subscribe 的 snapshot 闭包碰 view 读锁）。
 */

// 时序与容量常量（var 化供测试覆写；编码规则 1——时序缺陷不引入防抖掩盖）。
var (
	/*
	 * spawnHandshakeTimeout 引擎拉起握手预算：LaunchEngine 无内建预算，挂死引擎会无限
	 * 占住 PumpStderr goroutine（acpsession 分步超时同款纪律的 browser 单点总预算形态）。
	 */
	spawnHandshakeTimeout = 60 * time.Second
	// ensureWaitTimeout starting 等就绪上限。
	ensureWaitTimeout = 30 * time.Second
	// forwardCallTimeout per-call 超时上限（防 evaluate 死循环把 profile 永久挂死）。
	forwardCallTimeout = 120 * time.Second
	/*
	 * closePagesCaptureTimeout close 收尾前的真实页面捕获预算：浏览器窗口内手动导航只
	 * 有引擎知道，无头切换/重开恢复现场以捕获结果为准；引擎挂死时短预算放弃，不拖累
	 * 收尾（idle 释放同路径受益——重开恢复真实最后页面）。
	 */
	closePagesCaptureTimeout = 3 * time.Second
	// restorePagesTimeout 会话重建后页面现场恢复的总预算（逐页导航串行）。
	restorePagesTimeout = 30 * time.Second
	/*
	 * idleReleaseTimeout 主层 idle 释放计时（D4 双层的主层；每次成功调用即重置。兜底层
	 * 为引擎侧 --idle-timeout 15 分钟，见 pin.go——到期只关浏览器窗口，entry 状态无需
	 * 迁移。登录配方 wait_for 轮询与面板截图轮询天然保活，属预期）。
	 */
	idleReleaseTimeout = 10 * time.Minute
)

const (
	/*
	 * maxConcurrentProfiles 全局并发 profile 上限（manager 单飞互斥之外的总闸；每任务
	 * 闸 MaxSessionsPerTask 之外的资源兜底，多工作空间并行时封顶）。
	 */
	maxConcurrentProfiles = 10
	// consecutiveTimeoutLimit 连续调用超时阈值：置 failed 自动重建。
	consecutiveTimeoutLimit = 3
)

// 显式中文错误（调用方按 errors.Is 判别）。
var (
	ErrSessionBusy    = errors.New("浏览器会话忙：已有操作正在执行，请稍后重试")
	ErrSessionClosing = errors.New("浏览器会话正在关闭，请稍后重试")
	errSessionClosed  = errors.New("浏览器会话已关闭（引擎已释放），请重试")
)

/*
 * Manager 浏览器会话域门面：per-profile 引擎生命周期（ensure/forward/close）、SSE
 * 投影订阅与孤儿回收。进程模型 = per-profile 单引擎进程，用完即释放（D4）。
 */
type Manager struct {
	dirs Dirs
	log  *zap.Logger
	hub  *hub
	view *browserView

	mu      sync.Mutex
	entries map[string]*sessionEntry
	/*
	 * activeCount starting/ready 条目数（并发上限判据）。仅在 m.mu → entry.mu 双锁
	 * 临界区内增减（锁序见文件头；m.mu 永不反向嵌套 entry.mu 之外的路径）。
	 */
	activeCount int

	/*
	 * 观察通道域（T4.4，独立于 m.mu——帧分发路径零 manager 锁触达）。锁序：
	 * connectMu → viewerMu → m.mu → entry.mu；onLiveRefsChange 恒在 viewerMu 外调用。
	 */
	viewerMu  sync.Mutex
	viewers   map[string]*liveSlot
	connectMu sync.Mutex // 连接阶段串行化（拨号秒级慢操作不持 viewerMu）

	/*
	 * 页面观察者域（Pages 投影事件驱动 SSOT）：observerMu 守护注册表，观察者仅经
	 * applyPages 触达 view/hub（锁序独立）。observersEnabled 供测试关断（单测关闭
	 * 后台观察者，观察者专项测试显式开启）。
	 */
	observerMu       sync.Mutex
	observers        map[string]*pageObserver
	observersEnabled bool

	/*
	 * 页面级操作历史（per-profile per-target，跨页面关闭与会话重建留存）：调试排障的
	 * 取数层，独立锁自守（store 自带 mu），不与 entry/view 锁交叉。
	 */
	pageHist *pageHistoryStore
}

/*
 * NewManager 装配会话域（T2.1 由 main 懒启动调用，构造即返回）：启动即清扫残留引擎
 * 孤儿（recoverOrphans——sidecar 重启后引擎/浏览器残留的 pid 锚点回收）。
 */
func NewManager(dirs Dirs, log *zap.Logger) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
	m := &Manager{
		dirs:             dirs,
		log:              log,
		hub:              newHub(),
		view:             newBrowserView(),
		entries:          map[string]*sessionEntry{},
		viewers:          map[string]*liveSlot{},
		observers:        map[string]*pageObserver{},
		observersEnabled: true,
		pageHist:         newPageHistoryStore(),
	}
	m.recoverOrphans()
	return m
}

/*
 * sessionEntry 单 profile 运行时会话宿主。mu 守护 state/handle/closing/consecutive
 * Timeouts/idleTimer/spawnDone/callCancel；callMu 为调用互斥（长 Forward 持有期间不碰
 * m.mu/entry.mu 之外的慢操作）。idle = 已释放（条目驻留供投影，下次调用 ensure 重拉）。
 */
type sessionEntry struct {
	profile string
	mu      sync.Mutex

	state    SessionStatus
	headless bool
	handle   *EngineHandle
	/*
	 * closing Close 已受理——仅覆盖 starting 窗口（spawn 期间 Close 无法直接回收，
	 * 由 runSpawn 终结时消费复位；生命周期 = 一次 spawn），在途调用据此归类。
	 */
	closing             bool
	consecutiveTimeouts int
	idleTimer           *time.Timer
	spawnDone           chan struct{} // 本次 spawn 终结信号（close 广播，Ensure 等就绪用）
	/*
	 * callCancel 在途调用的取消函数（go-sdk 连接关闭不会使在途 CallTool 失败——悬挂至
	 * per-call 超时；Close/headless 切换时显式取消，在途调用得显式错误而非悬挂）。
	 */
	callCancel context.CancelFunc

	/*
	 * viewerRefs 活跃观察订户数（T4.4 idle 保活闸的判据）：写侧在 viewerMu 下
	 * Add，读侧 resetIdleTimerLocked 持 entry.mu 原子 Load——锁序 viewerMu → entry.mu
	 * 下 entry 侧不可反取 viewerMu，原子读是唯一合规路径。
	 */
	viewerRefs atomic.Int32

	/*
	 * restorePages 会话释放前捕获的页面现场（重开/无头切换恢复用，激活页垫底恢复）。
	 * 关闭后仍留存——恢复语义跨重建成立。
	 */
	restorePages []PageView

	/*
	 * authInjectedHash 已注入共享登录态的内容指纹（跨 idle 重建留存）：重开遇同内容
	 * 共享文件跳过二次注入。仅 spawn 路径读写（置 ready 前单线程，无并发触达）。
	 */
	authInjectedHash string

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

/*
 * resetIdleTimerLocked 重置主层 idle 计时（成功调用后；到期走 Close 释放路径）。
 * 观察保活闸（T4.4/D4 补充）：活跃订户在场时计时暂停——观看即使用，不因无 AI 调用释放。
 */
func (e *sessionEntry) resetIdleTimerLocked(m *Manager) {
	if e.viewerRefs.Load() > 0 {
		return
	}
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

/*
 * Ensure 幂等受理引擎会话：ready 复用（headless 一致时）；starting 等就绪（上限 ensureWaitTimeout）；failed/idle 自动（重）拉；ready 且显式 headless 与当前不同 = close + reopen（D10 切换，登录态在 profile 目录续存，重开成本 1-3s）。
 * headless/workspaceID 缺省读 profile 偏好文件，显式传入覆写回存（workspaceID 即共享登录态键，落盘后 idle 冷启动/无头切换重建自动读回，不依赖调用方每次携带）。
 */
func (m *Manager) Ensure(ctx context.Context, profile string, headless *bool, workspaceID string) (SessionView, error) {
	if err := ValidateProfileName(profile); err != nil {
		return SessionView{}, err
	}
	profileDir, err := ProfileDir(profile)
	if err != nil {
		return SessionView{}, err
	}
	wantHeadless, err := m.resolvePrefs(profileDir, headless, workspaceID)
	if err != nil {
		return SessionView{}, err
	}
	err = m.ensureSpawned(ctx, m.entryOf(profile), profile, wantHeadless)
	return m.view.row(profile), err
}

/*
 * resolvePrefs D10 偏好解析：缺省读 prefs.json（缺失取 ReadProfilePrefs 缺省），显式
 * headless/workspaceID 覆写回存——两字段合并写回，互不抹除；无显式入参零落盘。
 */
func (m *Manager) resolvePrefs(profileDir string, headless *bool, workspaceID string) (bool, error) {
	prefs, err := ReadProfilePrefs(profileDir)
	if err != nil {
		return false, err
	}
	if headless == nil && workspaceID == "" {
		return prefs.Headless, nil
	}
	if headless != nil {
		prefs.Headless = *headless
	}
	if workspaceID != "" {
		prefs.WorkspaceID = workspaceID
	}
	if err := WriteProfilePrefs(profileDir, prefs); err != nil {
		return false, err
	}
	return prefs.Headless, nil
}

/*
 * taskActiveCount 同一配额基座（issueQuotaBase 归组）下 starting/ready 条目数（每任务
 * 并发上限判据）。须持 m.mu 调用；他条目状态经各自 entry.mu 短锁读取（锁序同文件头纪律）；
 * self 为调用方条目——其 mu 已被持有不可重入，受理时恒为 idle/failed 本就不计活跃。
 */
func (m *Manager) taskActiveCount(self *sessionEntry, base string) int {
	n := 0
	for _, e := range m.entries {
		if e == self {
			continue
		}
		if eBase, ok := issueQuotaBase(e.profile); !ok || eBase != base {
			continue
		}
		e.mu.Lock()
		active := e.state == StatusStarting || e.state == StatusReady
		e.mu.Unlock()
		if active {
			n++
		}
	}
	return n
}

/*
 * ensureSpawned 就绪保证：join 快路径 → starting 等待 → idle/failed（重）拉 / ready-headless-切换重建；join 的一方在 spawn 终结后重评估（headless 与诉求不一致时走重建——D10 切换契约对 joining 方同样成立）。
 * spawn 链 ctx 预算由调用方 ctx + ensureWaitTimeout 双重约束，引擎握手自身预算见 runSpawn。
 */
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
			/*
			 * 诉求以 prefs 为仲裁（并发期显式覆写已回存 SSOT）：与 entry 不一致才走
			 * 重建——多方并发诉求收敛到 prefs，不互相追逐。
			 */
			profileDir, err := ProfileDir(profile)
			if err != nil {
				return err
			}
			wantHeadless, err = m.resolvePrefs(profileDir, nil, "")
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
			if base, isTask := issueQuotaBase(profile); isTask && m.taskActiveCount(entry, base) >= MaxSessionsPerTask {
				entry.mu.Unlock()
				m.mu.Unlock()
				return fmt.Errorf("任务并发浏览器会话达上限（%d），请先释放不需要的会话", MaxSessionsPerTask)
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
			/*
			 * 切换前对齐 Close 收尾语义：重捕获页面现场（restorePages 仅 Close 赋值，不重捕获
			 * 则拿陈旧现场恢复）；清页面投影并显式广播空行（publishPages 对空记录不广播）。
			 */
			captureCtx, cancel := context.WithTimeout(context.Background(), closePagesCaptureTimeout)
			captured := m.capturePagesCtx(captureCtx, old, profile)
			cancel()
			m.pageHist.closeAll(profile)
			svCleared := m.view.apply(profile, func(row *SessionView) { row.Pages = nil })
			m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{svCleared}}})
			entry.mu.Lock()
			entry.restorePages = captured
			entry.mu.Unlock()
			/*
			 * 页面观察者与观察通道收敛（重建后端口变化，残连接必死）：显式停机/拆让在订
			 * 方拿到终态，新会话就绪后各自重建。
			 */
			m.stopObserver(profile)
			m.dropLiveViewer(profile, "无头模式切换，实时画面已断开")
			// 切换前导出旧引擎共享登录态（对齐 Close 收尾语义，best-effort）。
			if wsID := m.resolveWorkspaceID(profile); wsID != "" {
				m.exportAuthState(profile, wsID, old)
			}
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

/*
 * settleSpawn spawn 终态检查：ready 即就绪；failed 显式报错（重试 Ensure 即重建）；starting = join 的一方已抢先重建（显式 headless 覆写已回写 prefs，缺省方跟随当前 prefs 即可）——让路返回 nil，调用方后续操作经 Forward 的 Ensure join 新 spawn。
 * 关闭/释放路径给关闭语义错误。
 */
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

/*
 * runSpawn 后台拉起链（ensureSpawned 受理的后续）：握手预算 spawnHandshakeTimeout；
 * 成功置 ready + 退出守护，失败置 failed（均锁外广播）。不捕获请求 ctx（调用方等待
 * 已由 waitSpawn 约束，引擎拉起不因请求返回而中止）。
 */
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

	entry.mu.Unlock()
	m.mu.Unlock()

	/*
	 * 工作空间共享登录态注入（置 ready 前锁外同步完成）：ready 即放行外部调用，注入
	 * 晚于 ready 会与首导航竞速——此刻 state 仍 starting，外部调用一律 waitSpawn 等待。
	 */
	wsID := m.resolveWorkspaceID(profile)
	if wsID != "" {
		m.injectAuthState(entry, profile, wsID, handle)
	}

	m.mu.Lock()
	entry.mu.Lock()
	if entry.state != StatusStarting || entry.spawnDone != spawnDone || entry.closing {
		// 注入窗口期条目被关闭/替换：按关闭收尾回收引擎（复刻上方防御分支语义）。
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
	m.startObserver(profile)          // 页面投影观察者随会话就绪启动（后台自愈，不阻塞）
	go m.restorePages(profile, entry) // 页面现场恢复（best-effort，不阻塞就绪返回）
	go m.watchExit(entry, handle)
	if wsID != "" {
		go m.runAuthSync(entry, profile, wsID, handle) // 周期导出（Exited 即退，随引擎生命周期收敛）
	}
	close(spawnDone)
}

/*
 * restorePages 会话重建后的页面现场恢复（best-effort）：逐页导航重建，原激活页垫底恢复
 * （末位导航后即激活态）；单页失败仅记日志跳过，不中断整体恢复。
 */
func (m *Manager) restorePages(profile string, entry *sessionEntry) {
	entry.mu.Lock()
	pages := entry.restorePages
	entry.mu.Unlock()
	if len(pages) == 0 {
		return
	}
	m.log.Info("开始恢复页面现场", zap.String("profile", profile), zap.Int("pages", len(pages)))
	ctx, cancel := context.WithTimeout(context.Background(), restorePagesTimeout)
	defer cancel()
	for _, p := range pages {
		if p.Closed || p.URL == "" || p.URL == "about:blank" {
			continue
		}
		if !p.Current {
			if err := m.openRestoredTab(ctx, profile); err != nil {
				m.log.Warn("页面恢复新建 tab 失败（跳过该页）", zap.String("profile", profile), zap.String("url", p.URL), zap.Error(err))
				continue
			}
		}
		if _, err := m.Forward(ctx, profile, EngineToolNavigate, map[string]any{"url": p.URL}, "恢复页面"); err != nil {
			m.log.Warn("页面恢复导航失败（跳过该页）", zap.String("profile", profile), zap.String("url", p.URL), zap.Error(err))
			continue
		}
	}
}

/*
 * openRestoredTab 新建 tab 并把引擎 current 对齐到它：tabs new 的选中语义不可依赖，
 * 不纠偏时 navigate 覆盖原激活页、新 tab 残留 about:blank。新 tab 以新建前后 tab 序
 * 差集定位（恢复期无 tab 关闭，序稳定），定位失败仅跳过 select（navigate 兜底）。
 */
func (m *Manager) openRestoredTab(ctx context.Context, profile string) error {
	before, err := m.tabPageIndexes(ctx, profile)
	if err != nil {
		return err
	}
	if _, err := m.Forward(ctx, profile, EngineToolTabs, map[string]any{"action": "new"}, "恢复页面"); err != nil {
		return err
	}
	after, err := m.tabPageIndexes(ctx, profile)
	if err != nil {
		return nil // 新 tab 已建成，对齐失败不阻断：navigate 落旧 current 属可接受降级
	}
	for idx := range after {
		if !before[idx] {
			_, _ = m.Forward(ctx, profile, EngineToolTabs, map[string]any{"action": "select", "index": idx}, "恢复页面")
			return nil
		}
	}
	return nil
}

// tabPageIndexes 引擎当前 tab 序号集（tabs list 调用失败或被引擎拒绝即报错）。
func (m *Manager) tabPageIndexes(ctx context.Context, profile string) (map[int]bool, error) {
	res, err := m.Forward(ctx, profile, EngineToolTabs, map[string]any{"action": "list"}, "恢复页面")
	if err != nil {
		return nil, err
	}
	if res.IsError {
		return nil, fmt.Errorf("tabs list: %s", ResultText(res))
	}
	indexes := make(map[int]bool, 4)
	for _, p := range ParseTabsOutput(ResultText(res)) {
		indexes[p.Index] = true
	}
	return indexes, nil
}

/*
 * watchExit 退出守护：引擎意外死亡（非调用方主动 Close）→ 置 failed 并广播（下次调用
 * ensure 自动重建）。主动收尾路径（Close/idle 释放/超时回收）由调用方自理。
 */
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
	// 观察者与观察通道收敛（引擎已死，CDP 连接随即断——显式停机/拆记终态而非自然断连表象）。
	m.stopObserver(profile)
	m.dropLiveViewer(profile, "引擎进程意外退出")
}

/*
 * acceptCall 调用受理共享纪律步（Forward / ListTools 同一节律，纪律只写一处）：profile 校验 → ensure 就绪 → callMu 互斥（busy 即拒不记流水）→ per-call 超时预算 → 双锁状态守卫（会话已关记 CallClosed 流水）→ 在途 callCancel 登记。
 * 返回的 done 收拢「在途登记清除 + cancel + callMu 释放」，拿到 handle 后调用终结时必须恰调一次（登记清除须 callMu 仍持有，无并发登记窗口）；err 非 nil 时不得调用（callMu 未持有或未登记）。
 */
func (m *Manager) acceptCall(ctx context.Context, profile, callName, issueTag string) (*EngineHandle, context.Context, func(), error) {
	if err := ValidateProfileName(profile); err != nil {
		return nil, nil, nil, err
	}
	if _, err := m.Ensure(ctx, profile, nil, ""); err != nil {
		return nil, nil, nil, err
	}
	entry := m.entryOf(profile)
	if !entry.callMu.TryLock() {
		return nil, nil, nil, ErrSessionBusy
	}
	callCtx, cancel := context.WithTimeout(ctx, forwardCallTimeout)

	m.mu.Lock()
	entry.mu.Lock()
	if entry.closing || entry.state != StatusReady || entry.handle == nil {
		entry.mu.Unlock()
		m.mu.Unlock()
		m.recordCall(profile, m.pageHist.currentTargetID(profile), callName, issueTag, CallClosed, "")
		cancel()
		entry.callMu.Unlock()
		return nil, nil, nil, errSessionClosed
	}
	handle := entry.handle
	entry.callCancel = cancel
	entry.mu.Unlock()
	m.mu.Unlock()
	done := func() {
		// 在途登记清除（调用终结后 Close 不再持失效 cancel；callMu 仍持有，无并发登记）。
		m.mu.Lock()
		entry.mu.Lock()
		entry.callCancel = nil
		entry.mu.Unlock()
		m.mu.Unlock()
		cancel()
		entry.callMu.Unlock()
	}
	return handle, callCtx, done, nil
}

/*
 * Forward 引擎调用统一入口（mcp_tool 转发与 controller navigate 均经此）：受理纪律（ensure → callMu 互斥 → per-call 超时 → 在途登记）与 ListTools 共用 acceptCall；成功重置 idle 计时（页面投影由页面观察者事件驱动维护，此处不再刷新）。
 * 转发结果按 isError:true 标志判错（引擎错误一律 isError 文本，非 JSON-RPC error）——结果原样返回，包装归调用方（T2.2 手工透传 Content 数组）。
 * issueTag 为归因展示标签（页面操作流水行，非过滤键）。
 */
func (m *Manager) Forward(ctx context.Context, profile, tool string, args map[string]any, issueTag string) (*mcp.CallToolResult, error) {
	handle, callCtx, done, err := m.acceptCall(ctx, profile, tool, issueTag)
	if err != nil {
		return nil, err
	}
	defer done()

	/*
	 * nil 入参归一空 map：引擎侧 zod 要求 arguments 为 record，null 会被拒
	 * （cookie_list 等无参工具与省录入参的调用路径都在此兜住）。
	 */
	if args == nil {
		args = map[string]any{}
	}
	res, err := handle.Session().CallTool(callCtx, &mcp.CallToolParams{Name: tool, Arguments: args})
	targetID := m.pageHist.currentTargetID(profile) // 操作流水按页归因（调用发起时的激活页）

	entry := m.entryOf(profile) // entries 只增不删，与 acceptCall 取到同一 entry（entryOf 自持 m.mu，须先取再进锁）
	m.mu.Lock()
	entry.mu.Lock()
	released := entry.handle != handle // 会话已释放/替换（close/idle 释放/超时回收）——在途调用据此归类
	if err != nil {
		timedOut := errors.Is(err, context.DeadlineExceeded)
		/*
		 * 连续超时计数只认引擎侧：父 ctx 自带 deadline 到期同报 DeadlineExceeded，
		 * 但非引擎之过，累计会误杀健康引擎重建。
		 */
		engineTimedOut := timedOut && ctx.Err() == nil
		/*
		 * released 时不做超时计数与置 failed：会话已被并发释放（计数已结清）。分类
		 * 口径 released 优先——超时与并发释放同帧命中记 CallClosed（见下方 switch）。
		 */
		if engineTimedOut && !released {
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
					/*
					 * 异步回收：引擎可能挂死，session.Close 等在途请求结算可能长阻塞，
					 * 不得拖住 Forward 返回（callMu 已随 done 释放，不影响后续调用）。
					 */
					go func(h *EngineHandle) { _ = h.Close() }(stale)
				}
				m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
				m.recordCall(profile, targetID, tool, issueTag, CallTimeout, err.Error())
				return nil, fmt.Errorf("%s: %w", failText, err)
			}
		}
		entry.mu.Unlock()
		m.mu.Unlock()
		outcome := CallError
		callErr := err
		switch {
		case released:
			outcome = CallClosed
			callErr = fmt.Errorf("%w: %w", errSessionClosed, err)
		case timedOut:
			outcome = CallTimeout
		}
		m.recordCall(profile, targetID, tool, issueTag, outcome, callErr.Error())
		return nil, callErr
	}
	entry.consecutiveTimeouts = 0
	entry.resetIdleTimerLocked(m)
	entry.mu.Unlock()
	m.mu.Unlock()

	outcome, errText := CallOK, ""
	if res.IsError {
		outcome, errText = CallError, ResultText(res)
		if strings.Contains(strings.ToLower(errText), "stale") {
			outcome = CallStaleRef
		}
	}
	m.recordCall(profile, targetID, tool, issueTag, outcome, errText)
	return res, nil
}

/*
 * listToolsCallName tools/list 调用在操作流水中的展示名（引擎侧为协议请求 tools/list，
 * 流水按 ocean 工具名 browser_tool_list 归类，与面板口径一致）。
 */
const listToolsCallName = "browser_tool_list"

/*
 * ListTools 查询引擎当前可用工具全集（T2.2 browser_tool_list 消费——skill 降级顺序「查引擎全集」的事实源，由引擎 --caps 决定）；受理纪律与 Forward 共用 acceptCall；成功重置 idle 计时与连续超时计数并记操作流水；页面投影不刷新（tools/list 不改 tab 态）。
 * 错误分类与包装与 Forward 对称（并发释放记 CallClosed 并包 errSessionClosed），但不参与连续超时置 failed——挂死引擎的状态纠正归 Forward 调用链，本方法只作查询。
 */
func (m *Manager) ListTools(ctx context.Context, profile, issueTag string) ([]*mcp.Tool, error) {
	handle, callCtx, done, err := m.acceptCall(ctx, profile, listToolsCallName, issueTag)
	if err != nil {
		return nil, err
	}
	defer done()

	res, err := handle.Session().ListTools(callCtx, nil)

	entry := m.entryOf(profile) // entries 只增不删，与 acceptCall 取到同一 entry（entryOf 自持 m.mu，须先取再进锁）
	m.mu.Lock()
	entry.mu.Lock()
	released := entry.handle != handle // 会话已释放/替换——在途调用据此归类
	if err == nil {
		entry.consecutiveTimeouts = 0
		entry.resetIdleTimerLocked(m)
	}
	entry.mu.Unlock()
	m.mu.Unlock()

	outcome, errText := CallOK, ""
	callErr := err
	if err != nil {
		outcome, errText = CallError, err.Error()
		switch {
		case released: // 分类口径与 Forward 对齐：并发释放优先记 CallClosed
			outcome = CallClosed
			callErr = fmt.Errorf("%w: %w", errSessionClosed, err)
			errText = callErr.Error()
		case errors.Is(err, context.DeadlineExceeded):
			outcome = CallTimeout
		}
	}
	m.recordCall(profile, m.pageHist.currentTargetID(profile), listToolsCallName, issueTag, outcome, errText)
	if callErr != nil {
		return nil, callErr
	}
	return res.Tools, nil
}

/*
 * recordCall 操作流水记录（per-page 归档 + 投影广播，锁外）：targetID 空 = 无法归因，
 * 仅落 store 留档，不进投影。
 */
func (m *Manager) recordCall(profile, targetID, tool, issueTag, outcome, errText string) {
	// rune 安全截断（字节截断会切断 UTF-8 序列，序列化端得 U+FFFD）。
	if runes := []rune(errText); len(runes) > 200 {
		errText = string(runes[:200]) + "…"
	}
	m.pageHist.record(profile, targetID, RecentCall{
		Tool:     tool,
		IssueTag: issueTag,
		Outcome:  outcome,
		Error:    errText,
		AtMs:     time.Now().UnixMilli(),
	}, time.Now().UnixMilli())
	m.publishPages(profile)
}

/*
 * Close 释放会话（D4 主层释放：idle 计时到期/面板切换/用户明示）：走 EngineHandle.Close 收敛序（捕获真实页面投影 → session.Close 优雅关停 → 杀组 → 删 pid 文件）；entry 回 idle，下次调用 ensure 重拉（登录态在 profile 目录续存，重拉 1-3s）。
 * starting 态受理 closing 由 runSpawn 收尾；在途调用因连接终结得到显式错误而非悬挂。
 */
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
		/*
		 * failed 的 activeCount 已在置 failed 的迁移点结清（handle 恒 nil），只归位
		 * idle 供投影与重拉，不落通用递减路径（防计数下穿、上限闸漂移）。
		 */
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
		/*
		 * 先取消在途调用：go-sdk 的 session.Close 会等在途请求结算，不先取消会阻塞至
		 * per-call 超时（最长 120s）。
		 */
		cancelCall()
	}
	/*
	 * 杀引擎前先停页面观察者并断观察通道（CDP 连接随引擎死亡本会断，显式收敛让在订方
	 * 拿到终态 reason 而非自然断连表象）。
	 */
	m.stopObserver(profile)
	m.dropLiveViewer(profile, "浏览器会话已关闭")
	/*
	 * idle 行广播先行：捕获帧最后到，最终投影恒为「idle + 捕获到的真实页面」，防旧
	 * pages 回填前端缓存。
	 */
	m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
	if handle != nil {
		/*
		 * 收尾前捕获真实页面（引擎还活着）：浏览器窗口内手动导航只有引擎知道，恢复现场
		 * 以此为准；捕获即标记关闭留档（操作历史跨重建可查）。
		 */
		captureCtx, cancel := context.WithTimeout(context.Background(), closePagesCaptureTimeout)
		captured := m.capturePagesCtx(captureCtx, handle, profile)
		cancel()
		m.pageHist.closeAll(profile)
		m.publishPages(profile)
		entry.mu.Lock()
		entry.restorePages = captured
		entry.mu.Unlock()
		/*
		 * 引擎收尾前导出共享登录态（引擎仍活、localStorage 可全集收集；best-effort 不
		 * 阻塞关闭，周期导出兜底）。工作空间缺失/解析失败时自然跳过。
		 */
		if wsID := m.resolveWorkspaceID(profile); wsID != "" {
			m.exportAuthState(profile, wsID, handle)
		}
		_ = handle.Close() // 锁外慢操作
	}
	return nil
}

/*
 * Row 单 profile 投影行只读导出（close 响应携带收尾捕获后的行，供前端恢复现场；未知
 * profile 返回零值行）。
 */
func (m *Manager) Row(profile string) SessionView {
	return m.view.row(profile)
}

/*
 * StopAll 全量释放（SIGTERM 收敛序列：Browser.StopAll() 在 AcpSessions.StopAll() 前，
 * T2.1 装配）——先逐 entry 释放再关停订阅。
 */
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

/*
 * recoverOrphans sidecar 启动清扫：扫全部 profile 的 .engine.pid 残留锚点，杀组前
 * 校验 cmdline 含 vendored 安装目录路径（PID 复用防护——引擎自发崩溃路径不删 pid
 * 文件，残留锚点同样经此校验防误杀）；校验不过或进程已消亡仅清 pid 文件。
 */
func (m *Manager) recoverOrphans() {
	entry, err := EngineEntry()
	if err != nil {
		m.log.Warn("浏览器引擎 pin 读取失败，跳过孤儿清扫", zap.Error(err))
		return
	}
	/*
	 * vendored 安装目录锚点（EnsureVendored 布局 <dstRoot>/<id>/<version>；Adapters
	 * 未配置时无法锚定——不杀，仅清 pid 文件）。
	 */
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

/*
 * orphanIsEngine PID 复用防护：校验目标 cmdline 属于 vendored 引擎（unix 含安装目录
 * 锚点；Windows 无便捷 cmdline 通道，tasklist 映像名 node.exe 弱校验——已知局限，
 * 引擎本身是 node 进程）。
 */
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

/*
 * pidCmdline 读进程命令行（unix：/proc 优先、ps 兜底——macOS 无 /proc；Windows：
 * tasklist CSV）。第二返回值 = 进程是否存活。
 */
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

/*
 * killOrphanPid 以 pid 垫板 exec.Cmd 复用 acp.KillProcessGroup（单一 SSOT：unix 杀
 * 进程组、Windows taskkill 树杀）。
 */
func killOrphanPid(pid int) error {
	return acp.KillProcessGroup(&exec.Cmd{Process: &os.Process{Pid: pid}})
}
