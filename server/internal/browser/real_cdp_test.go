package browser

/*
 * CDP 观察通道前置 spike（T4.1，本地项）：实测引擎 Chromium 在 --remote-debugging-port 注入下的四项承重行为，结论回写 docs/task_browser_automation.md 对应任务段——S1 pipe+port 并存（存亡级 gate：双通道共存拉起，MCP 全链与 CDP 独立会话互不干扰）；S2 DevToolsActivePort 时机（写入时机 / port=0 内容形态 / 引擎收敛后是否残留）；S3 多客户端并发（第二 CDP 客户端持续收帧期间引擎操作不劣化，域隔离确认）；S4 screencast 端到端（帧节奏 / ack 背压 / 静态页零帧 / captureScreenshot 初始帧 / metadata）。
 * 运行：OCEAN_BROWSER_REAL_SMOKE=1 go -C server test ./internal/browser -run TestRealCDP -v -timeout 600s。端口注入自 T4.2 起为 pin.go 生产配置常态（engineConfigJSON launchOptions.args），本文件直用生产 LaunchEngine，实测对象即生产链路；CDP 胶水自 T4.3 起复用生产 live.go wire 层（liveConn），S4 末尾另含生产 connectLive 全序端到端。
 * 站点用 www.baidu.com（example.com 在目标网络不可达，T2.1 定稿事实）。
 */

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// realSmokeResources staging 资源源解析（与 real_test.go 内联逻辑同款，spike 族保持自足）。
func realSmokeResources(t *testing.T) string {
	t.Helper()
	srcRoot := os.Getenv("OCEAN_ACP_RESOURCES_DIR")
	if srcRoot == "" {
		srcRoot = filepath.Join("..", "..", "..", "app", "resources", "acp-adapters")
	}
	if _, err := os.Stat(srcRoot); err != nil {
		t.Fatalf("vendored 资源源缺失（T3.1 后缺省取 app/resources/acp-adapters，或经 OCEAN_ACP_RESOURCES_DIR 指定）: %v", err)
	}
	return srcRoot
}

/*
 * launchCDPSpikeEngine 拉起引擎（headless=true，D10 缺省口径；S2 实测的 headless=new
 * 即此形态）。收尾自动 Close + 根目录恢复。
 */
func launchCDPSpikeEngine(t *testing.T, profile string) *EngineHandle {
	t.Helper()
	if os.Getenv("OCEAN_BROWSER_REAL_SMOKE") != "1" {
		t.Skip("CDP 观察通道 spike：需要 OCEAN_BROWSER_REAL_SMOKE=1 与本机 staging")
	}
	srcRoot := realSmokeResources(t)
	withTestRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	h, err := LaunchEngine(ctx, Dirs{Resources: srcRoot, Adapters: t.TempDir()}, profile, true, nil)
	if err != nil {
		t.Fatalf("LaunchEngine: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

// mustEngineCall 引擎 MCP 调用（transport 错误或 isError 即 Fatal），返回拼接文本。
func mustEngineCall(t *testing.T, ctx context.Context, h *EngineHandle, name string, args map[string]any) string {
	t.Helper()
	res, err := h.Session().CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if res.IsError {
		t.Fatalf("CallTool %s 引擎报错: %s", name, text)
	}
	return text
}

func devToolsActivePortPath(profileDir string) string {
	return filepath.Join(profileDir, devToolsActivePortFile)
}

// waitForDevToolsPort 轮询等待 DevToolsActivePort 落盘并解析（两行：实际端口 + browser ws path）。
func waitForDevToolsPort(t *testing.T, profileDir string, budget time.Duration) (port, wsPath string) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for {
		if data, err := os.ReadFile(devToolsActivePortPath(profileDir)); err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) >= 2 && strings.TrimSpace(lines[0]) != "" {
				return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("DevToolsActivePort %s 内未就绪（%s）", budget, profileDir)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

/*
 * chromeTreeCmdlines 按 --user-data-dir 检索该 profile 的浏览器进程树命令行（T1.1 手法：
 * Chrome 树全成员 argv 均含 --user-data-dir；剔除 node 引擎自身进程）。macOS 本地项。
 */
func chromeTreeCmdlines(t *testing.T, profileDir string) []string {
	t.Helper()
	out, err := exec.Command("ps", "-axo", "command=").Output()
	if err != nil {
		t.Fatalf("ps 检索进程树: %v", err)
	}
	var lines []string
	for _, ln := range strings.Split(string(out), "\n") {
		if strings.Contains(ln, "--user-data-dir="+profileDir) && !strings.Contains(ln, "cli.js") {
			lines = append(lines, strings.TrimSpace(ln))
		}
	}
	return lines
}

var snapshotRefRe = regexp.MustCompile(`ref=([^\]\s]+)`)

// firstSnapshotRef 从引擎 snapshot 文本提取首个 ref（spike 探针点击用）。
func firstSnapshotRef(snap string) string {
	m := snapshotRefRe.FindStringSubmatch(snap)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// ---- spike 侧 CDP 助手（T4.3 起复用生产 live.go wire 层 liveConn，胶水双份收敛）----

// dialSpikeCDP 经生产 wire 层拨号（/json/version → ws；预算 15s，收尾关停）。
func dialSpikeCDP(t *testing.T, port string) *liveConn {
	t.Helper()
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("端口解析 %q: %v", port, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	conn, err := dialLiveConn(ctx, n)
	if err != nil {
		t.Fatalf("CDP 拨号: %v", err)
	}
	t.Cleanup(func() { _ = conn.close() })
	return conn
}

// spikeCall 生产 wire 层 call 的测试包装（ctx 超时预算，失败 Fatal）。
func spikeCall(t *testing.T, conn *liveConn, sessionID, method string, params any, timeout time.Duration) json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	raw, err := conn.call(ctx, sessionID, method, params)
	if err != nil {
		t.Fatalf("CDP %s: %v", method, err)
	}
	return raw
}

/*
 * firstPageTarget 取首个 http page target（多页 URL 消歧归生产 matchPageTarget，spike
 * 探针恒取首个 http 页）。
 */
func firstPageTarget(t *testing.T, port string) cdpTargetInfo {
	t.Helper()
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("端口解析 %q: %v", port, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	targets, err := listPageTargets(ctx, n)
	if err != nil {
		t.Fatalf("/json/list: %v", err)
	}
	for _, tg := range targets {
		if strings.HasPrefix(tg.URL, "http") {
			return tg
		}
	}
	t.Fatalf("/json/list 无 http page target（共 %d 个 page target）", len(targets))
	return cdpTargetInfo{}
}

// attachSpikePage flatten attach 到 page target 并 Page.enable，返回 sessionId。
func attachSpikePage(t *testing.T, conn *liveConn, targetID string) string {
	t.Helper()
	var out struct {
		SessionID string `json:"sessionId"`
	}
	raw := spikeCall(t, conn, "", "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true}, 10*time.Second)
	if err := json.Unmarshal(raw, &out); err != nil || out.SessionID == "" {
		t.Fatalf("attachToTarget: %v sid=%q", err, out.SessionID)
	}
	spikeCall(t, conn, out.SessionID, "Page.enable", map[string]any{}, 10*time.Second)
	return out.SessionID
}

// ---- screencast 收帧记录器（逐帧 ack 可切换；帧结构与 metadata 定义见 live.go）----

type frameRecorder struct {
	mu      sync.Mutex
	acking  bool
	session string
	c       *liveConn
	frames  []screencastFrame
	ats     []time.Time
	ackErrs []error // pump 后台 goroutine 不触 testing，失败记账由测试体主动断言
}

// startSpikeScreencast 订阅帧流（参数即生产定稿口径），后台 pump 收帧，默认逐帧 ack。
func startSpikeScreencast(t *testing.T, conn *liveConn, session string) *frameRecorder {
	t.Helper()
	spikeCall(t, conn, session, "Page.startScreencast", map[string]any{
		"format": screencastFormat, "quality": screencastQuality,
		"maxWidth": screencastMaxWidth, "maxHeight": screencastMaxHeight, "everyNthFrame": screencastEveryNth,
	}, 10*time.Second)
	rec := &frameRecorder{session: session, c: conn, acking: true}
	go rec.pump()
	return rec
}

func (r *frameRecorder) setAcking(v bool) {
	r.mu.Lock()
	r.acking = v
	r.mu.Unlock()
}

/*
 * ackErrors ack 失败快照（测试体在关键窗口调用；测试收尾后 pump 的失败只记账不报告，
 * 杜绝「Log in goroutine after test completed」竞态）。
 */
func (r *frameRecorder) ackErrors() []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.ackErrs...)
}

func (r *frameRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.frames)
}

func (r *frameRecorder) firstFrame() (screencastFrame, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.frames) == 0 {
		return screencastFrame{}, false
	}
	return r.frames[0], true
}

func (r *frameRecorder) framesAfter(ts time.Time) []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []time.Time
	for _, at := range r.ats {
		if at.After(ts) {
			out = append(out, at)
		}
	}
	return out
}

func (r *frameRecorder) pump() {
	for ev := range r.c.events {
		if ev.Method != "Page.screencastFrame" || ev.SessionID != r.session {
			continue
		}
		var f screencastFrame
		if err := json.Unmarshal(ev.Params, &f); err != nil {
			continue
		}
		r.mu.Lock()
		ack := r.acking
		r.frames = append(r.frames, f)
		r.ats = append(r.ats, time.Now())
		r.mu.Unlock()
		if ack {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := r.c.call(ctx, r.session, "Page.screencastFrameAck", map[string]any{"sessionId": f.FrameNo})
			cancel()
			if err != nil {
				r.mu.Lock()
				r.ackErrs = append(r.ackErrs, err)
				r.mu.Unlock()
			}
		}
	}
}

/*
 * attachFirstPageCDP S1/S3/S4 同构四步序收敛：等端口落盘 → 生产 wire 层拨号 → 取首个
 * http page target → flatten attach（端口预算统一 30s；返回 target 供 S4 生产观察
 * 通道按 URL 构造匹配页）。
 */
func attachFirstPageCDP(t *testing.T, h *EngineHandle) (*liveConn, string, cdpTargetInfo) {
	t.Helper()
	port, wsPath := waitForDevToolsPort(t, h.ProfileDir, 30*time.Second)
	conn := dialSpikeCDP(t, port)
	tg := firstPageTarget(t, port)
	sid := attachSpikePage(t, conn, tg.ID)
	t.Logf("CDP attach: port=%s path=%s target=%s url=%s sid=%s", port, wsPath, tg.ID, tg.URL, sid)
	return conn, sid, tg
}

// ---- S1 pipe+port 并存（存亡级 gate）----

func TestRealCDPPipePortCoexist(t *testing.T) {
	h := launchCDPSpikeEngine(t, "cdpspike1")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 首个页面级调用拉起浏览器（引擎 MCP 域正常）。
	mustEngineCall(t, ctx, h, "browser_navigate", map[string]any{"url": "https://www.baidu.com"})

	/*
	 * 双 flag 并存：Chrome 树 argv 同时含 --remote-debugging-pipe（playwright 恒需）
	 * 与 --remote-debugging-port=0（config 注入）。
	 */
	cmdlines := chromeTreeCmdlines(t, h.ProfileDir)
	var dual []string
	for _, ln := range cmdlines {
		if strings.Contains(ln, "--remote-debugging-pipe") && strings.Contains(ln, "--remote-debugging-port=0") {
			dual = append(dual, ln)
		}
	}
	if len(dual) == 0 {
		for _, ln := range cmdlines {
			t.Logf("chrome cmdline: %s", ln)
		}
		t.Fatal("S1 gate：未找到同时含 --remote-debugging-pipe 与 --remote-debugging-port=0 的 Chrome 进程")
	}
	t.Logf("S1: 双 flag 并存 Chrome 进程 %d 个", len(dual))

	// 第二 CDP 客户端独立会话：等端口 → 拨号 → flatten attach（成功即证据）。
	attachFirstPageCDP(t, h)

	// MCP 域不受影响：snapshot / evaluate 全链可用。
	snap := mustEngineCall(t, ctx, h, "browser_snapshot", map[string]any{})
	t.Logf("S1: 引擎 snapshot 正常（%d 字节）", len(snap))
	title := mustEngineCall(t, ctx, h, "browser_evaluate", map[string]any{"function": "() => document.title"})
	t.Logf("S1: 引擎 evaluate 回读 title=%q", title)
}

// ---- S2 DevToolsActivePort 时机 ----

func TestRealCDPDevToolsPortTiming(t *testing.T) {
	h := launchCDPSpikeEngine(t, "cdpspike2")

	_, statErr := os.Stat(devToolsActivePortPath(h.ProfileDir))
	t.Logf("S2: LaunchEngine 返回时刻文件存在=%v（浏览器惰性拉起，预期 false）", statErr == nil)

	launchReturnedAt := time.Now()
	var appearedMu sync.Mutex
	var appearedAt time.Time
	// stop 为失败路径退出信号（成功路径文件出现即自退）；defer 收口防 Fatal 时泄漏。
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			if _, err := os.Stat(devToolsActivePortPath(h.ProfileDir)); err == nil {
				appearedMu.Lock()
				if appearedAt.IsZero() {
					appearedAt = time.Now()
				}
				appearedMu.Unlock()
				return
			}
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	navStart := time.Now()
	mustEngineCall(t, ctx, h, "browser_navigate", map[string]any{"url": "https://www.baidu.com"})
	navDur := time.Since(navStart)

	deadline := time.Now().Add(30 * time.Second)
	for {
		appearedMu.Lock()
		shown := !appearedAt.IsZero()
		appearedMu.Unlock()
		if shown {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("S2: 导航后 30s 内 DevToolsActivePort 未出现")
		}
		time.Sleep(20 * time.Millisecond)
	}
	appearedMu.Lock()
	appearRel := appearedAt.Sub(launchReturnedAt)
	appearedMu.Unlock()
	t.Logf("S2: 首次导航耗时 %s；文件出现于 LaunchEngine 返回后 %s（锚点为握手完成返回，非进程 spawn 时刻；随浏览器启动即写）", navDur, appearRel.Round(time.Millisecond))

	port, wsPath := waitForDevToolsPort(t, h.ProfileDir, 10*time.Second)
	if n, err := strconv.Atoi(port); err != nil || n <= 0 {
		t.Fatalf("S2: port=0 未解析为实际端口（port=%q path=%q）", port, wsPath)
	}
	t.Logf("S2: port=0 → 实际端口 %s，path=%s（headless=new 形态，本测试即无头）", port, wsPath)

	// 退出即删实测：引擎收敛后文件是否残留（结论决定 live.go readDevToolsPort 是否需容忍残档）。
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-h.Exited():
	case <-time.After(15 * time.Second):
		t.Fatal("Close 后 15s 引擎未收敛")
	}
	// 等 Chrome 树全退再判定残档（Chrome 收敛需数秒，过早 stat 会误判残留）。
	chromeGoneDeadline := time.Now().Add(15 * time.Second)
	for len(chromeTreeCmdlines(t, h.ProfileDir)) > 0 {
		if time.Now().After(chromeGoneDeadline) {
			t.Fatal("S2: Close 后 15s Chrome 树未全退")
		}
		time.Sleep(200 * time.Millisecond)
	}
	_, residualErr := os.Stat(devToolsActivePortPath(h.ProfileDir))
	t.Logf("S2: Chrome 树全退后文件存在=%v（true=残档需容忍，false=退出即删）", residualErr == nil)
}

// ---- S3 多客户端并发（域隔离确认）----

func TestRealCDPConcurrentClients(t *testing.T) {
	h := launchCDPSpikeEngine(t, "cdpspike3")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 基准三操作：navigate → snapshot（取 ref）→ click，记录耗时。
	bench := func(label string) {
		t0 := time.Now()
		mustEngineCall(t, ctx, h, "browser_navigate", map[string]any{"url": "https://www.baidu.com"})
		navDur := time.Since(t0)

		t1 := time.Now()
		snap := mustEngineCall(t, ctx, h, "browser_snapshot", map[string]any{})
		snapDur := time.Since(t1)
		ref := firstSnapshotRef(snap)

		clickDur := time.Duration(0)
		if ref != "" {
			t2 := time.Now()
			// 0.0.83 schema：element 为人读描述（可选），定位参数是必填 target（快照 ref 值）。
			mustEngineCall(t, ctx, h, "browser_click", map[string]any{"element": "spike 探针点击", "target": ref})
			clickDur = time.Since(t2)
		}
		t.Logf("S3 %s: navigate=%s snapshot=%s(%dB ref=%s) click=%s", label, navDur.Round(time.Millisecond), snapDur.Round(time.Millisecond), len(snap), ref, clickDur.Round(time.Millisecond))
	}
	bench("基准（无第二客户端）")

	c, sid, _ := attachFirstPageCDP(t, h)
	rec := startSpikeScreencast(t, c, sid)
	time.Sleep(500 * time.Millisecond)

	bench("并发（第二客户端持续收帧中）")
	frames := rec.count()
	t.Logf("S3: 并发期间第二客户端收帧 %d（引擎结果全对 + 耗时对比见上 = 域隔离证据）", frames)
	if frames == 0 {
		t.Fatal("S3: 并发期间第二客户端未收到任何帧（screencast 未生效，对照失效）")
	}
	if errs := rec.ackErrors(); len(errs) > 0 {
		t.Fatalf("S3: 并发期间 ack 失败 %d 次（首个: %v）", len(errs), errs[0])
	}
}

// ---- S4 screencast 端到端 ----

func TestRealCDPScreencast(t *testing.T) {
	h := launchCDPSpikeEngine(t, "cdpspike4")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	mustEngineCall(t, ctx, h, "browser_navigate", map[string]any{"url": "https://www.baidu.com"})
	c, sid, tg := attachFirstPageCDP(t, h)

	// captureScreenshot 初始帧（T4.3「断线重连初始帧重放」的可行性来源）。
	var shotOut struct {
		Data string `json:"data"`
	}
	raw := spikeCall(t, c, sid, "Page.captureScreenshot", map[string]any{"format": "jpeg", "quality": 60}, 15*time.Second)
	if err := json.Unmarshal(raw, &shotOut); err != nil {
		t.Fatalf("解析 captureScreenshot: %v", err)
	}
	jpeg, err := base64.StdEncoding.DecodeString(shotOut.Data)
	if err != nil || len(jpeg) == 0 {
		t.Fatalf("S4: captureScreenshot 初始帧无效: %v len=%d", err, len(jpeg))
	}
	t.Logf("S4: captureScreenshot 初始帧 %dB jpeg", len(jpeg))

	// 真实页面初始帧行为：startScreencast 是否主动推帧（决定重连是否必须 captureScreenshot 兜底）。
	rec := startSpikeScreencast(t, c, sid)
	time.Sleep(4 * time.Second)
	t.Logf("S4: 百度首页订阅后 4s 收帧 %d", rec.count())

	// 帧节奏：navigate 触发重绘，记录帧到达间隔。
	navAt := time.Now()
	mustEngineCall(t, ctx, h, "browser_navigate", map[string]any{"url": "https://www.baidu.com/s?wd=playwright"})
	time.Sleep(5 * time.Second)
	arrivals := rec.framesAfter(navAt)
	intervals := make([]int, 0, len(arrivals))
	for i := 1; i < len(arrivals); i++ {
		intervals = append(intervals, int(arrivals[i].Sub(arrivals[i-1]).Milliseconds()))
	}
	t.Logf("S4: 导航窗口收帧 %d，帧间隔(ms)=%v", len(arrivals), intervals)

	/*
	 * 真静态页（about:blank）零帧——真实页面常带持续动画致连续出帧（百度 /s 实测
	 * ~60fps 连续帧流），零帧口径只能在无动画页面测。
	 */
	mustEngineCall(t, ctx, h, "browser_navigate", map[string]any{"url": "about:blank"})
	time.Sleep(1500 * time.Millisecond) // 导航初始帧排空
	n := rec.count()
	time.Sleep(5 * time.Second)
	grew := rec.count() - n
	if grew > 10 {
		t.Fatalf("S4: about:blank 5s 新增 %d 帧（持续帧流，零帧结论不成立）", grew)
	}
	t.Logf("S4: about:blank 5s 新增 %d 帧（0 = 静态页零帧成立）", grew)

	// 损伤驱动推帧：evaluate 重绘应至少推一帧（帧流活性承重假设）。
	mustEngineCall(t, ctx, h, "browser_evaluate", map[string]any{"function": `() => { document.body.style.backgroundColor = '#336699' }`})
	time.Sleep(2 * time.Second)
	if painted := rec.count() - n - grew; painted == 0 {
		t.Fatal("S4: evaluate 重绘后无新帧推送（损伤驱动不成立）")
	} else {
		t.Logf("S4: evaluate 重绘后 2s 新增 %d 帧（损伤驱动成立）", painted)
	}

	// ack 背压：停止 ack 后导航触发损伤，帧应在少量后停住（Chrome 未 ack 上限）。
	if errs := rec.ackErrors(); len(errs) > 0 {
		t.Fatalf("S4: ack 窗口失败 %d 次（首个: %v）", len(errs), errs[0])
	}
	rec.setAcking(false)
	before := rec.count()
	mustEngineCall(t, ctx, h, "browser_navigate", map[string]any{"url": "https://www.baidu.com"})
	time.Sleep(3 * time.Second)
	mid := rec.count() - before
	time.Sleep(5 * time.Second)
	total := rec.count() - before
	if total-mid > 2 {
		t.Fatalf("S4: 不 ack 持续出帧（背压不成立）：前 3s=%d 帧，后 5s 再增 %d 帧", mid, total-mid)
	}
	t.Logf("S4: 不 ack 窗口：前 3s 收 %d 帧，后 5s 再增 %d 帧（停住 = 背压流控生效）", mid, total-mid)

	// metadata 字段（前端坐标换算的承重输入）。
	f, ok := rec.firstFrame()
	if !ok {
		t.Fatal("S4: 未收到任何帧")
	}
	if f.Metadata.DeviceWidth <= 0 || f.Metadata.DeviceHeight <= 0 {
		t.Fatalf("S4: metadata 尺寸异常: %+v", f.Metadata)
	}
	t.Logf("S4: metadata: device=%dx%d pageScaleFactor=%.2f scrollOffset=(%.0f,%.0f) offsetTop=%.0f 帧号=%d",
		f.Metadata.DeviceWidth, f.Metadata.DeviceHeight, f.Metadata.PageScaleFactor, f.Metadata.ScrollOffsetX, f.Metadata.ScrollOffsetY, f.Metadata.OffsetTop, f.FrameNo)

	/*
	 * T4.3 生产观察通道端到端：connectLive 全序——重读端口、独立连接（S3 已证多客户端域隔离，不受上面背压窗口影响）、setDiscoverTargets、URL 匹配、captureScreenshot 初始帧、帧泵经 mailbox 投递并即时 ack；页按已探明 target 的 URL 构造（等同投影同步态）。
	 */
	lv, err := connectLive(ctx, "cdpspike4", PageView{Index: 0, Title: tg.URL, URL: tg.URL}, nil, nil)
	if err != nil {
		t.Fatalf("S4: connectLive 生产观察通道: %v", err)
	}
	t.Cleanup(func() { _ = lv.conn.close() })
	_, initGen, ok := lv.frames.latest()
	if !ok || initGen != 1 {
		t.Fatalf("S4: 观察通道初始帧缺失（gen=%d ok=%v）", initGen, ok)
	}
	t.Logf("S4: 观察通道初始帧就绪（captureScreenshot 兜底）")
	evalAt := time.Now()
	mustEngineCall(t, ctx, h, "browser_evaluate", map[string]any{"function": `() => { document.body.style.outline = '2px solid #0af' }`})
	cur := initGen
	deadline := time.Now().Add(5 * time.Second)
	for cur <= initGen {
		if time.Now().After(deadline) {
			t.Fatal("S4: 观察通道 5s 内未收到损伤帧（帧泵/ack 链路失效）")
		}
		select {
		case <-lv.frames.changedC():
		case <-time.After(time.Second):
		}
		_, cur, _ = lv.frames.latest()
	}
	t.Logf("S4: 观察通道损伤帧经 mailbox 到达（gen %d → %d，%s 内）", initGen, cur, time.Since(evalAt).Round(time.Millisecond))
}
