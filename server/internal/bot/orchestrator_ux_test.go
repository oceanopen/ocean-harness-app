package bot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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

// recordingDriver 捕获 TurnRequest 的即返回合替身（freshSession/会话锚传递断言用）。
type recordingDriver struct {
	mu  sync.Mutex
	req TurnRequest
}

func (d *recordingDriver) RunTurn(_ context.Context, req TurnRequest) (<-chan TurnEvent, error) {
	d.mu.Lock()
	d.req = req
	d.mu.Unlock()
	events := make(chan TurnEvent, 1)
	events <- TurnEvent{Type: TurnDone, Result: "ok"}
	close(events)
	return events, nil
}

func (d *recordingDriver) captured() TurnRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.req
}

// presetExpiredSession 预置闲置过期的会话行：主锚 s-prev + 3h 前的最近消息。
func presetExpiredSession(t *testing.T, store *ConversationStore) {
	t.Helper()
	if _, err := store.GetOrCreate("bot-1", "single:u1"); err != nil {
		t.Fatalf("预置会话行: %v", err)
	}
	if err := store.FinishTurn("bot-1", "single:u1", "s-prev", time.Now().Add(-3*time.Hour)); err != nil {
		t.Fatalf("预置会话锚: %v", err)
	}
}

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
 * TestIdleExpiryStartsFreshSession 2h 闲置边界（headless）：过期后先发独立提醒气泡
 * （会话结束 + 新会话标题 + /继续 指引），回合以新会话启动（resume 锚不传）。
 */
func TestIdleExpiryStartsFreshSession(t *testing.T) {
	driver := &recordingDriver{}
	store := &ConversationStore{DB: newIssueTestDB(t)}
	o := NewOrchestrator(store, driver, nil, nil, zap.NewNop())
	presetExpiredSession(t, store)

	rs := &fakeNoticeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "继续优化登录页"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "过期后回合执行", func() bool { return driver.captured().Prompt != "" })

	notices := rs.noticeSnapshot()
	if len(notices) != 1 || !strings.Contains(notices[0], "上一会话已结束") ||
		!strings.Contains(notices[0], "《继续优化登录页》") || !strings.Contains(notices[0], "/继续") {
		t.Fatalf("过期提醒气泡不符: %v", notices)
	}
	if got := driver.captured().SessionID; got != "" {
		t.Fatalf("过期后回合应以新会话启动，got 锚 %q", got)
	}
}

// TestIdleExpiryNotTriggered 未过期（2h 内）不提醒、正常 resume。
func TestIdleExpiryNotTriggered(t *testing.T) {
	driver := &recordingDriver{}
	store := &ConversationStore{DB: newIssueTestDB(t)}
	o := NewOrchestrator(store, driver, nil, nil, zap.NewNop())
	if _, err := store.GetOrCreate("bot-1", "single:u1"); err != nil {
		t.Fatalf("预置会话行: %v", err)
	}
	if err := store.FinishTurn("bot-1", "single:u1", "s-prev", time.Now()); err != nil {
		t.Fatalf("预置会话锚: %v", err)
	}

	rs := &fakeNoticeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "接着说"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "回合执行", func() bool { return driver.captured().Prompt != "" })
	if notices := rs.noticeSnapshot(); len(notices) != 0 {
		t.Fatalf("未过期不得提醒: %v", notices)
	}
	if got := driver.captured().SessionID; got != "s-prev" {
		t.Fatalf("未过期应 resume 既有锚: %q", got)
	}
}

/*
 * TestIdleExpiryACPOnlyNotice ACP 共享会话（桌面共用）不切断：仅提醒「仍在延续」，
 * 锚照常传递（freshSession=false）。
 */
func TestIdleExpiryACPOnlyNotice(t *testing.T) {
	driver := &stubProberDriver{acp: true, bound: "i-1"}
	store := &ConversationStore{DB: newIssueTestDB(t)}
	o := NewOrchestrator(store, driver, nil, nil, zap.NewNop())
	presetExpiredSession(t, store)

	rs := &fakeNoticeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "继续优化登录页"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "ACP 回合执行", func() bool { return driver.callCount() > 0 })

	notices := rs.noticeSnapshot()
	if len(notices) != 1 || !strings.Contains(notices[0], "仍在延续") {
		t.Fatalf("ACP 过期应仅提醒不切断: %v", notices)
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

// TestSlashClear 清空上下文：主锚与备份锚一并清除，下条消息开新会话。
func TestSlashClear(t *testing.T) {
	o, store := newOrchestratorWithDB(t)
	presetExpiredSession(t, store)
	if _, expired, err := store.ExpireIdleSession("bot-1", "single:u1", time.Now()); err != nil || !expired {
		t.Fatalf("预置过期态: expired=%v err=%v", expired, err)
	}

	rs := &fakeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "/clear"}, func() (ReplyStream, error) {
		return rs, nil
	})
	frames := rs.snapshot()
	if len(frames) != 1 || !frames[0].final || !strings.Contains(frames[0].content, "上下文已清空") {
		t.Fatalf("/clear 应单终帧确认: %+v", frames)
	}
	if id, _ := store.SessionID("bot-1", "single:u1"); id != "" {
		t.Fatalf("主锚应清空: %q", id)
	}
	if ok, _ := store.ResumePrevSession("bot-1", "single:u1"); ok {
		t.Fatal("备份锚应一并清除（/继续 不得接回已清空会话）")
	}
}

// TestSlashResume 接回上一会话：备份锚回填主锚；无备份时中性提示。
func TestSlashResume(t *testing.T) {
	o, store := newOrchestratorWithDB(t)
	presetExpiredSession(t, store)
	if _, expired, err := store.ExpireIdleSession("bot-1", "single:u1", time.Now()); err != nil || !expired {
		t.Fatalf("预置过期态: expired=%v err=%v", expired, err)
	}

	rs := &fakeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "/继续"}, func() (ReplyStream, error) {
		return rs, nil
	})
	frames := rs.snapshot()
	if len(frames) != 1 || !frames[0].final || !strings.Contains(frames[0].content, "已接回上一会话") {
		t.Fatalf("/继续 应单终帧确认: %+v", frames)
	}
	if id, _ := store.SessionID("bot-1", "single:u1"); id != "s-prev" {
		t.Fatalf("备份锚应回填主锚: %q", id)
	}

	rs2 := &fakeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-2", ConversationKey: "single:u1", SenderID: "u1", Text: "/继续"}, func() (ReplyStream, error) {
		return rs2, nil
	})
	frames2 := rs2.snapshot()
	if len(frames2) != 1 || frames2[0].content != "没有可接回的上一会话。" {
		t.Fatalf("无备份 /继续 应中性提示: %+v", frames2)
	}
}

/*
 * TestIdleTouchPreventsRepeatNotice 活动时基回归：ACP 无锚回写链路（FinishTurn 不触发）
 * 此前 last_message_at 永久冻结 → 过期判定逐条重放。受理即触碰后，紧随消息不得再提醒。
 */
func TestIdleTouchPreventsRepeatNotice(t *testing.T) {
	driver := &stubProberDriver{acp: true, bound: "i-1"}
	store := &ConversationStore{DB: newIssueTestDB(t)}
	o := NewOrchestrator(store, driver, nil, nil, zap.NewNop())
	presetExpiredSession(t, store)

	rs := &fakeNoticeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "第一条"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "第一条回合执行", func() bool { return driver.callCount() > 0 })
	if notices := rs.noticeSnapshot(); len(notices) != 1 {
		t.Fatalf("过期首条应提醒一次: %v", notices)
	}

	rs2 := &fakeNoticeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-2", ConversationKey: "single:u1", SenderID: "u1", Text: "第二条"}, func() (ReplyStream, error) {
		return rs2, nil
	})
	waitFor(t, "第二条回合执行", func() bool { return driver.callCount() > 1 })
	if notices := rs2.noticeSnapshot(); len(notices) != 0 {
		t.Fatalf("受理触碰后紧随消息不得再提醒: %v", notices)
	}
}

/*
 * TestSlashResumeThenNoReexpire 接回后紧随消息不得二次过期：/继续 顺延时基，恢复的锚
 * 不被闲置判定再次备份（否则接回被架空）。
 */
func TestSlashResumeThenNoReexpire(t *testing.T) {
	driver := &recordingDriver{}
	store := &ConversationStore{DB: newIssueTestDB(t)}
	o := NewOrchestrator(store, driver, nil, nil, zap.NewNop())
	presetExpiredSession(t, store)
	if _, expired, err := store.ExpireIdleSession("bot-1", "single:u1", time.Now()); err != nil || !expired {
		t.Fatalf("预置过期态: expired=%v err=%v", expired, err)
	}

	rsResume := &fakeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "/继续"}, func() (ReplyStream, error) {
		return rsResume, nil
	})
	frames := rsResume.snapshot()
	if len(frames) < 1 || !strings.Contains(frames[0].content, "已接回上一会话") {
		t.Fatalf("/继续 应确认接回: %+v", frames)
	}

	rs := &fakeNoticeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-2", ConversationKey: "single:u1", SenderID: "u1", Text: "接着聊"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "回合执行", func() bool { return driver.captured().Prompt != "" })
	if notices := rs.noticeSnapshot(); len(notices) != 0 {
		t.Fatalf("接回后紧随消息不得再提醒: %v", notices)
	}
	if got := driver.captured().SessionID; got != "s-prev" {
		t.Fatalf("接回的锚应被消费: %q", got)
	}
}

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
