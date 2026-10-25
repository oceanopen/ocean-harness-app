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

// TestSetClaudeSessionID 终端锚回写三态：空锚落值、非空锚首捕获胜出、行不存在报错。
func TestSetClaudeSessionID(t *testing.T) {
	db := newConversationTestDB(t)
	seedConversation(t, db, "conv-1", "")
	svc := Conversation{Service: apis.Service{Context: context.Background(), Orm: db, Logger: zap.NewNop()}}

	row, err := svc.SetClaudeSessionID(&types.ConversationSetClaudeSessionIdRequest{ID: "conv-1", ClaudeSessionID: "sid-1"})
	if err != nil {
		t.Fatalf("空锚写入: %v", err)
	}
	if row.ClaudeSessionID != "sid-1" {
		t.Fatalf("空锚写入后锚值 = %q, want sid-1", row.ClaudeSessionID)
	}

	row, err = svc.SetClaudeSessionID(&types.ConversationSetClaudeSessionIdRequest{ID: "conv-1", ClaudeSessionID: "sid-2"})
	if err != nil {
		t.Fatalf("非空锚复设: %v", err)
	}
	if row.ClaudeSessionID != "sid-1" {
		t.Fatalf("首捕获胜出被破坏：锚值 = %q, want sid-1", row.ClaudeSessionID)
	}

	if _, err := svc.SetClaudeSessionID(&types.ConversationSetClaudeSessionIdRequest{ID: "conv-missing", ClaudeSessionID: "sid-3"}); err == nil || err.Error() != "会话不存在" {
		t.Fatalf("行不存在应报「会话不存在」，got: %v", err)
	}
}
