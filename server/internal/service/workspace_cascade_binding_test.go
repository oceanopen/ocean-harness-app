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
 * newCascadeBindingTestDB 内存 sqlite + 级联链路表集（workspace / bot / 会话映射 /
 * project / task / 任务仓库关联）。
 */
func newCascadeBindingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.ImBot{}, &model.Conversation{}, &model.Project{}, &model.ProjectTask{}, &model.TaskLocalRepository{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

/*
 * mkCascadeFixture 建 workspace + bot（绑 workspace）+ 两 bot 会话行（带归属/带续聊锚）+
 * project + 两任务（任务A / 对照组）+ 任务A 的仓库关联，返回 (workspaceID, taskAID)。
 */
func mkCascadeFixture(t *testing.T, db *gorm.DB) (string, string) {
	t.Helper()
	q := query.Use(db)
	ctx := context.Background()
	const workspaceID = "0198eeee-0000-7111-8111-3b6ac9e1f100"
	if err := q.Workspace.WithContext(ctx).Create(&model.Workspace{ID: workspaceID, Name: "ws"}); err != nil {
		t.Fatalf("建 workspace: %v", err)
	}
	if err := q.ImBot.WithContext(ctx).Create(&model.ImBot{
		ID: "bot-1", Name: "b1", Channel: enums.CHANNEL_WECOM, WorkspaceID: workspaceID,
		AccessPolicy: "{}", Enabled: enums.YES_NO_NO,
	}); err != nil {
		t.Fatalf("建 bot: %v", err)
	}
	for _, conv := range []*model.Conversation{
		{ID: "conv-u1", OwnerType: enums.OWNER_TYPE_TASK, OwnerID: "0198eeee-0000-7111-8111-3b6ac9e1f201", Mode: enums.CONVERSATION_MODE_ACP, BotID: "bot-1", BotChannel: enums.CHANNEL_WECOM, BotConversationKey: "single:u1"},
		{ID: "conv-u2", OwnerType: enums.OWNER_TYPE_WORKSPACE, OwnerID: workspaceID, Mode: enums.CONVERSATION_MODE_TERMINAL, ClaudeSessionID: "sess-keep", ClaudeSessionStatus: enums.CLAUDE_SESSION_STATUS_ACTIVE, BotID: "bot-1", BotChannel: enums.CHANNEL_WECOM, BotConversationKey: "single:u2"},
	} {
		if err := q.Conversation.WithContext(ctx).Create(conv); err != nil {
			t.Fatalf("建会话行: %v", err)
		}
	}
	if err := q.Project.WithContext(ctx).Create(&model.Project{ID: "proj-1", Name: "p1", Dir: t.TempDir()}); err != nil {
		t.Fatalf("建 project: %v", err)
	}
	const taskAID = "0198eeee-0000-7111-8111-3b6ac9e1f201"
	for _, task := range []*model.ProjectTask{
		{ID: taskAID, ProjectID: "proj-1", Name: "任务A"},
		{ID: "0198eeee-0000-7111-8111-3b6ac9e1f202", ProjectID: "proj-1", Name: "对照组"},
	} {
		if err := q.ProjectTask.WithContext(ctx).Create(task); err != nil {
			t.Fatalf("建 task: %v", err)
		}
	}
	if err := q.TaskLocalRepository.WithContext(ctx).Create(&model.TaskLocalRepository{
		TaskID: taskAID, LocalRepositoryID: 7, RepositoryBranch: "feat/x",
	}); err != nil {
		t.Fatalf("建任务仓库关联: %v", err)
	}
	return workspaceID, taskAID
}

/*
 * TestTaskDeleteCascade 删任务级联：任务行 + 其仓库关联硬删；会话行不动
 * （bound_issue_id 清绑随列下线移除，归属锚由 T4.3 会话域接管）。
 */
func TestTaskDeleteCascade(t *testing.T) {
	db := newCascadeBindingTestDB(t)
	_, taskAID := mkCascadeFixture(t, db)
	svc := ProjectTask{Service: apis.Service{Context: context.Background(), Orm: db, Logger: zap.NewNop()}}

	if err := svc.Delete(&types.ProjectTaskDeleteRequest{ID: taskAID}); err != nil {
		t.Fatalf("删除任务: %v", err)
	}
	q := query.Use(db)
	tasks, err := q.ProjectTask.WithContext(context.Background()).Find()
	if err != nil || len(tasks) != 1 || tasks[0].ID == taskAID {
		t.Fatalf("被删任务应消失、对照组保留: n=%d err=%v", len(tasks), err)
	}
	links, err := q.TaskLocalRepository.WithContext(context.Background()).Where(q.TaskLocalRepository.TaskID.Eq(taskAID)).Find()
	if err != nil || len(links) != 0 {
		t.Fatalf("被删任务的仓库关联应级联硬删: n=%d err=%v", len(links), err)
	}
	convs, err := q.Conversation.WithContext(context.Background()).Find()
	if err != nil || len(convs) != 2 {
		t.Fatalf("会话行不应被任务删除触碰: n=%d err=%v", len(convs), err)
	}
}

/*
 * TestWorkspaceDeleteUnbindsBots 删 workspace 级联：关联 bot 的 workspace_id 置空（回到
 * 未选态）；任务/会话行不属工作空间级联面（归项目域与会话域）。
 */
func TestWorkspaceDeleteUnbindsBots(t *testing.T) {
	db := newCascadeBindingTestDB(t)
	workspaceID, _ := mkCascadeFixture(t, db)
	svc := Workspace{Service: apis.Service{Context: context.Background(), Orm: db, Logger: zap.NewNop()}}

	if err := svc.Delete(&types.WorkspaceDeleteRequest{ID: workspaceID}); err != nil {
		t.Fatalf("删除 workspace: %v", err)
	}
	q := query.Use(db)
	bot, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.ID.Eq("bot-1")).First()
	if err != nil {
		t.Fatalf("读 bot: %v", err)
	}
	if bot.WorkspaceID != "" {
		t.Fatalf("bot workspace_id 应置空（回到未选态）: %q", bot.WorkspaceID)
	}
	tasks, err := q.ProjectTask.WithContext(context.Background()).Find()
	if err != nil || len(tasks) != 2 {
		t.Fatalf("任务不属工作空间级联面，应全部保留: n=%d err=%v", len(tasks), err)
	}
	convs, err := q.Conversation.WithContext(context.Background()).Find()
	if err != nil || len(convs) != 2 {
		t.Fatalf("会话行不应被 workspace 删除触碰: n=%d err=%v", len(convs), err)
	}
}
