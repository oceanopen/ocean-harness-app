package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"ocean-harness/server/internal/acp"
)

// newTestManager 测试装配：临时根目录 + 假 node + 伪 staging（EnsureVendored 链可走通）。
// 页面观察者默认关断（无真实 CDP 面；观察者专项测试显式开启）。
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	withTestRoot(t)
	withFakeNode(t, "v20.11.0")
	m := NewManager(fakeStaging(t), nil)
	m.observersEnabled = false
	return m
}

// withCallTimeouts 覆写 per-call 超时与主层 idle 计时（收尾恢复）。
func withCallTimeouts(t *testing.T, call, idle time.Duration) {
	t.Helper()
	oldCall, oldIdle := forwardCallTimeout, idleReleaseTimeout
	forwardCallTimeout, idleReleaseTimeout = call, idle
	t.Cleanup(func() { forwardCallTimeout, idleReleaseTimeout = oldCall, oldIdle })
}

/*
 * fakeEngineCtl 可控假引擎：spawns 拉起计数、active 在途调用计数、tabsCalls browser_tabs
 * 调用计数、toolFn 覆写工具行为（返回文本与 isError 标志）、spawnGate 一次性拉起闸门
 * （非 nil 时下一次拉起阻塞至关闭——测 starting 窗口竞态）。
 */
type fakeEngineCtl struct {
	mu        sync.Mutex
	spawns    int
	active    int
	tabsCalls int
	toolFn    func(tool string, args map[string]any) (string, bool)
	spawnGate chan struct{}
}

func (c *fakeEngineCtl) call(tool string, args map[string]any) (string, bool) {
	c.mu.Lock()
	c.active++
	if tool == EngineToolTabs {
		c.tabsCalls++
	}
	fn := c.toolFn
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.active--
		c.mu.Unlock()
	}()
	if fn == nil {
		return "ok: " + tool, false
	}
	return fn(tool, args)
}

func (c *fakeEngineCtl) stats() (spawns, active, tabsCalls int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.spawns, c.active, c.tabsCalls
}

/*
 * withEngineCtl 注入可控假引擎（navigate/tabs/screenshot/evaluate 四工具，覆盖被测
 * 路径）。
 */
func withEngineCtl(t *testing.T) *fakeEngineCtl {
	t.Helper()
	ctl := &fakeEngineCtl{}
	withEngineCtlDial(t, ctl, nil)
	return ctl
}

/*
 * withEngineCtlDial 可控假引擎的公共核心：注入 dialEngine 桩（in-memory transport 假
 * 引擎 + ctl 记账），onSpawn 非 nil 时在每次服务端会话建立后回调（测试捕获 kill 句柄
 * 等扩展用）。
 */
func withEngineCtlDial(t *testing.T, ctl *fakeEngineCtl, onSpawn func(ss *mcp.ServerSession)) {
	t.Helper()
	withDialFunc(t, func(ctx context.Context, _ *exec.Cmd) (*mcp.ClientSession, error) {
		ctl.mu.Lock()
		ctl.spawns++
		gate := ctl.spawnGate
		ctl.spawnGate = nil // 一次性闸门
		ctl.mu.Unlock()
		if gate != nil {
			<-gate
		}
		serverTransport, clientTransport := mcp.NewInMemoryTransports()
		srv := mcp.NewServer(&mcp.Implementation{Name: "fake-engine", Version: "test"}, nil)
		for _, name := range []string{
			EngineToolNavigate, EngineToolTabs, EngineToolTakeScreenshot, EngineToolEvaluate,
			EngineToolCookieList, EngineToolStorageState, EngineToolSetStorageState,
		} {
			tool := name
			mcp.AddTool(srv, &mcp.Tool{Name: tool, Description: "fake"},
				func(ctx context.Context, req *mcp.CallToolRequest, in map[string]any) (*mcp.CallToolResult, struct{}, error) {
					text, isErr := ctl.call(tool, in)
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: text}},
						IsError: isErr,
					}, struct{}{}, nil
				})
		}
		ss, err := srv.Connect(ctx, serverTransport, nil)
		if err != nil {
			return nil, fmt.Errorf("假引擎建连: %w", err)
		}
		if onSpawn != nil {
			onSpawn(ss)
		}
		client := mcp.NewClient(&mcp.Implementation{Name: "ocean-harness-browser-test", Version: "test"}, nil)
		return client.Connect(ctx, clientTransport, nil)
	})
}

// waitActive 等在途调用数达标（假引擎 handler 已进入）。
func waitActive(t *testing.T, ctl *fakeEngineCtl, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, active, _ := ctl.stats(); active >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("5s 内在途调用未达 %d", n)
}

// waitForStatus 等会话投影状态达标。
func waitForStatus(t *testing.T, m *Manager, profile string, status SessionStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, sv := range m.Snapshot().Sessions {
			if sv.Profile == profile && sv.Status == status {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("5s 内 %s 状态未达 %s", profile, status)
}

// sessionRow 取单 profile 投影行（不存在时 fatal）。
func sessionRow(t *testing.T, m *Manager, profile string) SessionView {
	t.Helper()
	for _, sv := range m.Snapshot().Sessions {
		if sv.Profile == profile {
			return sv
		}
	}
	t.Fatalf("投影无 %s 会话行", profile)
	return SessionView{}
}

// lastPageAction 取单 profile 最末条页面操作流水（不存在时 fatal）。
func lastPageAction(t *testing.T, m *Manager, profile string) RecentCall {
	t.Helper()
	rows := m.pageHist.rows(profile)
	if len(rows) == 0 {
		t.Fatalf("profile %s 无页面操作流水", profile)
	}
	actions := rows[len(rows)-1].Actions
	if len(actions) == 0 {
		t.Fatalf("profile %s 末页无操作流水", profile)
	}
	return actions[len(actions)-1]
}

// seedPage 预置一页（无观察者路径下的归因锚点）。
func seedPage(m *Manager, profile, targetID string) {
	m.pageHist.observe(profile, targetID, "https://seed.test/", "seed", false, time.Now().UnixMilli())
}

func TestManagerEnsureReuseAndHeadlessSwitch(t *testing.T) {
	ctl := withEngineCtl(t)
	m := newTestManager(t)

	sv, err := m.Ensure(context.Background(), "default", boolPtr(false), "") // 显式有头（新建 profile 缺省无头后，显式传参保持本用例的有头起点）
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if sv.Status != StatusReady || sv.Headless {
		t.Fatalf("sv = %+v, want ready 有头", sv)
	}
	// 同名复用：不重拉。
	if _, err := m.Ensure(context.Background(), "default", nil, ""); err != nil {
		t.Fatalf("二次 Ensure: %v", err)
	}
	if spawns, _, _ := ctl.stats(); spawns != 1 {
		t.Fatalf("二次 Ensure 后 spawns = %d, want 1", spawns)
	}

	// 显式 headless 覆写回存 + close+reopen（D10 切换）。
	sv, err = m.Ensure(context.Background(), "default", boolPtr(true), "")
	if err != nil {
		t.Fatalf("headless 切换 Ensure: %v", err)
	}
	if !sv.Headless || sv.Status != StatusReady {
		t.Fatalf("切换后 sv = %+v, want ready headless", sv)
	}
	if spawns, _, _ := ctl.stats(); spawns != 2 {
		t.Fatalf("切换后 spawns = %d, want 2（close+reopen）", spawns)
	}
	// 偏好已回存：prefs.json headless=true。
	prefs, err := ReadProfilePrefs(mustProfileDir(t, "default"))
	if err != nil || !prefs.Headless {
		t.Fatalf("prefs = %+v err = %v, want headless 已回存", prefs, err)
	}
}

func TestManagerConcurrentProfilesCap(t *testing.T) {
	withEngineCtl(t)
	m := newTestManager(t)

	for i := 0; i < maxConcurrentProfiles; i++ {
		if _, err := m.Ensure(context.Background(), fmt.Sprintf("p%d", i), nil, ""); err != nil {
			t.Fatalf("Ensure p%d: %v", i, err)
		}
	}
	next := fmt.Sprintf("p%d", maxConcurrentProfiles)
	_, err := m.Ensure(context.Background(), next, nil, "")
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("err = %v, want 并发上限报错", err)
	}
	// 关闭一个后可再拉。
	if err := m.Close("p0"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := m.Ensure(context.Background(), next, nil, ""); err != nil {
		t.Fatalf("释放后再 Ensure: %v", err)
	}
}

func TestManagerPerTaskSessionsCap(t *testing.T) {
	withEngineCtl(t)
	m := newTestManager(t)
	const id = "0197c0a2-7b6a-7333-8b1a-2f4e1d6c9a01"

	slotName := func(slot int) string {
		t.Helper()
		name, err := FormatIssueProfile(id, slot)
		if err != nil {
			t.Fatalf("FormatIssueProfile(%d): %v", slot, err)
		}
		return name
	}

	// 5 槽位并发拉起全部成功，任务活跃计数随之到位。
	for slot := 1; slot <= MaxSessionsPerTask; slot++ {
		if _, err := m.Ensure(context.Background(), slotName(slot), nil, ""); err != nil {
			t.Fatalf("槽位 %d Ensure: %v", slot, err)
		}
	}
	m.mu.Lock()
	n := m.taskActiveCount(nil, id)
	m.mu.Unlock()
	if n != MaxSessionsPerTask {
		t.Fatalf("taskActiveCount = %d, want %d", n, MaxSessionsPerTask)
	}

	// 越界槽位名与目标任务同配额（issueQuotaBase 归组）：第 6 个拒绝。
	sixth := "issue-" + id + "-6"
	if _, err := m.Ensure(context.Background(), sixth, nil, ""); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("err = %v, want 任务并发上限报错", err)
	}

	// 释放一个槽位腾出配额后可再拉。
	if err := m.Close(slotName(2)); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := m.Ensure(context.Background(), sixth, nil, ""); err != nil {
		t.Fatalf("释放后再 Ensure: %v", err)
	}
}

func TestManagerForwardIssueTag(t *testing.T) {
	ctl := withEngineCtl(t)
	ctl.toolFn = func(tool string, args map[string]any) (string, bool) {
		switch tool {
		case EngineToolNavigate:
			return "navigated: " + fmt.Sprint(args["url"]), false
		case EngineToolEvaluate:
			return "Element ref not found: stale element", true
		}
		return "ok", false
	}
	m := newTestManager(t)
	seedPage(m, "default", "t0")

	res, err := m.Forward(context.Background(), "default", EngineToolNavigate,
		map[string]any{"url": "https://example.com"}, "issue-1")
	if err != nil {
		t.Fatalf("Forward navigate: %v", err)
	}
	if res.IsError || ResultText(res) != "navigated: https://example.com" {
		t.Fatalf("navigate 结果 = %+v", res)
	}

	// 页面投影刷新已退役（SSOT 归页面观察者）：Forward 不再拉 tabs。
	if _, _, tabsCalls := ctl.stats(); tabsCalls != 0 {
		t.Fatalf("tabsCalls = %d, want 0", tabsCalls)
	}

	// 引擎 isError 结果原样透传 + stale_ref 分类。
	res, err = m.Forward(context.Background(), "default", EngineToolEvaluate, nil, "issue-2")
	if err != nil {
		t.Fatalf("Forward evaluate(isError): %v", err)
	}
	if !res.IsError {
		t.Fatal("isError 结果应原样透传")
	}

	if actions := m.pageHist.rows("default")[0].Actions; len(actions) != 2 {
		t.Fatalf("页面操作流水 = %d 条, want 2", len(actions))
	}
	if last := lastPageAction(t, m, "default"); last.IssueTag != "issue-2" {
		t.Fatalf("归因 issueTag = %q, want issue-2", last.IssueTag)
	}
	if last := lastPageAction(t, m, "default"); last.Outcome != CallStaleRef {
		t.Fatalf("最近一次 outcome = %s, want %s", last.Outcome, CallStaleRef)
	}
}

func TestManagerForwardBusy(t *testing.T) {
	ctl := withEngineCtl(t)
	release := make(chan struct{})
	ctl.toolFn = func(tool string, args map[string]any) (string, bool) {
		if tool == EngineToolEvaluate {
			<-release
		}
		return "done", false
	}
	m := newTestManager(t)

	done := make(chan error, 1)
	go func() {
		_, err := m.Forward(context.Background(), "default", EngineToolEvaluate, nil, "")
		done <- err
	}()
	waitActive(t, ctl, 1)

	_, err := m.Forward(context.Background(), "default", EngineToolNavigate,
		map[string]any{"url": "https://example.com"}, "")
	if !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("err = %v, want ErrSessionBusy", err)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("首个调用应正常完成: %v", err)
	}
}

func TestManagerForwardTimeoutRebuild(t *testing.T) {
	withCallTimeouts(t, 100*time.Millisecond, time.Hour)
	ctl := withEngineCtl(t)
	var hang atomic.Bool
	hang.Store(true)
	ctl.toolFn = func(tool string, args map[string]any) (string, bool) {
		if tool == EngineToolEvaluate && hang.Load() {
			time.Sleep(500 * time.Millisecond)
		}
		return "done", false
	}
	m := newTestManager(t)
	seedPage(m, "default", "t0")

	for i := 1; i <= consecutiveTimeoutLimit; i++ {
		_, err := m.Forward(context.Background(), "default", EngineToolEvaluate, nil, "")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("第 %d 次调用 err = %v, want 超时", i, err)
		}
	}
	waitForStatus(t, m, "default", StatusFailed)
	if last := lastPageAction(t, m, "default"); last.Outcome != CallTimeout {
		t.Fatalf("最近一次 outcome = %s, want timeout", last.Outcome)
	}

	// 下次调用自动重建（新引擎不挂）。
	hang.Store(false)
	res, err := m.Forward(context.Background(), "default", EngineToolEvaluate, nil, "")
	if err != nil {
		t.Fatalf("重建后 Forward: %v", err)
	}
	if res.IsError {
		t.Fatal("重建后调用应成功")
	}
	if spawns, _, _ := ctl.stats(); spawns != 2 {
		t.Fatalf("spawns = %d, want 2（failed 自动重建）", spawns)
	}
}

func TestManagerIdleReleaseAndRepull(t *testing.T) {
	withCallTimeouts(t, time.Hour, 100*time.Millisecond)
	ctl := withEngineCtl(t)
	m := newTestManager(t)

	if _, err := m.Forward(context.Background(), "default", EngineToolNavigate,
		map[string]any{"url": "https://example.com"}, ""); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	// idle 计时到期 → 自动释放（D4 主层）。
	waitForStatus(t, m, "default", StatusIdle)

	// 释放后再次调用自动重拉。
	if _, err := m.Forward(context.Background(), "default", EngineToolNavigate,
		map[string]any{"url": "https://example.com"}, ""); err != nil {
		t.Fatalf("重拉 Forward: %v", err)
	}
	if spawns, _, _ := ctl.stats(); spawns != 2 {
		t.Fatalf("spawns = %d, want 2（释放后重拉）", spawns)
	}
}

/*
 * TestManagerCloseCapturesPages close 收尾捕获真实页面投影：ensure 后零 Forward（模拟
 * 只在浏览器窗口内手动导航——pages 投影为空），close 时引擎还活着，收尾前捕获的 tab
 * 列表即真实当前页（无头切换/重开恢复现场的数据源）。
 */
func TestManagerCloseCapturesPages(t *testing.T) {
	ctl := withEngineCtl(t)
	m := newTestManager(t)
	ctl.mu.Lock()
	ctl.toolFn = func(tool string, args map[string]any) (string, bool) {
		if tool == EngineToolTabs {
			return "- 0: (current) [Bing](https://www.bing.com/)\n", false
		}
		return "ok: " + tool, false
	}
	ctl.mu.Unlock()

	if _, err := m.Ensure(context.Background(), "default", nil, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if row := sessionRow(t, m, "default"); len(row.Pages) != 0 {
		t.Fatalf("ensure 后 pages 应为空投影: %+v", row.Pages)
	}

	if err := m.Close("default"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	row := sessionRow(t, m, "default")
	if row.Status != StatusIdle {
		t.Fatalf("close 后状态 = %s, want idle", row.Status)
	}
	restored := m.entryOf("default").restorePages
	if len(restored) != 1 || restored[0].URL != "https://www.bing.com/" || !restored[0].Current {
		t.Fatalf("close 未捕获恢复现场: %+v", restored)
	}
	if _, _, tabsCalls := ctl.stats(); tabsCalls == 0 {
		t.Fatalf("close 收尾未发起 tab 列表捕获")
	}
	// Row 导出与投影行同源（close 响应携带的数据源）。
	if row2 := m.Row("default"); len(row2.Pages) != 0 || row2.Profile != "default" {
		t.Fatalf("Row 导出行异常: %+v", row2)
	}
}

func TestManagerCloseRace(t *testing.T) {
	ctl := withEngineCtl(t)
	release := make(chan struct{})
	ctl.toolFn = func(tool string, args map[string]any) (string, bool) {
		if tool == EngineToolEvaluate {
			<-release
		}
		return "done", false
	}
	m := newTestManager(t)
	seedPage(m, "default", "t0")
	if _, err := m.Ensure(context.Background(), "default", nil, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := m.Forward(context.Background(), "default", EngineToolEvaluate, nil, "")
		done <- err
	}()
	waitActive(t, ctl, 1)

	// 关闭期间在途调用得到显式错误而非悬挂。
	if err := m.Close("default"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(release)
	if err := <-done; !errors.Is(err, errSessionClosed) {
		t.Fatalf("在途调用 err = %v, want errSessionClosed", err)
	}
	waitForStatus(t, m, "default", StatusIdle)
}

func TestManagerCloseDuringStartingThenEnsure(t *testing.T) {
	ctl := withEngineCtl(t)
	gate := make(chan struct{})
	ctl.mu.Lock()
	ctl.spawnGate = gate
	ctl.mu.Unlock()
	m := newTestManager(t)

	done := make(chan error, 1)
	go func() {
		_, err := m.Ensure(context.Background(), "default", nil, "")
		done <- err
	}()
	waitSpawns(t, ctl, 1)

	// starting 窗口受理 Close：closing 置位，spawn 终结时消费复位。
	if err := m.Close("default"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(gate)
	if err := <-done; err == nil {
		t.Fatal("starting 期被 Close 的 Ensure 应得到显式错误")
	}
	waitForStatus(t, m, "default", StatusIdle)

	// closing 已复位：再 Ensure 正常重拉（不报「正在关闭」）。
	sv, err := m.Ensure(context.Background(), "default", nil, "")
	if err != nil {
		t.Fatalf("Close 后再 Ensure: %v", err)
	}
	if sv.Status != StatusReady {
		t.Fatalf("sv = %+v, want ready", sv)
	}
	if spawns, _, _ := ctl.stats(); spawns != 2 {
		t.Fatalf("spawns = %d, want 2", spawns)
	}
}

func TestManagerCloseFailedThenEnsure(t *testing.T) {
	withTestRoot(t)
	withFakeNode(t, "v20.11.0")
	// 第一段：dial 失败 → failed（activeCount 已在迁移点结清）。
	withDialFunc(t, func(ctx context.Context, _ *exec.Cmd) (*mcp.ClientSession, error) {
		return nil, fmt.Errorf("boom")
	})
	m := NewManager(fakeStaging(t), nil)
	if _, err := m.Ensure(context.Background(), "default", nil, ""); err == nil {
		t.Fatal("want 拉起失败报错")
	}
	waitForStatus(t, m, "default", StatusFailed)

	// failed 态 Close：不得再递减 activeCount（下穿会使并发上限闸漂移）。
	if err := m.Close("default"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	m.mu.Lock()
	count := m.activeCount
	m.mu.Unlock()
	if count != 0 {
		t.Fatalf("activeCount = %d, want 0", count)
	}

	// 恢复可用引擎 → Close 后重拉成功。
	withEngineCtl(t)
	sv, err := m.Ensure(context.Background(), "default", nil, "")
	if err != nil || sv.Status != StatusReady {
		t.Fatalf("Close 后再 Ensure: sv=%+v err=%v", sv, err)
	}
}

func TestManagerEnsureJoinHeadlessMismatch(t *testing.T) {
	ctl := withEngineCtl(t)
	gate := make(chan struct{})
	ctl.mu.Lock()
	ctl.spawnGate = gate
	ctl.mu.Unlock()
	m := newTestManager(t)

	aDone := make(chan error, 1)
	go func() {
		_, err := m.Ensure(context.Background(), "default", boolPtr(false), "") // 显式有头
		aDone <- err
	}()
	waitSpawns(t, ctl, 1)

	// B 并发显式 headless=true（join 在途 spawn 或就绪后撞见不一致，均应走重建）。
	bDone := make(chan SessionView, 1)
	go func() {
		sv, err := m.Ensure(context.Background(), "default", boolPtr(true), "")
		if err != nil {
			t.Errorf("B Ensure: %v", err)
		}
		bDone <- sv
	}()
	time.Sleep(50 * time.Millisecond) // 等 B 进入 join/评估窗口
	close(gate)

	if err := <-aDone; err != nil {
		t.Fatalf("A Ensure: %v", err)
	}
	sv := <-bDone
	if sv.Status != StatusReady || !sv.Headless {
		t.Fatalf("B sv = %+v, want ready headless（join 后不一致应重建）", sv)
	}
	if spawns, _, _ := ctl.stats(); spawns != 2 {
		t.Fatalf("spawns = %d, want 2（headless 切换 close+reopen）", spawns)
	}
}

// waitSpawns 等拉起计数达标。
func waitSpawns(t *testing.T, ctl *fakeEngineCtl, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if spawns, _, _ := ctl.stats(); spawns >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("5s 内拉起计数未达 %d", n)
}

func TestManagerSubscribeFrames(t *testing.T) {
	withEngineCtl(t)
	m := newTestManager(t)

	frames, cancel := m.Subscribe()
	defer cancel()

	// 建连首帧 = 全量 snapshot。
	f := readFrame(t, frames)
	if f.Type != FrameSnapshot || f.Snapshot == nil {
		t.Fatalf("首帧 = %+v, want snapshot", f)
	}

	// Ensure 产生 starting → ready 两帧 sessions。
	if _, err := m.Ensure(context.Background(), "default", nil, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	for _, wantStatus := range []SessionStatus{StatusStarting, StatusReady} {
		f := readFrame(t, frames)
		if f.Type != FrameSessions || len(f.Sessions) != 1 || f.Sessions[0].Status != wantStatus {
			t.Fatalf("帧 = %+v, want sessions %s", f.Sessions, wantStatus)
		}
	}
}

func TestRecoverOrphans(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix 侧孤儿回收实测（Windows 走 tasklist 弱校验，待实机）")
	}
	withTestRoot(t)
	withFakeNode(t, "v20.11.0")
	dirs := fakeStaging(t)

	version, err := EngineVersion()
	if err != nil {
		t.Fatalf("EngineVersion: %v", err)
	}
	// 造「引擎」孤儿：脚本路径落在 vendored 安装目录锚点下（cmdline 校验命中形态）。
	binDir := filepath.Join(dirs.Adapters, "playwright-mcp", version, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("建锚点目录: %v", err)
	}
	script := filepath.Join(binDir, "orphan-sleeper.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatalf("造孤儿脚本: %v", err)
	}
	cmd := exec.Command(script)
	cmd.SysProcAttr = acp.ProcessGroupAttr()
	if err := cmd.Start(); err != nil {
		t.Fatalf("拉起孤儿: %v", err)
	}
	t.Cleanup(func() { _ = acp.KillProcessGroup(cmd) })

	root := Root()
	orphanPidFile := filepath.Join(root, "profiles", "default", pidFileName)
	if err := os.MkdirAll(filepath.Dir(orphanPidFile), 0o755); err != nil {
		t.Fatalf("建 profile 目录: %v", err)
	}
	if err := os.WriteFile(orphanPidFile, []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0o644); err != nil {
		t.Fatalf("写 pid 锚点: %v", err)
	}
	// PID 易主形态：锚点指向本测试进程（cmdline 不含安装目录锚点）。
	selfPidFile := filepath.Join(root, "profiles", "other", pidFileName)
	if err := os.MkdirAll(filepath.Dir(selfPidFile), 0o755); err != nil {
		t.Fatalf("建 profile 目录: %v", err)
	}
	if err := os.WriteFile(selfPidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0o644); err != nil {
		t.Fatalf("写 pid 锚点: %v", err)
	}

	NewManager(dirs, nil)

	/*
	 * 引擎孤儿被杀组回收（被杀子进程为僵尸态——kill(pid,0) 恒成功，以 Wait4 WNOHANG
	 * 探终结）。
	 */
	deadline := time.Now().Add(5 * time.Second)
	for {
		var ws syscall.WaitStatus
		wpid, err := syscall.Wait4(cmd.Process.Pid, &ws, syscall.WNOHANG, nil)
		if err == nil && wpid == cmd.Process.Pid {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("5s 内孤儿进程未被回收")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// 两个 pid 锚点均被清除（杀掉的清、校验不过的也清）。
	for _, f := range []string{orphanPidFile, selfPidFile} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("pid 文件 %s 未清除: %v", f, err)
		}
	}
}

// boolPtr 便捷取 *bool。
func boolPtr(b bool) *bool { return &b }

// mustProfileDir 推导 profile 目录（测试内必成功）。
func mustProfileDir(t *testing.T, profile string) string {
	t.Helper()
	dir, err := ProfileDir(profile)
	if err != nil {
		t.Fatalf("ProfileDir: %v", err)
	}
	return dir
}

func TestManagerListTools(t *testing.T) {
	ctl := withEngineCtl(t)
	m := newTestManager(t)
	seedPage(m, "default", "t0")

	tools, err := m.ListTools(context.Background(), "default", "issue-1")
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
	}
	for _, want := range []string{
		EngineToolNavigate, EngineToolTabs, EngineToolTakeScreenshot, EngineToolEvaluate, EngineToolCookieList,
	} {
		if !names[want] {
			t.Fatalf("tools 缺 %s: %v", want, names)
		}
	}
	// 只读纪律：tools/list 不触发页面投影刷新（ensure 自身也不刷）。
	if _, _, tabsCalls := ctl.stats(); tabsCalls != 0 {
		t.Fatalf("tabsCalls = %d, want 0（tools/list 免刷新）", tabsCalls)
	}
	// 操作流水：browser_tool_list 名义 + ok 分类 + issueTag 归因。
	if last := lastPageAction(t, m, "default"); last.IssueTag != "issue-1" || last.Tool != listToolsCallName || last.Outcome != CallOK {
		t.Fatalf("操作流水末条 = %+v, want %s/%s 归因 issue-1", last, listToolsCallName, CallOK)
	}
}

func TestManagerListToolsBusy(t *testing.T) {
	ctl := withEngineCtl(t)
	release := make(chan struct{})
	ctl.toolFn = func(tool string, args map[string]any) (string, bool) {
		if tool == EngineToolEvaluate {
			<-release
		}
		return "done", false
	}
	m := newTestManager(t)
	seedPage(m, "default", "t0")

	done := make(chan error, 1)
	go func() {
		_, err := m.Forward(context.Background(), "default", EngineToolEvaluate, nil, "")
		done <- err
	}()
	waitActive(t, ctl, 1)

	// 调用互斥：Forward 在途时 tools/list 同样即拒（busy）。
	if _, err := m.ListTools(context.Background(), "default", ""); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("err = %v, want ErrSessionBusy", err)
	}
	// busy 拒绝不记流水（与 Forward 同口径）。
	if rows := m.pageHist.rows("default"); len(rows) > 0 && len(rows[0].Actions) > 0 {
		t.Fatalf("页面操作流水 = %+v, want 空（busy 拒绝不记）", rows[0].Actions)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("首个调用应正常完成: %v", err)
	}
}
