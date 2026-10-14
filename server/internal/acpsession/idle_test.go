package acpsession

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/appdb"
)

// overrideIdleTimeout 注入短阈值（测试结束恢复）：真实计时驱动回收路径。
func overrideIdleTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := idleTimeout
	idleTimeout = func(*Manager) time.Duration { return d }
	t.Cleanup(func() { idleTimeout = old })
}

// writeTestAppDB 生成带 app_config 表与阈值键的真实 sqlite 文件（模拟 Rust 侧共享 app.db）。
func writeTestAppDB(t *testing.T, value string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("打开临时 app.db: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE app_config (key TEXT PRIMARY KEY, value TEXT NOT NULL)"); err != nil {
		t.Fatalf("建 app_config 表: %v", err)
	}
	if value != "" {
		if _, err := db.Exec("INSERT INTO app_config (key, value) VALUES (?, ?)", appdb.KeyAcpSessionIdleTimeoutMinutes, value); err != nil {
			t.Fatalf("写入阈值: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭临时 app.db: %v", err)
	}
	return dbPath
}

// TestParseIdleTimeoutMinutes 配置解析：值域内取值；非法/越域回落默认（与前端 parse 同口径）。
func TestParseIdleTimeoutMinutes(t *testing.T) {
	cases := []struct {
		value string
		want  int
	}{
		{"5", 5}, {"15", 15}, {"30", 30}, {"60", 60},
		{"", defaultAcpSessionIdleTimeoutMinutes},
		{"abc", defaultAcpSessionIdleTimeoutMinutes},
		{"10", defaultAcpSessionIdleTimeoutMinutes}, // 值域外（非选项）
		{"0", defaultAcpSessionIdleTimeoutMinutes},
		{"-5", defaultAcpSessionIdleTimeoutMinutes},
		{" 30 ", 30}, // 容忍空白
	}
	for _, tc := range cases {
		if got := parseIdleTimeoutMinutes(tc.value); got != tc.want {
			t.Fatalf("parseIdleTimeoutMinutes(%q) = %d，want %d", tc.value, got, tc.want)
		}
	}
}

// TestIdleTimeoutFromConfig 真实 app.db 注入：配置值经短连接现读；越域脏值与未注入回落默认。
func TestIdleTimeoutFromConfig(t *testing.T) {
	mgr := &Manager{appDbPath: writeTestAppDB(t, "15")}
	if got := mgr.idleTimeoutMinutes(); got != 15 {
		t.Fatalf("配置 15 应读到 15，got %d", got)
	}
	mgr = &Manager{appDbPath: writeTestAppDB(t, "999")}
	if got := mgr.idleTimeoutMinutes(); got != defaultAcpSessionIdleTimeoutMinutes {
		t.Fatalf("越域脏值应回落默认，got %d", got)
	}
	mgr = &Manager{appDbPath: ""}
	if got := mgr.idleTimeoutMinutes(); got != defaultAcpSessionIdleTimeoutMinutes {
		t.Fatalf("未注入应回落默认，got %d", got)
	}
}

/*
 * newIdleTestManager 装配被测 Manager + 种子 issue。spawn 顶替按用例定制：script 定
 * fakeagent 脚本；resumeIDs 非 nil 时记录每次收到的 resumeID（回收闭环断言用）。
 */
func newIdleTestManager(t *testing.T, script string, resumeIDs *[]string) (*Manager, *gorm.DB, string) {
	t.Helper()
	db := newTestDB(t)
	issueID := seedIssue(t, db, t.TempDir(), `{"mode":"acp"}`, "")
	mgr, err := NewManager(db, Dirs{}, 0, "", zap.NewNop())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.StopAll)
	old := spawnFunc
	spawnFunc = fakeSpawn(script, nil, &atomic.Int64{}, resumeIDs)
	t.Cleanup(func() { spawnFunc = old })
	return mgr, db, issueID
}

/*
 * TestReclaimIdlePostponesOnThresholdIncrease 阈值改长顺延（I1 回归锁）：计时中途改长
 * 阈值，到期回调按空闲起点重算剩余时长——原期限到期不得回收，顺延至新期限后才回收。
 */
func TestReclaimIdlePostponesOnThresholdIncrease(t *testing.T) {
	old := idleTimeout
	idleTimeout = func(*Manager) time.Duration { return 250 * time.Millisecond }
	t.Cleanup(func() { idleTimeout = old })
	mgr, _, issueID := newIdleTestManager(t, "happy", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitForCondition(t, "会话就绪", func() bool { return mgr.Get(issueID).Status == StatusReady })
	// 白盒等首表起好再翻转阈值：先到期的起表必须用到旧值（250ms），顺延分支才被真正行使。
	waitForCondition(t, "闲置倒计时起表", func() bool {
		mgr.mu.Lock()
		entry, ok := mgr.entries[issueID]
		mgr.mu.Unlock()
		if !ok {
			return false
		}
		entry.mu.Lock()
		defer entry.mu.Unlock()
		return entry.idleTimer != nil
	})
	/*
	 * 翻转阈值（写侧持同一 entry.mu——生产两处读取都在 entry.mu 临界区内，同锁建立
	 * happens-before，直接写包级变量与到期回调竞态，race 检测实锤）。
	 */
	flip := func(d time.Duration) {
		mgr.mu.Lock()
		entry := mgr.entries[issueID]
		mgr.mu.Unlock()
		entry.mu.Lock()
		idleTimeout = func(*Manager) time.Duration { return d }
		entry.mu.Unlock()
	}
	// 改长阈值：到期回调应按新值顺延，而非按旧 250ms 直接回收。
	flip(700 * time.Millisecond)
	time.Sleep(500 * time.Millisecond) // 原期限（250ms）已过、新期限（700ms）未到
	mgr.mu.Lock()
	_, ok := mgr.entries[issueID]
	mgr.mu.Unlock()
	if !ok {
		t.Fatal("阈值改长后原期限到期不应回收（顺延分支失效）")
	}
	waitForCondition(t, "新期限到期回收", func() bool {
		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		_, ok := mgr.entries[issueID]
		return !ok
	})
}

/*
 * TestReclaimIdleKeepsAnchorAndResumes 回收闭环（T4 核心验收）：完全空闲到期 → entry 摘除
 * + 进程回收 + 锚点保留 → Ensure 重开走 resume（同 id 恢复）。
 */
func TestReclaimIdleKeepsAnchorAndResumes(t *testing.T) {
	// 阈值远大于就绪快照读取窗口（firstID 在回收前必读到），又足够短让回收必然发生。
	overrideIdleTimeout(t, 400*time.Millisecond)
	var resumeIDs []string
	mgr, db, issueID := newIdleTestManager(t, "load", &resumeIDs)

	// 首开：空锚全新会话（load 脚本 NewSession 通用，正常生成 id）。
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("首次 Ensure: %v", err)
	}
	waitForCondition(t, "会话就绪", func() bool { return mgr.Get(issueID).Status == StatusReady })
	firstID := mgr.Get(issueID).AcpSessionID
	if firstID == "" {
		t.Fatal("首开会话应有 sessionId")
	}

	// 无订户（本测试不订阅）+ 无回合：闲置到期自动回收——entry 摘除、进程回收、锚保留。
	waitForCondition(t, "闲置回收摘除 entry", func() bool {
		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		_, ok := mgr.entries[issueID]
		return !ok
	})
	/*
	 * 宽限等回收收尾：pump 终结哨兵若被回归破坏将在此窗口落 SaveError 清锚——锚点断言
	 * 必须落在「若回归存在则已发生」之后，否则对 released 早退路径不设防。
	 */
	time.Sleep(300 * time.Millisecond)
	if row := bindingRow(t, db, issueID); row.AcpSessionID != firstID {
		t.Fatalf("回收必须保留锚点（resume 依据），got %q want %q", row.AcpSessionID, firstID)
	}

	// 重开：锚点非空 → resume 同 id 恢复（新 entry、同会话）。
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("重开 Ensure: %v", err)
	}
	waitForCondition(t, "重开会话就绪", func() bool { return mgr.Get(issueID).Status == StatusReady })
	if got := mgr.Get(issueID).AcpSessionID; got != firstID {
		t.Fatalf("重开应 resume 同 id 会话，got %q want %q", got, firstID)
	}
	if n := len(resumeIDs); n != 2 || resumeIDs[0] != "" || resumeIDs[1] != firstID {
		t.Fatalf("两次 spawn 的 resumeID = %v，want [空, %q]", resumeIDs, firstID)
	}
}

/*
 * TestReclaimIdleGuards 完全空闲判据逐项负例：回合进行中 / 有订户 / 有等位者，任一为真
 * 即不回收。预订阅挡住就绪等待期的倒计时，判据搭建完成、撤预订阅后观察——守卫隔离，
 * 不因其他判据在场而假通过。
 */
func TestReclaimIdleGuards(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, mgr *Manager, issueID string) (release func())
	}{
		{"回合进行中", func(t *testing.T, mgr *Manager, issueID string) func() {
			frames, cancel := mgr.Subscribe(issueID)
			if err := mgr.Prompt(issueID, "长回合"); err != nil {
				t.Fatalf("Prompt: %v", err)
			}
			waitFrame(t, frames, "turnStarted", func(f Frame) bool { return f.Type == FrameTurnStarted })
			cancel() // 撤本用例订阅：守卫只剩 turnActive（隔离订户判据）
			return func() { _ = mgr.Cancel(issueID) }
		}},
		{"有订户", func(t *testing.T, mgr *Manager, issueID string) func() {
			_, cancel := mgr.Subscribe(issueID)
			return cancel // 订阅持有至用例结束
		}},
		{"有等位者", func(t *testing.T, mgr *Manager, issueID string) func() {
			mgr.mu.Lock()
			entry := mgr.entries[issueID]
			mgr.mu.Unlock()
			w := entry.turnQueue.join() // 白盒：模拟 bot 排队回合在场
			return func() { entry.turnQueue.depart(w) }
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			overrideIdleTimeout(t, 40*time.Millisecond)
			mgr, _, issueID := newIdleTestManager(t, "slow-cancel", nil)
			_, preCancel := mgr.Subscribe(issueID) // 预订阅：等待就绪期间判据恒不满足，不启计时
			if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			waitForCondition(t, "会话就绪", func() bool { return mgr.Get(issueID).Status == StatusReady })
			release := tc.setup(t, mgr, issueID)
			defer release()
			preCancel() // 撤预订阅 → 重估被守卫挡回（不启计时）
			// 阈值数十倍于观察窗口：若守卫失效，倒计时早到期回收。
			time.Sleep(300 * time.Millisecond)
			mgr.mu.Lock()
			_, ok := mgr.entries[issueID]
			mgr.mu.Unlock()
			if !ok {
				t.Fatalf("%s 场景下不应回收", tc.name)
			}
		})
	}
}
