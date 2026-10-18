package browser

import (
	"sort"
	"sync"
)

/*
 * pagehistory.go：per-profile per-target 页面状态与操作流水存储（页面维度调试视图的
 * SSOT）。观察者只上报事件，行状态归本层；页面关闭即出列（列表恒等当前真实 tab 集），
 * 同 URL 重建由新 target 行顶替旧行。
 */

// pageActionsCap 单页操作流水上限（环形淘汰最旧）。
const pageActionsCap = 30

// pageHistory 单页面状态与操作流水（时间戳均为 epoch 毫秒）。
type pageHistory struct {
	targetID     string
	url          string
	title        string
	visible      bool
	order        int64 // 首见序（开页排列判据）
	lastActive   int64 // 最近活动序（「当前」回退判据）
	openedAtMs   int64 // 打开时刻
	lastActiveMs int64 // 最近活动时刻
	actions      []RecentCall
}

// pageHistoryStore 页面历史注册表：profile → targetID → 历史。
type pageHistoryStore struct {
	mu      sync.Mutex
	seq     int64
	entries map[string]map[string]*pageHistory
}

func newPageHistoryStore() *pageHistoryStore {
	return &pageHistoryStore{entries: map[string]map[string]*pageHistory{}}
}

/*
 * observe 目标发现/信息变更：新建记开页时刻（返回是否新建，供 attach 决策），
 * 已见刷新 url/title/可见态/活动序与时刻。
 */
func (s *pageHistoryStore) observe(profile, targetID, url, title string, visible bool, atMs int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	pages, ok := s.entries[profile]
	if !ok {
		pages = map[string]*pageHistory{}
		s.entries[profile] = pages
	}
	s.seq++
	h, ok := pages[targetID]
	if !ok {
		// 同 URL 旧行（targetId 已换代）先行出列，防同页多行。
		for id, old := range pages {
			if old.url == url {
				delete(pages, id)
			}
		}
		// 新开 tab 即浏览器激活语义：其余在开页可见态置否，保证激活页唯一。
		for _, old := range pages {
			old.visible = false
		}
		pages[targetID] = &pageHistory{targetID: targetID, url: url, title: title, visible: visible, order: s.seq, openedAtMs: atMs, lastActive: s.seq, lastActiveMs: atMs}
		return true
	}
	h.url, h.title, h.visible = url, title, visible
	h.lastActive, h.lastActiveMs = s.seq, atMs
	return false
}

// bump 仅推进活动序与时刻（targetInfoChanged 的轻量路径）。
func (s *pageHistoryStore) bump(profile, targetID string, atMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h, ok := s.entries[profile][targetID]; ok {
		s.seq++
		h.lastActiveMs = atMs
	}
}

// close 页面关闭即出列（操作流水随行移除，列表恒等当前真实 tab 集）。
func (s *pageHistoryStore) close(profile, targetID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries[profile], targetID)
}

/*
 * reconcile 对账清理：以浏览器实际存活 target 集为准，删除不在册的陈旧行——覆盖
 * 观察者断连期间漏掉销毁事件的窗口，保证重连后列表与真实 tab 集一致。
 */
func (s *pageHistoryStore) reconcile(profile string, liveIDs map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pages, ok := s.entries[profile]
	if !ok {
		return
	}
	for id := range pages {
		if !liveIDs[id] {
			delete(pages, id)
		}
	}
}

// closeAll 全量移除（会话释放；重建后由观察者重建全量行）。
func (s *pageHistoryStore) closeAll(profile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, profile)
}

// record 追加一条操作流水（环形 cap 兜底）并刷新活动时刻；targetID 空 = 无法归因，丢弃。
func (s *pageHistoryStore) record(profile, targetID string, call RecentCall, atMs int64) {
	if targetID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pages, ok := s.entries[profile]
	if !ok {
		pages = map[string]*pageHistory{}
		s.entries[profile] = pages
	}
	h, ok := pages[targetID]
	if !ok {
		h = &pageHistory{targetID: targetID, order: s.seq, openedAtMs: atMs, lastActive: s.seq, lastActiveMs: atMs}
		pages[targetID] = h
	}
	s.seq++
	h.lastActive, h.lastActiveMs = s.seq, atMs
	h.actions = append(h.actions, call)
	if len(h.actions) > pageActionsCap {
		h.actions = h.actions[len(h.actions)-pageActionsCap:]
	}
}

/*
 * rows 投影行集（仅存活页）：按首见序排列；Current 判据 = 唯一可见页，无唯一可见时
 * 回退最近活动序。
 */
func (s *pageHistoryStore) rows(profile string) []PageView {
	s.mu.Lock()
	defer s.mu.Unlock()
	pages, ok := s.entries[profile]
	if !ok {
		return nil
	}
	live := make([]*pageHistory, 0, len(pages))
	for _, h := range pages {
		live = append(live, h)
	}
	sort.Slice(live, func(i, j int) bool { return live[i].order < live[j].order })
	current := ""
	visible := 0
	for _, h := range live {
		if h.visible {
			visible++
			current = h.targetID
		}
	}
	if visible != 1 && len(live) > 0 {
		last := live[0]
		for _, h := range live[1:] {
			if h.lastActive > last.lastActive {
				last = h
			}
		}
		current = last.targetID
	}
	rows := make([]PageView, 0, len(live))
	for _, h := range live {
		if !isUserPageURL(h.url, h.title) {
			continue // 出口兜底：任何路径写入的内部/空 target 不进投影
		}
		actions := make([]RecentCall, len(h.actions))
		copy(actions, h.actions)
		rows = append(rows, PageView{
			Index:          len(rows),
			TargetID:       h.targetID,
			Title:          h.title,
			URL:            h.url,
			Current:        h.targetID == current,
			OpenedAtMs:     h.openedAtMs,
			LastActiveAtMs: h.lastActiveMs,
			Actions:        actions,
		})
	}
	return rows
}

// currentTargetID 当前激活页 targetId（rows 同判据，供操作流水归因；无页返回空串）。
func (s *pageHistoryStore) currentTargetID(profile string) string {
	for _, p := range s.rows(profile) {
		if p.Current {
			return p.TargetID
		}
	}
	return ""
}
