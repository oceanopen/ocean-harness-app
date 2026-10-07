package bot

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

// fakeReplyStream 测试替身：顺序记录帧（content/final），恒成功。写入方在 worker
// goroutine（回合泵），读取方在测试 goroutine——互斥保护，跨 goroutine 断言走 snapshot。
type fakeReplyStream struct {
	mu     sync.Mutex
	frames []fakeFrame
}

type fakeFrame struct {
	content string
	final   bool
}

func (f *fakeReplyStream) Flush(content string, final bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.frames = append(f.frames, fakeFrame{content: content, final: final})
	return nil
}

// snapshot 帧序只读拷贝（跨 goroutine 读取入口）。
func (f *fakeReplyStream) snapshot() []fakeFrame {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeFrame(nil), f.frames...)
}

func (f *fakeReplyStream) ByteLimit() int { return 20480 }

// fakeCardReplyStream 卡能力流替身（CardReplyStream 实现）：帧记录之上追加卡事件记录
// （SendCard 附卡帧 / UpdateCard 卡更新），可注入 SendCard 失败（无卡/发送失败回落纯文本
// 路径）。
type fakeCardReplyStream struct {
	fakeReplyStream
	mu      sync.Mutex
	cards   []fakeCardMsg
	sendErr error
}

type fakeCardMsg struct {
	spec    CardSpec
	content string
	final   bool
	update  bool
}

func (f *fakeCardReplyStream) SendCard(spec CardSpec, content string, final bool) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cards = append(f.cards, fakeCardMsg{spec: spec, content: content, final: final})
	return nil
}

func (f *fakeCardReplyStream) UpdateCard(spec CardSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cards = append(f.cards, fakeCardMsg{spec: spec, update: true})
	return nil
}

// cardSnapshot 卡事件序只读拷贝（跨 goroutine 读取入口）。
func (f *fakeCardReplyStream) cardSnapshot() []fakeCardMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeCardMsg(nil), f.cards...)
}

// newOrchestratorWithDB 空引擎编排器（指令步/门禁/探针步只消费 store 与 driver 探针，
// 主路径未触达时 driver nil 安全）；夹具为 #任务 链路双表，store.DB 供预置会话行与建
// issue。
func newOrchestratorWithDB(t *testing.T) (*Orchestrator, *ConversationStore) {
	t.Helper()
	store := &ConversationStore{DB: newIssueTestDB(t)}
	return NewOrchestrator(store, nil, nil, nil, zap.NewNop()), store
}

// newOrchestratorWithDriver 指定引擎装配（探针步全链路用例：driver 兼任 routeProber）。
func newOrchestratorWithDriver(t *testing.T, driver ClaudeDriver) (*Orchestrator, *ConversationStore) {
	t.Helper()
	store := &ConversationStore{DB: newIssueTestDB(t)}
	return NewOrchestrator(store, driver, nil, nil, zap.NewNop()), store
}

// stubProberDriver 探针步替身：ClaudeDriver + routeProber 双角色，注入探针结论与回合
// 调用计数（断言「探针出卡不出回合」）。calls 由 worker goroutine 写、测试 goroutine
// 读（waitFor 轮询），互斥保护。
type stubProberDriver struct {
	mu    sync.Mutex
	calls int
	acp   bool
	bound string
	err   error
}

func (s *stubProberDriver) RunTurn(_ context.Context, _ TurnRequest) (<-chan TurnEvent, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	events := make(chan TurnEvent, 1)
	events <- TurnEvent{Type: TurnDone, Result: "ok"}
	close(events)
	return events, nil
}

func (s *stubProberDriver) ProbeTarget(_ BotRuntimeConfig, _ string) (bool, string, error) {
	return s.acp, s.bound, s.err
}

func (s *stubProberDriver) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// openAccess 测试用开放白名单（全链路用例过受理步——空策略 fail closed 会拦在白名单）。
func openAccess() AccessPolicy { return AccessPolicy{Mode: AccessModeOpen} }

// issueCmdCfg 测试用运行配置。
func issueCmdCfg() BotRuntimeConfig {
	return BotRuntimeConfig{BotID: 1, WorkspaceID: 1, WorkspaceDir: "/tmp/ocean-test", AccessPolicy: openAccess()}
}

// queryIssueByID 直查 issue 行（解绑卡用例的卡构造同源数据取回）。
func queryIssueByID(t *testing.T, db *gorm.DB, id string) (*model.ProjectIssue, error) {
	t.Helper()
	pi := query.Use(db).ProjectIssue
	return pi.WithContext(context.Background()).Where(pi.ID.Eq(id)).First()
}

// TestSendUnbindCard 解绑确认卡出口矩阵：已绑定出单选项确认卡（卡锚与同帧文本同源、
// 不清锚——解绑动作唯一入口是 taskunbind 卡点击）；未绑定回教学终帧不出卡；悬空锚
// （issue 行缺失）按未绑定收口；无卡能力回落纯文本。
func TestSendUnbindCard(t *testing.T) {
	t.Run("已绑定出确认卡不清锚", func(t *testing.T) {
		o, store := newOrchestratorWithDB(t)
		db := store.DB
		mkIssue(t, db, 1, "0198aaa1-0000-7111-8111-3b6ac9e1f201", "登录页样式", 1)
		// 生产链路 runTurn 先 MarkSeen（GetOrCreate 建行）再进指令步——测试等价预置。
		if _, err := store.GetOrCreate(1, "single:u1"); err != nil {
			t.Fatalf("预置会话行: %v", err)
		}
		if _, err := store.SaveBoundIssueID(1, "single:u1", "0198aaa1-0000-7111-8111-3b6ac9e1f201"); err != nil {
			t.Fatalf("预置绑定: %v", err)
		}

		rs := &fakeCardReplyStream{}
		o.sendUnbindCard(issueCmdCfg(), InboundMessage{ConversationKey: "single:u1", Text: "#任务解绑"}, rs)
		frames, cards := rs.snapshot(), rs.cardSnapshot()
		if len(frames) != 0 || len(cards) != 1 || !cards[0].final || cards[0].update {
			t.Fatalf("应恰一张终帧确认卡: frames=%+v cards=%+v", frames, cards)
		}
		issue, err := queryIssueByID(t, db, "0198aaa1-0000-7111-8111-3b6ac9e1f201")
		if err != nil {
			t.Fatalf("读回任务行: %v", err)
		}
		if !bindingTaskIDMatches(cards[0].spec.TaskID, taskunbindCardPrefix, "", []string{issue.ID}) ||
			cards[0].content != taskunbindCardText(issue) {
			t.Fatalf("卡锚与同帧文本应与绑定任务同源: %+v", cards[0])
		}
		if bound, _ := store.BoundIssueID(1, "single:u1"); bound != "0198aaa1-0000-7111-8111-3b6ac9e1f201" {
			t.Fatalf("出卡不得清锚（解绑唯一入口是点击）: %q", bound)
		}
	})

	t.Run("未绑定回教学终帧", func(t *testing.T) {
		o, _ := newOrchestratorWithDB(t)
		rs := &fakeCardReplyStream{}
		o.sendUnbindCard(issueCmdCfg(), InboundMessage{ConversationKey: "single:u1"}, rs)
		frames, cards := rs.snapshot(), rs.cardSnapshot()
		if len(frames) != 1 || !frames[0].final || frames[0].content != issueNotBoundText() || len(cards) != 0 {
			t.Fatalf("未绑定应回教学纯文本: frames=%+v cards=%+v", frames, cards)
		}
	})

	t.Run("悬空锚按未绑定收口", func(t *testing.T) {
		o, store := newOrchestratorWithDB(t)
		if _, err := store.GetOrCreate(1, "single:u1"); err != nil {
			t.Fatalf("预置会话行: %v", err)
		}
		if _, err := store.SaveBoundIssueID(1, "single:u1", "i-gone"); err != nil { // 无对应 issue 行
			t.Fatalf("预置悬空绑定: %v", err)
		}
		rs := &fakeCardReplyStream{}
		o.sendUnbindCard(issueCmdCfg(), InboundMessage{ConversationKey: "single:u1"}, rs)
		frames, cards := rs.snapshot(), rs.cardSnapshot()
		if len(frames) != 1 || !frames[0].final || frames[0].content != issueNotBoundText() || len(cards) != 0 {
			t.Fatalf("悬空锚应按未绑定教学收口: frames=%+v cards=%+v", frames, cards)
		}
	})

	t.Run("无卡能力回落纯文本", func(t *testing.T) {
		o, store := newOrchestratorWithDB(t)
		mkIssue(t, store.DB, 1, "0198aaa1-0000-7111-8111-3b6ac9e1f201", "登录页样式", 1)
		if _, err := store.GetOrCreate(1, "single:u1"); err != nil {
			t.Fatalf("预置会话行: %v", err)
		}
		if _, err := store.SaveBoundIssueID(1, "single:u1", "0198aaa1-0000-7111-8111-3b6ac9e1f201"); err != nil {
			t.Fatalf("预置绑定: %v", err)
		}

		rs := &fakeReplyStream{}
		o.sendUnbindCard(issueCmdCfg(), InboundMessage{ConversationKey: "single:u1"}, rs)
		frames := rs.snapshot()
		issue, err := queryIssueByID(t, store.DB, "0198aaa1-0000-7111-8111-3b6ac9e1f201")
		if err != nil {
			t.Fatalf("读回任务行: %v", err)
		}
		if len(frames) != 1 || !frames[0].final || frames[0].content != taskunbindCardText(issue) {
			t.Fatalf("无卡能力应回落纯文本终帧: %+v", frames)
		}
	})
}

// TestSendBindingCardIssueCards 搜索出卡矩阵（T2.2 统一模型）：裸指令全列表 / 关键词子集 /
// 唯一命中同样出卡；零命中回纯文本教学；出卡恒终帧且不写绑定（绑定唯一入口是卡点击）。
func TestSendBindingCardIssueCards(t *testing.T) {
	o, store := newOrchestratorWithDB(t)
	db := store.DB
	mkIssue(t, db, 1, "0198aaa1-0000-7111-8111-3b6ac9e1f201", "登录页样式", 1)
	mkIssue(t, db, 1, "0198aaa2-0000-7111-8111-3b6ac9e1f202", "登录页接口", 2)
	mkIssue(t, db, 1, "0198aaa3-0000-7111-8111-3b6ac9e1f203", "网关重构", 3)

	card := func(keyword string) *fakeCardReplyStream {
		rs := &fakeCardReplyStream{}
		o.sendBindingCard(issueCmdCfg(), InboundMessage{ConversationKey: "single:u1"}, bindingIssue, keyword, "", rs)
		return rs
	}

	t.Run("裸指令全列表出卡", func(t *testing.T) {
		rs := card("")
		frames, cards := rs.snapshot(), rs.cardSnapshot()
		if len(frames) != 0 || len(cards) != 1 || !cards[0].final || cards[0].update {
			t.Fatalf("应恰一张终帧卡: frames=%+v cards=%+v", frames, cards)
		}
		issues, _ := bindingIssueCandidates(db, 1, "")
		if !bindingTaskIDMatches(cards[0].spec.TaskID, taskbindCardPrefix, "", issueIDKeys(issues)) ||
			cards[0].content != issueCardText("", issues) {
			t.Fatalf("卡锚与同帧文本应与候选同源: %+v", cards[0])
		}
		// 出卡即登记 last_message_card（pending，spec 快照与所发卡同锚）——点击消费闸的 SSOT。
		lc, ok := store.LastMessageCard(1, "single:u1")
		if !ok || lc.Kind != "taskbind" || lc.Status != lastMessageCardPending || lc.Spec.TaskID != cards[0].spec.TaskID {
			t.Fatalf("出卡应登记 last_message_card（taskbind/pending/同锚）: %+v ok=%v", lc, ok)
		}
	})

	t.Run("关键词子集与唯一命中同样出卡", func(t *testing.T) {
		rs := card("登录页")
		cards := rs.cardSnapshot()
		if len(cards) != 1 || len(cards[0].spec.Options) != 2 {
			t.Fatalf("关键词卡应为过滤子集: %+v", cards)
		}
		rs = card("网关")
		cards = rs.cardSnapshot()
		if len(cards) != 1 || len(cards[0].spec.Options) != 1 {
			t.Fatalf("唯一命中同样出卡（不直绑）: %+v", cards)
		}
	})

	t.Run("零命中回纯文本教学", func(t *testing.T) {
		rs := card("不存在")
		frames, cards := rs.snapshot(), rs.cardSnapshot()
		if len(frames) != 1 || !frames[0].final || frames[0].content != issueEmptyText("不存在") || len(cards) != 0 {
			t.Fatalf("零命中应回纯文本: frames=%+v cards=%+v", frames, cards)
		}
	})

	t.Run("出卡不写绑定", func(t *testing.T) {
		card("登录页")
		if bound, _ := store.BoundIssueID(1, "single:u1"); bound != "" {
			t.Fatalf("指令步不得写绑定: %q", bound)
		}
	})
}

// TestSendBindingCardIssueNoCardFallback 流无卡能力：出卡路径回落纯文本（同帧完整名称列表
// 文本不丢）。
func TestSendBindingCardIssueNoCardFallback(t *testing.T) {
	o, store := newOrchestratorWithDB(t)
	mkIssue(t, store.DB, 1, "0198aaa1-0000-7111-8111-3b6ac9e1f201", "登录页样式", 1)

	rs := &fakeReplyStream{}
	o.sendBindingCard(issueCmdCfg(), InboundMessage{ConversationKey: "single:u1"}, bindingIssue, "登录页", "", rs)
	frames := rs.snapshot()
	issues, _ := bindingIssueCandidates(store.DB, 1, "登录页")
	if len(frames) != 1 || !frames[0].final || frames[0].content != issueCardText("登录页", issues) {
		t.Fatalf("无卡能力应回落纯文本终帧: %+v", frames)
	}
}

// TestRunTurnWorkspaceCommandCard #工作空间 指令步全链路（置于门禁之前）：未选工作空间
// 的 bot 也能出卡（未选域恰是该指令要解决的场景）；占位帧后接终帧卡，不出回合。
func TestRunTurnWorkspaceCommandCard(t *testing.T) {
	store := &ConversationStore{DB: newBindingTestDB(t)}
	mkWorkspace(t, store.DB, "前端仓A")
	mkWorkspace(t, store.DB, "前端仓B")
	o := NewOrchestrator(store, nil, nil, nil, zap.NewNop())

	rs := &fakeCardReplyStream{}
	cfg := BotRuntimeConfig{BotID: 1, AccessPolicy: openAccess()} // 无 WorkspaceDir：门禁态
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "#工作空间 前端"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "workspace 指令出卡", func() bool { return len(rs.cardSnapshot()) > 0 })

	frames, cards := rs.snapshot(), rs.cardSnapshot()
	if len(frames) != 1 || frames[0].content != "正在思考…" || frames[0].final {
		t.Fatalf("受理占位帧应恰一帧非终态: %+v", frames)
	}
	if len(cards) != 1 || !cards[0].final || len(cards[0].spec.Options) != 2 {
		t.Fatalf("应恰一张终帧 workspace 卡: %+v", cards)
	}
	wss, _ := bindingWorkspaces(store.DB, "前端")
	if !bindingTaskIDMatches(cards[0].spec.TaskID, wsbindCardPrefix, "前端", workspaceIDKeys(wss)) ||
		cards[0].content != workspaceCardText("前端", wss) {
		t.Fatalf("卡锚与同帧文本应与候选同源: %+v", cards[0])
	}
}

// TestRunTurnUnbindCommandCard #任务解绑 指令步全链路（置于门禁之前）：未选工作空间的
// bot 也能解绑历史锚点（若置于门禁之后则被 workspace 卡拦截）；占位帧后接终帧确认卡，
// 不清锚不出回合。
func TestRunTurnUnbindCommandCard(t *testing.T) {
	store := &ConversationStore{DB: newIssueTestDB(t)}
	mkIssue(t, store.DB, 1, "0198aaa1-0000-7111-8111-3b6ac9e1f201", "登录页样式", 1)
	if _, err := store.GetOrCreate(1, "single:u1"); err != nil {
		t.Fatalf("预置会话行: %v", err)
	}
	if _, err := store.SaveBoundIssueID(1, "single:u1", "0198aaa1-0000-7111-8111-3b6ac9e1f201"); err != nil {
		t.Fatalf("预置绑定: %v", err)
	}
	o := NewOrchestrator(store, nil, nil, nil, zap.NewNop())

	rs := &fakeCardReplyStream{}
	cfg := BotRuntimeConfig{BotID: 1, AccessPolicy: openAccess()} // 无 WorkspaceDir：门禁态——解绑步在前不受拦
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "#任务解绑"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "解绑指令出卡", func() bool { return len(rs.cardSnapshot()) > 0 })

	frames, cards := rs.snapshot(), rs.cardSnapshot()
	if len(frames) != 1 || frames[0].content != "正在思考…" || frames[0].final {
		t.Fatalf("受理占位帧应恰一帧非终态: %+v", frames)
	}
	if len(cards) != 1 || !cards[0].final || len(cards[0].spec.Options) != 1 ||
		cards[0].spec.TaskID == "" || cards[0].spec.TaskID[:len(taskunbindCardPrefix)] != taskunbindCardPrefix {
		t.Fatalf("应恰一张终帧解绑确认卡（单选项）: %+v", cards)
	}
	issue, err := queryIssueByID(t, store.DB, "0198aaa1-0000-7111-8111-3b6ac9e1f201")
	if err != nil {
		t.Fatalf("读回任务行: %v", err)
	}
	if !bindingTaskIDMatches(cards[0].spec.TaskID, taskunbindCardPrefix, "", []string{issue.ID}) ||
		cards[0].content != taskunbindCardText(issue) {
		t.Fatalf("卡锚与同帧文本应与绑定任务同源: %+v", cards[0])
	}
	if bound, _ := store.BoundIssueID(1, "single:u1"); bound != "0198aaa1-0000-7111-8111-3b6ac9e1f201" {
		t.Fatalf("出卡不得清锚（解绑唯一入口是点击）: %q", bound)
	}
}

// TestRunTurnGateCard 门禁附卡：指引文本与 workspace 卡同帧（lead 前置 + 完整名称列表）；
// 无可选 workspace（库空）保纯文本指引。
func TestRunTurnGateCard(t *testing.T) {
	newGateOrch := func(t *testing.T) (*Orchestrator, *fakeCardReplyStream) {
		store := &ConversationStore{DB: newBindingTestDB(t)}
		o := NewOrchestrator(store, nil, nil, nil, zap.NewNop())
		rs := &fakeCardReplyStream{}
		return o, rs
	}
	cfg := BotRuntimeConfig{BotID: 1, AccessPolicy: openAccess()} // WorkspaceDir 空：门禁态

	t.Run("指引与卡同帧", func(t *testing.T) {
		o, rs := newGateOrch(t)
		mkWorkspace(t, o.store.DB, "后端仓")
		o.HandleInbound(cfg, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "跑一下测试"}, func() (ReplyStream, error) {
			return rs, nil
		})
		waitFor(t, "门禁附卡", func() bool { return len(rs.cardSnapshot()) > 0 })
		cards := rs.cardSnapshot()
		if len(cards) != 1 || !cards[0].final {
			t.Fatalf("门禁应恰一张终帧卡: %+v", cards)
		}
		if cards[0].content != workspaceGateLead+"\n\n"+workspaceCardText("", bindWsList(t, o.store.DB, "")) {
			t.Fatalf("同帧文本应为指引 + 完整名称列表: %q", cards[0].content)
		}
		if !bindingTaskIDMatches(cards[0].spec.TaskID, wsbindCardPrefix, "", workspaceIDKeys(bindWsList(t, o.store.DB, ""))) {
			t.Fatalf("卡锚应为全列表空关键词: %q", cards[0].spec.TaskID)
		}
	})

	t.Run("库空指引 + 库空教学", func(t *testing.T) {
		o, rs := newGateOrch(t)
		o.HandleInbound(cfg, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "跑一下测试"}, func() (ReplyStream, error) {
			return rs, nil
		})
		waitFor(t, "门禁纯文本", func() bool {
			for _, f := range rs.snapshot() {
				if f.final {
					return true
				}
			}
			return false
		})
		frames, cards := rs.snapshot(), rs.cardSnapshot()
		if len(cards) != 0 || len(frames) != 2 || frames[1].content != workspaceGateLead+"\n\n"+workspaceEmptyText("") || !frames[1].final {
			t.Fatalf("库空应回指引 + 库空教学（占位帧后接终帧）: frames=%+v cards=%+v", frames, cards)
		}
	})
}

// TestRunTurnProbeCard ACP 未绑探针步全链路：出任务卡终帧收口、不出回合（driver 零调用）；
// 探针报错或已绑定不拦截，落回主路径照常出回合；流无卡能力回落纯文本。
func TestRunTurnProbeCard(t *testing.T) {
	msg := InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "跑一下测试"}

	t.Run("ACP 未绑出卡不出回合", func(t *testing.T) {
		driver := &stubProberDriver{acp: true}
		o, store := newOrchestratorWithDriver(t, driver)
		mkIssue(t, store.DB, 1, "0198aaa1-0000-7111-8111-3b6ac9e1f201", "登录页样式", 1)
		rs := &fakeCardReplyStream{}
		o.HandleInbound(issueCmdCfg(), msg, func() (ReplyStream, error) { return rs, nil })
		waitFor(t, "探针出卡", func() bool { return len(rs.cardSnapshot()) > 0 })

		cards := rs.cardSnapshot()
		if len(cards) != 1 || !cards[0].final || cards[0].spec.TaskID == "" ||
			cards[0].spec.TaskID[:len(taskbindCardPrefix)] != taskbindCardPrefix {
			t.Fatalf("应恰一张终帧任务卡: %+v", cards)
		}
		if driver.callCount() != 0 {
			t.Fatalf("探针出卡不得出回合: calls=%d", driver.callCount())
		}
	})

	t.Run("探针报错与已绑定落回主路径", func(t *testing.T) {
		errDriver := &stubProberDriver{err: errors.New("db boom")}
		o, _ := newOrchestratorWithDriver(t, errDriver)
		rs := &fakeCardReplyStream{}
		o.HandleInbound(issueCmdCfg(), msg, func() (ReplyStream, error) { return rs, nil })
		waitFor(t, "回合完成", func() bool { return errDriver.callCount() > 0 })
		if cards := rs.cardSnapshot(); len(cards) != 0 {
			t.Fatalf("探针报错不得出卡: %+v", cards)
		}

		boundDriver := &stubProberDriver{acp: true, bound: "0198aaaa-0000-7111-8111-3b6ac9e1f201"}
		o2, _ := newOrchestratorWithDriver(t, boundDriver)
		rs2 := &fakeCardReplyStream{}
		o2.HandleInbound(issueCmdCfg(), msg, func() (ReplyStream, error) { return rs2, nil })
		waitFor(t, "回合完成", func() bool { return boundDriver.callCount() > 0 })
		if cards := rs2.cardSnapshot(); len(cards) != 0 {
			t.Fatalf("已绑定不得出卡: %+v", cards)
		}
	})

	t.Run("无卡能力回落纯文本", func(t *testing.T) {
		driver := &stubProberDriver{acp: true}
		o, store := newOrchestratorWithDriver(t, driver)
		mkIssue(t, store.DB, 1, "0198aaa1-0000-7111-8111-3b6ac9e1f201", "登录页样式", 1)
		rs := &fakeReplyStream{}
		o.HandleInbound(issueCmdCfg(), msg, func() (ReplyStream, error) { return rs, nil })
		waitFor(t, "探针纯文本回落", func() bool {
			for _, f := range rs.snapshot() {
				if f.final {
					return true
				}
			}
			return false
		})
		frames := rs.snapshot()
		issues, _ := bindingIssueCandidates(store.DB, 1, "")
		if len(frames) != 2 || frames[1].content != issueCardText("", issues) || !frames[1].final {
			t.Fatalf("无卡能力应回落纯文本终帧: %+v", frames)
		}
		if driver.callCount() != 0 {
			t.Fatalf("回落纯文本同样不出回合: calls=%d", driver.callCount())
		}
	})
}

// TestRunTurnIssueCommandFullPath #任务 指令步全链路：占位帧后终帧卡收口，不出回合
// （nil driver 不 panic 即证明主路径未触达）。
func TestRunTurnIssueCommandFullPath(t *testing.T) {
	o, store := newOrchestratorWithDB(t)
	mkIssue(t, store.DB, 1, "0198aaa1-0000-7111-8111-3b6ac9e1f201", "登录页样式", 1)
	rs := &fakeCardReplyStream{}
	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "#任务 登录页"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "#任务 指令出卡", func() bool { return len(rs.cardSnapshot()) > 0 })

	frames, cards := rs.snapshot(), rs.cardSnapshot()
	if len(frames) != 1 || frames[0].content != "正在思考…" {
		t.Fatalf("受理占位帧应恰一帧: %+v", frames)
	}
	if len(cards) != 1 || !cards[0].final || len(cards[0].spec.Options) != 1 {
		t.Fatalf("应恰一张终帧任务卡: %+v", cards)
	}
	if bound, _ := store.BoundIssueID(1, "single:u1"); bound != "" {
		t.Fatalf("指令步不得写绑定: %q", bound)
	}
}

// TestRunTurnMarkSeenGate 门禁前的 #工作空间 指令同样过幂等复查：MarkSeen 落库
// （SeenMessageIds 滚窗，经 HasSeen 读回验证——防「指令步绕过受理路径副作用」回归）。
func TestRunTurnMarkSeenGate(t *testing.T) {
	store := &ConversationStore{DB: newBindingTestDB(t)}
	mkWorkspace(t, store.DB, "仓")
	o := NewOrchestrator(store, nil, nil, nil, zap.NewNop())
	rs := &fakeCardReplyStream{}
	o.HandleInbound(BotRuntimeConfig{BotID: 1, AccessPolicy: openAccess()}, InboundMessage{MessageID: "m-42", ConversationKey: "single:u1", SenderID: "u1", Text: "#工作空间"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "#工作空间 出卡", func() bool { return len(rs.cardSnapshot()) > 0 })
	if seen, err := store.HasSeen(1, "single:u1", "m-42"); err != nil || !seen {
		t.Fatalf("MarkSeen 应落库: seen=%v err=%v", seen, err)
	}
}

// TestRunTurnHelpCommand #帮助 指令步全链路（置于绑定指令与门禁之前——未选工作空间时恰是
// 最需要指令列表的时刻）：占位帧后纯文本指令列表终帧收口，不出卡不出回合（nil driver
// 不 panic 即证明主路径未触达）。
func TestRunTurnHelpCommand(t *testing.T) {
	o, _ := newOrchestratorWithDB(t) // 无 workspace 数据：帮助步须先于门禁收口
	rs := &fakeCardReplyStream{}
	o.HandleInbound(BotRuntimeConfig{BotID: 1, AccessPolicy: openAccess()}, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "#帮助"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "#帮助 终帧", func() bool {
		for _, f := range rs.snapshot() {
			if f.final {
				return true
			}
		}
		return false
	})
	frames, cards := rs.snapshot(), rs.cardSnapshot()
	if len(frames) != 2 || frames[0].content != "正在思考…" || frames[1].content != helpText() || !frames[1].final {
		t.Fatalf("帮助应占位帧后接纯文本指令列表终帧: %+v", frames)
	}
	if len(cards) != 0 {
		t.Fatalf("帮助不出卡: %+v", cards)
	}
}
