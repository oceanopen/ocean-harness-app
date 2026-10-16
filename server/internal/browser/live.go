package browser

/*
 * live.go：per-profile CDP 观察通道（T4.3）——wire 层 JSON-RPC 会话（liveConn）与 viewer 层（拨号自愈 connectLive、target 匹配、keep-latest 帧箱 + screencast 订阅）；生命周期挂接 manager（refs 计数/三处收敛）归 T4.4。
 * 协议基线 = T4.1 实测：帧号字段实名 sessionId（ack 以 {sessionId: 帧号} 回执）、maxFramesInFlight（默认 3）背压窗须逐帧即时 ack、动画页 ~60fps 损伤驱动推帧。
 */

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var (
	/*
	 * liveRetryDelays 拨号自愈退避表：每次失败唤醒一次浏览器后等待重拨（合计 ~6s，
	 * 总预算由调用方 ctx 覆盖）。
	 */
	liveRetryDelays = []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}
	/*
	 * liveDialTimeout 单次拨号预算（T4.1 实测全序 ~0.5s，6× 余量）：僵尸端口「accept
	 * 不响应」会无限挂住拨号，剥夺自愈重试机会——超时即本轮失败进退避重试。
	 */
	liveDialTimeout = 3 * time.Second
	// liveAckTimeout 帧回执单次预算（连接已死时快速失败，不积压帧泵）。
	liveAckTimeout = 5 * time.Second
	// screencast 参数（T4.1 S4 实测口径）。
	screencastFormat    = "jpeg"
	screencastQuality   = 60
	screencastMaxWidth  = 960
	screencastMaxHeight = 600
	screencastEveryNth  = 1
)

/*
 * liveWake 拨号失败时的浏览器唤醒钩子（T4.4 接 Manager.Forward(browser_tabs list)，
 * 复用 Ensure + callMu 语义；busy 拒绝由 connectLive 按「浏览器活着」正交解读）。
 */
type liveWake func(ctx context.Context) error

// ---- wire 层：JSON-RPC 会话胶水（browser 级 ws、id 配对、事件分流）----

type cdpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cdpResp struct {
	Result json.RawMessage `json:"result"`
	Err    *cdpRPCError    `json:"error"`
}

type cdpEvent struct {
	Method    string          `json:"method"`
	SessionID string          `json:"sessionId"`
	Params    json.RawMessage `json:"params"`
}

type cdpWireMsg struct {
	ID        int             `json:"id"`
	Result    json.RawMessage `json:"result"`
	Error     *cdpRPCError    `json:"error"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	SessionID string          `json:"sessionId"`
}

/*
 * liveConn CDP wire 层会话：id 配对命令 + 事件缓冲通道，无策略——ack 节律与订阅生命
 * 周期归 viewer 层。事件缓冲满即丢（screencast ~60fps 下防 reader 卡死优先，mailbox
 * keep-latest 语义天然容忍）。
 */
type liveConn struct {
	conn *websocket.Conn

	mu     sync.Mutex
	nextID int
	pend   map[int]chan cdpResp

	writeMu sync.Mutex // gorilla Conn 写侧单写者纪律
	events  chan cdpEvent
}

// cdpVersion /json/version 响应（browser 级 ws 入口）。
type cdpVersion struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

/*
 * dialLiveConn 单次拨号中段：/json/version 取 browser 级 ws 地址并连接、启动 readLoop。
 * 端口活性裁决归 connectLive 自愈循环（本函数单次成败，不重试）。
 */
func dialLiveConn(ctx context.Context, port int) (*liveConn, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", port), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /json/version: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /json/version 状态 %d", resp.StatusCode)
	}
	var ver cdpVersion
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
		return nil, fmt.Errorf("解析 /json/version: %w", err)
	}
	if ver.WebSocketDebuggerURL == "" {
		return nil, errors.New("/json/version 未返回 webSocketDebuggerUrl")
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, ver.WebSocketDebuggerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ws 拨号: %w", err)
	}
	c := &liveConn{
		conn:   conn,
		pend:   map[int]chan cdpResp{},
		events: make(chan cdpEvent, 512),
	}
	go c.readLoop()
	return c, nil
}

// close 关停连接（readLoop 随之退出：在途调用全数失败、events 关闭）。
func (c *liveConn) close() error { return c.conn.Close() }

func (c *liveConn) readLoop() {
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.mu.Lock()
			for id, ch := range c.pend {
				ch <- cdpResp{Err: &cdpRPCError{Message: "连接关闭: " + err.Error()}}
				delete(c.pend, id)
			}
			c.mu.Unlock()
			close(c.events)
			return
		}
		var m cdpWireMsg
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.ID != 0 {
			c.mu.Lock()
			ch, ok := c.pend[m.ID]
			if ok {
				delete(c.pend, m.ID)
			}
			c.mu.Unlock()
			if ok {
				ch <- cdpResp{Result: m.Result, Err: m.Error}
			}
			continue
		}
		select {
		case c.events <- cdpEvent{Method: m.Method, SessionID: m.SessionID, Params: m.Params}:
		default:
		}
	}
}

/*
 * call 发送命令并等配对响应（写失败/ctx 到期即除名，迟到响应丢弃；sessionID 非空 =
 * flatten 会话内调用）。预算由 ctx 给定。
 */
func (c *liveConn) call(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan cdpResp, 1)
	c.pend[id] = ch
	c.mu.Unlock()

	msg := map[string]any{"id": id, "method": method, "params": params}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	c.writeMu.Lock()
	err := c.conn.WriteJSON(msg)
	c.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pend, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("发送 %s: %w", method, err)
	}
	select {
	case resp := <-ch:
		if resp.Err != nil {
			return nil, fmt.Errorf("%s: code=%d %s", method, resp.Err.Code, resp.Err.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pend, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

// ---- 端口解析与 target 枚举 ----

/*
 * devToolsActivePortFile Chrome 在 user-data-dir 根部落盘的 CDP 锚点（行1 实际端口、
 * 行2 browser ws path）。
 */
const devToolsActivePortFile = "DevToolsActivePort"

/*
 * readDevToolsPort 读取 profile 的 CDP 端口。文件缺失/单行/损坏均报错（浏览器未活的
 * 表象之一）；文件在浏览器退出后残留不删（T4.1 S2），存在不证活性——活性唯一判据是
 * 拨号成败。不缓存端口：每次连接重读（浏览器重开后端口变化）。
 */
func readDevToolsPort(profileDir string) (int, string, error) {
	data, err := os.ReadFile(filepath.Join(profileDir, devToolsActivePortFile))
	if err != nil {
		return 0, "", fmt.Errorf("读取 DevToolsActivePort: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, "", fmt.Errorf("DevToolsActivePort 内容不完整（%d 行）", len(fields))
	}
	port, err := strconv.Atoi(fields[0])
	if err != nil || port <= 0 {
		return 0, "", fmt.Errorf("DevToolsActivePort 端口损坏: %q", fields[0])
	}
	return port, fields[1], nil
}

// cdpTargetInfo /json/list 元素（观察通道与页面观察者共用消费字段；url/title 供 SSE target 事件回显与投影行建行）。
type cdpTargetInfo struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

/*
 * listPageTargets GET /json/list 取全部 page target（findPageTarget 的候选全集；
 * 本调用同时是端口活性的一次 HTTP 预检——残档端口在此失败走自愈）。
 */
func listPageTargets(ctx context.Context, port int) ([]cdpTargetInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/list", port), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /json/list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /json/list 状态 %d", resp.StatusCode)
	}
	var all []cdpTargetInfo
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, fmt.Errorf("解析 /json/list: %w", err)
	}
	targets := make([]cdpTargetInfo, 0, len(all))
	for _, tg := range all {
		if tg.Type == "page" {
			targets = append(targets, tg)
		}
	}
	return targets, nil
}

/*
 * findPageTarget 纯函数：按 targetId 直取 CDP page target（targetId 是稳定锚——导航不
 * 换锚，无 URL 匹配竞态）；未命中显式报错（页面已关闭等，归调用方重解析），实际 url/title
 * 经 SSE target 事件回显供前端消歧。
 */
func findPageTarget(targetID string, targets []cdpTargetInfo) (cdpTargetInfo, error) {
	for _, tg := range targets {
		if tg.ID == targetID {
			return tg, nil
		}
	}
	return cdpTargetInfo{}, fmt.Errorf("页面 target %s 不在浏览器中（可能已关闭），请刷新页面列表后重试", targetID)
}

// ---- viewer 层：帧箱与 screencast 订阅 ----

// LiveFrame 单帧投递物（jpeg 数据 + screencast metadata；T4.5 SSE frame 事件直取）。
type LiveFrame struct {
	Data     []byte
	Metadata LiveFrameMeta
}

/*
 * frameMailbox keep-latest 单槽帧箱：视频流语义——新帧覆盖旧帧，慢订户跳帧不断连；
 * 无 seq 无补帧（断线重连 = captureScreenshot 初始帧重放，免 seq 的合法性来源）。
 * changed 每次 deliver 关闭并换新：多订户全唤醒，等待方 select 直连（ctx 并联自身 Done）。
 */
type frameMailbox struct {
	mu      sync.Mutex
	frame   LiveFrame
	gen     uint64 // 投递代数（订户判新帧用；非 wire seq，不承载补帧语义）
	changed chan struct{}
}

func newFrameMailbox() *frameMailbox {
	return &frameMailbox{changed: make(chan struct{})}
}

// deliver 覆盖投递新帧，返回投递后代数（永不阻塞）。
func (mb *frameMailbox) deliver(f LiveFrame) uint64 {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.frame, mb.gen = f, mb.gen+1
	close(mb.changed)
	mb.changed = make(chan struct{})
	return mb.gen
}

// latest 当前帧与其代数（尚无帧 ok=false）。
func (mb *frameMailbox) latest() (LiveFrame, uint64, bool) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if mb.gen == 0 {
		return LiveFrame{}, 0, false
	}
	return mb.frame, mb.gen, true
}

// changedC 当前变更信号 channel（deliver 关闭换新；select 收到信号后须重取新 channel）。
func (mb *frameMailbox) changedC() <-chan struct{} {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return mb.changed
}

/*
 * screencastFrame Page.screencastFrame 载荷。帧号字段实名 sessionId（Chromium 155 将
 * frameNumber 更名 sessionId，语义仍是帧号；ack 以 {sessionId: 帧号} 回执，T4.1 实测）。
 */
type screencastFrame struct {
	FrameNo  int64         `json:"sessionId"`
	Data     string        `json:"data"`
	Metadata LiveFrameMeta `json:"metadata"`
}

// LiveFrameMeta screencast metadata（T4.1 S4 实测全字段齐备；T4.6 坐标换算入参）。
type LiveFrameMeta struct {
	PageScaleFactor float64 `json:"pageScaleFactor"`
	DeviceWidth     int     `json:"deviceWidth"`
	DeviceHeight    int     `json:"deviceHeight"`
	ScrollOffsetX   float64 `json:"scrollOffsetX"`
	ScrollOffsetY   float64 `json:"scrollOffsetY"`
	OffsetTop       float64 `json:"offsetTop"`
}

// LiveTarget target 变更通知（SSE target 事件载荷：导航回显消歧 / 页面关闭终态）。
type LiveTarget struct {
	URL    string
	Title  string
	Closed bool
}

// LiveEventType 帧流 SSE 事件型全集（T4.5 /api/browserSession/stream，type 判别载荷）。
type LiveEventType string

const (
	LiveEventFrame    LiveEventType = "frame"
	LiveEventTarget   LiveEventType = "target"
	LiveEventError    LiveEventType = "error"
	LiveEventClosed   LiveEventType = "closed"
	LiveEventRejected LiveEventType = "reject"
)

/*
 * LiveEvent 帧流 SSE 事件信封（JSON 序列化即 wire 形态；至多一个非零载荷字段组，口径同投影 Frame）。
 * frame：jpeg base64 + screencast metadata（初始帧 metadata 零值——损伤驱动首帧到达前前端按位图尺寸兜底）；target：closed 置真 = 页面已关闭，随后补发 closed 终态。
 * error：自然断连可重订自愈；closed：终态 reason（管理面强拆/页面关闭），前端停止重订；reject：订阅拒绝终态（载荷真实原因，前端停止重订）。
 */
type LiveEvent struct {
	Type     LiveEventType  `json:"type"`
	Data     string         `json:"data,omitempty"`
	Metadata *LiveFrameMeta `json:"metadata,omitempty"`
	URL      string         `json:"url,omitempty"`
	Title    string         `json:"title,omitempty"`
	Closed   bool           `json:"closed,omitempty"`
	Message  string         `json:"message,omitempty"`
	Reason   string         `json:"reason,omitempty"`
}

/*
 * liveViewer 单个页面 target 的观察会话：wire 连接 + flatten attach + screencast 帧泵。
 * 生命周期归调用方（T4.4 refs 计数挂接），连接断开以 done 广播。
 */
type liveViewer struct {
	conn    *liveConn
	session string
	page    cdpTargetInfo
	log     *zap.Logger

	frames  *frameMailbox
	targets chan LiveTarget
	done    chan struct{}
}

/*
 * dialViewer 单次拨号全序（不重试）：读端口（每次重读，不缓存）→ /json/version →
 * ws 拨号 → target 发现与匹配 → flatten attach → 初始帧 → screencast。任一步失败即
 * 关停连接返回，重试归 connectLive。
 */
func dialViewer(ctx context.Context, profileDir string, page PageView, log *zap.Logger) (*liveViewer, error) {
	port, _, err := readDevToolsPort(profileDir)
	if err != nil {
		return nil, err
	}
	conn, err := dialLiveConn(ctx, port)
	if err != nil {
		return nil, err
	}
	v := &liveViewer{
		conn:    conn,
		log:     log,
		frames:  newFrameMailbox(),
		targets: make(chan LiveTarget, 16),
		done:    make(chan struct{}),
	}
	if err := v.attach(ctx, page, port); err != nil {
		_ = conn.close()
		return nil, err
	}
	/*
	 * 事件泵在 session/page 写定后启动：attach 期间事件由 events 缓冲吸收（screencast
	 * 帧本就在最后一步 startScreencast 后才产生），写读竞争随启动序消除。
	 */
	go v.dispatch()
	return v, nil
}

/*
 * attach 浏览器级 target 发现与页面会话建立：setDiscoverTargets 开启 target 生命周期事件（targetDestroyed/targetInfoChanged 事件面的前提）→ targetId 直取 → flatten attach → captureScreenshot 初始帧（静态页零推帧的不白屏保底；此时 metadata 无源——损伤驱动首帧到达前前端按位图尺寸兜底）→ startScreencast。
 */
func (v *liveViewer) attach(ctx context.Context, page PageView, port int) error {
	if _, err := v.conn.call(ctx, "", "Target.setDiscoverTargets", map[string]any{"discover": true}); err != nil {
		return err
	}
	targets, err := listPageTargets(ctx, port)
	if err != nil {
		return err
	}
	tg, err := findPageTarget(page.TargetID, targets)
	if err != nil {
		return err
	}
	raw, err := v.conn.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": tg.ID, "flatten": true})
	if err != nil {
		return err
	}
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.SessionID == "" {
		return fmt.Errorf("attachToTarget 响应无效: %v", err)
	}
	v.session, v.page = out.SessionID, tg
	if _, err := v.conn.call(ctx, out.SessionID, "Page.enable", map[string]any{}); err != nil {
		return err
	}
	raw, err = v.conn.call(ctx, out.SessionID, "Page.captureScreenshot", map[string]any{
		"format": screencastFormat, "quality": screencastQuality,
	})
	if err != nil {
		return err
	}
	var shot struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &shot); err != nil {
		return fmt.Errorf("解析 captureScreenshot: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return fmt.Errorf("初始帧解码: %w", err)
	}
	v.frames.deliver(LiveFrame{Data: data})
	_, err = v.conn.call(ctx, out.SessionID, "Page.startScreencast", map[string]any{
		"format": screencastFormat, "quality": screencastQuality,
		"maxWidth": screencastMaxWidth, "maxHeight": screencastMaxHeight,
		"everyNthFrame": screencastEveryNth,
	})
	return err
}

/*
 * dispatch 事件泵：screencast 帧 → 帧箱 + 即时 ack（背压窗逐帧回执，T4.1 S4）；target
 * 生命周期 → 低频通知。连接断开即退出并以 done 广播。
 */
func (v *liveViewer) dispatch() {
	defer close(v.done)
	for ev := range v.conn.events {
		switch ev.Method {
		case "Page.screencastFrame":
			if ev.SessionID != v.session {
				continue
			}
			var f screencastFrame
			if err := json.Unmarshal(ev.Params, &f); err != nil {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(f.Data)
			if err != nil {
				continue
			}
			v.frames.deliver(LiveFrame{Data: data, Metadata: f.Metadata})
			ackCtx, cancel := context.WithTimeout(context.Background(), liveAckTimeout)
			_, err = v.conn.call(ackCtx, v.session, "Page.screencastFrameAck", map[string]any{"sessionId": f.FrameNo})
			cancel()
			if err != nil {
				v.log.Debug("screencast ack 失败", zap.Error(err))
			}
		case "Target.targetDestroyed", "Target.targetInfoChanged":
			v.notifyTarget(ev)
		}
	}
}

/*
 * notifyTarget 解析 target 生命周期事件并过滤本页（缓冲满即丢——低频事件，重连自愈
 * 兜底）。
 */
func (v *liveViewer) notifyTarget(ev cdpEvent) {
	var params struct {
		TargetId   string `json:"targetId"`
		TargetInfo struct {
			TargetId string `json:"targetId"`
			URL      string `json:"url"`
			Title    string `json:"title"`
		} `json:"targetInfo"`
	}
	if json.Unmarshal(ev.Params, &params) != nil {
		return
	}
	id := params.TargetId
	if id == "" {
		id = params.TargetInfo.TargetId
	}
	if id != v.page.ID {
		return
	}
	up := LiveTarget{URL: v.page.URL, Title: v.page.Title, Closed: ev.Method == "Target.targetDestroyed"}
	if ev.Method == "Target.targetInfoChanged" {
		// 导航/标题变更回写锚点：target 事件回显与后续通知取实时值。
		v.page.URL, v.page.Title = params.TargetInfo.URL, params.TargetInfo.Title
		up.URL, up.Title = v.page.URL, v.page.Title
	}
	select {
	case v.targets <- up:
	default:
	}
}

/*
 * DispatchInput 输入分发原语（远程桌面语义：不经引擎 MCP、不占 callMu——管理面挂接
 * 归 T4.4；本层只负责在已 attach 会话上发一条 Input 域命令）。
 */
func (v *liveViewer) DispatchInput(ctx context.Context, method string, params any) error {
	_, err := v.conn.call(ctx, v.session, method, params)
	return err
}

/*
 * connectLive 拨号 + 自愈：直拨一次；失败逐轮唤醒浏览器（busy 拒绝 = 浏览器活着的
 * 正交解读，继续重试）→ 按退避表等待 → 重读端口重拨（残档/陈旧端口自愈——活性唯一
 * 判据是拨号成败，T4.1 S2）。成功返回已订阅 screencast 的 viewer。
 */
func connectLive(ctx context.Context, profile string, page PageView, wake liveWake, log *zap.Logger) (*liveViewer, error) {
	if log == nil {
		log = zap.NewNop()
	}
	profileDir, err := ProfileDir(profile)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt <= len(liveRetryDelays); attempt++ {
		if attempt > 0 {
			if wake != nil {
				if werr := wake(ctx); werr != nil && !errors.Is(werr, ErrSessionBusy) {
					log.Warn("CDP 自愈唤醒失败", zap.String("profile", profile), zap.Error(werr))
				}
			}
			select {
			case <-time.After(liveRetryDelays[attempt-1]):
			case <-ctx.Done():
				return nil, fmt.Errorf("CDP 观察通道连接中断: %w", ctx.Err())
			}
		}
		dialCtx, dialCancel := context.WithTimeout(ctx, liveDialTimeout)
		v, err := dialViewer(dialCtx, profileDir, page, log)
		dialCancel()
		if err == nil {
			return v, nil
		}
		lastErr = err
		log.Debug("CDP 拨号失败，自愈重试", zap.String("profile", profile), zap.Int("attempt", attempt+1), zap.Error(err))
	}
	return nil, fmt.Errorf("CDP 观察通道连接失败: %w", lastErr)
}
