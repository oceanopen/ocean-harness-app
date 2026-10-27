package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/types"
)

// newConversationTestDB 临时目录 sqlite 文件库 + 会话单表。
func newConversationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Conversation{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

// seedConversation 种一行指定锚的会话（锚空 = 未锚定；模型时间列无 autoCreateTime，显式赋值）。
func seedConversation(t *testing.T, db *gorm.DB, id, claudeSessionID string) {
	t.Helper()
	now := time.Now()
	conv := &model.Conversation{
		ID: id, OwnerType: enums.OWNER_TYPE_WORKSPACE, OwnerID: "ws-1",
		Title: "会话", Mode: enums.CONVERSATION_MODE_TERMINAL,
		ClaudeSessionID: claudeSessionID, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(conv).Error; err != nil {
		t.Fatalf("种会话行: %v", err)
	}
}

// TestSetClaudeSessionID 终端锚回写三态：捕获落值并置 active、id 轮换持续同步（终端内
// /clear 新会话即新 id）、行不存在报错。
func TestSetClaudeSessionID(t *testing.T) {
	db := newConversationTestDB(t)
	seedConversation(t, db, "conv-1", "")
	svc := Conversation{Service: apis.Service{Context: context.Background(), Orm: db, Logger: zap.NewNop()}}

	row, err := svc.SetClaudeSessionID(&types.ConversationSetClaudeSessionIdRequest{ID: "conv-1", ClaudeSessionID: "sid-1"})
	if err != nil {
		t.Fatalf("锚写入: %v", err)
	}
	if row.ClaudeSessionID != "sid-1" || row.ClaudeSessionStatus != enums.CLAUDE_SESSION_STATUS_ACTIVE {
		t.Fatalf("捕获应落锚并置 active: id=%q status=%q", row.ClaudeSessionID, row.ClaudeSessionStatus)
	}

	// 轮换同步：观测到新 id 即覆写（绝对一致以实际值为准）。
	row, err = svc.SetClaudeSessionID(&types.ConversationSetClaudeSessionIdRequest{ID: "conv-1", ClaudeSessionID: "sid-2"})
	if err != nil {
		t.Fatalf("轮换同步: %v", err)
	}
	if row.ClaudeSessionID != "sid-2" {
		t.Fatalf("轮换应覆写锚值 = %q, want sid-2", row.ClaudeSessionID)
	}

	if _, err := svc.SetClaudeSessionID(&types.ConversationSetClaudeSessionIdRequest{ID: "conv-missing", ClaudeSessionID: "sid-3"}); err == nil || err.Error() != "会话不存在" {
		t.Fatalf("行不存在应报「会话不存在」，got: %v", err)
	}
}
