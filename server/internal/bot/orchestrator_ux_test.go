package bot

import (
	"context"
	"errors"
	"fmt"
	"ocean-harness/server/internal/dal/enums"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
)

/*
 * fakeNoticeReplyStream 提示面流替身（SendNotice 记录）：会话边界提醒气泡的断言入口。
 */
type fakeNoticeReplyStream struct {
	fakeReplyStream
	mu      sync.Mutex
	notices []string
}

func (f *fakeNoticeReplyStream) SendNotice(text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notices = append(f.notices, text)
	return nil
}

func (f *fakeNoticeReplyStream) noticeSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.notices...)
}

// syncFailDriver 同步失败替身：RunTurn 即返回 error（spawn 失败 / 受理拒绝两分支的宿主）。
type syncFailDriver struct{ err error }

func (d *syncFailDriver) RunTurn(_ context.Context, _ TurnRequest) (<-chan TurnEvent, error) {
	return nil, d.err
}

// presetSessionAnchor 预置会话行 + 续聊锚（SaveSessionID 不建行，须先 GetOrCreate）。
func presetSessionAnchor(t *testing.T, store *ConversationStore, sessionID string) {
	t.Helper()
	if _, err := store.GetOrCreate("bot-1", "single:u1"); err != nil {
		t.Fatalf("预置会话行: %v", err)
	}
	if err := store.SaveSessionID("bot-1", "single:u1", sessionID); err != nil {
		t.Fatalf("预置会话锚: %v", err)
	}
}

/*
 * blockingDriver 阻塞至 ctx 取消的回合替身：/stop 打断链路的宿主（ctx 取消后发 TurnError，
 * 与 claude 进程被杀同构）。
 */
type blockingDriver struct {
	started chan struct{}
	once    sync.Once
}

func (b *blockingDriver) RunTurn(ctx context.Context, _ TurnRequest) (<-chan TurnEvent, error) {
	b.once.Do(func() { close(b.started) })
	events := make(chan TurnEvent, 1)
	go func() {
		<-ctx.Done()
		events <- TurnEvent{Type: TurnError, Err: fmt.Errorf("claude 回合失败: %w", ctx.Err())}
		close(events)
	}()
	return events, nil
}

// recordingDriver 捕获 TurnRequest 的即返回合替身（门禁/锚链断言用）；resultSessionID 为
// 终态帧回放的会话 id（空 = 不回写锚）。
type recordingDriver struct {
	mu              sync.Mutex
	req             TurnRequest
	resultSessionID string
}

func (d *recordingDriver) RunTurn(_ context.Context, req TurnRequest) (<-chan TurnEvent, error) {
	d.mu.Lock()
	d.req = req
	d.mu.Unlock()
	events := make(chan TurnEvent, 1)
	events <- TurnEvent{Type: TurnDone, Result: "ok", SessionID: d.resultSessionID}
	close(events)
	return events, nil
}

func (d *recordingDriver) captured() TurnRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.req
}

// presetExpiredSession 预置闲置过期的会话行：主锚 s-prev + 3h 前的最近消息。

// finalFrameOf 等待首个终帧并返回帧序（控制指令/被打断回合的终态断言入口）。
func finalFrameOf(t *testing.T, name string, rs ReplyStream) []fakeFrame {
	t.Helper()
	frs := rs.(*fakeReplyStream)
	waitFor(t, name, func() bool {
		for _, f := range frs.snapshot() {
			if f.final {
				return true
			}
		}
		return false
	})
	return frs.snapshot()
}

/*
 * TestIdleExpirySendsNotice 2h 闲置边界：过期后发提醒气泡（共享会话不切断）且回合照常执行。
 */

// TestIdleExpiryNotTriggered 未过期（2h 内）不提醒、正常 resume。

/*
 * TestIdleExpiryACPOnlyNotice 已由 TestIdleExpirySendsNotice 覆盖（ACP 单驱动后恒此语义）；
 * 保留独立用例占位删除。本用例改为验证未过期＋无提醒的组合下不重复提醒。
 */

/*
 * TestRunTurnProjectTaskUnboundGate 项目档未关联任务门禁（D8 提示后拦截）：已 markSeen，
 * 纯指引文本终帧收口，不出回合（nil driver 不 panic 即证明主路径未触达）。
 */
func TestRunTurnProjectTaskUnboundGate(t *testing.T) {
	o, _ := newOrchestratorWithDB(t)
	rs := &fakeReplyStream{}
	o.HandleInbound(BotRuntimeConfig{BotID: "bot-1", ConversationType: enums.CONVERSATION_TYPE_PROJECT, ProjectID: "proj-1", WorkspaceDir: "/tmp/ocean-test", AccessPolicy: openAccess()}, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "跑一下测试"}, func() (ReplyStream, error) {
		return rs, nil
	})
	frames := finalFrameOf(t, "项目档未关联任务门禁", rs)
	if len(frames) != 2 || frames[0].content != "> 收到，处理中…" || frames[1].content != taskUnboundGateLead || !frames[1].final {
		t.Fatalf("项目档未关联任务应单指引终帧: %+v", frames)
	}
}

// TestSlashStopIdle /stop 无在途回合：中性提示终帧（受理现场收口，无占位帧不入队）。
func TestSlashStopIdle(t *testing.T) {
	o, _ := newOrchestratorWithDB(t)
	rs := &fakeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "/stop"}, func() (ReplyStream, error) {
		return rs, nil
	})
	frames := rs.snapshot()
	if len(frames) != 1 || !frames[0].final || frames[0].content != "当前没有正在处理的回复。" {
		t.Fatalf("/stop 空闲应单终帧中性提示: %+v", frames)
	}
}

/*
 * TestSlashStopKillsTurn /stop 全链路：取消在途回合（终帧「⏹ 已停止当前回复。」取消叙事）
 * + 排空队列（被排空消息各自终帧，不再执行）。
 */
func TestSlashStopKillsTurn(t *testing.T) {
	driver := &blockingDriver{started: make(chan struct{})}
	o, store := newOrchestratorWithDB(t)
	o.driver = driver
	cfg := issueCmdCfg()

	rs1 := &fakeReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "跑个任务"}, func() (ReplyStream, error) {
		return rs1, nil
	})
	<-driver.started

	// 在途回合占用中：第二条消息应见排队占位文案并入队挂起。
	rs2 := &fakeReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-2", ConversationKey: "single:u1", SenderID: "u1", Text: "再来一条"}, func() (ReplyStream, error) {
		return rs2, nil
	})
	if frames := rs2.snapshot(); len(frames) != 1 || frames[0].content != "> 收到。当前有任务处理中，排队中，稍后自动继续…" {
		t.Fatalf("忙碌会话占位帧应为排队文案: %+v", frames)
	}

	rs3 := &fakeReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-3", ConversationKey: "single:u1", SenderID: "u1", Text: "/stop"}, func() (ReplyStream, error) {
		return rs3, nil
	})
	frames3 := rs3.snapshot()
	if len(frames3) != 1 || !frames3[0].final || !strings.Contains(frames3[0].content, "已停止当前回复") {
		t.Fatalf("/stop 应单终帧确认: %+v", frames3)
	}
	frames2 := finalFrameOf(t, "被排空消息终帧", rs2)
	if len(frames2) != 2 || frames2[1].content != "⏹ 已停止，本条消息未执行。" || !frames2[1].final {
		t.Fatalf("被排空消息应占位帧 + 停止终帧: %+v", frames2)
	}
	frames1 := finalFrameOf(t, "被打断回合终帧", rs1)
	if frames1[len(frames1)-1].content != "⏹ 已停止当前回复。" {
		t.Fatalf("被打断回合终帧应为取消叙事: %+v", frames1)
	}
	if seen, _ := store.HasSeen("bot-1", "single:u1", "m-3"); !seen {
		t.Fatal("控制指令应 MarkSeen")
	}
}

// TestSlashClear 清空上下文：主锚清除，下条消息开新会话。
func TestSlashClear(t *testing.T) {
	o, store := newOrchestratorWithDB(t)
	presetSessionAnchor(t, store, "s-prev")
	rs := &fakeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "/clear"}, func() (ReplyStream, error) {
		return rs, nil
	})
	frames := rs.snapshot()
	if len(frames) != 1 || !frames[0].final || !strings.Contains(frames[0].content, "上下文已清空") {
		t.Fatalf("/clear 应单终帧确认: %+v", frames)
	}
	if id, _ := store.SessionID("bot-1", "single:u1"); id != "" {
		t.Fatalf("/clear 应清空 headless 会话锚: %q", id)
	}
}

/*
 * TestRunTurnResumesSessionAnchor headless 锚链·读：bot 会话行既有 claude_session_id 随
 * TurnRequest.SessionID 传入引擎（--resume 续聊）。
 */
func TestRunTurnResumesSessionAnchor(t *testing.T) {
	driver := &recordingDriver{}
	o, store := newOrchestratorWithDriver(t, driver)
	presetSessionAnchor(t, store, "s-prev")
	rs := &fakeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "接着说"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "回合执行", func() bool { return driver.captured().Prompt != "" })
	if got := driver.captured().SessionID; got != "s-prev" {
		t.Fatalf("应 resume 既有锚: %q", got)
	}
}

/*
 * TestRunTurnPersistsSessionAnchor headless 锚链·写：终态帧携带的会话 id 回写 bot 会话行
 * （--resume 后 id 可能刷新，每回合以捕获值为准）。
 */
func TestRunTurnPersistsSessionAnchor(t *testing.T) {
	driver := &recordingDriver{resultSessionID: "s-new"}
	o, store := newOrchestratorWithDriver(t, driver)
	rs := &fakeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "开场"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "锚回写", func() bool {
		id, _ := store.SessionID("bot-1", "single:u1")
		return id == "s-new"
	})
}

/*
 * TestRunTurnSpawnFailureHealsAnchor headless 失败自愈：spawn 失败且本轮带 resume 尝试 →
 * 清空锚点（下条消息开新会话）；受理拒绝（turnNotAcceptedError，回合未发生）不清锚。
 */
func TestRunTurnSpawnFailureHealsAnchor(t *testing.T) {
	store := &ConversationStore{DB: newIssueTestDB(t)}
	presetSessionAnchor(t, store, "s-prev")
	o := NewOrchestrator(store, &syncFailDriver{err: errors.New("claude 启动失败")}, nil, nil, zap.NewNop())
	rs := &fakeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "跑一下"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "失败终帧", func() bool {
		for _, f := range rs.snapshot() {
			if f.final {
				return true
			}
		}
		return false
	})
	if id, _ := store.SessionID("bot-1", "single:u1"); id != "" {
		t.Fatalf("spawn 失败应清空锚点自愈: %q", id)
	}

	// 受理拒绝：回合未发生，锚保持原值。
	presetSessionAnchor(t, store, "s-prev")
	o2 := NewOrchestrator(store, &syncFailDriver{err: turnNotAcceptedError{errors.New("ACP 引导失败")}}, nil, nil, zap.NewNop())
	rs2 := &fakeReplyStream{}
	o2.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-2", ConversationKey: "single:u1", SenderID: "u1", Text: "再跑"}, func() (ReplyStream, error) {
		return rs2, nil
	})
	waitFor(t, "受理拒绝终帧", func() bool {
		for _, f := range rs2.snapshot() {
			if f.final {
				return true
			}
		}
		return false
	})
	if id, _ := store.SessionID("bot-1", "single:u1"); id != "s-prev" {
		t.Fatalf("受理拒绝不得清锚: %q", id)
	}
}

/*
 * TestIdleTouchPreventsRepeatNotice 活动时基回归：ACP 无锚回写链路（FinishTurn 不触发）
 * 此前 last_message_at 永久冻结 → 过期判定逐条重放。受理即触碰后，紧随消息不得再提醒。
 */

// TestUnknownSlashCommandReply 未识别 slash 指令：占位帧后指回 /help 终帧（不出回合）。
func TestUnknownSlashCommandReply(t *testing.T) {
	o, _ := newOrchestratorWithDB(t)
	rs := &fakeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "/helpp"}, func() (ReplyStream, error) {
		return rs, nil
	})
	frames := finalFrameOf(t, "未识别指令终帧", rs)
	if len(frames) != 2 || frames[0].content != "> 收到，处理中…" ||
		frames[1].content != "未识别的指令。回复 /help 查看支持的指令。" || !frames[1].final {
		t.Fatalf("未识别指令应占位帧 + 指引终帧: %+v", frames)
	}
}
