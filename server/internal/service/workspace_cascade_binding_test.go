package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
)

/*
 * newCascadeBindingTestDB 内存 sqlite + 级联链路表集（workspace / issue / 两张仓库关联 /
 * workspace type 关联 / IM 会话映射）。
 */
func newCascadeBindingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.WorkspaceIssue{}, &model.IssueLocalRepository{}, &model.WorkspaceLocalRepository{}, &model.WorkspaceType{}, &model.ImBot{}, &model.ImBotConversation{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

/*
 * mkCascadeFixture 建 workspace + 两个 issue + 三个会话行（绑 issue-a、绑 issue-b、绑外部
 * issue 且带 headless 续聊锚），返回 (workspaceID, issueAID, issueBID, otherIssueID)。
 */
func mkCascadeFixture(t *testing.T, db *gorm.DB) (string, string, string, string) {
	t.Helper()
	q := query.Use(db)
	ctx := context.Background()
	// workspace 主键为 uuid 文本（无自增），两个 workspace 显式赋唯一 id（other 为独立 workspace，对照组）。
	const workspaceID = "0198eeee-0000-7111-8111-3b6ac9e1f100"
	const otherWorkspaceID = "0198eeee-0000-7111-8111-3b6ac9e1f199"
	if err := q.Workspace.WithContext(ctx).Create(&model.Workspace{ID: workspaceID, Name: "ws"}); err != nil {
		t.Fatalf("建 workspace: %v", err)
	}
	const issueAID = "0198eeee-0000-7111-8111-3b6ac9e1f201"
	const issueBID = "0198eeee-0000-7111-8111-3b6ac9e1f202"
	const otherID = "0198eeee-0000-7111-8111-3b6ac9e1f299"
	for _, is := range []*model.WorkspaceIssue{
		{ID: issueAID, WorkspaceID: workspaceID, Name: "任务A", StateCode: enums.STATE_CODE_BACKLOG, TypeID: 1},
		{ID: issueBID, WorkspaceID: workspaceID, Name: "任务B", StateCode: enums.STATE_CODE_BACKLOG, TypeID: 1},
		{ID: otherID, WorkspaceID: otherWorkspaceID, Name: "无关任务", StateCode: enums.STATE_CODE_BACKLOG, TypeID: 1}, // 独立 workspace：两删除场景均不被级联，充当「未删 issue」对照组
	} {
		if err := q.WorkspaceIssue.WithContext(ctx).Create(is); err != nil {
			t.Fatalf("建 issue: %v", err)
		}
	}
	for _, conv := range []*model.ImBotConversation{
		{ID: "conv-u1", BotID: "bot-1", ConversationKey: "single:u1", BoundIssueID: issueAID},
		{ID: "conv-u2", BotID: "bot-1", ConversationKey: "single:u2", BoundIssueID: issueBID},
		{ID: "conv-u3", BotID: "bot-1", ConversationKey: "single:u3", BoundIssueID: otherID, ClaudeSessionID: "sess-keep"},
	} {
		if err := q.ImBotConversation.WithContext(ctx).Create(conv); err != nil {
			t.Fatalf("建会话行: %v", err)
		}
	}
	return workspaceID, issueAID, issueBID, otherID
}

/*
 * assertBindings 清绑断言：want 为 会话键 → 期望绑定锚（空串 = 已清空，行保留）；
 * headless 续聊锚与绑定锚正交，恒不动。
 */
func assertBindings(t *testing.T, db *gorm.DB, want map[string]string) {
	t.Helper()
	q := query.Use(db)
	ctx := context.Background()
	convs, err := q.ImBotConversation.WithContext(ctx).Find()
	if err != nil {
		t.Fatalf("读会话行: %v", err)
	}
	if len(convs) != len(want) {
		t.Fatalf("清绑必须清列保行: got %d 行 want %d 行", len(convs), len(want))
	}
	for _, c := range convs {
		wantBound, ok := want[c.ConversationKey]
		if !ok {
			t.Fatalf("意外会话行: %s", c.ConversationKey)
		}
		if c.BoundIssueID != wantBound {
			t.Fatalf("%s 绑定锚不符: got %q want %q", c.ConversationKey, c.BoundIssueID, wantBound)
		}
		if c.ConversationKey == "single:u3" && c.ClaudeSessionID != "sess-keep" {
			t.Fatalf("headless 续聊锚须正交保留: %q", c.ClaudeSessionID)
		}
	}
}

/*
 * TestWorkspaceIssueDeleteClearsBinding 删 issue 事务内清绑：被删 issue 的绑定锚清空、
 * 行保留、外部绑定与续聊锚不动。
 */
func TestWorkspaceIssueDeleteClearsBinding(t *testing.T) {
	db := newCascadeBindingTestDB(t)
	_, issueAID, issueBID, otherID := mkCascadeFixture(t, db)
	svc := WorkspaceIssue{Service: apis.Service{Context: context.Background(), Orm: db, Logger: zap.NewNop()}}

	if err := svc.Delete(&types.WorkspaceIssueDeleteRequest{ID: issueAID}); err != nil {
		t.Fatalf("删除 issue: %v", err)
	}
	q := query.Use(db)
	issues, err := q.WorkspaceIssue.WithContext(context.Background()).Find()
	if err != nil || len(issues) != 2 || issues[0].ID == issueAID {
		t.Fatalf("被删 issue 应消失、其余保留: n=%d err=%v", len(issues), err)
	}
	// 仅被删 issue 的绑定清空；存活的 issueB 绑定与外部绑定不动。
	assertBindings(t, db, map[string]string{"single:u1": "", "single:u2": issueBID, "single:u3": otherID})
}

/*
 * TestWorkspaceDeleteCascadeClearsBinding 删 workspace 级联同款清绑契约：
 * 两入口（单删/工作空间级联）语义一致。
 */
func TestWorkspaceDeleteCascadeClearsBinding(t *testing.T) {
	db := newCascadeBindingTestDB(t)
	workspaceID, _, _, otherID := mkCascadeFixture(t, db)
	svc := Workspace{Service: apis.Service{Context: context.Background(), Orm: db, Logger: zap.NewNop()}}

	if err := svc.Delete(&types.WorkspaceDeleteRequest{ID: workspaceID}); err != nil {
		t.Fatalf("删除 workspace: %v", err)
	}
	q := query.Use(db)
	issues, err := q.WorkspaceIssue.WithContext(context.Background()).Find()
	// workspace 下 issue 全删，独立 workspace 的对照组行保留。
	if err != nil || len(issues) != 1 || issues[0].ID != otherID {
		t.Fatalf("workspace 下 issue 应级联删除、对照组保留: n=%d err=%v", len(issues), err)
	}
	assertBindings(t, db, map[string]string{"single:u1": "", "single:u2": "", "single:u3": otherID})
}
