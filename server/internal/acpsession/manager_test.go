package acpsession

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BrokkAi/acp-go/schema"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/dal/model"
)

// fakeAgentBin TestMain 现场编译的假 agent 二进制（复用 internal/acp 的 fakeagent：真实
// stdio + agent.New().Serve，manager 集成测试以它顶替 vendored 生产链）。
var fakeAgentBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ocean-acpsession-fakeagent-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "创建临时目录失败:", err)
		os.Exit(1)
	}
	bin := filepath.Join(dir, "fakeagent")
	cmd := exec.Command("go", "build", "-o", bin, "../acp/fakeagent")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "构建 fakeagent 失败: %v\n%s", err, out)
		os.Exit(1)
	}
	fakeAgentBin = bin
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// fakeSpawn 顶替 spawnAgentSession 的测试实现：固定脚本拉起 fakeagent（wantErr 非 nil 时
// 直接失败，模拟 vendored/握手链报错）；calls 统计实际调用次数（join 幂等断言用）。
func fakeSpawn(script string, wantErr error, calls *atomic.Int64) func(SessionConfig, Dirs, *zap.Logger) (*acp.AgentClient, *acp.Session, error) {
	return func(cfg SessionConfig, _ Dirs, log *zap.Logger) (*acp.AgentClient, *acp.Session, error) {
		calls.Add(1)
		if wantErr != nil {
			return nil, nil, wantErr
		}
		client, err := acp.Launch(context.Background(), acp.SpawnConfig{
			Command: []string{fakeAgentBin, script},
			Cwd:     cfg.Cwd,
		}, log)
		if err != nil {
			return nil, nil, err
		}
		if _, err := client.Initialize(context.Background()); err != nil {
			client.Close()
			return nil, nil, err
		}
		session, err := client.NewSession(context.Background(), acp.NewSessionParams{Cwd: cfg.Cwd})
		if err != nil {
			client.Close()
			return nil, nil, err
		}
		return client, session, nil
	}
}

// newManagerWithIssue 装配被测 Manager + 种子 issue + 预订阅帧流（Ensure 前订阅，从
// idle 快照起全帧可见）。spawnFunc 已替换为 fakeSpawn，测试结束恢复。
func newManagerWithIssue(t *testing.T, script string, wantErr error) (*Manager, *gorm.DB, string, <-chan Frame, *atomic.Int64) {
	t.Helper()
	db := newTestDB(t)
	wsDir := t.TempDir()
	issueID := seedIssue(t, db, wsDir, `{"mode":"acp"}`, "")
	mgr, err := NewManager(db, Dirs{}, zap.NewNop())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.StopAll)
	calls := &atomic.Int64{}
	old := spawnFunc
	spawnFunc = fakeSpawn(script, wantErr, calls)
	t.Cleanup(func() { spawnFunc = old })
	frames, cancel := mgr.Subscribe(issueID)
	t.Cleanup(cancel)
	return mgr, db, issueID, frames, calls
}

// waitFrame 排流等待首个命中谓词的帧（其余帧丢弃——不重放，快照兜底补态）。
func waitFrame(t *testing.T, frames <-chan Frame, what string, pred func(Frame) bool) Frame {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				t.Fatalf("帧通道关闭，等待 %s", what)
			}
			if pred(frame) {
				return frame
			}
		case <-deadline:
			t.Fatalf("等待 %s 超时", what)
		}
	}
}

// statusFrameIs 会话状态帧谓词（sessionStatus 与 terminated 两帧型共用 Status 载荷）。
func statusFrameIs(want SessionStatus) func(Frame) bool {
	return func(f Frame) bool {
		return (f.Type == FrameSessionStatus || f.Type == FrameTerminated) &&
			f.Status != nil && f.Status.Status == want
	}
}

// bindingRow 按 issueId 查锚点行（断言辅助）。
func bindingRow(t *testing.T, db *gorm.DB, issueID string) *model.IssueAcpSession {
	t.Helper()
	q := queryOf(db)
	row, err := q.IssueAcpSession.WithContext(context.Background()).Where(
		q.IssueAcpSession.IssueID.Eq(issueID)).First()
	if err != nil {
		t.Fatalf("查绑定行: %v", err)
	}
	return row
}

// bindingExists 锚点行是否存在（存在性断言不 Fatal）。
func bindingExists(db *gorm.DB, issueID string) bool {
	q := queryOf(db)
	_, err := q.IssueAcpSession.WithContext(context.Background()).Where(
		q.IssueAcpSession.IssueID.Eq(issueID)).First()
	return err == nil
}

// waitForCondition 轮询等待条件成立（异步落库路径的最终一致断言）。
func waitForCondition(t *testing.T, what string, pred func() bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if pred() {
			return
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatalf("等待 %s 超时", what)
		}
	}
}

func TestEnsureReadyPromptAndAnchor(t *testing.T) {
	mgr, db, issueID, frames, calls := newManagerWithIssue(t, "happy", nil)
	ctx := context.Background()

	// 受理：立即返回 starting 快照；随后 sessionStatus ready 帧到达。
	snap, err := mgr.Ensure(ctx, issueID)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if snap.Status != StatusStarting {
		t.Fatalf("受理快照应为 starting，got %s", snap.Status)
	}
	ready := waitFrame(t, frames, "sessionStatus ready", statusFrameIs(StatusReady))
	if ready.Status.AcpSessionID == "" || ready.Status.AgentCode == "" {
		t.Fatalf("ready 帧应带 sessionId/agentCode，got %+v", ready.Status)
	}

	// 回合全链：用户条目 + turnStarted + agent chunk 条目 + turnEnded。
	if err := mgr.Prompt(issueID, "打声招呼"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	turnStarted := waitFrame(t, frames, "turnStarted", func(f Frame) bool { return f.Type == FrameTurnStarted })
	if !turnStarted.Turn.Active {
		t.Fatalf("turnStarted 应 active，got %+v", turnStarted.Turn)
	}
	ended := waitFrame(t, frames, "turnEnded", func(f Frame) bool { return f.Type == FrameTurnEnded })
	if ended.Turn.Active || ended.Turn.StopReason != string(schema.StopReasonEndTurn) {
		t.Fatalf("turnEnded 应 end_turn 收敛，got %+v", ended.Turn)
	}

	// 会话视图终态：agent 条目由 pump 异步积累，可能晚于 turnEnded 帧广播且聚合分段
	// 到齐（轮询等文本完整）；stopReason/turnActive 在 endTurn 内先于广播同步落视图，
	// 帧到达即终值。
	waitForCondition(t, "agent 条目聚合完成", func() bool {
		snap = mgr.Get(issueID)
		return len(snap.Entries) == 2 && strings.Contains(snap.Entries[1].Text, "ACP 回合完成")
	})
	if snap.Entries[1].Kind != entryKindAgentMessage || !strings.Contains(snap.Entries[1].Text, "ACP 回合完成") {
		t.Fatalf("agent 条目应聚合 chunk，got %+v", snap.Entries[1])
	}
	if snap.StopReason != string(schema.StopReasonEndTurn) || snap.TurnActive {
		t.Fatalf("回合终态应落快照，got %+v", snap)
	}

	// 锚点落库：acp_session_id 非空 + last_error 清空 + spawn 恰好一次。
	row := bindingRow(t, db, issueID)
	if row.AcpSessionID == "" || row.LastError != "" || row.AgentCode == "" {
		t.Fatalf("锚点应就绪落库，got %+v", row)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("spawn 应恰好一次，got %d", n)
	}
}

func TestEnsureJoinIdempotent(t *testing.T) {
	mgr, _, issueID, frames, calls := newManagerWithIssue(t, "happy", nil)
	if _, err := mgr.Ensure(context.Background(), issueID); err != nil {
		t.Fatalf("Ensure#1: %v", err)
	}
	waitFrame(t, frames, "sessionStatus ready", statusFrameIs(StatusReady))
	// 二次受理：join 返回 ready 快照，不重复 spawn。
	snap, err := mgr.Ensure(context.Background(), issueID)
	if err != nil {
		t.Fatalf("Ensure#2: %v", err)
	}
	if snap.Status != StatusReady {
		t.Fatalf("join 应返回 ready，got %s", snap.Status)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("join 不应触发二次 spawn，got %d", n)
	}
}

func TestEnsureSpawnFailureAndRetry(t *testing.T) {
	mgr, db, issueID, frames, _ := newManagerWithIssue(t, "", errors.New("vendored 目录未配置"))
	ctx := context.Background()
	// 受理同步成功（失败在后台链）；failed 帧带原因。
	if _, err := mgr.Ensure(ctx, issueID); err != nil {
		t.Fatalf("受理应成功（失败在后台链），got %v", err)
	}
	failed := waitFrame(t, frames, "sessionStatus failed", statusFrameIs(StatusFailed))
	if failed.Status.Error == "" {
		t.Fatalf("failed 帧应带原因，got %+v", failed.Status)
	}
	// 落因清锚：last_error 非空 + acp_session_id 空。
	waitForCondition(t, "失败落因", func() bool {
		row := bindingRow(t, db, issueID)
		return row.LastError != "" && row.AcpSessionID == ""
	})
	// failed 态可重试：换成功 spawn 重建 → ready + 重新落锚清错。
	old := spawnFunc
	spawnFunc = fakeSpawn("happy", nil, &atomic.Int64{})
	defer func() { spawnFunc = old }()
	frames2, cancel := mgr.Subscribe(issueID)
	defer cancel()
	if _, err := mgr.Ensure(ctx, issueID); err != nil {
		t.Fatalf("重试 Ensure: %v", err)
	}
	waitFrame(t, frames2, "重建 ready", statusFrameIs(StatusReady))
	row := bindingRow(t, db, issueID)
	if row.AcpSessionID == "" || row.LastError != "" {
		t.Fatalf("重建应重新落锚清错，got %+v", row)
	}
}

func TestProcessDeathTerminates(t *testing.T) {
	mgr, db, issueID, frames, _ := newManagerWithIssue(t, "crash", nil)
	if _, err := mgr.Ensure(context.Background(), issueID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	terminated := waitFrame(t, frames, "terminated 帧", func(f Frame) bool { return f.Type == FrameTerminated })
	if terminated.Status.Status != StatusTerminated || terminated.Status.Error == "" {
		t.Fatalf("terminated 帧应带原因，got %+v", terminated.Status)
	}
	if !strings.Contains(terminated.Status.Error, "异常退出") {
		t.Fatalf("意外死亡应携带异常退出证据，got %q", terminated.Status.Error)
	}
	// 清锚落因（pump 收尾异步落库）。
	waitForCondition(t, "锚点清零落因", func() bool {
		row := bindingRow(t, db, issueID)
		return row.AcpSessionID == "" && strings.Contains(row.LastError, "异常退出")
	})
}

func TestPromptTurnGateAndCancel(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "slow-cancel", nil)
	if _, err := mgr.Ensure(context.Background(), issueID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	if err := mgr.Prompt(issueID, "跑起来"); err != nil {
		t.Fatalf("Prompt#1: %v", err)
	}
	// 活动回合中再受理拒绝（视图侧前置闸门，acp.ErrTurnActive 同语义）。
	if err := mgr.Prompt(issueID, "并发回合"); !errors.Is(err, acp.ErrTurnActive) {
		t.Fatalf("并发回合应 ErrTurnActive，got %v", err)
	}
	// 等首个 tick 条目（回合已推进、acp 层 turnCancel 必已置位）再取消——过早 Cancel
	// 会在后台回合尚未注册时静默丢失（对齐 acp 包 TestCancelTurnSoftCancel 惯例）。
	waitFrame(t, frames, "首个 tick 条目", func(f Frame) bool {
		return f.Type == FrameEntry && f.Entry != nil && strings.HasPrefix(f.Entry.Text, "tick-")
	})
	if err := mgr.Cancel(issueID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	ended := waitFrame(t, frames, "cancelled 终态", func(f Frame) bool { return f.Type == FrameTurnEnded })
	if ended.Turn.StopReason != string(schema.StopReasonCancelled) {
		t.Fatalf("取消应 cancelled 收敛，got %+v", ended.Turn)
	}
	// 取消后回合闸门释放：再受理成功即证明（fakeagent 的 canceled 标记残留，第二轮
	// 不再推 tick、立即以 cancelled 收敛）。
	if err := mgr.Prompt(issueID, "再来一轮"); err != nil {
		t.Fatalf("取消后应可再受理回合，got %v", err)
	}
	ended2 := waitFrame(t, frames, "第二轮 cancelled", func(f Frame) bool { return f.Type == FrameTurnEnded })
	if ended2.Turn.StopReason != string(schema.StopReasonCancelled) {
		t.Fatalf("第二轮应 cancelled 收敛，got %+v", ended2.Turn)
	}
}

func TestRespondPermissionFlow(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "permission", nil)
	if _, err := mgr.Ensure(context.Background(), issueID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	if err := mgr.Prompt(issueID, "需要权限"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	opened := waitFrame(t, frames, "pendingOpened", func(f Frame) bool { return f.Type == FramePendingOpened })
	if opened.Pending == nil || opened.Pending.Kind != "permission" || len(opened.Pending.Options) == 0 {
		t.Fatalf("pendingOpened 应带权限投影，got %+v", opened.Pending)
	}
	pendingID := opened.Pending.PendingID
	// 挂起期快照可见。
	if pendings := mgr.Get(issueID).Pendings; len(pendings) != 1 || pendings[0].PendingID != pendingID {
		t.Fatalf("挂起应入快照，got %+v", pendings)
	}
	// 未知 optionId 拒绝且不消耗挂起（agent 侧仍等待）。
	if err := mgr.RespondPermission(issueID, pendingID, "bogus"); err == nil {
		t.Fatal("未知 optionId 应拒绝")
	}
	if pendings := mgr.Get(issueID).Pendings; len(pendings) != 1 {
		t.Fatalf("未知选项不应消耗挂起，got %+v", pendings)
	}
	// 合法应答 → pendingClosed → 回合 end_turn 收敛 + 决策回显条目。
	if err := mgr.RespondPermission(issueID, pendingID, "allow"); err != nil {
		t.Fatalf("RespondPermission: %v", err)
	}
	closed := waitFrame(t, frames, "pendingClosed", func(f Frame) bool {
		return f.Type == FramePendingClosed && f.PendingID == pendingID
	})
	if closed.PendingID != pendingID {
		t.Fatalf("pendingClosed 应带 pendingId，got %d", closed.PendingID)
	}
	// 决策回显条目（pump 广播）与回合终态（回合收尾 goroutine 广播）并发，到达顺序
	// 不定：双谓词合并等待，不做顺序假设。
	var sawEcho, sawEnd bool
	deadline := time.After(20 * time.Second)
	for !sawEcho || !sawEnd {
		select {
		case frame, ok := <-frames:
			if !ok {
				t.Fatal("帧通道关闭，等待决策回显/回合终态")
			}
			if frame.Type == FrameEntry && frame.Entry != nil && strings.Contains(frame.Entry.Text, "permission:selected:allow") {
				sawEcho = true
			}
			if frame.Type == FrameTurnEnded && frame.Turn.StopReason == string(schema.StopReasonEndTurn) {
				sawEnd = true
			}
		case <-deadline:
			t.Fatalf("等待决策回显/回合终态超时（sawEcho=%v sawEnd=%v）", sawEcho, sawEnd)
		}
	}
	if pendings := mgr.Get(issueID).Pendings; len(pendings) != 0 {
		t.Fatalf("应答后挂起应清空，got %+v", pendings)
	}
	// 重复应答：已摘除 → 未知挂起错误。
	if err := mgr.RespondPermission(issueID, pendingID, "allow"); err == nil {
		t.Fatal("已应答的挂起应拒绝重复应答")
	}
}

func TestDiscardCascade(t *testing.T) {
	mgr, db, issueID, frames, _ := newManagerWithIssue(t, "happy", nil)
	if _, err := mgr.Ensure(context.Background(), issueID); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	mgr.Discard(issueID)
	// 绑定行删除 + 会话终结帧（ready 态终结帧经 pump 哨兵路径到达）。
	waitForCondition(t, "绑定行删除", func() bool { return !bindingExists(db, issueID) })
	waitFrame(t, frames, "terminated 帧", func(f Frame) bool { return f.Type == FrameTerminated })
	// 幂等：二次 Discard 无害；Discard 后 Get 回 idle。
	mgr.Discard(issueID)
	if snap := mgr.Get(issueID); snap.Status != StatusIdle {
		t.Fatalf("Discard 后应为 idle，got %s", snap.Status)
	}
}

func TestBindingStoreLifecycle(t *testing.T) {
	db := newTestDB(t)
	store := &BindingStore{DB: db}
	ctx := context.Background()

	row, err := store.GetOrCreate(ctx, "i-1")
	if err != nil || row.IssueID != "i-1" || row.AcpSessionID != "" {
		t.Fatalf("GetOrCreate 建行: %v %+v", err, row)
	}
	row2, err := store.GetOrCreate(ctx, "i-1")
	if err != nil || row2.ID != row.ID {
		t.Fatalf("GetOrCreate 幂等: %v %+v", err, row2)
	}
	if err := store.SaveReady(ctx, "i-1", "claude-acp", "sess-9"); err != nil {
		t.Fatalf("SaveReady: %v", err)
	}
	row = bindingRow(t, db, "i-1")
	if row.AcpSessionID != "sess-9" || row.AgentCode != "claude-acp" || row.LastError != "" {
		t.Fatalf("SaveReady 落锚: %+v", row)
	}
	if err := store.SaveError(ctx, "i-1", "claude-acp", "握手失败"); err != nil {
		t.Fatalf("SaveError: %v", err)
	}
	row = bindingRow(t, db, "i-1")
	if row.AcpSessionID != "" || row.LastError != "握手失败" {
		t.Fatalf("SaveError 清锚落因: %+v", row)
	}
	// 无行 issue 的 SaveError 为 no-op（不复活行）。
	if err := store.SaveError(ctx, "i-none", "x", "y"); err != nil {
		t.Fatalf("无行 SaveError 应 no-op: %v", err)
	}
	if bindingExists(db, "i-none") {
		t.Fatal("无行 SaveError 不应建行")
	}
	// 启动清扫：锚清零、行保留、错误保留。
	if err := store.SweepSessionIDs(ctx); err != nil {
		t.Fatalf("SweepSessionIDs: %v", err)
	}
	row = bindingRow(t, db, "i-1")
	if row.AcpSessionID != "" || row.LastError != "握手失败" {
		t.Fatalf("清扫应只清锚: %+v", row)
	}
	// 删除 + 幂等。
	if err := store.Delete(ctx, "i-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if bindingExists(db, "i-1") {
		t.Fatal("Delete 应删行")
	}
	if err := store.Delete(ctx, "i-1"); err != nil {
		t.Fatalf("Delete 幂等: %v", err)
	}
}
