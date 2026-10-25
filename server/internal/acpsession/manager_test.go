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

	"github.com/ironpark/acp-go/acp1"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/dal/model"
)

/*
 * fakeAgentBin TestMain 现场编译的假 agent 二进制（复用 internal/acp 的 fakeagent：真实
 * stdio + agent.New().Serve，manager 集成测试以它顶替 vendored 生产链）。
 */
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

/*
 * fakeSpawn 顶替 spawnAgentSession 的测试实现：固定脚本拉起 fakeagent（wantErr 非 nil 时
 * 直接失败，模拟 vendored/握手链报错）；calls 统计实际调用次数（join 幂等断言用）；
 * resumeIDs 记录每次收到的 resumeID（Ensure 锚点接线断言用，可 nil）。
 */
func fakeSpawn(script string, wantErr error, calls *atomic.Int64, resumeIDs *[]string) func(SessionConfig, Dirs, int, *zap.Logger, string) (*acp.AgentClient, *acp.Session, bool, error) {
	return func(cfg SessionConfig, _ Dirs, _ int, log *zap.Logger, resumeID string) (*acp.AgentClient, *acp.Session, bool, error) {
		calls.Add(1)
		if resumeIDs != nil {
			*resumeIDs = append(*resumeIDs, resumeID)
		}
		if wantErr != nil {
			return nil, nil, false, wantErr
		}
		client, err := acp.Launch(context.Background(), acp.SpawnConfig{
			Command: []string{fakeAgentBin, script},
			Cwd:     cfg.Cwd,
		}, log)
		if err != nil {
			return nil, nil, false, err
		}
		if _, err := client.Initialize(context.Background()); err != nil {
			client.Close()
			return nil, nil, false, err
		}
		session, resumed, err := newOrResumedSession(context.Background(), client, cfg, resumeID, log)
		if err != nil {
			client.Close()
			return nil, nil, false, err
		}
		return client, session, resumed, nil
	}
}

/*
 * newManagerWithIssue 装配被测 Manager + 种子 issue + 预订阅帧流（Ensure 前订阅，从
 * idle 快照起全帧可见）。spawnFunc 已替换为 fakeSpawn，测试结束恢复。
 */
func newManagerWithIssue(t *testing.T, script string, wantErr error) (*Manager, *gorm.DB, string, <-chan Frame, *atomic.Int64) {
	t.Helper()
	db := newTestDB(t)
	wsDir := t.TempDir()
	taskID := seedIssue(t, db, wsDir, `{"mode":"acp"}`, "")
	issueID := ConversationSessionKey(seedTaskConversation(t, db, taskID))
	mgr, err := NewManager(db, Dirs{}, 0, "", "", zap.NewNop())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.StopAll)
	calls := &atomic.Int64{}
	old := spawnFunc
	spawnFunc = fakeSpawn(script, wantErr, calls, nil)
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

/*
 * waitFirstTick 等首个 tick 条目（slow-cancel 脚本：回合已推进、acp 层 turnCancel 必已
 * 注册）——过早取消会在后台回合尚未注册时静默丢失（对齐 acp 包 TestCancelTurnSoftCancel
 * 惯例）。取消类用例的前置步骤。
 */
func waitFirstTick(t *testing.T, frames <-chan Frame) {
	t.Helper()
	waitFrame(t, frames, "首个 tick 条目", func(f Frame) bool {
		return f.Type == FrameEntry && f.Entry != nil && strings.HasPrefix(f.Entry.Text, "tick-")
	})
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

func TestEnsureReadyPrompt(t *testing.T) {
	mgr, db, issueID, frames, calls := newManagerWithIssue(t, "happy", nil)
	ctx := context.Background()

	// 受理：立即返回 starting 快照；随后 sessionStatus ready 帧到达。
	snap, err := mgr.Ensure(ctx, issueID, "")
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
	if ended.Turn.Active || ended.Turn.StopReason != string(acp1.StopReasonEndTurn) {
		t.Fatalf("turnEnded 应 end_turn 收敛，got %+v", ended.Turn)
	}

	/*
	 * 会话视图终态：agent 条目由 pump 异步积累，可能晚于 turnEnded 帧广播且聚合分段
	 * 到齐（轮询等文本完整）；stopReason/turnActive 在 endTurn 内先于广播同步落视图，
	 * 帧到达即终值。
	 */
	waitForCondition(t, "agent 条目聚合完成", func() bool {
		snap = mgr.Get(issueID)
		return len(snap.Entries) == 2 && strings.Contains(snap.Entries[1].Text, "ACP 回合完成")
	})
	if snap.Entries[1].Kind != entryKindAgentMessage || !strings.Contains(snap.Entries[1].Text, "ACP 回合完成") {
		t.Fatalf("agent 条目应聚合 chunk，got %+v", snap.Entries[1])
	}
	if snap.StopReason != string(acp1.StopReasonEndTurn) || snap.TurnActive {
		t.Fatalf("回合终态应落快照，got %+v", snap)
	}

	// 锚点落库：会话实体行 claude_session_id 非空 + agent_code 记录。
	convID, _ := conversationRowID(issueID)
	convRow, err := queryOf(db).Conversation.WithContext(ctx).Where(queryOf(db).Conversation.ID.Eq(convID)).First()
	if err != nil {
		t.Fatalf("读会话行: %v", err)
	}
	if convRow.ClaudeSessionID == "" || convRow.AgentCode != "claude-acp" {
		t.Fatalf("锚点应就绪落库: %+v", convRow)
	}

	// spawn 恰好一次。
	if n := calls.Load(); n != 1 {
		t.Fatalf("spawn 应恰好一次，got %d", n)
	}
}

func TestEnsureJoinIdempotent(t *testing.T) {
	mgr, _, issueID, frames, calls := newManagerWithIssue(t, "happy", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("Ensure#1: %v", err)
	}
	waitFrame(t, frames, "sessionStatus ready", statusFrameIs(StatusReady))
	// 二次受理：join 返回 ready 快照，不重复 spawn。
	snap, err := mgr.Ensure(context.Background(), issueID, "")
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
	if _, err := mgr.Ensure(ctx, issueID, ""); err != nil {
		t.Fatalf("受理应成功（失败在后台链），got %v", err)
	}
	failed := waitFrame(t, frames, "sessionStatus failed", statusFrameIs(StatusFailed))
	if failed.Status.Error == "" {
		t.Fatalf("failed 帧应带原因，got %+v", failed.Status)
	}
	// 落因清锚：会话实体行 claude_session_id 归零。
	convID, _ := conversationRowID(issueID)
	waitForCondition(t, "失败清锚", func() bool {
		row, err := queryOf(db).Conversation.WithContext(ctx).Where(queryOf(db).Conversation.ID.Eq(convID)).First()
		return err == nil && row.ClaudeSessionID == ""
	})
	// failed 态可重试：换成功 spawn 重建 → ready + 重新落锚。
	old := spawnFunc
	spawnFunc = fakeSpawn("happy", nil, &atomic.Int64{}, nil)
	defer func() { spawnFunc = old }()
	frames2, cancel := mgr.Subscribe(issueID)
	defer cancel()
	if _, err := mgr.Ensure(ctx, issueID, ""); err != nil {
		t.Fatalf("重试 Ensure: %v", err)
	}
	waitFrame(t, frames2, "重建 ready", statusFrameIs(StatusReady))
	waitForCondition(t, "重建落锚", func() bool {
		row, err := queryOf(db).Conversation.WithContext(ctx).Where(queryOf(db).Conversation.ID.Eq(convID)).First()
		return err == nil && row.ClaudeSessionID != ""
	})
}

func TestProcessDeathTerminates(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "crash", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
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
}

// TestTerminateBroadcastsReason 主动终结：预设原因随 terminated 帧广播；幂等与 no-op 防御。
func TestTerminateBroadcastsReason(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "happy", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))

	const reason = "回合长时间无事件无响应，已强制终结会话"
	mgr.Terminate(issueID, reason)
	terminated := waitFrame(t, frames, "terminated 帧", func(f Frame) bool { return f.Type == FrameTerminated })
	if terminated.Status.Status != StatusTerminated || terminated.Status.Error != reason {
		t.Fatalf("terminated 帧应携带预设原因，got %+v", terminated.Status)
	}

	// 幂等：已终态再 Terminate 与未知 issue 均为 no-op。
	mgr.Terminate(issueID, "again")
	mgr.Terminate("no-such-issue", "x")
}

func TestPromptTurnGateAndCancel(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "slow-cancel", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
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
	/*
	 * 等首个 tick（acp 层 cancel 必已注册）再取消——过早 Cancel 会在后台回合尚未注册时
	 * 静默丢失。
	 */
	waitFirstTick(t, frames)
	if err := mgr.Cancel(issueID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	ended := waitFrame(t, frames, "cancelled 终态", func(f Frame) bool { return f.Type == FrameTurnEnded })
	if ended.Turn.StopReason != string(acp1.StopReasonCancelled) {
		t.Fatalf("取消应 cancelled 收敛，got %+v", ended.Turn)
	}
	/*
	 * 取消后回合闸门释放：再受理成功即证明（fakeagent 的 canceled 标记残留，第二轮
	 * 不再推 tick、立即以 cancelled 收敛）。
	 */
	if err := mgr.Prompt(issueID, "再来一轮"); err != nil {
		t.Fatalf("取消后应可再受理回合，got %v", err)
	}
	ended2 := waitFrame(t, frames, "第二轮 cancelled", func(f Frame) bool { return f.Type == FrameTurnEnded })
	if ended2.Turn.StopReason != string(acp1.StopReasonCancelled) {
		t.Fatalf("第二轮应 cancelled 收敛，got %+v", ended2.Turn)
	}
}

func TestRespondPermissionFlow(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "permission", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
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
	if err := mgr.RespondPermission(issueID, pendingID, "bogus", PendingSourcePanel); err == nil {
		t.Fatal("未知 optionId 应拒绝")
	}
	if pendings := mgr.Get(issueID).Pendings; len(pendings) != 1 {
		t.Fatalf("未知选项不应消耗挂起，got %+v", pendings)
	}
	/*
	 * 合法应答 → pendingClosed → 回合 end_turn 收敛 + 决策回显条目。source=bot 模拟
	 * IM 快路径先到（first-writer-wins 先手方）。
	 */
	if err := mgr.RespondPermission(issueID, pendingID, "allow", PendingSourceBot); err != nil {
		t.Fatalf("RespondPermission: %v", err)
	}
	closed := waitFrame(t, frames, "pendingClosed", func(f Frame) bool {
		return f.Type == FramePendingClosed && f.PendingID == pendingID
	})
	if closed.PendingID != pendingID {
		t.Fatalf("pendingClosed 应带 pendingId，got %d", closed.PendingID)
	}
	/*
	 * 决策回显条目（pump 广播）与回合终态（回合收尾 goroutine 广播）并发，到达顺序
	 * 不定：双谓词合并等待，不做顺序假设。
	 */
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
			if frame.Type == FrameTurnEnded && frame.Turn.StopReason == string(acp1.StopReasonEndTurn) {
				sawEnd = true
			}
		case <-deadline:
			t.Fatalf("等待决策回显/回合终态超时（sawEcho=%v sawEnd=%v）", sawEcho, sawEnd)
		}
	}
	if pendings := mgr.Get(issueID).Pendings; len(pendings) != 0 {
		t.Fatalf("应答后挂起应清空，got %+v", pendings)
	}
	/*
	 * 重复应答（后到方，source=panel）：命中 closed-history 回判别哨兵，文案带先手方
	 * 入口与选项（T2.4 收敛语义——「已在另一端处理」而非笼统的「不存在」）。
	 */
	err := mgr.RespondPermission(issueID, pendingID, "allow", PendingSourcePanel)
	if !errors.Is(err, ErrPendingAlreadyHandled) {
		t.Fatalf("后到方应答应回 ErrPendingAlreadyHandled，got %v", err)
	}
	var handled *PendingAlreadyHandledError
	if !errors.As(err, &handled) || handled.Source != PendingSourceBot ||
		handled.Label != "允许" || handled.Settled {
		t.Fatalf("判别详情应记先手方 bot/允许，got %+v", handled)
	}
	if msg := err.Error(); !strings.Contains(msg, "IM") || !strings.Contains(msg, "允许") {
		t.Fatalf("后到方文案应带入口与选项，got %q", msg)
	}
}

func TestRespondElicitationFlow(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "elicitation", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	if err := mgr.Prompt(issueID, "要一个输入"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	opened := waitFrame(t, frames, "pendingOpened", func(f Frame) bool { return f.Type == FramePendingOpened })
	// 投影全保真：wire 经拦截层到快照，requestedSchema 不被上游生成模型丢弃。
	if opened.Pending == nil || opened.Pending.Kind != "elicitation" || opened.Pending.Request == nil {
		t.Fatalf("pendingOpened 应带 elicitation wire 投影，got %+v", opened.Pending)
	}
	wire := opened.Pending.Request
	if wire.Mode != "form" || wire.Message != "请选择一个选项" || wire.SessionID == "" {
		t.Fatalf("wire 投影字段不符，got %+v", wire)
	}
	if !strings.Contains(string(wire.RequestedSchema), `"choice"`) {
		t.Fatalf("requestedSchema 应全保真透传，got %s", wire.RequestedSchema)
	}
	pendingID := opened.Pending.PendingID
	// accept 带内容应答 → pendingClosed → 内容回显条目 + 回合终态（并发到达，双谓词合流）。
	if err := mgr.RespondElicitation(issueID, pendingID, "accept", map[string]any{"choice": "b"}, PendingSourcePanel); err != nil {
		t.Fatalf("RespondElicitation: %v", err)
	}
	closed := waitFrame(t, frames, "pendingClosed", func(f Frame) bool {
		return f.Type == FramePendingClosed && f.PendingID == pendingID
	})
	if closed.PendingID != pendingID {
		t.Fatalf("pendingClosed 应带 pendingId，got %d", closed.PendingID)
	}
	var sawEcho, sawEnd bool
	deadline := time.After(20 * time.Second)
	for !sawEcho || !sawEnd {
		select {
		case frame, ok := <-frames:
			if !ok {
				t.Fatal("帧通道关闭，等待内容回显/回合终态")
			}
			if frame.Type == FrameEntry && frame.Entry != nil && strings.Contains(frame.Entry.Text, "elicitation:accept:choice=b") {
				sawEcho = true
			}
			if frame.Type == FrameTurnEnded && frame.Turn.StopReason == string(acp1.StopReasonEndTurn) {
				sawEnd = true
			}
		case <-deadline:
			t.Fatalf("等待内容回显/回合终态超时（sawEcho=%v sawEnd=%v）", sawEcho, sawEnd)
		}
	}
	if pendings := mgr.Get(issueID).Pendings; len(pendings) != 0 {
		t.Fatalf("应答后挂起应清空，got %+v", pendings)
	}
	// 非法 action 拒绝（服务入口已 oneof 校验，此处覆盖 manager 直调路径）。
	if err := mgr.RespondElicitation(issueID, pendingID, "bogus", nil, PendingSourcePanel); err == nil {
		t.Fatal("非法 action 应拒绝")
	}
	// 后到方应答（回合已收敛、挂起已摘）：判别哨兵 + 「表单」名词。
	err := mgr.RespondElicitation(issueID, pendingID, "decline", nil, PendingSourceBot)
	if !errors.Is(err, ErrPendingAlreadyHandled) {
		t.Fatalf("后到方 elicitation 应答应回哨兵，got %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "桌面端") || !strings.Contains(msg, "接受") {
		t.Fatalf("后到方文案应带先手方与动作，got %q", msg)
	}
}

// kind 错配拒绝：跨类型应答在摘除前拒绝（nil 槽位不 panic、不消耗挂起），双向覆盖。
func TestRespondKindMismatch(t *testing.T) {
	mgrP, _, issueP, framesP, _ := newManagerWithIssue(t, "permission", nil)
	if _, err := mgrP.Ensure(context.Background(), issueP, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, framesP, "ready", statusFrameIs(StatusReady))
	if err := mgrP.Prompt(issueP, "需要权限"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	openedP := waitFrame(t, framesP, "pendingOpened", func(f Frame) bool { return f.Type == FramePendingOpened })
	if openedP.Pending == nil {
		t.Fatal("pendingOpened 应带投影")
	}
	if err := mgrP.RespondElicitation(issueP, openedP.Pending.PendingID, "accept", nil, PendingSourcePanel); err == nil {
		t.Fatal("elicitation 应答打 permission 挂起应拒绝")
	}
	if pendings := mgrP.Get(issueP).Pendings; len(pendings) != 1 {
		t.Fatalf("错配应答不应消耗挂起，got %+v", pendings)
	}

	mgrE, _, issueE, framesE, _ := newManagerWithIssue(t, "elicitation", nil)
	if _, err := mgrE.Ensure(context.Background(), issueE, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, framesE, "ready", statusFrameIs(StatusReady))
	if err := mgrE.Prompt(issueE, "要一个输入"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	openedE := waitFrame(t, framesE, "pendingOpened", func(f Frame) bool { return f.Type == FramePendingOpened })
	if openedE.Pending == nil {
		t.Fatal("pendingOpened 应带投影")
	}
	if err := mgrE.RespondPermission(issueE, openedE.Pending.PendingID, "allow", PendingSourcePanel); err == nil {
		t.Fatal("permission 应答打 elicitation 挂起应拒绝")
	}
	if pendings := mgrE.Get(issueE).Pendings; len(pendings) != 1 {
		t.Fatalf("错配应答不应消耗挂起，got %+v", pendings)
	}
}

/*
 * 回合收尾结算后的迟到应答（T2.4）：endTurn 把残余挂起登记为 settled——迟到方拿到
 * 「已随回合结束自动结算」判别哨兵，而非笼统的「不存在或已关闭」。
 */
func TestRespondAfterTurnSettled(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "permission", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	if err := mgr.Prompt(issueID, "需要权限"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	opened := waitFrame(t, frames, "pendingOpened", func(f Frame) bool { return f.Type == FramePendingOpened })
	pendingID := opened.Pending.PendingID
	/*
	 * pendingOpened 已证明回合与挂起注册在案（permission 脚本无 tick，无需等 tick），
	 * 直接软取消，回合以 cancelled 收敛。
	 */
	if err := mgr.Cancel(issueID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waitFrame(t, frames, "cancelled 终态", func(f Frame) bool { return f.Type == FrameTurnEnded })
	err := mgr.RespondPermission(issueID, pendingID, "allow", PendingSourceBot)
	if !errors.Is(err, ErrPendingAlreadyHandled) {
		t.Fatalf("回合结束后迟到应答应回哨兵，got %v", err)
	}
	var handled *PendingAlreadyHandledError
	if !errors.As(err, &handled) || !handled.Settled {
		t.Fatalf("迟到应答应判 settled，got %+v", handled)
	}
	if msg := err.Error(); !strings.Contains(msg, "随回合结束自动结算") {
		t.Fatalf("迟到应答文案应带结算语义，got %q", msg)
	}
}

func TestDiscardCascade(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "happy", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	mgr.Discard(issueID)
	// 会话终结帧（ready 态终结帧经 pump 哨兵路径到达）。
	waitFrame(t, frames, "terminated 帧", func(f Frame) bool { return f.Type == FrameTerminated })
	// 幂等：二次 Discard 无害；Discard 后 Get 回 idle。
	mgr.Discard(issueID)
	if snap := mgr.Get(issueID); snap.Status != StatusIdle {
		t.Fatalf("Discard 后应为 idle，got %s", snap.Status)
	}
}

// —— T2.2 per-session 跨入口回合锁（PromptQueued 排队受理）——

/*
 * queuedWaiters 测试观察点：等位队列当前长度（集成测试以「入队完成」做确定性时序同步，
 * 不依赖 sleep）。
 */
func queuedWaiters(mgr *Manager, issueID string) int {
	mgr.mu.Lock()
	entry, ok := mgr.entries[issueID]
	mgr.mu.Unlock()
	if !ok {
		return 0
	}
	entry.turnQueue.mu.Lock()
	defer entry.turnQueue.mu.Unlock()
	return len(entry.turnQueue.waiters)
}

// queueResult 收排队受理结果（超时失败，防测试悬挂）。
func queueResult(t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("等待 %s 超时", what)
		return nil
	}
}

/*
 * TestPromptQueuedAutoStartAfterTurn 主链路：活动回合中排队受理阻塞等位（不开新回合），
 * 回合终态释放槽位后自动开跑（受理式：受理即返回，终态经帧流/视图可见）。
 */
func TestPromptQueuedAutoStartAfterTurn(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "slow-cancel", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	if err := mgr.Prompt(issueID, "第一轮"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	queued := make(chan error, 1)
	go func() {
		queued <- mgr.PromptQueued(context.Background(), issueID, "排队消息", PromptMeta{Source: EntrySourceBot})
	}()
	// 等位入队（确定性时序同步）后：等位期间不受理新回合（用户条目不落视图）。
	waitForCondition(t, "等位入队", func() bool { return queuedWaiters(mgr, issueID) == 1 })
	snap := mgr.Get(issueID)
	if !snap.TurnActive {
		t.Fatalf("第一轮应仍在进行，got %+v", snap)
	}
	for _, e := range snap.Entries {
		if e.Text == "排队消息" {
			t.Fatalf("等位期间不应受理新回合，got %+v", snap.Entries)
		}
	}
	// 等首个 tick（acp 层 cancel 必已注册）再取消——过早取消会静默丢失（既有惯例）。
	waitFirstTick(t, frames)
	if err := mgr.Cancel(issueID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waitFrame(t, frames, "第一轮 cancelled 终态", func(f Frame) bool { return f.Type == FrameTurnEnded })
	// 槽位释放 → 等位者自动受理开跑（canceled 标记残留，第二轮立即 cancelled 收敛）。
	if err := queueResult(t, queued, "排队受理自动开跑"); err != nil {
		t.Fatalf("排队受理应在回合结束后自动开跑: %v", err)
	}
	waitForCondition(t, "排队回合条目落视图", func() bool {
		snap := mgr.Get(issueID)
		return !snap.TurnActive && len(snap.Entries) >= 2 && snap.Entries[len(snap.Entries)-1].Text == "排队消息"
	})
	/*
	 * 双入口来源装配（T1.1）：Prompt 恒 panel，PromptQueued 透传 meta.Source（首轮
	 * cancelled 前的 tick 条目落在两 user 条目之间，按文本定位各自的 user 条目）。
	 */
	sourceOf := func(text string) string {
		for _, e := range mgr.Get(issueID).Entries {
			if e.Kind == entryKindUser && e.Text == text {
				return e.Source
			}
		}
		return ""
	}
	if sourceOf("第一轮") != EntrySourcePanel || sourceOf("排队消息") != EntrySourceBot {
		t.Fatalf("user 条目来源应按入口装配（panel/bot），got %+v", mgr.Get(issueID).Entries)
	}
}

/*
 * TestPromptQueuedFIFOOrder 两个等位者按入队序依次开跑：A 先受理，其回合终态释放后 B
 * 才受理（用户条目落视图顺序即受理顺序）。
 */
func TestPromptQueuedFIFOOrder(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "slow-cancel", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	if err := mgr.Prompt(issueID, "第一轮"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	queuedA := make(chan error, 1)
	queuedB := make(chan error, 1)
	go func() {
		queuedA <- mgr.PromptQueued(context.Background(), issueID, "排队A", PromptMeta{Source: EntrySourceBot})
	}()
	waitForCondition(t, "A 入队", func() bool { return queuedWaiters(mgr, issueID) == 1 })
	go func() {
		queuedB <- mgr.PromptQueued(context.Background(), issueID, "排队B", PromptMeta{Source: EntrySourceBot})
	}()
	waitForCondition(t, "B 入队", func() bool { return queuedWaiters(mgr, issueID) == 2 })
	waitFirstTick(t, frames)
	if err := mgr.Cancel(issueID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waitFrame(t, frames, "第一轮 cancelled 终态", func(f Frame) bool { return f.Type == FrameTurnEnded })
	if err := queueResult(t, queuedA, "A 受理"); err != nil {
		t.Fatalf("A 应自动开跑: %v", err)
	}
	if err := queueResult(t, queuedB, "B 受理"); err != nil {
		t.Fatalf("B 应在 A 终态后自动开跑: %v", err)
	}
	entryIndex := func(snap ViewSnapshot, text string) int {
		for i, e := range snap.Entries {
			if e.Text == text {
				return i
			}
		}
		return -1
	}
	waitForCondition(t, "受理顺序 FIFO 落视图", func() bool {
		snap := mgr.Get(issueID)
		ia, ib := entryIndex(snap, "排队A"), entryIndex(snap, "排队B")
		return !snap.TurnActive && ia > 0 && ib > ia
	})
}

/*
 * TestPromptQueuedContextCancelSelfRemoves ctx 取消自摘：等位者取消后出队不占槽——回合
 * 结束的 release 不误投（若自摘失败，幽灵等位者会在释放后开跑并落出「排队消息」条目）。
 */
func TestPromptQueuedContextCancelSelfRemoves(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "slow-cancel", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	if err := mgr.Prompt(issueID, "第一轮"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() { queued <- mgr.PromptQueued(ctx, issueID, "排队消息", PromptMeta{Source: EntrySourceBot}) }()
	waitForCondition(t, "等位入队", func() bool { return queuedWaiters(mgr, issueID) == 1 })
	cancel()
	if err := queueResult(t, queued, "ctx 取消退出"); err == nil || !strings.Contains(err.Error(), "取消") {
		t.Fatalf("ctx 取消应报错退出: %v", err)
	}
	waitForCondition(t, "自摘出队", func() bool { return queuedWaiters(mgr, issueID) == 0 })
	// 回合结束后释放槽位：闸门空闲（即时受理成功即证）且无幽灵回合条目。
	waitFirstTick(t, frames)
	if err := mgr.Cancel(issueID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waitFrame(t, frames, "第一轮 cancelled 终态", func(f Frame) bool { return f.Type == FrameTurnEnded })
	if err := mgr.Prompt(issueID, "手动轮"); err != nil {
		t.Fatalf("取消后闸门应空闲可受理: %v", err)
	}
	waitForCondition(t, "手动轮受理落视图", func() bool {
		snap := mgr.Get(issueID)
		return !snap.TurnActive && len(snap.Entries) >= 2 && snap.Entries[len(snap.Entries)-1].Text == "手动轮"
	})
	for _, e := range mgr.Get(issueID).Entries {
		if e.Text == "排队消息" {
			t.Fatalf("幽灵等位者不应被唤醒开跑: %+v", mgr.Get(issueID).Entries)
		}
	}
}

/*
 * TestPromptQueuedDiscardAborts 等位中会话删除：close 广播 → 受理失败退出（回合未发生，
 * 语义 = 受理被拒）。
 */
func TestPromptQueuedDiscardAborts(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "slow-cancel", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	if err := mgr.Prompt(issueID, "第一轮"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	queued := make(chan error, 1)
	go func() {
		queued <- mgr.PromptQueued(context.Background(), issueID, "排队消息", PromptMeta{Source: EntrySourceBot})
	}()
	waitForCondition(t, "等位入队", func() bool { return queuedWaiters(mgr, issueID) == 1 })
	mgr.Discard(issueID)
	if err := queueResult(t, queued, "等位中 Discard 退出"); err == nil || !strings.Contains(err.Error(), "终结") {
		t.Fatalf("等位中会话删除应报错退出: %v", err)
	}
}

/*
 * TestPromptQueuedReadyRecheckAfterWake 等位唤醒后的就绪复验（白盒）：等位期间会话终结
 * 收尾完成（白盒置态模拟 pump 哨兵路径），槽位释放唤醒后旧 entry 不得续接受理。
 */
func TestPromptQueuedReadyRecheckAfterWake(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "slow-cancel", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	if err := mgr.Prompt(issueID, "第一轮"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	queued := make(chan error, 1)
	go func() {
		queued <- mgr.PromptQueued(context.Background(), issueID, "排队消息", PromptMeta{Source: EntrySourceBot})
	}()
	waitForCondition(t, "等位入队", func() bool { return queuedWaiters(mgr, issueID) == 1 })
	mgr.mu.Lock()
	entry := mgr.entries[issueID]
	mgr.mu.Unlock()
	entry.mu.Lock()
	entry.state = StatusTerminated
	entry.mu.Unlock()
	entry.turnQueue.release()
	if err := queueResult(t, queued, "唤醒后复验退出"); err == nil || !strings.Contains(err.Error(), "未就绪") {
		t.Fatalf("等位中终结的旧 entry 不得续接受理: %v", err)
	}
}

/*
 * TestPromptQueuedDeathReleasesAllWaiters 进程死亡释放全部等位者（回归锁）：终结收尾
 * 必须关等位队列——release 只弹队首，不 close 会让其余等位者无限滞留（堵死编排器会话
 * 队列并占住全局并发槽位）。
 */
func TestPromptQueuedDeathReleasesAllWaiters(t *testing.T) {
	mgr, _, issueID, frames, _ := newManagerWithIssue(t, "slow-cancel", nil)
	if _, err := mgr.Ensure(context.Background(), issueID, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitFrame(t, frames, "ready", statusFrameIs(StatusReady))
	if err := mgr.Prompt(issueID, "第一轮"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	queuedB := make(chan error, 1)
	queuedC := make(chan error, 1)
	go func() {
		queuedB <- mgr.PromptQueued(context.Background(), issueID, "排队B", PromptMeta{Source: EntrySourceBot})
	}()
	waitForCondition(t, "B 入队", func() bool { return queuedWaiters(mgr, issueID) == 1 })
	go func() {
		queuedC <- mgr.PromptQueued(context.Background(), issueID, "排队C", PromptMeta{Source: EntrySourceBot})
	}()
	waitForCondition(t, "C 入队", func() bool { return queuedWaiters(mgr, issueID) == 2 })
	// 走真实终结收尾路径（置态 + close 队列 + 落因 + 广播）；entry 尚 ready 态不被幂等挡回。
	mgr.mu.Lock()
	entry := mgr.entries[issueID]
	mgr.mu.Unlock()
	mgr.finishTerminated(entry)
	for name, ch := range map[string]<-chan error{"B": queuedB, "C": queuedC} {
		if err := queueResult(t, ch, name+" 随终结退出"); err == nil || !strings.Contains(err.Error(), "终结") {
			t.Fatalf("等位者 %s 应随会话终结报错退出: %v", name, err)
		}
	}
}

/*
 * TestBindingStoreConversationAnchor conv- 键的窄桩契约：GetOrCreate 校验会话行在场
 * （缺失报错不建行）；锚读写 SaveReady/SaveError/Delete 窄桩期恒 no-op 返回 nil。
 */
func TestBindingStoreConversationAnchor(t *testing.T) {
	db := newTestDB(t)
	store := &BindingStore{DB: db}
	ctx := context.Background()
	key := ConversationSessionKey(seedBotConversation(t, db, "ws-conv"))

	if err := store.GetOrCreate(ctx, key); err != nil {
		t.Fatalf("行在场应通过: %v", err)
	}
	if err := store.GetOrCreate(ctx, ConversationSessionKey("no-such-row")); err == nil {
		t.Fatal("行缺失应报错（不自动建锚行）")
	}
	if err := store.SaveReady(ctx, key, "claude-acp", "sess-conv"); err != nil {
		t.Fatalf("SaveReady 窄桩应 no-op: %v", err)
	}
	if peeked, err := store.Peek(ctx, key); err != nil || peeked != "" {
		t.Fatalf("Peek 窄桩期应恒空串: %v %q", err, peeked)
	}
	if err := store.SaveError(ctx, key, "claude-acp", "boom"); err != nil {
		t.Fatalf("SaveError 窄桩应 no-op: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete 窄桩应 no-op: %v", err)
	}
}

// launchACPClient 拉起指定脚本 fakeagent（决策函数直测用；acp 包内同款 helper 不跨包）。
func launchACPClient(t *testing.T, script string) *acp.AgentClient {
	t.Helper()
	client, err := acp.Launch(context.Background(), acp.SpawnConfig{
		Command: []string{fakeAgentBin, script},
		Cwd:     t.TempDir(),
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("Launch(%s): %v", script, err)
	}
	t.Cleanup(client.Close)
	return client
}

/*
 * TestNewOrResumedSession 建/恢复决策三态：空锚走 NewSession（fake 自生成 id）；锚点恢复
 * 以请求 id 注册；能力未声明降级 NewSession（id 为 fake 自生成，非请求值）。
 */
func TestNewOrResumedSession(t *testing.T) {
	ctx := context.Background()
	cfg := SessionConfig{Cwd: t.TempDir()}
	log := zap.NewNop()

	t.Run("空锚走NewSession", func(t *testing.T) {
		session, resumed, err := newOrResumedSession(ctx, launchACPClient(t, "happy"), cfg, "", log)
		if err != nil {
			t.Fatalf("NewSession: %v", err)
		}
		if resumed {
			t.Fatal("空锚不应标记 resumed")
		}
		if session.ID() == "sess-anchor" {
			t.Fatalf("空锚应自生成新 id，got %q", session.ID())
		}
	})

	t.Run("锚点恢复以请求id注册", func(t *testing.T) {
		session, resumed, err := newOrResumedSession(ctx, launchACPClient(t, "load"), cfg, "sess-anchor", log)
		if err != nil {
			t.Fatalf("LoadSession: %v", err)
		}
		if !resumed {
			t.Fatal("LoadSession 成功应标记 resumed")
		}
		if session.ID() != "sess-anchor" {
			t.Fatalf("恢复应以请求 id 注册，got %q", session.ID())
		}
	})

	t.Run("能力未声明降级NewSession", func(t *testing.T) {
		session, resumed, err := newOrResumedSession(ctx, launchACPClient(t, "happy"), cfg, "sess-anchor", log)
		if err != nil {
			t.Fatalf("降级 NewSession: %v", err)
		}
		if resumed {
			t.Fatal("降级不应标记 resumed")
		}
		if session.ID() == "sess-anchor" {
			t.Fatalf("降级应全新会话（非请求 id），got %q", session.ID())
		}
	})
}

/*
 * TestBindingStoreLifecycle 会话实体锚生命周期：GetOrCreate 校验行在场（不代建，实体行归
 * conversation CRUD 管理）、SaveReady 落锚（agentCode + claudeSessionId）、Peek 读回、
 * SaveError 清锚、Delete 清锚保行。
 */
func TestBindingStoreLifecycle(t *testing.T) {
	db := newTestDB(t)
	store := &BindingStore{DB: db}
	ctx := context.Background()

	if err := store.GetOrCreate(ctx, ConversationSessionKey("no-such-conv")); err == nil {
		t.Fatal("行不存在应报错")
	}
	taskID := seedIssue(t, db, t.TempDir(), `{"mode":"acp"}`, "")
	convID := seedTaskConversation(t, db, taskID)
	key := ConversationSessionKey(convID)
	if err := store.GetOrCreate(ctx, key); err != nil {
		t.Fatalf("行在场应通过: %v", err)
	}
	convRow := func() *model.Conversation {
		t.Helper()
		row, err := queryOf(db).Conversation.WithContext(ctx).Where(queryOf(db).Conversation.ID.Eq(convID)).First()
		if err != nil {
			t.Fatalf("读会话行: %v", err)
		}
		return row
	}
	if err := store.SaveReady(ctx, key, "claude-acp", "sess-9"); err != nil {
		t.Fatalf("SaveReady: %v", err)
	}
	if row := convRow(); row.ClaudeSessionID != "sess-9" || row.AgentCode != "claude-acp" {
		t.Fatalf("SaveReady 落锚: %+v", row)
	}
	peeked, err := store.Peek(ctx, key)
	if err != nil || peeked != "sess-9" {
		t.Fatalf("Peek 读回锚: %v %q", err, peeked)
	}
	if err := store.SaveError(ctx, key, "claude-acp", "握手失败"); err != nil {
		t.Fatalf("SaveError: %v", err)
	}
	if row := convRow(); row.ClaudeSessionID != "" {
		t.Fatalf("SaveError 应清锚: %+v", row)
	}
	if err := store.SaveReady(ctx, key, "claude-acp", "sess-10"); err != nil {
		t.Fatalf("重落锚: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if row := convRow(); row.ClaudeSessionID != "" {
		t.Fatalf("Delete 应清锚保行: %+v", row)
	}
}

/*
 * TestEnsureResumeAnchorPassthrough 锚点接线：非空锚 → spawn 收到原 id 且恢复同 id 会话；
 * 空锚 → spawn 收到空串（全新会话）。
 */
func TestEnsureResumeAnchorPassthrough(t *testing.T) {
	cases := []struct {
		name       string
		seedID     string
		script     string
		wantResume string
	}{
		{"非空锚恢复", "sess-anchor-1", "load", "sess-anchor-1"},
		{"空锚全新会话", "", "happy", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			taskID := seedIssue(t, db, t.TempDir(), `{"mode":"acp"}`, "")
			key := ConversationSessionKey(seedTaskConversation(t, db, taskID))
			mgr, err := NewManager(db, Dirs{}, 0, "", "", zap.NewNop())
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			t.Cleanup(mgr.StopAll)
			if tc.seedID != "" {
				// 锚写入由 SaveReady 承担（行已由种子就绪，update-only 直写）。
				if err := (&BindingStore{DB: db}).SaveReady(context.Background(), key, "claude-acp", tc.seedID); err != nil {
					t.Fatalf("种锚: %v", err)
				}
			}
			calls := &atomic.Int64{}
			var resumeIDs []string
			old := spawnFunc
			spawnFunc = fakeSpawn(tc.script, nil, calls, &resumeIDs)
			t.Cleanup(func() { spawnFunc = old })
			if _, err := mgr.Ensure(context.Background(), key, ""); err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			waitForCondition(t, "会话就绪", func() bool {
				return mgr.Get(key).Status == StatusReady
			})
			if n := len(resumeIDs); n != 1 {
				t.Fatalf("spawn 应恰好一次，got %d", n)
			}
			if resumeIDs[0] != tc.wantResume {
				t.Fatalf("spawn 应收到 resumeID %q，got %q", tc.wantResume, resumeIDs[0])
			}
			snap := mgr.Get(key)
			if tc.wantResume == "" {
				if snap.AcpSessionID == "" {
					t.Fatal("空锚全新会话的快照 sessionId 不应为空")
				}
				return
			}
			if snap.AcpSessionID != tc.wantResume {
				t.Fatalf("就绪快照 sessionId 应为 %q，got %q", tc.wantResume, snap.AcpSessionID)
			}
		})
	}
}

/*
 * TestEnsureResumeFallbackNotice R2 显式提示：带锚受理但恢复未发生（fake 强制 resumed=false），
 * 就绪后快照应含一条 system 条目告知「历史会话不可恢复」。
 */
func TestEnsureResumeFallbackNotice(t *testing.T) {
	db := newTestDB(t)
	taskID := seedIssue(t, db, t.TempDir(), `{"mode":"acp"}`, "")
	key := ConversationSessionKey(seedTaskConversation(t, db, taskID))
	mgr, err := NewManager(db, Dirs{}, 0, "", "", zap.NewNop())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.StopAll)
	if err := (&BindingStore{DB: db}).SaveReady(context.Background(), key, "claude-acp", "sess-gone"); err != nil {
		t.Fatalf("种锚: %v", err)
	}
	calls := &atomic.Int64{}
	old := spawnFunc
	spawnFunc = func(cfg SessionConfig, dirs Dirs, port int, log *zap.Logger, resumeID string) (*acp.AgentClient, *acp.Session, bool, error) {
		client, session, _, err := fakeSpawn("happy", nil, calls, nil)(cfg, dirs, port, log, resumeID)
		return client, session, false, err
	}
	t.Cleanup(func() { spawnFunc = old })
	if _, err := mgr.Ensure(context.Background(), key, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitForCondition(t, "会话就绪", func() bool { return mgr.Get(key).Status == StatusReady })
	snap := mgr.Get(key)
	if len(snap.Entries) != 1 || snap.Entries[0].Kind != "system" || !strings.Contains(snap.Entries[0].Text, "历史会话不可恢复") {
		t.Fatalf("降级应落 system 条目: %+v", snap.Entries)
	}
}
