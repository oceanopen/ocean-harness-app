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
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
)

// 会话绑定链路测试用的 issue id（本域 / 外域 workspace 各一）。
const (
	imBotIssueLocal   = "0199aaaa-0000-7111-8111-3b6ac9e1f001"
	imBotIssueForeign = "0199aaaa-0000-7111-8111-3b6ac9e1f002"
)

// newImBotTestDB 临时目录 sqlite 文件库 + 会话链路四表（workspace / bot / 会话映射 / 任务）。
func newImBotTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.ImBot{}, &model.ImBotConversation{}, &model.ProjectTask{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

/*
 * mkImBotFixture 建 2 workspace（本域/外域）+ 2 任务 + 2 bot（绑 ws1 / 无 ws）+ 3 会话行
 * （带归属锚的活跃单聊 / 带 headless 续聊锚的较早群聊 / 从未活跃单聊）。
 */
func mkImBotFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	q := query.Use(db)
	ctx := context.Background()
	for _, ws := range []*model.Workspace{
		{ID: "ws-1", Name: "ws1", Dir: t.TempDir()},
		{ID: "ws-2", Name: "ws2", Dir: t.TempDir()},
	} {
		if err := q.Workspace.WithContext(ctx).Create(ws); err != nil {
			t.Fatalf("建 workspace: %v", err)
		}
	}
	for _, is := range []*model.ProjectTask{
		{ID: imBotIssueLocal, ProjectID: "proj-1", Name: "本域任务", TypeID: 1},
		{ID: imBotIssueForeign, ProjectID: "proj-2", Name: "外域任务", TypeID: 1},
	} {
		if err := q.ProjectTask.WithContext(ctx).Create(is); err != nil {
			t.Fatalf("建 task: %v", err)
		}
	}
	for _, b := range []*model.ImBot{
		{ID: "bot-1", Name: "b1", Channel: enums.CHANNEL_WECOM, WorkspaceID: "ws-1", AccessPolicy: "{}", Enabled: enums.YES_NO_NO},
		{ID: "bot-2", Name: "b2", Channel: enums.CHANNEL_WECOM, WorkspaceID: "", AccessPolicy: "{}", Enabled: enums.YES_NO_NO},
	} {
		if err := q.ImBot.WithContext(ctx).Create(b); err != nil {
			t.Fatalf("建 bot: %v", err)
		}
	}
	latest := time.Now()
	earlier := latest.Add(-time.Hour)
	for _, conv := range []*model.ImBotConversation{
		{ID: "conv-1", BotID: "bot-1", ConversationKey: "single:u1", ConversationID: imBotIssueLocal, LastMessageAt: &latest},
		{ID: "conv-2", BotID: "bot-1", ConversationKey: "group:g1", ClaudeSessionID: "sess-keep", LastMessageAt: &earlier},
		{ID: "conv-3", BotID: "bot-1", ConversationKey: "single:u2"},
	} {
		if err := q.ImBotConversation.WithContext(ctx).Create(conv); err != nil {
			t.Fatalf("建会话行: %v", err)
		}
	}
}

/*
 * TestImBotGetConversations 会话列表装配：排序（活跃倒序 + 从未活跃殿后）/ chatType 前缀派生 /
 * 归属会话锚直出 / bot 不存在拒绝。
 */
func TestImBotGetConversations(t *testing.T) {
	db := newImBotTestDB(t)
	mkImBotFixture(t, db)
	svc := ImBot{Service: apis.Service{Context: context.Background(), Orm: db, Logger: zap.NewNop()}}

	rows, err := svc.GetConversations(&types.ImBotGetConversationsRequest{BotId: "bot-1"})
	if err != nil {
		t.Fatalf("GetConversations: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("会话数 = %d, want 3", len(rows))
	}
	// 排序：single:u1（最新）→ group:g1（较早）→ single:u2（从未活跃殿后）。
	if rows[0].ConversationKey != "single:u1" || rows[1].ConversationKey != "group:g1" || rows[2].ConversationKey != "single:u2" {
		t.Fatalf("排序不符（活跃倒序 + 从未活跃殿后）: %s / %s / %s",
			rows[0].ConversationKey, rows[1].ConversationKey, rows[2].ConversationKey)
	}
	if rows[0].ChatType != "single" || rows[1].ChatType != "group" {
		t.Fatalf("chatType 前缀派生错误: %q / %q", rows[0].ChatType, rows[1].ChatType)
	}
	if rows[0].ConversationId != imBotIssueLocal {
		t.Fatalf("归属会话锚装配错误: %+v", rows[0])
	}
	if rows[1].ConversationId != "" || rows[2].LastMessageAt != nil {
		t.Fatalf("未关联/从未活跃形态错误: %+v %+v", rows[1], rows[2])
	}
	if _, err := svc.GetConversations(&types.ImBotGetConversationsRequest{BotId: "bot-99"}); err == nil {
		t.Fatal("bot 不存在应拒绝")
	}
}
