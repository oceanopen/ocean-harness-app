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

// newCascadeBindingTestDB 内存 sqlite + 级联链路四表（project / issue / issue 仓库关联 /
// IM 会话映射）。
func newCascadeBindingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.WorkspaceProject{}, &model.ProjectIssue{}, &model.IssueLocalRepository{}, &model.ProjectLocalRepository{}, &model.ImBotConversation{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

// mkCascadeFixture 建 project + 父 issue + 子 issue + 三个会话行（绑父、绑子、绑外部 issue
// 且带 headless 续聊锚），返回 (projectID, parentID, childID, otherIssueID)。
func mkCascadeFixture(t *testing.T, db *gorm.DB) (int, string, string, string) {
	t.Helper()
	q := query.Use(db)
	ctx := context.Background()
	project := &model.WorkspaceProject{WorkspaceID: 1, Name: "p"}
	if err := q.WorkspaceProject.WithContext(ctx).Create(project); err != nil {
		t.Fatalf("建 project: %v", err)
	}
	const parentID = "0198eeee-0000-7111-8111-3b6ac9e1f201"
	const childID = "0198eeee-0000-7111-8111-3b6ac9e1f202"
	const otherID = "0198eeee-0000-7111-8111-3b6ac9e1f299"
	for _, is := range []*model.ProjectIssue{
		{ID: parentID, ProjectID: project.ID, WorkspaceID: 1, Name: "父任务", StateCode: enums.STATE_CODE_BACKLOG, Priority: enums.PRIORITY_NONE, TypeID: 1},
		{ID: childID, ProjectID: project.ID, WorkspaceID: 1, Name: "子任务", StateCode: enums.STATE_CODE_BACKLOG, Priority: enums.PRIORITY_NONE, TypeID: 1, ParentID: parentID},
		{ID: otherID, ProjectID: project.ID + 1, WorkspaceID: 1, Name: "无关任务", StateCode: enums.STATE_CODE_BACKLOG, Priority: enums.PRIORITY_NONE, TypeID: 1}, // 独立 project：两删除场景均不被级联，充当「未删 issue」对照组
	} {
		if err := q.ProjectIssue.WithContext(ctx).Create(is); err != nil {
			t.Fatalf("建 issue: %v", err)
		}
	}
	for _, conv := range []*model.ImBotConversation{
		{BotID: 1, ConversationKey: "single:u1", BoundIssueID: parentID},
		{BotID: 1, ConversationKey: "single:u2", BoundIssueID: childID},
		{BotID: 1, ConversationKey: "single:u3", BoundIssueID: otherID, ClaudeSessionID: "sess-keep"},
	} {
		if err := q.ImBotConversation.WithContext(ctx).Create(conv); err != nil {
			t.Fatalf("建会话行: %v", err)
		}
	}
	return project.ID, parentID, childID, otherID
}

// assertBindings 清绑断言：u1/u2 绑定清空（行保留），u3 不受牵连且 headless 续聊锚不动。
func assertBindings(t *testing.T, db *gorm.DB, otherID string) {
	t.Helper()
	q := query.Use(db)
	ctx := context.Background()
	convs, err := q.ImBotConversation.WithContext(ctx).Find()
	if err != nil {
		t.Fatalf("读会话行: %v", err)
	}
	if len(convs) != 3 {
		t.Fatalf("清绑必须清列保行（3 行俱在）: got %d 行", len(convs))
	}
	for _, c := range convs {
		switch c.ConversationKey {
		case "single:u1", "single:u2":
			if c.BoundIssueID != "" {
				t.Fatalf("%s 绑定应清空: %q", c.ConversationKey, c.BoundIssueID)
			}
		case "single:u3":
			if c.BoundIssueID != otherID {
				t.Fatalf("未删 issue 的绑定不得牵连: %q", c.BoundIssueID)
			}
			if c.ClaudeSessionID != "sess-keep" {
				t.Fatalf("headless 续聊锚须正交保留: %q", c.ClaudeSessionID)
			}
		}
	}
}

// TestProjectIssueDeleteClearsBinding 删 issue（含子任务）事务内清绑：被删 issue 的绑定
// 锚清空、行保留、外部绑定与续聊锚不动。
func TestProjectIssueDeleteClearsBinding(t *testing.T) {
	db := newCascadeBindingTestDB(t)
	_, parentID, _, otherID := mkCascadeFixture(t, db)
	svc := ProjectIssue{Service: apis.Service{Context: context.Background(), Orm: db, Logger: zap.NewNop()}}

	if err := svc.Delete(&types.ProjectIssueDeleteRequest{ID: parentID}); err != nil {
		t.Fatalf("删除 issue: %v", err)
	}
	q := query.Use(db)
	issues, err := q.ProjectIssue.WithContext(context.Background()).Find()
	if err != nil || len(issues) != 1 || issues[0].ID != otherID {
		t.Fatalf("父+子应被级联删除、外部 issue 保留: n=%d err=%v", len(issues), err)
	}
	assertBindings(t, db, otherID)
}

// TestProjectDeleteCascadeClearsBinding 删 project 级联（deleteProjectCascade）同款清绑
// 契约：两入口（单删/项目级联）语义一致。
func TestProjectDeleteCascadeClearsBinding(t *testing.T) {
	db := newCascadeBindingTestDB(t)
	projectID, _, _, otherID := mkCascadeFixture(t, db)
	svc := Project{Service: apis.Service{Context: context.Background(), Orm: db, Logger: zap.NewNop()}}

	if err := svc.Delete(&types.ProjectDeleteRequest{ID: projectID}); err != nil {
		t.Fatalf("删除 project: %v", err)
	}
	q := query.Use(db)
	issues, err := q.ProjectIssue.WithContext(context.Background()).Find()
	// project 下 issue 全删，独立 project 的对照组行保留。
	if err != nil || len(issues) != 1 || issues[0].ID != otherID {
		t.Fatalf("project 下父+子应级联删除、对照组保留: n=%d err=%v", len(issues), err)
	}
	assertBindings(t, db, otherID)
}
