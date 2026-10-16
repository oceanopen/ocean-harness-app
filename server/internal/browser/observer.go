package browser

/*
 * observer.go：per-profile browser 级 CDP 页面观察者——target 生命周期与 visibility 探测
 * 事件驱动的 Pages 投影 SSOT（Forward 后拉取式刷新的替代）。会话 ready 启动，随三处收敛
 * 点（Close/无头切换/引擎退出）停机。
 */

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

/*
 * observerBindingName 页面侧 visibility 上报 binding（window.<name> 调用）：载荷无意义，
 * 事件即触发服务端重探测，取值以 evaluate 为准——页面脚本任意调用至多引发一次重探测。
 */
const observerBindingName = "__oceanVisReport"

// observerStepTimeout 观察者单步（attach/探测/拨号）预算。
const observerStepTimeout = 3 * time.Second

/*
 * observerVisScript 注入页面的 visibility 上报脚本：visibilitychange 即回报（新文档经
 * addScriptToEvaluateOnNewDocument 重挂，SPA 导航监听存续）；当前文档初始态归 attach 时
 * 的显式 evaluate 探测。
 */
const observerVisScript = `(() => { const r = () => window.` + observerBindingName + `(1); document.addEventListener('visibilitychange', r); })()`

// obsPage 单 page target 的观察行。
type obsPage struct {
	targetID string
	url      string
	title    string
	visible  bool
	seen     int64 // 首见序（Index 编号判据）
	touch    int64 // 最近活动序（「当前」无唯一可见信号时的回退判据）
}

/*
 * pageObserver 页面观察者：browser 级连接 + 全 page target attach 探测。mu 守护 pages/
 * sessions；事件泵为唯一写者（attach/探测亦在泵内执行——初始化在泵启动前完成）。
 */
type pageObserver struct {
	m       *Manager
	profile string
	conn    *liveConn
	log     *zap.Logger

	mu       sync.Mutex
	pages    map[string]*obsPage
	sessions map[string]string // CDP flatten sessionId → targetId
	seq      int64             // seen/touch 共用单调序
}

/*
 * newPageObserver 初始化观察者：读端口 → 拨号 → 开 target 发现 → /json/list 全量建行 →
 * 逐页 attach + visibility 探测。单页 attach 失败仅降级（该页暂无可见性信号），不阻断整体。
 */
func newPageObserver(ctx context.Context, m *Manager, profile string) (*pageObserver, error) {
	profileDir, err := ProfileDir(profile)
	if err != nil {
		return nil, err
	}
	port, _, err := readDevToolsPort(profileDir)
	if err != nil {
		return nil, err
	}
	conn, err := dialLiveConn(ctx, port)
	if err != nil {
		return nil, err
	}
	o := &pageObserver{
		m: m, profile: profile, conn: conn, log: m.log,
		pages: map[string]*obsPage{}, sessions: map[string]string{},
	}
	if _, err := conn.call(ctx, "", "Target.setDiscoverTargets", map[string]any{"discover": true}); err != nil {
		_ = conn.close()
		return nil, err
	}
	targets, err := listPageTargets(ctx, port)
	if err != nil {
		_ = conn.close()
		return nil, err
	}
	for _, tg := range targets {
		o.record(tg)
		o.attachProbe(tg.ID)
	}
	o.publish()
	return o, nil
}

// record 登记 target 行（首见建行取序，已见更新 url/title），返回是否新建。
func (o *pageObserver) record(tg cdpTargetInfo) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if p, ok := o.pages[tg.ID]; ok {
		p.url, p.title = tg.URL, tg.Title
		return false
	}
	o.seq++
	o.pages[tg.ID] = &obsPage{targetID: tg.ID, url: tg.URL, title: tg.Title, seen: o.seq, touch: o.seq}
	return true
}

// touch 活动序推进（「当前」回退判据）。
func (o *pageObserver) touch(targetID string) {
	o.mu.Lock()
	if p, ok := o.pages[targetID]; ok {
		o.seq++
		p.touch = o.seq
	}
	o.mu.Unlock()
}

/*
 * attachProbe 单页 attach + visibility 探测：flatten attach → Runtime binding 与新文档
 * 脚本注册 → 初始 visibilityState 探测（当前文档初始态；binding 事件仅触发重探测）。
 */
func (o *pageObserver) attachProbe(targetID string) {
	ctx, cancel := context.WithTimeout(context.Background(), observerStepTimeout)
	defer cancel()
	raw, err := o.conn.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true})
	if err != nil {
		o.log.Debug("观察者 attach 失败", zap.String("profile", o.profile), zap.String("targetId", targetID), zap.Error(err))
		return
	}
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(raw, &out) != nil || out.SessionID == "" {
		return
	}
	if _, err := o.conn.call(ctx, out.SessionID, "Runtime.enable", map[string]any{}); err != nil {
		return
	}
	if _, err := o.conn.call(ctx, out.SessionID, "Runtime.addBinding", map[string]any{"name": observerBindingName}); err != nil {
		return
	}
	_, _ = o.conn.call(ctx, out.SessionID, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": observerVisScript})
	o.mu.Lock()
	o.sessions[out.SessionID] = targetID
	o.mu.Unlock()
	o.probe(out.SessionID, ctx)
}

/*
 * probe 重探测单页实际状态（visibilityState/url/title 一次取全）并回写观察行；探测失败
 * 静默（观察行保持原值，后续事件再自愈）。
 */
func (o *pageObserver) probe(sessionID string, ctx context.Context) {
	raw, err := o.conn.call(ctx, sessionID, "Runtime.evaluate", map[string]any{
		"expression":    "JSON.stringify({v:document.visibilityState,u:location.href,t:document.title})",
		"returnByValue": true,
	})
	if err != nil {
		return
	}
	var out struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return
	}
	var st struct {
		V string `json:"v"`
		U string `json:"u"`
		T string `json:"t"`
	}
	if json.Unmarshal([]byte(out.Result.Value), &st) != nil {
		return
	}
	o.mu.Lock()
	targetID, ok := o.sessions[sessionID]
	if ok {
		if p, ok := o.pages[targetID]; ok {
			p.visible = st.V == "visible"
			if st.U != "" {
				p.url = st.U
			}
			if st.T != "" {
				p.title = st.T
			}
			o.seq++
			p.touch = o.seq
		}
	}
	o.mu.Unlock()
	o.publish()
}

// run 事件泵：target 生命周期与 visibility 上报驱动观察行更新；连接关闭即返回。
func (o *pageObserver) run() {
	for ev := range o.conn.events {
		switch ev.Method {
		case "Target.targetCreated", "Target.targetInfoChanged":
			o.onTargetInfo(ev)
		case "Target.targetDestroyed":
			o.onTargetDestroyed(ev)
		case "Runtime.bindingCalled":
			o.onBindingCalled(ev)
		}
	}
}

// onTargetInfo target 创建/信息变更：建行或更新（type=page 过滤），创建即 attach 探测。
func (o *pageObserver) onTargetInfo(ev cdpEvent) {
	var params struct {
		TargetInfo struct {
			TargetId string `json:"targetId"`
			Type     string `json:"type"`
			URL      string `json:"url"`
			Title    string `json:"title"`
		} `json:"targetInfo"`
	}
	if json.Unmarshal(ev.Params, &params) != nil || params.TargetInfo.TargetId == "" || params.TargetInfo.Type != "page" {
		return
	}
	created := o.record(cdpTargetInfo{ID: params.TargetInfo.TargetId, URL: params.TargetInfo.URL, Title: params.TargetInfo.Title})
	if created {
		o.attachProbe(params.TargetInfo.TargetId)
	} else {
		o.touch(params.TargetInfo.TargetId)
	}
	o.publish()
}

// onTargetDestroyed target 销毁：删行删会话映射。
func (o *pageObserver) onTargetDestroyed(ev cdpEvent) {
	var params struct {
		TargetId string `json:"targetId"`
	}
	if json.Unmarshal(ev.Params, &params) != nil || params.TargetId == "" {
		return
	}
	o.mu.Lock()
	delete(o.pages, params.TargetId)
	for sid, tid := range o.sessions {
		if tid == params.TargetId {
			delete(o.sessions, sid)
		}
	}
	o.mu.Unlock()
	o.publish()
}

// onBindingCalled visibility 上报：会话归属过滤后重探测该页（取值以 evaluate 为准）。
func (o *pageObserver) onBindingCalled(ev cdpEvent) {
	o.mu.Lock()
	_, ok := o.sessions[ev.SessionID]
	o.mu.Unlock()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), observerStepTimeout)
	defer cancel()
	o.probe(ev.SessionID, ctx)
}

/*
 * publish 重算 Pages 投影并广播：行序按首见序编号；「当前」= 唯一可见页（visibility
 * 探测），无唯一可见（无头全可见/窗口最小化全隐藏）回退最近活动页。
 */
func (o *pageObserver) publish() {
	o.mu.Lock()
	pages := make([]obsPage, 0, len(o.pages))
	for _, p := range o.pages {
		pages = append(pages, *p)
	}
	o.mu.Unlock()
	sort.Slice(pages, func(i, j int) bool { return pages[i].seen < pages[j].seen })

	current := ""
	visible := 0
	for _, p := range pages {
		if p.visible {
			visible++
			current = p.targetID
		}
	}
	if visible != 1 && len(pages) > 0 {
		last := pages[0]
		for _, p := range pages[1:] {
			if p.touch > last.touch {
				last = p
			}
		}
		current = last.targetID
	}

	rows := make([]PageView, 0, len(pages))
	for i, p := range pages {
		rows = append(rows, PageView{
			Index: i, TargetID: p.targetID, Title: p.title, URL: p.url,
			Current: p.targetID == current,
		})
	}
	o.m.applyPages(o.profile, rows)
}

// startObserver 启动页面观察者（会话 ready 后调用；已在跑或未启用则跳过），不等待结果。
func (m *Manager) startObserver(profile string) {
	m.observerMu.Lock()
	defer m.observerMu.Unlock()
	if !m.observersEnabled {
		return
	}
	if _, ok := m.observers[profile]; ok {
		return
	}
	go m.runObserver(profile)
}

/*
 * runObserver 观察者运行循环：初始化失败/意外断连按退避表重拉（浏览器刚就绪时 DevTools
 * 可能未及受理），连续失败放弃（投影暂不更新，会话重建时自动重试）；每轮重拉前校验会话
 * 仍需要观察者（防 stop 后复活——重建竞态下与新会话观察者双跑）。
 */
func (m *Manager) runObserver(profile string) {
	fails := 0
	for {
		if !m.observerWanted(profile) {
			return
		}
		if fails > len(liveRetryDelays) {
			m.log.Warn("浏览器页面观察者启动失败，页面投影暂停更新（会话重建时自动重试）", zap.String("profile", profile))
			return
		}
		if fails > 0 {
			time.Sleep(liveRetryDelays[fails-1])
		}
		ctx, cancel := context.WithTimeout(context.Background(), observerStepTimeout)
		o, err := newPageObserver(ctx, m, profile)
		cancel()
		if err != nil {
			fails++
			m.log.Debug("浏览器页面观察者连接失败", zap.String("profile", profile), zap.Int("attempt", fails), zap.Error(err))
			continue
		}
		/*
		 * 注册前复查：会话已不再需要（Close/重建竞态）或已有在册观察者（重试中的旧
		 * goroutine 与新会话观察者并存——重读端口拨号会指向新浏览器）即丢弃本连接退出，
		 * 防双重发布与覆盖注册。
		 */
		if !m.observerWanted(profile) {
			_ = o.conn.close()
			return
		}
		m.observerMu.Lock()
		_, exists := m.observers[profile]
		if !exists {
			m.observers[profile] = o
		}
		m.observerMu.Unlock()
		if exists {
			_ = o.conn.close()
			return
		}
		o.run() // 阻塞至连接关闭（stopObserver 或断连）
		m.observerMu.Lock()
		_, alive := m.observers[profile]
		delete(m.observers, profile)
		m.observerMu.Unlock()
		if !alive {
			return
		}
		fails = 1 // 意外断连：按失败口径退避重拉
	}
}

// observerWanted 会话是否仍需要观察者（ready 且引擎在册）。
func (m *Manager) observerWanted(profile string) bool {
	m.mu.Lock()
	entry, ok := m.entries[profile]
	m.mu.Unlock()
	if !ok {
		return false
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.state == StatusReady && entry.handle != nil
}

// stopObserver 停止页面观察者（会话关闭/重建路径；未在跑为 no-op）。
func (m *Manager) stopObserver(profile string) {
	m.observerMu.Lock()
	o, ok := m.observers[profile]
	delete(m.observers, profile)
	m.observerMu.Unlock()
	if ok {
		_ = o.conn.close()
	}
}
