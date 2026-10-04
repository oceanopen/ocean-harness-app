package bot

import (
	"sync"
	"testing"

	"go.uber.org/zap"

	"ocean-harness/server/internal/dal/model"
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

// newOrchestratorWithDB 空引擎编排器（applyIssueCommand 只消费 store 与 log，driver nil
// 安全）；夹具为 #issue 链路双表，store.DB 供预置会话行与建 issue。
func newOrchestratorWithDB(t *testing.T) (*Orchestrator, *ConversationStore) {
	t.Helper()
	store := &ConversationStore{DB: newIssueTestDB(t)}
	return NewOrchestrator(store, nil, nil, nil, zap.NewNop()), store
}

// issueCmdCfg 测试用运行配置。
func issueCmdCfg() BotRuntimeConfig {
	return BotRuntimeConfig{BotID: 1, WorkspaceID: 1, WorkspaceDir: "/tmp/ocean-test"}
}

// TestApplyIssueCommandUnbind 解绑分支：清绑定锚 + 终态确认帧 + 终结本条消息（不出回合）。
func TestApplyIssueCommandUnbind(t *testing.T) {
	o, store := newOrchestratorWithDB(t)
	// 生产链路 runTurn 先 MarkSeen（GetOrCreate 建行）再进指令步；SaveBoundIssueID 是
	// UPDATE 语义，无行则空转——测试等价预置。
	if _, err := store.GetOrCreate(1, "single:u1"); err != nil {
		t.Fatalf("预置会话行: %v", err)
	}
	if err := store.SaveBoundIssueID(1, "single:u1", "issue-1"); err != nil {
		t.Fatalf("预置绑定: %v", err)
	}
	rs := &fakeReplyStream{}
	msg := InboundMessage{ConversationKey: "single:u1", Text: "#issue 解绑"}

	if done := o.applyIssueCommand(issueCmdCfg(), &msg, &issueCommand{Unbind: true}, rs); !done {
		t.Fatal("解绑应终结本条消息")
	}
	if len(rs.frames) != 1 || !rs.frames[0].final || rs.frames[0].content != issueUnboundText() {
		t.Fatalf("解绑应恰发一帧终态确认: %+v", rs.frames)
	}
	if bound, _ := store.BoundIssueID(1, "single:u1"); bound != "" {
		t.Fatalf("解绑后绑定锚应为空: %q", bound)
	}
}

// TestApplyIssueCommandUnbindWhenUnbound 未绑定时解绑幂等：仍回确认帧（不区分原态）。
func TestApplyIssueCommandUnbindWhenUnbound(t *testing.T) {
	o, _ := newOrchestratorWithDB(t)
	rs := &fakeReplyStream{}
	if done := o.applyIssueCommand(issueCmdCfg(), &InboundMessage{ConversationKey: "single:u1"}, &issueCommand{Unbind: true}, rs); !done {
		t.Fatal("未绑定解绑同样终结消息")
	}
	if len(rs.frames) != 1 || rs.frames[0].content != issueUnboundText() {
		t.Fatalf("未绑定解绑应回确认帧: %+v", rs.frames)
	}
}

// TestApplyIssueCommandUsage 裸指令：仅回用法，不产生绑定写。
func TestApplyIssueCommandUsage(t *testing.T) {
	o, store := newOrchestratorWithDB(t)
	rs := &fakeReplyStream{}
	if done := o.applyIssueCommand(issueCmdCfg(), &InboundMessage{ConversationKey: "single:u1"}, &issueCommand{}, rs); !done {
		t.Fatal("裸指令应终结消息")
	}
	if len(rs.frames) != 1 || !rs.frames[0].final || rs.frames[0].content != issueUsageText() {
		t.Fatalf("裸指令应回用法终帧: %+v", rs.frames)
	}
	if bound, _ := store.BoundIssueID(1, "single:u1"); bound != "" {
		t.Fatalf("用法查询不得产生绑定: %q", bound)
	}
}

// TestApplyIssueCommandMatchFailures 零命中与多命中：各回终态引导文案，不写绑定。
func TestApplyIssueCommandMatchFailures(t *testing.T) {
	o, store := newOrchestratorWithDB(t)
	db := store.DB
	mkIssue(t, db, 1, "0198aaa1-0000-7111-8111-3b6ac9e1f201", "登录页样式", 1)
	mkIssue(t, db, 1, "0198aaa2-0000-7111-8111-3b6ac9e1f202", "登录页接口", 2)

	// 零命中。
	rs := &fakeReplyStream{}
	if done := o.applyIssueCommand(issueCmdCfg(), &InboundMessage{ConversationKey: "single:u1"}, &issueCommand{Target: "不存在"}, rs); !done {
		t.Fatal("零命中应终结消息")
	}
	if len(rs.frames) != 1 || rs.frames[0].content != issueZeroHitText("不存在") {
		t.Fatalf("零命中应回引导文案: %+v", rs.frames)
	}
	// 多命中：候选列表文案（与纯函数产物一致），不写绑定。
	rs = &fakeReplyStream{}
	if done := o.applyIssueCommand(issueCmdCfg(), &InboundMessage{ConversationKey: "single:u1"}, &issueCommand{Target: "登录页"}, rs); !done {
		t.Fatal("多命中应终结消息")
	}
	hits, err := resolveIssueTarget(db, 1, "登录页")
	if err != nil || len(hits) != 2 {
		t.Fatalf("预置多命中查询: hits=%d err=%v", len(hits), err)
	}
	if len(rs.frames) != 1 || rs.frames[0].content != issueCandidatesText("登录页", hits) {
		t.Fatalf("多命中应回候选文案: %+v", rs.frames)
	}
	if bound, _ := store.BoundIssueID(1, "single:u1"); bound != "" {
		t.Fatalf("匹配失败路径不得写绑定: %q", bound)
	}
}

// TestApplyIssueCommandBindSwitchOnly 纯切换：唯一命中即绑定 + 终态确认，不出回合。
func TestApplyIssueCommandBindSwitchOnly(t *testing.T) {
	o, store := newOrchestratorWithDB(t)
	db := store.DB
	const issueID = "0198bbbb-0000-7111-8111-3b6ac9e1f201"
	// SaveBoundIssueID 是 UPDATE 语义（无行空转）：生产链路 runTurn 先 MarkSeen 建行，
	// 测试等价预置会话行后再断言落库。
	if _, err := store.GetOrCreate(1, "single:u1"); err != nil {
		t.Fatalf("预置会话行: %v", err)
	}
	mkIssue(t, db, 1, issueID, "网关重构", 1)

	rs := &fakeReplyStream{}
	if done := o.applyIssueCommand(issueCmdCfg(), &InboundMessage{ConversationKey: "single:u1"}, &issueCommand{Target: "网关"}, rs); !done {
		t.Fatal("纯切换应终结消息（不出回合）")
	}
	// 绑定确认文案只消费 ID/Name，直接构造断言基准（免查询）。
	want := issueBoundText(&model.ProjectIssue{ID: issueID, Name: "网关重构"})
	if len(rs.frames) != 1 || !rs.frames[0].final || rs.frames[0].content != want {
		t.Fatalf("纯切换应回绑定确认终帧: %+v want=%s", rs.frames, want)
	}
	if bound, _ := store.BoundIssueID(1, "single:u1"); bound != issueID {
		t.Fatalf("绑定应落库: got=%q want=%q", bound, issueID)
	}
}

// TestApplyIssueCommandBindWithBody 绑定+首回合一步：确认作中间帧、正文改写、落回主路径。
func TestApplyIssueCommandBindWithBody(t *testing.T) {
	o, store := newOrchestratorWithDB(t)
	db := store.DB
	const issueID = "0198cccc-0000-7111-8111-3b6ac9e1f201"
	// 同纯切换用例：先预置会话行（MarkSeen 等价），绑定落库断言才真实。
	if _, err := store.GetOrCreate(1, "single:u1"); err != nil {
		t.Fatalf("预置会话行: %v", err)
	}
	mkIssue(t, db, 1, issueID, "发布流水线", 1)

	rs := &fakeReplyStream{}
	msg := InboundMessage{ConversationKey: "single:u1", Text: "#issue 发布流水线 把构建并行化"}
	done := o.applyIssueCommand(issueCmdCfg(), &msg, &issueCommand{Target: "发布流水线", Body: "把构建并行化"}, rs)
	if done {
		t.Fatal("绑定+首回合应落回主路径（返回 false）")
	}
	if len(rs.frames) != 1 || rs.frames[0].final {
		t.Fatalf("确认应为中间帧（终帧留给回复泵）: %+v", rs.frames)
	}
	if msg.Text != "把构建并行化" {
		t.Fatalf("正文应改写为指令 Body: %q", msg.Text)
	}
	if bound, _ := store.BoundIssueID(1, "single:u1"); bound != issueID {
		t.Fatalf("绑定应先行落库: got=%q want=%q", bound, issueID)
	}
}
