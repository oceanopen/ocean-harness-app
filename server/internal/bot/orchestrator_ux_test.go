package bot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ocean-harness/server/internal/appdb"
	"ocean-harness/server/internal/dal/enums"

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

// headlessAnchorOf 读 ux 会话的 active 续聊锚（空 = 无锚）。
func headlessAnchorOf(t *testing.T, store *ConversationStore) string {
	t.Helper()
	resume, _, err := store.HeadlessAnchor(uxCfg(), "single:u1")
	if err != nil {
		t.Fatalf("读续聊锚: %v", err)
	}
	return resume
}

// uxCfg ux 用例的工作空间档运行配置（幂等键 bot-1/single:u1 的行 owner）。
func uxCfg() BotRuntimeConfig {
	return BotRuntimeConfig{BotID: "bot-1", Channel: enums.CHANNEL_WEIXIN, ConversationType: enums.CONVERSATION_TYPE_WORKSPACE, WorkspaceID: "ws-1", WorkspaceDir: "/tmp/ocean-test", AccessPolicy: openAccess()}
}

// presetSessionAnchor 预置会话行 + active 态续聊锚（EnsureConversation 建行后 FinishTurn 落 active 锚）。
func presetSessionAnchor(t *testing.T, store *ConversationStore, sessionID string) {
	t.Helper()
	cfg := uxCfg()
	if _, err := store.EnsureConversation(cfg, "single:u1"); err != nil {
		t.Fatalf("预置会话行: %v", err)
	}
	if err := store.FinishTurn(cfg, "single:u1", sessionID, time.Now()); err != nil {
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
 * presetIncomingPolicy 临时 app.db 写入忙时新消息策略（表结构与 Rust 侧 app_config 同构），
 * 返回路径供用例中途改档（ReadAppConfigValue 无缓存，改后即读即生效）。
 */
func presetIncomingPolicy(t *testing.T, policy string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	setIncomingPolicy(t, path, policy)
	return path
}

// setIncomingPolicy 向临时 app.db 覆写策略值（INSERT OR REPLACE，支持用例中途改档）。
func setIncomingPolicy(t *testing.T, path, policy string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开临时 app.db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS app_config (key TEXT PRIMARY KEY, value TEXT NOT NULL)"); err != nil {
		t.Fatalf("建 app_config 表: %v", err)
	}
	if _, err := db.Exec("INSERT OR REPLACE INTO app_config (key, value) VALUES (?, ?)", appdb.KeyBotIncomingPolicy, policy); err != nil {
		t.Fatalf("写入忙时新消息策略: %v", err)
	}
}

/*
 * preemptDriver 首回合阻塞至 ctx 取消、后续回合即完成（priority/interrupt 抢占链路宿主）：
 * prompts 记录回合受理序，承载「插队先于存量队列」的顺序断言。
 */
type preemptDriver struct {
	started chan struct{}
	once    sync.Once
	mu      sync.Mutex
	prompts []string
}

func (d *preemptDriver) RunTurn(ctx context.Context, req TurnRequest) (<-chan TurnEvent, error) {
	d.mu.Lock()
	first := len(d.prompts) == 0
	d.prompts = append(d.prompts, req.Prompt)
	d.mu.Unlock()
	d.once.Do(func() { close(d.started) })
	events := make(chan TurnEvent, 2)
	if first {
		// 首回合先产一段正文再阻塞（驱动暂停场景的部分正文与恢复续写断言）。
		events <- TurnEvent{Type: TurnText, Delta: "已输出的一半"}
		go func() {
			<-ctx.Done()
			events <- TurnEvent{Type: TurnError, Err: fmt.Errorf("claude 回合失败: %w", ctx.Err())}
			close(events)
		}()
		return events, nil
	}
	events <- TurnEvent{Type: TurnDone, Result: "ok"}
	close(events)
	return events, nil
}

// capturedPrompts 回合 prompt 受理序只读拷贝。
func (d *preemptDriver) capturedPrompts() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.prompts...)
}

/*
 * TestIncomingPriorityJumpsQueue priority 档（默认档）：忙时新消息暂停在途回合（非终帧 ⏸
 * 保持流开启）并插到队首；执行序 = 新消息 → 原队列 → 被暂停回合，恢复轮携带暂停上下文在
 * 同一回复流上续写至终帧。
 */
func TestIncomingPriorityJumpsQueue(t *testing.T) {
	dbPath := presetIncomingPolicy(t, string(BotIncomingQueue))
	driver := &preemptDriver{started: make(chan struct{})}
	o, _ := newOrchestratorWithDB(t)
	o.driver = driver
	o.AppDbPath = dbPath
	cfg := issueCmdCfg()

	rs1 := &fakeReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "首条任务"}, func() (ReplyStream, error) {
		return rs1, nil
	})
	<-driver.started

	// 排队档下第二条消息按现状排到队尾。
	rs2 := &fakeReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-2", ConversationKey: "single:u1", SenderID: "u1", Text: "排队任务"}, func() (ReplyStream, error) {
		return rs2, nil
	})
	if frames := rs2.snapshot(); len(frames) != 1 || frames[0].content != "> 收到。当前有任务处理中，排队中，稍后自动继续…" {
		t.Fatalf("排队档占位帧应为排队文案: %+v", frames)
	}

	// 中途改档 priority：第三条消息暂停在途回合并插队首，存量 m-2 与被暂停的 m-1 依次恢复。
	setIncomingPolicy(t, dbPath, string(BotIncomingPriority))
	rs3 := &fakeReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-3", ConversationKey: "single:u1", SenderID: "u1", Text: "插队任务"}, func() (ReplyStream, error) {
		return rs3, nil
	})
	if frames := rs3.snapshot(); len(frames) != 1 || frames[0].content != "> 收到。上一条回复已暂停，本条将优先处理，完成后自动继续上一条…" {
		t.Fatalf("priority 档占位帧应为抢占文案: %+v", frames)
	}
	waitFor(t, "被暂停回合出现 ⏸ 非终帧", func() bool {
		for _, f := range rs1.snapshot() {
			if !f.final && strings.Contains(f.content, "⏸") {
				return true
			}
		}
		return false
	})

	// 受理序 = 首条 → 插队条 → 存量排队条 → 首条恢复轮（携带暂停上下文续写）。
	waitFor(t, "四回合按抢占序受理", func() bool { return len(driver.capturedPrompts()) == 4 })
	prompts := driver.capturedPrompts()
	if !strings.Contains(prompts[0], "首条任务") || !strings.Contains(prompts[1], "插队任务") ||
		!strings.Contains(prompts[2], "排队任务") || !strings.Contains(prompts[3], "首条任务") {
		t.Fatalf("回合受理序不符（首条/插队/排队/首条恢复）: %q", prompts)
	}
	if !strings.Contains(prompts[3], "<paused_context>") || !strings.Contains(prompts[3], "已输出的一半") {
		t.Fatalf("恢复轮应携带暂停上下文与已产出部分: %.300q", prompts[3])
	}

	// 被暂停回合在同一流上续写至终帧：⏸ 非终帧之后仍有产出帧，最后终帧收口。
	frames1 := finalFrameOf(t, "被暂停回合恢复终帧", rs1)
	pauseIdx := -1
	for i, f := range frames1 {
		if strings.Contains(f.content, "⏸") {
			pauseIdx = i
		}
	}
	if pauseIdx < 0 || pauseIdx == len(frames1)-1 {
		t.Fatalf("⏸ 后应在同一流继续产出并终帧收口: %+v", frames1)
	}
	if frames2 := finalFrameOf(t, "存量排队消息终帧", rs2); len(frames2) < 2 || !frames2[len(frames2)-1].final {
		t.Fatalf("存量排队消息应保留并继续执行收终帧: %+v", frames2)
	}
}

/*
 * TestIncomingLatestRunsOnlyNewest interrupt 档：忙时新消息中断在途回合并排空队列，仅处理
 * 最新消息（被排空消息各收 ⏹ 未执行提示，文案与 /stop 排空同源，已由该链路用例覆盖）。
 */
func TestIncomingLatestRunsOnlyNewest(t *testing.T) {
	dbPath := presetIncomingPolicy(t, string(BotIncomingLatest))
	driver := &preemptDriver{started: make(chan struct{})}
	o, _ := newOrchestratorWithDB(t)
	o.driver = driver
	o.AppDbPath = dbPath
	cfg := issueCmdCfg()

	rs1 := &fakeReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "首条任务"}, func() (ReplyStream, error) {
		return rs1, nil
	})
	<-driver.started

	rs2 := &fakeReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-2", ConversationKey: "single:u1", SenderID: "u1", Text: "最新任务"}, func() (ReplyStream, error) {
		return rs2, nil
	})
	if frames := rs2.snapshot(); len(frames) != 1 || frames[0].content != "> 收到。已中断上一条消息的处理，正在处理本条…" {
		t.Fatalf("interrupt 档占位帧应为抢占文案: %+v", frames)
	}
	frames1 := finalFrameOf(t, "被打断回合终帧", rs1)
	if frames1[len(frames1)-1].content != "⏹ 已停止当前回复。" {
		t.Fatalf("被打断回合终帧应为取消叙事: %+v", frames1)
	}
	waitFor(t, "最新消息回合已受理", func() bool { return len(driver.capturedPrompts()) == 2 })
	frames2 := finalFrameOf(t, "最新消息终帧", rs2)
	if len(frames2) < 2 || !frames2[len(frames2)-1].final {
		t.Fatalf("最新消息应执行收终帧: %+v", frames2)
	}
}

// estBlockDriver 建立期阻塞替身：首回合 RunTurn 阻塞至 ctx 取消后返回错误（建立期抢占宿主），
// 后续回合即完成。
type estBlockDriver struct {
	started chan struct{}
	once    sync.Once
	mu      sync.Mutex
	calls   int
}

func (d *estBlockDriver) RunTurn(ctx context.Context, _ TurnRequest) (<-chan TurnEvent, error) {
	d.mu.Lock()
	d.calls++
	first := d.calls == 1
	d.mu.Unlock()
	d.once.Do(func() { close(d.started) })
	if first {
		<-ctx.Done()
		return nil, fmt.Errorf("排队等待回合结束被取消: %w", ctx.Err())
	}
	events := make(chan TurnEvent, 1)
	events <- TurnEvent{Type: TurnDone, Result: "ok"}
	close(events)
	return events, nil
}

/*
 * TestIncomingPriorityPreemptsEstablishingTurn 建立期抢占：RunTurn 未返回即被抢占（泵未启动）
 * → ⏸ 非终帧 + 重新入队待恢复，恢复轮在同一回复流上执行收终帧，消息不丢。
 */
func TestIncomingPriorityPreemptsEstablishingTurn(t *testing.T) {
	dbPath := presetIncomingPolicy(t, string(BotIncomingPriority))
	driver := &estBlockDriver{started: make(chan struct{})}
	o, _ := newOrchestratorWithDB(t)
	o.driver = driver
	o.AppDbPath = dbPath
	cfg := issueCmdCfg()

	rs1 := &fakeReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "首条"}, func() (ReplyStream, error) {
		return rs1, nil
	})
	<-driver.started

	rs2 := &fakeReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-2", ConversationKey: "single:u1", SenderID: "u1", Text: "抢占"}, func() (ReplyStream, error) {
		return rs2, nil
	})

	frames1 := finalFrameOf(t, "建立期抢占后恢复终帧", rs1)
	paused := false
	for _, f := range frames1 {
		if !f.final && strings.Contains(f.content, "⏸") {
			paused = true
		}
	}
	if !paused {
		t.Fatalf("建立期抢占应有 ⏸ 非终帧: %+v", frames1)
	}
	if frames2 := finalFrameOf(t, "抢占消息终帧", rs2); len(frames2) < 2 || !frames2[len(frames2)-1].final {
		t.Fatalf("抢占消息应执行收终帧: %+v", frames2)
	}
}

/*
 * TestIncomingPriorityQueueFullKeepsTurn priority 档队满回归：插队被拒（排队已满）时在途
 * 回合必须存活（先入队后取消的时序保证），存量队列原样保留继续执行。
 */
func TestIncomingPriorityQueueFullKeepsTurn(t *testing.T) {
	dbPath := presetIncomingPolicy(t, string(BotIncomingQueue))
	driver := &preemptDriver{started: make(chan struct{})}
	o, _ := newOrchestratorWithDB(t)
	o.driver = driver
	o.AppDbPath = dbPath
	cfg := issueCmdCfg()

	rs1 := &fakeReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "首条任务"}, func() (ReplyStream, error) {
		return rs1, nil
	})
	<-driver.started

	// 排队档灌满队列（queueCapacity 条）。
	for i := 2; i <= 1+queueCapacity; i++ {
		o.HandleInbound(cfg, InboundMessage{MessageID: fmt.Sprintf("m-%d", i), ConversationKey: "single:u1", SenderID: "u1", Text: "占位任务"}, func() (ReplyStream, error) {
			return &fakeReplyStream{}, nil
		})
	}

	// 改档 priority 后新消息插队：队满被拒，在途回合不得被波及。
	setIncomingPolicy(t, dbPath, string(BotIncomingPriority))
	rsFull := &fakeReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-full", ConversationKey: "single:u1", SenderID: "u1", Text: "插队任务"}, func() (ReplyStream, error) {
		return rsFull, nil
	})
	frames := finalFrameOf(t, "队满拒绝终帧", rsFull)
	if frames[len(frames)-1].content != "当前消息较多，排队已满，请稍后再发。" {
		t.Fatalf("队满应拒绝收终帧: %+v", frames)
	}
	// 留出取消竞态窗口后复核：在途回合无终帧、无新回合受理。
	time.Sleep(100 * time.Millisecond)
	if len(driver.capturedPrompts()) != 1 {
		t.Fatalf("队满拒绝不应触发新回合: %q", driver.capturedPrompts())
	}
	for _, f := range rs1.snapshot() {
		if f.final {
			t.Fatalf("队满拒绝后在途回合应存活（不该有终帧）: %+v", rs1.snapshot())
		}
	}
}

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
	// 排队档注入：本用例专测 /stop 链路，与忙时新消息默认档（priority）解耦。
	o.AppDbPath = presetIncomingPolicy(t, string(BotIncomingQueue))
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
	if !store.HasSeen(uxCfg(), "single:u1", "m-3") {
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
	if id := headlessAnchorOf(t, store); id != "" {
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
 * TestRunTurnPassesPresetSessionAnchor headless 锚链·预置：pending 态建行预生成值随
 * TurnRequest.PresetSessionID 传入引擎（--session-id 开新会话即预置 id）。
 */
func TestRunTurnPassesPresetSessionAnchor(t *testing.T) {
	driver := &recordingDriver{}
	o, store := newOrchestratorWithDriver(t, driver)
	conv, err := store.EnsureConversation(issueCmdCfg(), "single:u1")
	if err != nil {
		t.Fatalf("预置会话行: %v", err)
	}
	rs := &fakeReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "开场"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "回合执行", func() bool { return driver.captured().Prompt != "" })
	if got := driver.captured().PresetSessionID; got != conv.ClaudeSessionID {
		t.Fatalf("pending 态应传预置锚: got=%q want=%q", got, conv.ClaudeSessionID)
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
		id := headlessAnchorOf(t, store)
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
	if id := headlessAnchorOf(t, store); id != "" {
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
	if id := headlessAnchorOf(t, store); id != "s-prev" {
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
