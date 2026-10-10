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
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	withTestRoot(t)
	withFakeNode(t, "v20.11.0")
	return NewManager(fakeStaging(t), nil)
}

// withCallTimeouts 覆写 per-call 超时与主层 idle 计时（收尾恢复）。
func withCallTimeouts(t *testing.T, call, idle time.Duration) {
	t.Helper()
	oldCall, oldIdle := forwardCallTimeout, idleReleaseTimeout
	forwardCallTimeout, idleReleaseTimeout = call, idle
	t.Cleanup(func() { forwardCallTimeout, idleReleaseTimeout = oldCall, oldIdle })
}

// fakeEngineCtl 可控假引擎：spawns 拉起计数、active 在途调用计数、tabsCalls 投影刷新
// 计数、toolFn 覆写工具行为（返回文本与 isError 标志）、spawnGate 一次性拉起闸门
// （非 nil 时下一次拉起阻塞至关闭——测 starting 窗口竞态）。
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

// withEngineCtl 注入可控假引擎（navigate/tabs/screenshot/evaluate 四工具，覆盖被测
// 路径）。
func withEngineCtl(t *testing.T) *fakeEngineCtl {
	t.Helper()
	ctl := &fakeEngineCtl{}
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
			EngineToolCookieList,
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
		if _, err := srv.Connect(ctx, serverTransport, nil); err != nil {
			return nil, fmt.Errorf("假引擎建连: %w", err)
		}
		client := mcp.NewClient(&mcp.Implementation{Name: "ocean-harness-browser-test", Version: "test"}, nil)
		return client.Connect(ctx, clientTransport, nil)
	})
	return ctl
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

func TestManagerEnsureReuseAndHeadlessSwitch(t *testing.T) {
	ctl := withEngineCtl(t)
	m := newTestManager(t)

	sv, err := m.Ensure(context.Background(), "default", nil)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if sv.Status != StatusReady || sv.Headless {
		t.Fatalf("sv = %+v, want ready 有头", sv)
	}
	// 同名复用：不重拉。
	if _, err := m.Ensure(context.Background(), "default", nil); err != nil {
		t.Fatalf("二次 Ensure: %v", err)
	}
	if spawns, _, _ := ctl.stats(); spawns != 1 {
		t.Fatalf("二次 Ensure 后 spawns = %d, want 1", spawns)
	}

	// 显式 headless 覆写回存 + close+reopen（D10 切换）。
	sv, err = m.Ensure(context.Background(), "default", boolPtr(true))
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
		if _, err := m.Ensure(context.Background(), fmt.Sprintf("p%d", i), nil); err != nil {
			t.Fatalf("Ensure p%d: %v", i, err)
		}
	}
	_, err := m.Ensure(context.Background(), "p3", nil)
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("err = %v, want 并发上限报错", err)
	}
	// 关闭一个后可再拉。
	if err := m.Close("p0"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := m.Ensure(context.Background(), "p3", nil); err != nil {
		t.Fatalf("释放后再 Ensure: %v", err)
	}
}

func TestManagerForwardPagesAndIssueTag(t *testing.T) {
	ctl := withEngineCtl(t)
	ctl.toolFn = func(tool string, args map[string]any) (string, bool) {
		switch tool {
		case EngineToolNavigate:
			return "navigated: " + fmt.Sprint(args["url"]), false
		case EngineToolTabs:
			return tabsSample, false
		case EngineToolEvaluate:
			return "Element ref not found: stale element", true
		}
		return "ok", false
	}
	m := newTestManager(t)

	res, err := m.Forward(context.Background(), "default", EngineToolNavigate,
		map[string]any{"url": "https://example.com"}, "issue-1")
	if err != nil {
		t.Fatalf("Forward navigate: %v", err)
	}
	if res.IsError || resultText(res) != "navigated: https://example.com" {
		t.Fatalf("navigate 结果 = %+v", res)
	}

	// 页面投影刷新：一次调用一次刷新（tabs 被拉一次）。
	sv := sessionRow(t, m, "default")
	if len(sv.Pages) != 3 || sv.Pages[1].Title != "React • TodoMVC" || !sv.Pages[1].Current {
		t.Fatalf("pages = %+v, want 三页且 current 落在 index 1", sv.Pages)
	}
	if _, _, tabsCalls := ctl.stats(); tabsCalls != 1 {
		t.Fatalf("tabsCalls = %d, want 1", tabsCalls)
	}

	// 截图轮询豁免：screenshot 不再触发 tabs 刷新。
	if _, err := m.Forward(context.Background(), "default", EngineToolTakeScreenshot, nil, ""); err != nil {
		t.Fatalf("Forward screenshot: %v", err)
	}
	if _, _, tabsCalls := ctl.stats(); tabsCalls != 1 {
		t.Fatalf("screenshot 后 tabsCalls = %d, want 1（豁免）", tabsCalls)
	}

	// 引擎 isError 结果原样透传 + stale_ref 分类。
	res, err = m.Forward(context.Background(), "default", EngineToolEvaluate, nil, "issue-2")
	if err != nil {
		t.Fatalf("Forward evaluate(isError): %v", err)
	}
	if !res.IsError {
		t.Fatal("isError 结果应原样透传")
	}

	sv = sessionRow(t, m, "default")
	if sv.LastIssueTag != "issue-2" {
		t.Fatalf("lastIssueTag = %q, want issue-2", sv.LastIssueTag)
	}
	if len(sv.RecentCalls) != 3 {
		t.Fatalf("recentCalls = %d 条, want 3", len(sv.RecentCalls))
	}
	if last := sv.RecentCalls[2]; last.Outcome != CallStaleRef {
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

	for i := 1; i <= consecutiveTimeoutLimit; i++ {
		_, err := m.Forward(context.Background(), "default", EngineToolEvaluate, nil, "")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("第 %d 次调用 err = %v, want 超时", i, err)
		}
	}
	waitForStatus(t, m, "default", StatusFailed)
	if last := sessionRow(t, m, "default").RecentCalls[len(sessionRow(t, m, "default").RecentCalls)-1]; last.Outcome != CallTimeout {
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
	if _, err := m.Ensure(context.Background(), "default", nil); err != nil {
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
	if last := sessionRow(t, m, "default").RecentCalls[len(sessionRow(t, m, "default").RecentCalls)-1]; last.Outcome != CallClosed {
		t.Fatalf("最近一次 outcome = %s, want closed", last.Outcome)
	}
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
		_, err := m.Ensure(context.Background(), "default", nil)
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
	sv, err := m.Ensure(context.Background(), "default", nil)
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
	if _, err := m.Ensure(context.Background(), "default", nil); err == nil {
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
	sv, err := m.Ensure(context.Background(), "default", nil)
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
		_, err := m.Ensure(context.Background(), "default", nil) // 缺省有头
		aDone <- err
	}()
	waitSpawns(t, ctl, 1)

	// B 并发显式 headless=true（join 在途 spawn 或就绪后撞见不一致，均应走重建）。
	bDone := make(chan SessionView, 1)
	go func() {
		sv, err := m.Ensure(context.Background(), "default", boolPtr(true))
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
	if _, err := m.Ensure(context.Background(), "default", nil); err != nil {
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

	// 引擎孤儿被杀组回收（被杀子进程为僵尸态——kill(pid,0) 恒成功，以 Wait4 WNOHANG
	// 探终结）。
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
