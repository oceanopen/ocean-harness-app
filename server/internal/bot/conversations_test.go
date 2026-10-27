package bot

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/enums"
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

// newConversationTestDB 内存 sqlite + 会话表（唯一会话源 t_conversations）。
func newConversationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newBotTestDB(t, &model.Conversation{})
}

/*
 * newIssueTestDB 编排器链路夹具：会话表 + 工作空间/任务表（stub 驱动用例仅消费会话表，
 * 真路由用例按需读写工作空间/项目任务行）。
 */
func newIssueTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newBotTestDB(t, &model.Conversation{}, &model.Workspace{}, &model.Project{}, &model.ProjectTask{})
}

// workspaceBotCfg workspace 档测试配置（workspace bot 的最小完备形态）。
func workspaceBotCfg(botID, workspaceID string) BotRuntimeConfig {
	return BotRuntimeConfig{
		BotID:            botID,
		Channel:          enums.CHANNEL_WEIXIN,
		ConversationType: enums.CONVERSATION_TYPE_WORKSPACE,
		WorkspaceID:      workspaceID,
	}
}

// projectBotCfg project 档测试配置（任务 bot 的最小完备形态）。
func projectBotCfg(botID, taskID string) BotRuntimeConfig {
	return BotRuntimeConfig{
		BotID:            botID,
		Channel:          enums.CHANNEL_WECOM,
		ConversationType: enums.CONVERSATION_TYPE_PROJECT,
		TaskID:           taskID,
	}
}

/*
 * TestConversationStorePresetAnchor 建行即预置：EnsureConversation 新行携带预生成 claude 会话
 * 锚（uuid 非空）+ pending 态；pending 态读锚视同无锚（SessionID 空 = 下条消息开新会话）。
 */
func TestConversationStorePresetAnchor(t *testing.T) {
	store := &ConversationStore{DB: newConversationTestDB(t)}
	const convKey = "group:c1"

	conv, err := store.EnsureConversation(workspaceBotCfg("bot-1", "ws-1"), convKey)
	if err != nil {
		t.Fatalf("定位会话行: %v", err)
	}
	if conv.ClaudeSessionID == "" || conv.ClaudeSessionStatus != enums.CLAUDE_SESSION_STATUS_PENDING {
		t.Fatalf("建行应预置锚与 pending 态: id=%q status=%q", conv.ClaudeSessionID, conv.ClaudeSessionStatus)
	}
	if resume, preset, err := store.HeadlessAnchor(workspaceBotCfg("bot-1", "ws-1"), convKey); err != nil || resume != "" || preset == "" {
		t.Fatalf("pending 态应走预置锚: resume=%q preset=%q err=%v", resume, preset, err)
	}
	if err := store.FinishTurn(workspaceBotCfg("bot-1", "ws-1"), convKey, "sess-real", time.Now()); err != nil {
		t.Fatalf("回合回写: %v", err)
	}
	resume, preset, err := store.HeadlessAnchor(workspaceBotCfg("bot-1", "ws-1"), convKey)
	if err != nil || resume != "sess-real" || preset != "" {
		t.Fatalf("active 态应走续聊锚: resume=%q preset=%q err=%v", resume, preset, err)
	}
}

/*
 * TestConversationStoreResetAnchor 锚重置（/clear 与失败自愈）：换新预置 id 并回 pending，
 * 下条消息以新 id 开新会话（与既有 id 不同）。
 */
func TestConversationStoreResetAnchor(t *testing.T) {
	store := &ConversationStore{DB: newConversationTestDB(t)}
	const convKey = "single:u1"
	cfg := workspaceBotCfg("bot-1", "ws-1")

	conv, err := store.EnsureConversation(cfg, convKey)
	if err != nil {
		t.Fatalf("定位会话行: %v", err)
	}
	if err := store.FinishTurn(cfg, convKey, "sess-old", time.Now()); err != nil {
		t.Fatalf("回写旧锚: %v", err)
	}
	if err := store.ResetSessionID(cfg, convKey); err != nil {
		t.Fatalf("锚重置: %v", err)
	}
	q := query.Use(store.DB)
	row, err := q.Conversation.WithContext(t.Context()).Where(q.Conversation.ID.Eq(conv.ID)).First()
	if err != nil {
		t.Fatalf("读回会话行: %v", err)
	}
	if row.ClaudeSessionID == "sess-old" || row.ClaudeSessionID == "" {
		t.Fatalf("重置应换新预置 id: %q", row.ClaudeSessionID)
	}
	if row.ClaudeSessionStatus != enums.CLAUDE_SESSION_STATUS_PENDING {
		t.Fatalf("重置应回 pending 态: %q", row.ClaudeSessionStatus)
	}
}

/*
 * TestConversationStoreClaimsTaskMain project 档认领主会话：无主会话时 bot 行即主会话
 * （parent=''、bot 字落在行）；桌面先建主会话时认领既有行（bot 字段迁移其上，共享同会话）。
 */
func TestConversationStoreClaimsTaskMain(t *testing.T) {
	store := &ConversationStore{DB: newConversationTestDB(t)}
	const convKey = "group:task-1"

	// bot 先建：bot 行即任务主会话。
	conv, err := store.EnsureConversation(projectBotCfg("bot-1", "task-1"), convKey)
	if err != nil {
		t.Fatalf("定位会话行: %v", err)
	}
	if conv.ParentID != "" || conv.OwnerType != enums.OWNER_TYPE_TASK || conv.OwnerID != "task-1" {
		t.Fatalf("bot 先建应即任务主会话行: %+v", conv)
	}
	if conv.BotChannel != enums.CHANNEL_WECOM {
		t.Fatalf("渠道标识应随 bot 落行: %q", conv.BotChannel)
	}

	// 桌面先建主会话：bot 认领既有行（bot 字段迁移其上）。
	q := query.Use(store.DB)
	if _, err := q.Conversation.WithContext(t.Context()).Where(q.Conversation.ID.Eq("conv-desk-1")).First(); err == nil {
		t.Fatalf("前置应为不存在主会话行")
	}
	if err := q.Conversation.WithContext(t.Context()).Create(&model.Conversation{
		ID:                  "conv-desk-1",
		OwnerType:           enums.OWNER_TYPE_TASK,
		OwnerID:             "task-2",
		Title:               "新会话",
		Mode:                enums.CONVERSATION_MODE_TERMINAL,
		ClaudeSessionID:     "desk-preset",
		ClaudeSessionStatus: enums.CLAUDE_SESSION_STATUS_PENDING,
	}); err != nil {
		t.Fatalf("桌面建主会话: %v", err)
	}
	claimed, err := store.EnsureConversation(projectBotCfg("bot-2", "task-2"), "single:u2")
	if err != nil {
		t.Fatalf("认领主会话: %v", err)
	}
	if claimed.ID != "conv-desk-1" || claimed.BotID != "bot-2" || claimed.BotConversationKey != "single:u2" {
		t.Fatalf("认领应迁移 bot 字段到主会话行: %+v", claimed)
	}
}

/*
 * TestConversationStoreMultiBotDegradesToSub 同任务第二 bot：主会话已被他 bot 占用时降级
 * 建子会话行（parent=主会话 id），各自独立会话（多 bot 同任务边界）。
 */
func TestConversationStoreMultiBotDegradesToSub(t *testing.T) {
	store := &ConversationStore{DB: newConversationTestDB(t)}

	if _, err := store.EnsureConversation(projectBotCfg("bot-1", "task-1"), "group:a"); err != nil {
		t.Fatalf("bot-1 定位会话行: %v", err)
	}
	sub, err := store.EnsureConversation(projectBotCfg("bot-2", "task-1"), "group:b")
	if err != nil {
		t.Fatalf("bot-2 定位会话行: %v", err)
	}
	if sub.ParentID == "" {
		t.Fatalf("第二 bot 应降级建子会话行: %+v", sub)
	}
}

/*
 * TestConversationStoreSeenWindow 幂等滚窗：HasSeen/MarkSeen 按 bot 幂等键隔离（渠道间天然
 * 不同行），滚窗上限丢最旧。
 */
func TestConversationStoreSeenWindow(t *testing.T) {
	store := &ConversationStore{DB: newConversationTestDB(t)}
	const convKey = "group:c1"
	cfgA := workspaceBotCfg("bot-a", "ws-1")
	cfgB := workspaceBotCfg("bot-b", "ws-1")

	store.MarkSeen(cfgA, convKey, "m-1")
	if !store.HasSeen(cfgA, convKey, "m-1") {
		t.Fatal("bot-a 应已见 m-1")
	}
	if store.HasSeen(cfgB, convKey, "m-1") {
		t.Fatal("bot-b 不应见 bot-a 的消息（滚窗按 bot 隔离）")
	}
}
