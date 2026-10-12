package browser

// manager_live.go 单测（T4.4）：refs 计数与槽复用/重建、idle 保活闸（观看期暂停、末订
// 恢复计时并释放）、三处收敛点强拆（Close/headless 切换/引擎意外退出）、页码解析自愈
// 与输入面。CDP 面复用 live_test.go 的 cdpTestServer 桩件，引擎面复用 withEngineCtl。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// liveTestEnv 观察通道单测环境：假 CDP 服务器 + 端口文件 + 种子页面投影。
type liveTestEnv struct {
	m      *Manager
	ts     *cdpTestServer
	ctl    *fakeEngineCtl
	killEv func() // 非 nil：可杀假引擎（watchExit 用例）
}

func newLiveTestEnv(t *testing.T) *liveTestEnv {
	t.Helper()
	ts := newCDPTestServer(t, []cdpTargetInfo{{ID: "t1", Type: "page", URL: "https://x.test/", Title: "例页"}})
	m := newTestManager(t)
	dir, err := ProfileDir("default")
	if err != nil {
		t.Fatal(err)
	}
	writePortFile(t, dir, ts.port)
	zeroLiveDelays(t)
	m.view.apply("default", func(row *SessionView) {
		row.Pages = []PageView{{Index: 0, URL: "https://x.test/", Title: "例页", Current: true}}
	})
	return &liveTestEnv{m: m, ts: ts}
}

// idleTimerOf 读 entry 当前 idle 计时器（entry.mu 下）。
func idleTimerOf(t *testing.T, m *Manager, profile string) *time.Timer {
	t.Helper()
	entry := m.entryOf(profile)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.idleTimer
}

// waitClosed 等通道关闭（观察面 done 语义）。
func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("5s 内未观测到 %s", what)
	}
}

func TestManagerLiveSubscribeRefs(t *testing.T) {
	env := newLiveTestEnv(t)
	m := env.m
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s1, err := m.SubscribeLive(ctx, "default", 0)
	if err != nil {
		t.Fatalf("SubscribeLive: %v", err)
	}
	entry := m.entryOf("default")
	if n := entry.viewerRefs.Load(); n != 1 {
		t.Fatalf("refs = %d, want 1", n)
	}
	if f, gen, ok := s1.LatestFrame(); !ok || len(f.Data) == 0 || gen != 1 {
		t.Fatalf("初始帧 = len=%d gen=%d ok=%v", len(f.Data), gen, ok)
	}

	// 同页复用：同一槽，不重复拨号（attachToTarget 只此一次）。
	s2, err := m.SubscribeLive(ctx, "default", 0)
	if err != nil {
		t.Fatalf("复用 SubscribeLive: %v", err)
	}
	if s1.slot != s2.slot {
		t.Fatal("同页订阅应复用同一槽")
	}
	if n := entry.viewerRefs.Load(); n != 2 {
		t.Fatalf("refs = %d, want 2", n)
	}
	attachCalls := 0
	for _, c := range env.ts.calls {
		if c == "Target.attachToTarget" {
			attachCalls++
		}
	}
	if attachCalls != 1 {
		t.Fatalf("attachToTarget 调用 %d 次, want 1（复用不重拨）", attachCalls)
	}

	// 输入面：活跃期可分发；退订后拒绝。
	if err := m.DispatchInput(ctx, "default", "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved"}); err != nil {
		t.Fatalf("DispatchInput: %v", err)
	}
	if !env.ts.called("Input.dispatchMouseEvent") {
		t.Fatal("输入未达 CDP 会话")
	}

	s1.Unsubscribe()
	if n := entry.viewerRefs.Load(); n != 1 {
		t.Fatalf("退订一支后 refs = %d, want 1", n)
	}
	s2.Unsubscribe()
	if n := entry.viewerRefs.Load(); n != 0 {
		t.Fatalf("末订退订后 refs = %d, want 0", n)
	}
	m.viewerMu.Lock()
	slot := m.viewers["default"]
	m.viewerMu.Unlock()
	if slot != nil {
		t.Fatal("末订退订后槽应拆除")
	}
	waitClosed(t, s2.Done(), "viewer 断连广播")
	if err := m.DispatchInput(ctx, "default", "Input.dispatchMouseEvent", map[string]any{}); !errors.Is(err, ErrLiveNotActive) {
		t.Fatalf("无流输入应 ErrLiveNotActive, got %v", err)
	}
	// 双退订防御。
	s2.Unsubscribe()
	if n := entry.viewerRefs.Load(); n != 0 {
		t.Fatalf("双退订后 refs = %d, want 0", n)
	}
}

// TestManagerLiveIdleKeepAlive 观察保活闸（D4 补充）：订阅期 idle 计时暂停（含
// resetIdleTimerLocked 直呼的闸复检），末订退订恢复计时并如期释放。
func TestManagerLiveIdleKeepAlive(t *testing.T) {
	withCallTimeouts(t, time.Hour, 100*time.Millisecond)
	env := newLiveTestEnv(t)
	env.ctl = withEngineCtl(t)
	m := env.m
	if _, err := m.Ensure(context.Background(), "default", nil); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if idleTimerOf(t, m, "default") == nil {
		t.Fatal("ready 后 idle 计时应已武装")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := m.SubscribeLive(ctx, "default", 0)
	if err != nil {
		t.Fatalf("SubscribeLive: %v", err)
	}
	if idleTimerOf(t, m, "default") != nil {
		t.Fatal("订阅后 idle 计时应暂停")
	}
	entry := m.entryOf("default")
	entry.mu.Lock()
	entry.resetIdleTimerLocked(m) // 闸复检：订户在场不武装
	paused := entry.idleTimer == nil
	entry.mu.Unlock()
	if !paused {
		t.Fatal("保活闸失效：订户在场 resetIdleTimer 不应武装计时")
	}

	s.Unsubscribe()
	if idleTimerOf(t, m, "default") == nil {
		t.Fatal("末订退订后 idle 计时应恢复")
	}
	waitForStatus(t, m, "default", StatusIdle) // 100ms 到期 → 自动释放
}

// TestManagerLiveDropOnClose 收敛点①：Close 强拆在订观察面，终态 reason 可读，refs
// 随流退订归零。
func TestManagerLiveDropOnClose(t *testing.T) {
	withCallTimeouts(t, time.Hour, time.Minute)
	env := newLiveTestEnv(t)
	env.ctl = withEngineCtl(t)
	m := env.m
	if _, err := m.Ensure(context.Background(), "default", nil); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := m.SubscribeLive(ctx, "default", 0)
	if err != nil {
		t.Fatalf("SubscribeLive: %v", err)
	}

	if err := m.Close("default"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitClosed(t, s.Done(), "Close 强拆断连")
	if reason := s.CloseReason(); reason != "浏览器会话已关闭" {
		t.Fatalf("CloseReason = %q", reason)
	}
	if err := m.DispatchInput(ctx, "default", "Input.dispatchMouseEvent", map[string]any{}); !errors.Is(err, ErrLiveNotActive) {
		t.Fatalf("强拆后输入应 ErrLiveNotActive, got %v", err)
	}
	s.Unsubscribe()
	if n := m.entryOf("default").viewerRefs.Load(); n != 0 {
		t.Fatalf("退订后 refs = %d, want 0", n)
	}
}

// TestManagerLiveDropOnHeadlessSwitch 收敛点②：headless 切换 close+reopen 强拆在订
// 观察面（重建后端口变化，残连接必死——显式拆给终态）。
func TestManagerLiveDropOnHeadlessSwitch(t *testing.T) {
	env := newLiveTestEnv(t)
	env.ctl = withEngineCtl(t)
	m := env.m
	if _, err := m.Ensure(context.Background(), "default", boolPtr(false)); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := m.SubscribeLive(ctx, "default", 0)
	if err != nil {
		t.Fatalf("SubscribeLive: %v", err)
	}

	if _, err := m.Ensure(context.Background(), "default", boolPtr(true)); err != nil {
		t.Fatalf("切换 Ensure: %v", err)
	}
	waitClosed(t, s.Done(), "无头切换强拆断连")
	if reason := s.CloseReason(); reason != "无头模式切换，实时画面已断开" {
		t.Fatalf("CloseReason = %q", reason)
	}
}

// withKillableEngineCtl 可杀假引擎：同 withEngineCtl 桩面，另暴露 kill（关停 in-memory
// 服务端会话 → client session.Wait 返回 → 真实 watchExit 链）。
func withKillableEngineCtl(t *testing.T) (*fakeEngineCtl, func()) {
	t.Helper()
	ctl := &fakeEngineCtl{}
	var mu sync.Mutex
	var kill func()
	withEngineCtlDial(t, ctl, func(ss *mcp.ServerSession) {
		mu.Lock()
		kill = func() { _ = ss.Close() }
		mu.Unlock()
	})
	return ctl, func() {
		mu.Lock()
		k := kill
		mu.Unlock()
		if k != nil {
			k()
		}
	}
}

// TestManagerLiveDropOnWatchExit 收敛点③：引擎意外死亡 → watchExit 强拆在订观察面 +
// 置 failed。
func TestManagerLiveDropOnWatchExit(t *testing.T) {
	env := newLiveTestEnv(t)
	ctl, kill := withKillableEngineCtl(t)
	env.ctl = ctl
	m := env.m
	if _, err := m.Ensure(context.Background(), "default", nil); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := m.SubscribeLive(ctx, "default", 0)
	if err != nil {
		t.Fatalf("SubscribeLive: %v", err)
	}

	kill()
	waitClosed(t, s.Done(), "引擎退出强拆断连")
	if reason := s.CloseReason(); reason != "引擎进程意外退出" {
		t.Fatalf("CloseReason = %q", reason)
	}
	waitForStatus(t, m, "default", StatusFailed)
}

// TestManagerLivePageResolve 页码解析自愈：空投影经 liveTabsRefresh 刷新后命中；
// 刷新后仍 miss 显式报错。
func TestManagerLivePageResolve(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pageID  int
		tabs    string
		wantErr string
	}{
		{"刷新后命中", 0, "- 0: (current) [例页](https://x.test/)\n", ""},
		{"越界页码刷新后仍未命中", 5, "- 0: (current) [例页](https://x.test/)\n", "页码 5 不存在"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newLiveTestEnv(t)
			env.ctl = withEngineCtl(t)
			env.m.view.apply("default", func(row *SessionView) { row.Pages = nil }) // 空投影起步
			env.ctl.mu.Lock()
			env.ctl.toolFn = func(tool string, args map[string]any) (string, bool) {
				if tool == EngineToolTabs {
					return tc.tabs, false
				}
				return "ok: " + tool, false
			}
			env.ctl.mu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s, err := env.m.SubscribeLive(ctx, "default", tc.pageID)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want 含 %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SubscribeLive: %v", err)
			}
			defer s.Unsubscribe()
			if row := sessionRow(t, env.m, "default"); len(row.Pages) != 1 || row.Pages[0].URL != "https://x.test/" {
				t.Fatalf("刷新未回填投影: %+v", row.Pages)
			}
		})
	}
}
