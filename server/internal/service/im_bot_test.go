package service

import (
	"context"
	"path/filepath"
	"strings"
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

// newImBotTestDB 临时目录 sqlite 文件库 + 会话绑定链路四表（workspace / issue / bot / 会话映射）。
func newImBotTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.ProjectIssue{}, &model.ImBot{}, &model.ImBotConversation{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

// mkImBotFixture 建 2 workspace（本域/外域）+ 2 issue + 2 bot（绑 ws1 / 无 ws）+ 3 会话行
// （绑本域 issue 的活跃单聊 / 带 headless 续聊锚的较早群聊 / 从未活跃单聊）。
func mkImBotFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	q := query.Use(db)
	ctx := context.Background()
	for _, ws := range []*model.Workspace{
		{ID: 1, Name: "ws1", Dir: t.TempDir()},
		{ID: 2, Name: "ws2", Dir: t.TempDir()},
	} {
		if err := q.Workspace.WithContext(ctx).Create(ws); err != nil {
			t.Fatalf("建 workspace: %v", err)
		}
	}
	for _, is := range []*model.ProjectIssue{
		{ID: imBotIssueLocal, ProjectID: 1, WorkspaceID: 1, Name: "本域任务", StateCode: enums.STATE_CODE_BACKLOG, Priority: enums.PRIORITY_NONE, TypeID: 1},
		{ID: imBotIssueForeign, ProjectID: 2, WorkspaceID: 2, Name: "外域任务", StateCode: enums.STATE_CODE_BACKLOG, Priority: enums.PRIORITY_NONE, TypeID: 1},
	} {
		if err := q.ProjectIssue.WithContext(ctx).Create(is); err != nil {
			t.Fatalf("建 issue: %v", err)
		}
	}
	for _, b := range []*model.ImBot{
		{ID: 1, Name: "b1", Channel: enums.CHANNEL_WECOM, WorkspaceID: 1, AccessPolicy: "{}", Enabled: enums.YES_NO_NO},
		{ID: 2, Name: "b2", Channel: enums.CHANNEL_WECOM, WorkspaceID: 0, AccessPolicy: "{}", Enabled: enums.YES_NO_NO},
	} {
		if err := q.ImBot.WithContext(ctx).Create(b); err != nil {
			t.Fatalf("建 bot: %v", err)
		}
	}
	latest := time.Now()
	earlier := latest.Add(-time.Hour)
	for _, conv := range []*model.ImBotConversation{
		{BotID: 1, ConversationKey: "single:u1", BoundIssueID: imBotIssueLocal, LastMessageAt: &latest},
		{BotID: 1, ConversationKey: "group:g1", ClaudeSessionID: "sess-keep", LastMessageAt: &earlier},
		{BotID: 1, ConversationKey: "single:u2"},
	} {
		if err := q.ImBotConversation.WithContext(ctx).Create(conv); err != nil {
			t.Fatalf("建会话行: %v", err)
		}
	}
}

// TestImBotGetConversations 会话列表装配：排序（活跃倒序 + 从未活跃殿后）/ chatType 前缀派生 /
// 绑定任务名批量 join / bot 不存在拒绝。
func TestImBotGetConversations(t *testing.T) {
	db := newImBotTestDB(t)
	mkImBotFixture(t, db)
	svc := ImBot{Service: apis.Service{Context: context.Background(), Orm: db, Logger: zap.NewNop()}}

	rows, err := svc.GetConversations(&types.ImBotGetConversationsRequest{BotId: 1})
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
	if rows[0].BoundIssueId != imBotIssueLocal || rows[0].BoundIssueName != "本域任务" {
		t.Fatalf("绑定锚/名称装配错误: %+v", rows[0])
	}
	if rows[1].BoundIssueId != "" || rows[1].BoundIssueName != "" || rows[2].LastMessageAt != nil {
		t.Fatalf("未绑定/从未活跃形态错误: %+v %+v", rows[1], rows[2])
	}
	if _, err := svc.GetConversations(&types.ImBotGetConversationsRequest{BotId: 99}); err == nil {
		t.Fatal("bot 不存在应拒绝")
	}
}

// TestImBotBindConversationIssue 绑定/解绑主链路：落库生效（含列表口径回读）+ claude_session_id
// 不受扰动 + 解绑清锚。
func TestImBotBindConversationIssue(t *testing.T) {
	db := newImBotTestDB(t)
	mkImBotFixture(t, db)
	svc := ImBot{Service: apis.Service{Context: context.Background(), Orm: db, Logger: zap.NewNop()}}

	if err := svc.BindConversationIssue(&types.ImBotBindIssueRequest{
		BotId: 1, ConversationKey: "group:g1", IssueId: imBotIssueLocal,
	}); err != nil {
		t.Fatalf("绑定: %v", err)
	}
	q := query.Use(db)
	conv, err := q.ImBotConversation.WithContext(context.Background()).
		Where(q.ImBotConversation.BotID.Eq(1), q.ImBotConversation.ConversationKey.Eq("group:g1")).First()
	if err != nil || conv.BoundIssueID != imBotIssueLocal {
		t.Fatalf("绑定未落库: %+v err=%v", conv, err)
	}
	if conv.ClaudeSessionID != "sess-keep" {
		t.Fatalf("绑定不得扰动 headless 续聊锚: %q", conv.ClaudeSessionID)
	}
	rows, err := svc.GetConversations(&types.ImBotGetConversationsRequest{BotId: 1})
	if err != nil {
		t.Fatalf("GetConversations: %v", err)
	}
	for _, row := range rows {
		if row.ConversationKey == "group:g1" && row.BoundIssueName != "本域任务" {
			t.Fatalf("列表口径未见绑定名: %+v", row)
		}
	}

	if err := svc.BindConversationIssue(&types.ImBotBindIssueRequest{
		BotId: 1, ConversationKey: "group:g1",
	}); err != nil {
		t.Fatalf("解绑: %v", err)
	}
	conv, err = q.ImBotConversation.WithContext(context.Background()).
		Where(q.ImBotConversation.BotID.Eq(1), q.ImBotConversation.ConversationKey.Eq("group:g1")).First()
	if err != nil || conv.BoundIssueID != "" {
		t.Fatalf("解绑未清锚: %+v err=%v", conv, err)
	}
}

// TestImBotBindConversationIssueRejects 校验矩阵：issue 不存在 / 跨工作空间 / bot 无工作空间 /
// 会话行不在场（绑定与解绑同拒）/ bot 不存在。
func TestImBotBindConversationIssueRejects(t *testing.T) {
	db := newImBotTestDB(t)
	mkImBotFixture(t, db)
	svc := ImBot{Service: apis.Service{Context: context.Background(), Orm: db, Logger: zap.NewNop()}}

	cases := []struct {
		name string
		req  *types.ImBotBindIssueRequest
		want string
	}{
		{"issue 不存在", &types.ImBotBindIssueRequest{BotId: 1, ConversationKey: "single:u1", IssueId: "nope"}, "任务不存在"},
		{"跨工作空间 issue", &types.ImBotBindIssueRequest{BotId: 1, ConversationKey: "single:u1", IssueId: imBotIssueForeign}, "不属于"},
		{"bot 无工作空间", &types.ImBotBindIssueRequest{BotId: 2, ConversationKey: "single:u1", IssueId: imBotIssueLocal}, "请先为机器人选择工作空间"},
		{"会话行不在场（绑定）", &types.ImBotBindIssueRequest{BotId: 1, ConversationKey: "single:none", IssueId: imBotIssueLocal}, "会话不存在"},
		{"会话行不在场（解绑）", &types.ImBotBindIssueRequest{BotId: 1, ConversationKey: "single:none"}, "会话不存在"},
		{"bot 不存在", &types.ImBotBindIssueRequest{BotId: 99, ConversationKey: "single:u1", IssueId: imBotIssueLocal}, "bot 不存在"},
	}
	for _, tc := range cases {
		err := svc.BindConversationIssue(tc.req)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want 含 %q", tc.name, err, tc.want)
		}
	}
}
