package browser

import (
	"sort"
	"sync"
)

// SessionStatus 会话状态机（D4）：idle（已释放，条目驻留供投影）→ starting → ready；
// 任一态可 → failed（拉起失败/意外退出/连续超时）；failed/idle 经 ensure 自动（重）拉。
type SessionStatus string

const (
	StatusIdle     SessionStatus = "idle"
	StatusStarting SessionStatus = "starting"
	StatusReady    SessionStatus = "ready"
	StatusFailed   SessionStatus = "failed"
)

// PageView 页面投影行（pages.go 每次 Forward 成功后刷新）。
type PageView struct {
	Index   int    `json:"index"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Current bool   `json:"current"`
}

// CallOutcome 操作流水结果分类（recentCalls 失败 chip 展示口径，轻量分类——仅区分
// 展示形态，不承载重试语义）。
const (
	CallOK       = "ok"
	CallError    = "error"
	CallTimeout  = "timeout"
	CallStaleRef = "stale_ref"
	CallClosed   = "closed"
)

// RecentCall 轻量操作流水行。
type RecentCall struct {
	Tool     string `json:"tool"`
	IssueTag string `json:"issueTag,omitempty"`
	Outcome  string `json:"outcome"`
	Error    string `json:"error,omitempty"`
}

// recentCallsCap recentCalls 环形缓冲容量（面板「最近操作」展示上限）。
const recentCallsCap = 20

// SessionView 单 profile 会话投影（SSE snapshot 帧的 sessions 元素与 sessions 增量帧
// 的 upsert 载荷，同一结构）。
type SessionView struct {
	Profile      string        `json:"profile"`
	Status       SessionStatus `json:"status"`
	Headless     bool          `json:"headless"`
	Error        string        `json:"error,omitempty"`
	Pages        []PageView    `json:"pages,omitempty"`
	LastIssueTag string        `json:"lastIssueTag,omitempty"`
	RecentCalls  []RecentCall  `json:"recentCalls,omitempty"`
}

// BrowserViewSnapshot 全量快照（建连首帧载荷；sessions 按 profile 字典序稳定排列）。
type BrowserViewSnapshot struct {
	Sessions []SessionView `json:"sessions"`
}

// FrameType 帧型全集（D6：snapshot + sessions 两类）。
type FrameType string

const (
	// FrameSnapshot 建连首帧：当前 BrowserViewSnapshot。
	FrameSnapshot FrameType = "snapshot"
	// FrameSessions 增量帧：载荷为 SessionView 列表，按 profile 覆写式 upsert。
	FrameSessions FrameType = "sessions"
)

// Frame SSE 帧信封：seq 全局单调递增（前端检测 gap 即重连取快照），type 判别载荷
// （至多一个非零载荷字段）。JSON 序列化即 wire 形态。
type Frame struct {
	Seq      uint64               `json:"seq"`
	Type     FrameType            `json:"type"`
	Snapshot *BrowserViewSnapshot `json:"snapshot,omitempty"`
	Sessions []SessionView        `json:"sessions,omitempty"`
}

// browserView 全局投影（view 锁守护；锁序 m.mu → entry.mu → view，绝不反向，view 不
// 感知 manager/entry）。写方法在锁内完成置态并返回副本，帧广播归调用方锁外执行。
type browserView struct {
	mu       sync.RWMutex
	sessions map[string]*SessionView
}

func newBrowserView() *browserView {
	return &browserView{sessions: map[string]*SessionView{}}
}

// snapshot 全量快照（hub 建连闭包与 REST getInfo 消费；只碰 view 锁——一个长调用
// 期间面板 SSE 建连不被卡死的前提）。
func (v *browserView) snapshot() *BrowserViewSnapshot {
	v.mu.RLock()
	defer v.mu.RUnlock()
	snap := &BrowserViewSnapshot{Sessions: make([]SessionView, 0, len(v.sessions))}
	for _, row := range v.sessions {
		snap.Sessions = append(snap.Sessions, *row)
	}
	sort.Slice(snap.Sessions, func(i, j int) bool {
		return snap.Sessions[i].Profile < snap.Sessions[j].Profile
	})
	return snap
}

// row 读单行副本（Ensure 返回值；行不存在返回零值行——status 空串，调用方按需处理）。
func (v *browserView) row(profile string) SessionView {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if row, ok := v.sessions[profile]; ok {
		return *row
	}
	return SessionView{Profile: profile}
}

// apply 行级变更：fn 在锁内变更行（行不存在以 idle 零值行起步），返回变更后副本供
// 锁外组装 FrameSessions 帧广播。调用方须持 entry.mu（锁序约定）或独立调用。
func (v *browserView) apply(profile string, fn func(*SessionView)) SessionView {
	v.mu.Lock()
	defer v.mu.Unlock()
	row, ok := v.sessions[profile]
	if !ok {
		row = &SessionView{Profile: profile, Status: StatusIdle}
		v.sessions[profile] = row
	}
	fn(row)
	return *row
}
