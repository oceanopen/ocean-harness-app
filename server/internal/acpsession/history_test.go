package acpsession

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TestHistoryFileRoundTrip 历史文件读写删闭环：缺失读回为空、增量追加按序读回、
// 删除幂等、畸形会话 id 拒绝（不外逸目录）。
func TestHistoryFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if records, err := loadHistoryEntries(dir, "conv-1"); err != nil || len(records) != 0 {
		t.Fatalf("缺失文件应读回空: %v %d", err, len(records))
	}
	e1 := ConversationEntry{EntryID: "t1-user", Kind: entryKindUser, Text: "第一问"}
	e2 := ConversationEntry{EntryID: "t1-agentMessage", Kind: entryKindAgentMessage, Text: "你好，"}
	e2b := ConversationEntry{EntryID: "t1-agentMessage", Kind: entryKindAgentMessage, Text: "你好，完成"}
	for _, e := range []ConversationEntry{e1, e2, e2b} {
		if err := appendHistoryEntry(dir, "conv-1", e); err != nil {
			t.Fatalf("appendHistoryEntry: %v", err)
		}
	}
	records, err := loadHistoryEntries(dir, "conv-1")
	if err != nil {
		t.Fatalf("loadHistoryEntries: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("应按行读回 3 条记录，got %d", len(records))
	}
	if records[0].Entry.Text != "第一问" || records[2].Entry.Text != "你好，完成" {
		t.Fatalf("读回顺序或内容不符: %+v", records)
	}
	if records[0].RecordedAt.IsZero() {
		t.Fatal("记录应带落盘时刻")
	}
	if err := RemoveHistoryFile(dir, "conv-1"); err != nil {
		t.Fatalf("RemoveHistoryFile: %v", err)
	}
	if records, _ = loadHistoryEntries(dir, "conv-1"); len(records) != 0 {
		t.Fatalf("删除后应读回空，got %d", len(records))
	}
	if err := RemoveHistoryFile(dir, "conv-1"); err != nil {
		t.Fatalf("重复删除应幂等: %v", err)
	}
	if err := appendHistoryEntry(dir, "../evil", e1); err == nil {
		t.Fatal("含路径分隔符的会话 id 应拒绝")
	}
}

// TestHistoryReplayFoldAndRename 回放折算：同 id last-wins 收敛、重编 hist- 命名空间、
// 与后续实时回合的 entryId 命名空间互不冲突。
func TestHistoryReplayFoldAndRename(t *testing.T) {
	records := []historyRecord{
		{Entry: ConversationEntry{EntryID: "g1-t1-user", Kind: entryKindUser, Text: "问"}},
		{Entry: ConversationEntry{EntryID: "g1-t1-agentMessage", Kind: entryKindAgentMessage, Text: "答，"}},
		{Entry: ConversationEntry{EntryID: "g1-t1-agentMessage", Kind: entryKindAgentMessage, Text: "答，完整"}},
		{Entry: ConversationEntry{EntryID: "g2-t1-user", Kind: entryKindUser, Text: "续问"}},
	}
	view := newSessionView()
	if frames := view.replayHistory(records); len(frames) != 3 {
		t.Fatalf("同 id 折算后应剩 3 帧，got %d", len(frames))
	}
	snap := view.snapshot()
	want := []struct {
		id   string
		text string
	}{
		{"hist-1", "问"},
		{"hist-2", "答，完整"},
		{"hist-3", "续问"},
	}
	for i, w := range want {
		if snap.Entries[i].EntryID != w.id || snap.Entries[i].Text != w.text {
			t.Fatalf("回放条目 %d 不符: got %+v want %+v", i, snap.Entries[i], w)
		}
	}
	// 实时回合在 hist 之后开跑，命名空间互不冲突。
	if _, err := view.beginTurn("新回合", PromptMeta{}); err != nil {
		t.Fatalf("beginTurn: %v", err)
	}
	snap = view.snapshot()
	if last := snap.Entries[len(snap.Entries)-1]; last.EntryID != "t1-user" || last.Text != "新回合" {
		t.Fatalf("实时回合应落在 hist 之后且用 t1- 命名空间: %+v", last)
	}
}

/*
 * newHistoryManager 装配带历史目录的被测 Manager（spawn 顶替 fakeSpawn，会话行由调用方
 * 预种）。
 */
func newHistoryManager(t *testing.T, db *gorm.DB, script, historyDir string) *Manager {
	t.Helper()
	mgr, err := NewManager(db, Dirs{}, 0, "", historyDir, zap.NewNop())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	old := spawnFunc
	spawnFunc = fakeSpawn(script, nil, &atomic.Int64{}, nil)
	t.Cleanup(func() { spawnFunc = old })
	t.Cleanup(mgr.StopAll)
	return mgr
}

// seedHistoryIssue 种 workspace + task + 会话实体行，返回 conv- 键与会话行 id。
func seedHistoryIssue(t *testing.T, db *gorm.DB) (string, string) {
	t.Helper()
	taskID := seedIssue(t, db, t.TempDir(), `{"mode":"acp"}`, "")
	convID := seedTaskConversation(t, db, taskID)
	return ConversationSessionKey(convID), convID
}

/*
 * TestHistoryPersistAndOfflineReplay 跨重启主链路：实况回合条目实时落盘 → 新 Manager
 * （重启模拟）受理同会话 → 文件历史按序回放进视图 + 双系统提示（离线恢复/上下文重置）。
 */
func TestHistoryPersistAndOfflineReplay(t *testing.T) {
	db := newTestDB(t)
	key, convID := seedHistoryIssue(t, db)
	historyDir := t.TempDir()
	ctx := context.Background()

	mgr := newHistoryManager(t, db, "happy", historyDir)
	if _, err := mgr.Ensure(ctx, key, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitForCondition(t, "会话就绪", func() bool { return mgr.Get(key).Status == StatusReady })
	if err := mgr.Prompt(key, "打声招呼"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	waitForCondition(t, "会话历史落盘", func() bool {
		records, err := loadHistoryEntries(historyDir, convID)
		return err == nil && len(records) >= 2 && records[len(records)-1].Entry.Text == "你好，ACP 回合完成"
	})
	mgr.StopAll()

	// 重启模拟：新 Manager 打开同会话（锚恢复降级为全新会话，历史由文件回放）。
	mgr2 := newHistoryManager(t, db, "happy", historyDir)
	if _, err := mgr2.Ensure(ctx, key, ""); err != nil {
		t.Fatalf("重启后 Ensure: %v", err)
	}
	waitForCondition(t, "离线回放就位", func() bool { return len(mgr2.Get(key).Entries) >= 4 })
	e := mgr2.Get(key).Entries
	if e[0].EntryID != "hist-1" || e[0].Kind != entryKindUser || e[0].Text != "打声招呼" {
		t.Fatalf("回放用户条目不符: %+v", e[0])
	}
	if e[1].EntryID != "hist-2" || e[1].Kind != entryKindAgentMessage || e[1].Text != "你好，ACP 回合完成" {
		t.Fatalf("回放 agent 条目不符: %+v", e[1])
	}
	if e[2].Kind != entryKindSystem || e[2].Text != "历史消息已离线恢复" {
		t.Fatalf("缺离线恢复提示: %+v", e[2])
	}
	if e[3].Kind != entryKindSystem || e[3].Text != "Agent 上下文已重置" {
		t.Fatalf("缺上下文重置提示: %+v", e[3])
	}
}

// TestHistoryDiscardCascade Discard 级联删除历史文件（幂等重入无害）。
func TestHistoryDiscardCascade(t *testing.T) {
	db := newTestDB(t)
	key, convID := seedHistoryIssue(t, db)
	historyDir := t.TempDir()
	mgr := newHistoryManager(t, db, "happy", historyDir)
	if _, err := mgr.Ensure(context.Background(), key, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitForCondition(t, "会话就绪", func() bool { return mgr.Get(key).Status == StatusReady })
	if err := mgr.Prompt(key, "落盘轮"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	waitForCondition(t, "历史已落盘", func() bool {
		records, err := loadHistoryEntries(historyDir, convID)
		return err == nil && len(records) > 0
	})
	mgr.Discard(key)
	waitForCondition(t, "历史文件随删除级联移除", func() bool {
		ids, err := HistoryConversationIDs(historyDir)
		return err == nil && len(ids) == 0
	})
	mgr.Discard(key) // 幂等：二次 Discard 不复活文件。
	if ids, _ := HistoryConversationIDs(historyDir); len(ids) != 0 {
		t.Fatalf("二次 Discard 不应产生历史文件: %v", ids)
	}
}

/*
 * TestHistoryResumeWireReplayNotPersisted resume 成功的 wire 重放不落盘（首回合前闸门），
 * 其后实况回合增量追加且不混入重放文本。
 */
func TestHistoryResumeWireReplayNotPersisted(t *testing.T) {
	db := newTestDB(t)
	key, convID := seedHistoryIssue(t, db)
	historyDir := t.TempDir()
	if err := (&BindingStore{DB: db}).SaveReady(context.Background(), key, "claude-acp", "sess-anchor"); err != nil {
		t.Fatalf("种锚: %v", err)
	}
	mgr := newHistoryManager(t, db, "load", historyDir)
	if _, err := mgr.Ensure(context.Background(), key, ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitForCondition(t, "wire 重放进视图", func() bool {
		e := mgr.Get(key).Entries
		return len(e) > 0 && strings.Contains(e[len(e)-1].Text, "历史-2")
	})
	if records, err := loadHistoryEntries(historyDir, convID); err != nil || len(records) != 0 {
		t.Fatalf("wire 重放不应落盘: %v %d", err, len(records))
	}
	if err := mgr.Prompt(key, "实况轮"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	waitForCondition(t, "实况回合落盘", func() bool {
		records, err := loadHistoryEntries(historyDir, convID)
		return err == nil && len(records) >= 2 && records[len(records)-1].Entry.Text == "你好，ACP 回合完成"
	})
	records, _ := loadHistoryEntries(historyDir, convID)
	for _, rec := range records {
		if strings.Contains(rec.Entry.Text, "历史-") {
			t.Fatalf("重放文本不应混入历史文件: %+v", records)
		}
	}
}
