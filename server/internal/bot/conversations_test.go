package bot

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/model"
)

// newBotTestDB bot 域测试共用夹具：临时 sqlite + 调用方声明的表集（AutoMigrate）。
// 各测试按链路需要传表，不再各自内联 gorm.Open 样板。
func newBotTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

// newConversationTestDB 内存 sqlite + 会话映射表。
func newConversationTestDB(t *testing.T) *gorm.DB {
	return newBotTestDB(t, &model.ImBotConversation{})
}

// TestConversationStoreBoundIssueRoundtrip 绑定锚读写验收：新会话默认空、写入读回、
// 幂等覆盖、解绑（空串）回到未绑定态。
func TestConversationStoreBoundIssueRoundtrip(t *testing.T) {
	store := &ConversationStore{DB: newConversationTestDB(t)}
	const botID = 1
	const convKey = "single:u1"

	if got, err := store.BoundIssueID(botID, convKey); err != nil || got != "" {
		t.Fatalf("新会话应未绑定且 GetOrCreate 不报错: got=%q err=%v", got, err)
	}
	if err := store.SaveBoundIssueID(botID, convKey, "issue-1"); err != nil {
		t.Fatalf("写绑定: %v", err)
	}
	if got, err := store.BoundIssueID(botID, convKey); err != nil || got != "issue-1" {
		t.Fatalf("绑定读回: got=%q err=%v", got, err)
	}
	// 幂等覆盖（换绑）。
	if err := store.SaveBoundIssueID(botID, convKey, "issue-2"); err != nil {
		t.Fatalf("换绑: %v", err)
	}
	if got, _ := store.BoundIssueID(botID, convKey); got != "issue-2" {
		t.Fatalf("换绑后应指向新 issue: got=%q", got)
	}
	// 解绑 = 空串覆盖，回到未绑定态。
	if err := store.SaveBoundIssueID(botID, convKey, ""); err != nil {
		t.Fatalf("解绑: %v", err)
	}
	if got, _ := store.BoundIssueID(botID, convKey); got != "" {
		t.Fatalf("解绑后应为空: got=%q", got)
	}
	// 会话键隔离：同 bot 其他会话不受影响（各自独立的绑定锚）。
	if got, _ := store.BoundIssueID(botID, "single:u2"); got != "" {
		t.Fatalf("其他会话不应受绑定影响: got=%q", got)
	}
}

// TestConversationStoreBoundIssueOrthogonalToSession 两锚正交契约：绑定/解绑均不触碰
// headless 续聊锚 claude_session_id（反之亦然）。
func TestConversationStoreBoundIssueOrthogonalToSession(t *testing.T) {
	store := &ConversationStore{DB: newConversationTestDB(t)}
	const botID, convKey = 1, "group:c1"

	// SaveSessionID/SaveBoundIssueID 均为 UPDATE 语义（无行空转）：先显式建行。
	if _, err := store.GetOrCreate(botID, convKey); err != nil {
		t.Fatalf("预置会话行: %v", err)
	}
	if err := store.SaveSessionID(botID, convKey, "sess-abc"); err != nil {
		t.Fatalf("写续聊锚: %v", err)
	}
	if err := store.SaveBoundIssueID(botID, convKey, "issue-1"); err != nil {
		t.Fatalf("写绑定: %v", err)
	}
	if err := store.SaveBoundIssueID(botID, convKey, ""); err != nil {
		t.Fatalf("解绑: %v", err)
	}
	if sess, err := store.SessionID(botID, convKey); err != nil || sess != "sess-abc" {
		t.Fatalf("解绑不得清续聊锚: sess=%q err=%v", sess, err)
	}
	if err := store.SaveSessionID(botID, convKey, "sess-def"); err != nil {
		t.Fatalf("回写续聊锚: %v", err)
	}
	if bound, _ := store.BoundIssueID(botID, convKey); bound != "" {
		t.Fatalf("续聊锚回写不得触碰绑定: bound=%q", bound)
	}
}
