package browser

/*
 * observer.go：per-profile browser 级 CDP 页面观察者——target 生命周期与 visibility 探测
 * 事件驱动页面投影（SSOT 归 pageHistoryStore）。会话 ready 启动，随三处收敛点
 * （Close/无头切换/引擎退出）停机。
 */

import (
	"context"
	"encoding/json"
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

// observerIdleRetry 退避耗尽后的慢速轮询间隔（自愈等待，非失败重试）。
const observerIdleRetry = 5 * time.Second

/*
 * observerVisScript 注入页面的 visibility 上报脚本：visibilitychange 即回报（新文档经
 * addScriptToEvaluateOnNewDocument 重挂，SPA 导航监听存续）；当前文档初始态归 attach 时
 * 的显式 evaluate 探测。
 */
const observerVisScript = `(() => { const r = () => window.` + observerBindingName + `(1); document.addEventListener('visibilitychange', r); })()`

/*
 * pageObserver 页面观察者：browser 级连接 + 全 page target attach 探测。sessions 守护
 * CDP 会话路由；页面状态一律落 pageHistoryStore（跨观察者重建留存）。
 */
type pageObserver struct {
	m       *Manager
	profile string
	conn    *liveConn
	log     *zap.Logger

	mu       sync.Mutex
	sessions map[string]string // CDP flatten sessionId → targetId
}

/*
 * newPageObserver 初始化观察者：读端口 → 拨号 → 开 target 发现 → 全量建行 →
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
	o := &pageObserver{m: m, profile: profile, conn: conn, log: m.log, sessions: map[string]string{}}
	if _, err := conn.call(ctx, "", "Target.setDiscoverTargets", map[string]any{"discover": true}); err != nil {
		_ = conn.close()
		return nil, err
	}
	targets, err := listPageTargets(ctx, port)
	if err != nil {
		_ = conn.close()
		return nil, err
	}
	liveIDs := make(map[string]bool, len(targets))
	for _, tg := range targets {
		liveIDs[tg.ID] = true
	}
	// 先对账再建行：清掉断连期间遗留的陈旧条目，列表恒等真实 tab 集。
	o.m.pageHist.reconcile(o.profile, liveIDs)
	for _, tg := range targets {
		o.record(tg)
		o.attachProbe(tg.ID)
	}
	o.m.publishPages(o.profile)
	return o, nil
}

// record 登记 target 行（新建返回 true，供 attach 决策）。
func (o *pageObserver) record(tg cdpTargetInfo) bool {
	created := o.m.pageHist.observe(o.profile, tg.ID, tg.URL, tg.Title, false, time.Now().UnixMilli())
	return created
}

// touch 页面活动序推进（「当前」回退判据）。
func (o *pageObserver) touch(targetID string) {
	o.m.pageHist.bump(o.profile, targetID, time.Now().UnixMilli())
}

/*
 * attachProbe 单页 attach + visibility 探测：flatten attach → Runtime binding 与新文档
 * 脚本注册 → 初始 visibilityState 探测（当前文档初始态；binding 事件仅触发重探测）。
 */
func (o *pageObserver) attachProbe(targetID string) {
	ctx, cancel := context.WithTimeout(context.Background(), observerStepTimeout)
	defer cancel()
	raw, err := o.conn.call(ctx, "", "Target.getTargets", map[string]any{"targetIds": []string{targetID}})
	if err == nil {
		var out struct {
			TargetInfos []cdpTargetInfo `json:"targetInfos"`
		}
		if json.Unmarshal(raw, &out) == nil && len(out.TargetInfos) > 0 {
			tg := out.TargetInfos[0]
			o.m.pageHist.observe(o.profile, tg.ID, tg.URL, tg.Title, false, time.Now().UnixMilli())
		}
	}
	raw, err = o.conn.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true})
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
 * probe 重探测单页实际状态（visibilityState/url/title 一次取全）并回写存储；探测失败
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
	o.mu.Unlock()
	if !ok {
		return
	}
	o.m.pageHist.observe(o.profile, targetID, st.U, st.T, st.V == "visible", time.Now().UnixMilli())
	o.m.publishPages(o.profile)
}

// run 事件泵：target 生命周期与 visibility 上报驱动页面投影更新；连接关闭即退出。
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

// onTargetInfo target 创建/信息变更：建档或刷新（type=page 过滤），创建即 attach 探测。
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
	if !isUserPageURL(params.TargetInfo.URL, params.TargetInfo.Title) {
		return
	}
	info := params.TargetInfo
	created := o.m.pageHist.observe(o.profile, info.TargetId, info.URL, info.Title, false, time.Now().UnixMilli())
	if created {
		o.attachProbe(info.TargetId)
	}
	o.m.publishPages(o.profile)
}

// onTargetDestroyed target 销毁：标记关闭留档（历史随行保留供排障），删会话映射。
func (o *pageObserver) onTargetDestroyed(ev cdpEvent) {
	var params struct {
		TargetId string `json:"targetId"`
	}
	if json.Unmarshal(ev.Params, &params) != nil || params.TargetId == "" {
		return
	}
	o.mu.Lock()
	for sid, tid := range o.sessions {
		if tid == params.TargetId {
			delete(o.sessions, sid)
		}
	}
	o.mu.Unlock()
	o.m.pageHist.close(o.profile, params.TargetId)
	o.m.publishPages(o.profile)
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
 * runObserver 观察者运行循环：初始失败按退避表重拉（浏览器刚就绪时 DevTools 可能未及
 * 受理）；退避耗尽转慢速轮询自愈（页面投影不自弃——浏览器稍后就绪即恢复），仅进入慢速
 * 轮询时落一条 WARN 留痕；每轮重拉前校验会话仍需要观察者（防 stop 后复活——重建竞态下
 * 与新会话观察者双跑）。
 */
func (m *Manager) runObserver(profile string) {
	fails := 0
	for {
		if !m.observerWanted(profile) {
			return
		}
		if fails == len(liveRetryDelays)+1 {
			m.log.Warn("浏览器页面观察者连接暂未就绪，转慢速轮询等待（页面投影暂停更新）", zap.String("profile", profile))
		}
		if fails > 0 {
			delay := observerIdleRetry
			if fails <= len(liveRetryDelays) {
				delay = liveRetryDelays[fails-1]
			}
			time.Sleep(delay)
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
