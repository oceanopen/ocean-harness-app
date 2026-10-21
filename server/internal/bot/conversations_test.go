package bot

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

/*
 * newBotTestDB bot 域测试共用夹具：临时 sqlite + 调用方声明的表集（AutoMigrate）。
 * 各测试按链路需要传表，不再各自内联 gorm.Open 样板。
 */
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
	t.Helper()
	return newBotTestDB(t, &model.ImBotConversation{})
}

/*
 * newIssueTestDB 编排器链路夹具：会话映射 + 工作空间/任务表（stub 驱动用例仅消费会话表，
 * 真路由用例按需读写工作空间/任务行）。
 */
func newIssueTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newBotTestDB(t, &model.ImBotConversation{}, &model.Workspace{}, &model.WorkspaceIssue{})
}

/*
 * TestConversationStoreSessionAnchorsOrthogonal 同表双锚正交契约：SaveSessionID 只写
 * claude_session_id，acp_session_id（acpsession.BindingStore 维护）不受触碰。
 */
func TestConversationStoreSessionAnchorsOrthogonal(t *testing.T) {
	store := &ConversationStore{DB: newConversationTestDB(t)}
	const botID, convKey = "bot-1", "group:c1"

	if _, err := store.GetOrCreate(botID, convKey); err != nil {
		t.Fatalf("预置会话行: %v", err)
	}
	if err := store.SaveSessionID(botID, convKey, "sess-abc"); err != nil {
		t.Fatalf("写续聊锚: %v", err)
	}
	// 直写 ACP 锚模拟 BindingStore 行为，再回写续聊锚互验。
	q := query.Use(store.DB)
	if _, err := q.ImBotConversation.WithContext(context.Background()).
		Where(q.ImBotConversation.BotID.Eq(botID), q.ImBotConversation.ConversationKey.Eq(convKey)).
		UpdateSimple(q.ImBotConversation.AcpSessionID.Value("acp-1")); err != nil {
		t.Fatalf("直写 ACP 锚: %v", err)
	}
	if err := store.SaveSessionID(botID, convKey, "sess-def"); err != nil {
		t.Fatalf("回写续聊锚: %v", err)
	}
	row, err := q.ImBotConversation.WithContext(context.Background()).Where(
		q.ImBotConversation.BotID.Eq(botID), q.ImBotConversation.ConversationKey.Eq(convKey)).First()
	if err != nil {
		t.Fatalf("读回会话行: %v", err)
	}
	if row.ClaudeSessionID != "sess-def" || row.AcpSessionID != "acp-1" {
		t.Fatalf("双锚应互不触碰: claude=%q acp=%q", row.ClaudeSessionID, row.AcpSessionID)
	}
}
