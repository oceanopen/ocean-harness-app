package bot

import (
	"testing"

	"gorm.io/gorm"

	"github.com/google/uuid"

	"ocean-harness/server/internal/acpsession"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
)

/*
 * TestDriverRouteWorkspaceEntitySession workspace 档路由（mode=acp）：取/建工作空间 ACP 会话实体行
 * （进工作空间会话列表），锚指向实体行；bot 会话行回绑 conversation_id（归属可倒推）。
 */
func TestDriverRouteWorkspaceEntitySession(t *testing.T) {
	db := routeTestDB(t)
	seedWorkspaceRow(t, db, "ws-1", `{"mode":"acp"}`)
	seedBotConv(t, db, "bot-1", "single:u1")
	r := newDriverRoute(db, nil)

	target, err := r.resolve(BotRuntimeConfig{BotID: "bot-1", ConversationType: enums.CONVERSATION_TYPE_WORKSPACE, WorkspaceID: "ws-1"}, "single:u1")
	if err != nil {
		t.Fatalf("workspace 档路由: %v", err)
	}
	if !target.ACP {
		t.Fatalf("acp 档应路由 ACP 引擎: %+v", target)
	}
	var convs []model.Conversation
	if err := db.Where(&model.Conversation{OwnerType: enums.OWNER_TYPE_WORKSPACE, OwnerID: "ws-1"}).Find(&convs).Error; err != nil {
		t.Fatalf("查工作空间会话实体: %v", err)
	}
	if len(convs) != 1 || convs[0].Mode != enums.CONVERSATION_MODE_ACP {
		t.Fatalf("应取/建工作空间 ACP 会话实体: %+v", convs)
	}
	if target.SessionKey != acpsession.ConversationSessionKey(convs[0].ID) {
		t.Fatalf("锚应指向会话实体行: %q", target.SessionKey)
	}
	var botConv model.ImBotConversation
	if err := db.Where(&model.ImBotConversation{BotID: "bot-1", ConversationKey: "single:u1"}).First(&botConv).Error; err != nil {
		t.Fatalf("查 bot 会话行: %v", err)
	}
	if botConv.ConversationID != convs[0].ID {
		t.Fatalf("bot 会话行应回绑 conversation_id: %q", botConv.ConversationID)
	}
}

/*
 * TestDriverRouteWorkspaceHeadless workspace 档非 acp 档（terminal-auto）：headless 目标
 * （ACP=false、无会话锚），不建会话实体、不回绑 conversation_id。
 */
func TestDriverRouteWorkspaceHeadless(t *testing.T) {
	db := routeTestDB(t)
	seedWorkspaceRow(t, db, "ws-1", `{"mode":"terminal-auto"}`)
	seedBotConv(t, db, "bot-1", "single:u1")
	r := newDriverRoute(db, nil)

	target, err := r.resolve(BotRuntimeConfig{BotID: "bot-1", ConversationType: enums.CONVERSATION_TYPE_WORKSPACE, WorkspaceID: "ws-1"}, "single:u1")
	if err != nil {
		t.Fatalf("workspace 档路由: %v", err)
	}
	if target.ACP || target.SessionKey != "" {
		t.Fatalf("非 acp 档应路由 headless: %+v", target)
	}
	var convs []model.Conversation
	if err := db.Where(&model.Conversation{OwnerType: enums.OWNER_TYPE_WORKSPACE, OwnerID: "ws-1"}).Find(&convs).Error; err != nil {
		t.Fatalf("查工作空间会话实体: %v", err)
	}
	if len(convs) != 0 {
		t.Fatalf("headless 档不得建 ACP 会话实体: %+v", convs)
	}
	var botConv model.ImBotConversation
	if err := db.Where(&model.ImBotConversation{BotID: "bot-1", ConversationKey: "single:u1"}).First(&botConv).Error; err != nil {
		t.Fatalf("查 bot 会话行: %v", err)
	}
	if botConv.ConversationID != "" {
		t.Fatalf("headless 档不得回绑 conversation_id: %q", botConv.ConversationID)
	}
}

/*
 * TestDriverRouteWorkspaceUnconfiguredHeadless 未配置（launch_settings 空）与容器行已删：
 * 均按未配置回落 headless（fail safe，不误入 ACP）。
 */
func TestDriverRouteWorkspaceUnconfiguredHeadless(t *testing.T) {
	db := routeTestDB(t)
	seedBotConv(t, db, "bot-1", "single:u1")
	r := newDriverRoute(db, nil)

	cfg := BotRuntimeConfig{BotID: "bot-1", ConversationType: enums.CONVERSATION_TYPE_WORKSPACE, WorkspaceID: "ws-1"}
	if target, err := r.resolve(cfg, "single:u1"); err != nil || target.ACP || target.SessionKey != "" {
		t.Fatalf("容器行已删应回落 headless: %+v err=%v", target, err)
	}
	seedWorkspaceRow(t, db, "ws-2", "")
	cfg.WorkspaceID = "ws-2"
	if target, err := r.resolve(cfg, "single:u1"); err != nil || target.ACP || target.SessionKey != "" {
		t.Fatalf("未配置应回落 headless: %+v err=%v", target, err)
	}
}

/*
 * TestDriverRouteHeadlessAnchorUntouched headless 会话锚（bot 会话行 claude_session_id）不被
 * 路由层触碰：预置续聊锚后 resolve，锚保持原值（读写链在编排器 runTurn）。
 */
func TestDriverRouteHeadlessAnchorUntouched(t *testing.T) {
	db := routeTestDB(t)
	seedWorkspaceRow(t, db, "ws-1", "")
	seedBotConv(t, db, "bot-1", "single:u1")
	store := &ConversationStore{DB: db}
	if err := store.SaveSessionID("bot-1", "single:u1", "s-prev"); err != nil {
		t.Fatalf("预置续聊锚: %v", err)
	}
	r := newDriverRoute(db, nil)

	if _, err := r.resolve(BotRuntimeConfig{BotID: "bot-1", ConversationType: enums.CONVERSATION_TYPE_WORKSPACE, WorkspaceID: "ws-1"}, "single:u1"); err != nil {
		t.Fatalf("headless 路由: %v", err)
	}
	if id, err := store.SessionID("bot-1", "single:u1"); err != nil || id != "s-prev" {
		t.Fatalf("路由层不得触碰 headless 锚: id=%q err=%v", id, err)
	}
}

// TestDriverRouteWorkspaceMissing workspace 档未选工作空间：fail closed 报错。
func TestDriverRouteWorkspaceMissing(t *testing.T) {
	db := routeTestDB(t)
	seedBotConv(t, db, "bot-1", "single:u1")
	r := newDriverRoute(db, nil)

	if _, err := r.resolve(BotRuntimeConfig{BotID: "bot-1", ConversationType: enums.CONVERSATION_TYPE_WORKSPACE}, "single:u1"); err == nil {
		t.Fatalf("未选工作空间应报错")
	}
}

/*
 * TestDriverRouteProjectCreatesMain project 档·任务无主会话（容器合并档 = acp）：新建 ACP
 * 主会话（owner=task、parent 空）并以 conv-<主会话行id> 为锚。
 */
func TestDriverRouteProjectCreatesMain(t *testing.T) {
	db := routeTestDB(t)
	taskID := seedRouteTask(t, db)
	seedBotConv(t, db, "bot-1", "single:u1")
	r := newDriverRoute(db, nil)

	target, err := r.resolve(taskCfg(taskID), "single:u1")
	if err != nil {
		t.Fatalf("project 档路由: %v", err)
	}
	if !target.ACP {
		t.Fatalf("acp 档应路由 ACP 引擎: %+v", target)
	}
	var convs []model.Conversation
	if err := db.Where(&model.Conversation{OwnerType: enums.OWNER_TYPE_TASK, OwnerID: taskID}).Find(&convs).Error; err != nil {
		t.Fatalf("查会话实体: %v", err)
	}
	if len(convs) != 1 || convs[0].Mode != enums.CONVERSATION_MODE_ACP || convs[0].ParentID != "" {
		t.Fatalf("应新建 ACP 主会话: %+v", convs)
	}
	if target.SessionKey != acpsession.ConversationSessionKey(convs[0].ID) {
		t.Fatalf("锚应指向新建主会话: %q", target.SessionKey)
	}
}

/*
 * TestDriverRouteProjectReusesAcpMain project 档·主会话已为 ACP 档：直接复用（不新建会话行）
 * 并回绑 bot 会话行 conversation_id。
 */
func TestDriverRouteProjectReusesAcpMain(t *testing.T) {
	db := routeTestDB(t)
	taskID := seedRouteTask(t, db)
	mainID := seedConversation(t, db, taskID, "", enums.CONVERSATION_MODE_ACP)
	seedBotConv(t, db, "bot-1", "single:u1")
	r := newDriverRoute(db, nil)

	target, err := r.resolve(taskCfg(taskID), "single:u1")
	if err != nil {
		t.Fatalf("project 档路由: %v", err)
	}
	if target.SessionKey != acpsession.ConversationSessionKey(mainID) {
		t.Fatalf("应复用既有 ACP 主会话: %q", target.SessionKey)
	}
	var convs []model.Conversation
	if err := db.Where(&model.Conversation{OwnerType: enums.OWNER_TYPE_TASK, OwnerID: taskID}).Find(&convs).Error; err != nil {
		t.Fatalf("查会话实体: %v", err)
	}
	if len(convs) != 1 {
		t.Fatalf("复用不得新建会话行: %d", len(convs))
	}
	var botConv model.ImBotConversation
	if err := db.Where(&model.ImBotConversation{BotID: "bot-1", ConversationKey: "single:u1"}).First(&botConv).Error; err != nil {
		t.Fatalf("查 bot 会话行: %v", err)
	}
	if botConv.ConversationID != mainID {
		t.Fatalf("bot 会话行应回绑 conversation_id: %q", botConv.ConversationID)
	}
}

/*
 * TestDriverRouteProjectTerminalMainCreatesSub project 档·主会话为终端档：并建 ACP 子会话
 * （parent=主会话行id），桌面主会话不受影响。
 */
func TestDriverRouteProjectTerminalMainCreatesSub(t *testing.T) {
	db := routeTestDB(t)
	taskID := seedRouteTask(t, db)
	mainID := seedConversation(t, db, taskID, "", enums.CONVERSATION_MODE_TERMINAL)
	seedBotConv(t, db, "bot-1", "single:u1")
	r := newDriverRoute(db, nil)

	target, err := r.resolve(taskCfg(taskID), "single:u1")
	if err != nil {
		t.Fatalf("project 档路由: %v", err)
	}
	var subs []model.Conversation
	if err := db.Where(&model.Conversation{OwnerType: enums.OWNER_TYPE_TASK, OwnerID: taskID, ParentID: mainID}).Find(&subs).Error; err != nil {
		t.Fatalf("查子会话: %v", err)
	}
	if len(subs) != 1 || subs[0].Mode != enums.CONVERSATION_MODE_ACP {
		t.Fatalf("应并建 ACP 子会话: %+v", subs)
	}
	if target.SessionKey != acpsession.ConversationSessionKey(subs[0].ID) {
		t.Fatalf("锚应指向新建子会话: %q", target.SessionKey)
	}
}

/*
 * TestDriverRouteProjectHeadless project 档非 acp 档（terminal-auto）：headless 目标，
 * 终端主会话不被并建 ACP 子会话。
 */
func TestDriverRouteProjectHeadless(t *testing.T) {
	db := routeTestDB(t)
	seedProjectRow(t, db, "proj-1", `{"mode":"terminal-auto"}`)
	taskID := seedTaskRow(t, db, "proj-1", "")
	mainID := seedConversation(t, db, taskID, "", enums.CONVERSATION_MODE_TERMINAL)
	seedBotConv(t, db, "bot-1", "single:u1")
	r := newDriverRoute(db, nil)

	target, err := r.resolve(taskCfg(taskID), "single:u1")
	if err != nil {
		t.Fatalf("project 档路由: %v", err)
	}
	if target.ACP || target.SessionKey != "" {
		t.Fatalf("非 acp 档应路由 headless: %+v", target)
	}
	var subs []model.Conversation
	if err := db.Where(&model.Conversation{OwnerType: enums.OWNER_TYPE_TASK, OwnerID: taskID, ParentID: mainID}).Find(&subs).Error; err != nil {
		t.Fatalf("查子会话: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("headless 档不得并建 ACP 子会话: %+v", subs)
	}
}

/*
 * TestDriverRouteProjectModeMerge 双级合并（task 显式键覆盖 project）：acp 档被 task 显式
 * none 覆盖 → headless；未配置 project 被 task 显式 acp 覆盖 → ACP。
 */
func TestDriverRouteProjectModeMerge(t *testing.T) {
	db := routeTestDB(t)
	seedProjectRow(t, db, "proj-1", `{"mode":"acp"}`)
	overridden := seedTaskRow(t, db, "proj-1", `{"mode":"none"}`)
	seedProjectRow(t, db, "proj-2", "")
	coverage := seedTaskRow(t, db, "proj-2", `{"mode":"acp"}`)
	seedBotConv(t, db, "bot-1", "single:u1")
	r := newDriverRoute(db, nil)

	if target, err := r.resolve(taskCfg(overridden), "single:u1"); err != nil || target.ACP || target.SessionKey != "" {
		t.Fatalf("task 显式 none 应覆盖为 headless: %+v err=%v", target, err)
	}
	target, err := r.resolve(taskCfg(coverage), "single:u1")
	if err != nil {
		t.Fatalf("task 显式 acp 路由: %v", err)
	}
	if !target.ACP {
		t.Fatalf("task 显式 acp 应覆盖未配置 project 为 ACP: %+v", target)
	}
	var mains []model.Conversation
	if err := db.Where(&model.Conversation{OwnerType: enums.OWNER_TYPE_TASK, OwnerID: coverage, ParentID: ""}).Find(&mains).Error; err != nil {
		t.Fatalf("查主会话: %v", err)
	}
	if len(mains) != 1 || mains[0].Mode != enums.CONVERSATION_MODE_ACP {
		t.Fatalf("应新建 ACP 主会话: %+v", mains)
	}
}

// TestDriverRouteProjectUnboundTaskRejected project 档未关联任务：路由层兜底拒绝（编排器门禁已先行拦截）。
func TestDriverRouteProjectUnboundTaskRejected(t *testing.T) {
	db := routeTestDB(t)
	seedBotConv(t, db, "bot-1", "single:u1")
	r := newDriverRoute(db, nil)

	cfg := BotRuntimeConfig{BotID: "bot-1", ConversationType: enums.CONVERSATION_TYPE_PROJECT, ProjectID: "proj-1", AccessPolicy: openAccess()}
	if _, err := r.resolve(cfg, "single:u1"); err == nil {
		t.Fatalf("未关联任务应拒绝")
	}
}

// TestDriverRouteTypeUnconfigured 会话类型未配置（遗留哨兵行）：fail closed 报错。
func TestDriverRouteTypeUnconfigured(t *testing.T) {
	db := routeTestDB(t)
	seedBotConv(t, db, "bot-1", "single:u1")
	r := newDriverRoute(db, nil)

	cfg := BotRuntimeConfig{BotID: "bot-1", AccessPolicy: openAccess()}
	if _, err := r.resolve(cfg, "single:u1"); err == nil {
		t.Fatalf("会话类型未配置应报错")
	}
}

// routeTestDB 任务路由测试库：bot 会话行 + 会话实体 + 任务/项目/工作空间容器表。
func routeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newBotTestDB(t, &model.ImBotConversation{}, &model.Conversation{}, &model.ProjectTask{}, &model.Project{}, &model.Workspace{})
}

// seedBotConv 预置 bot 会话行并返回行 id（conv- 锚载体）。
func seedBotConv(t *testing.T, db *gorm.DB, botID, convKey string) string {
	t.Helper()
	row, err := (&ConversationStore{DB: db}).GetOrCreate(botID, convKey)
	if err != nil {
		t.Fatalf("预置 bot 会话行: %v", err)
	}
	return row.ID
}

// seedWorkspaceRow 预置工作空间行（launchSettingsRaw 为 JSON 列原文，空串 = 未配置）。
func seedWorkspaceRow(t *testing.T, db *gorm.DB, id, launchSettingsRaw string) {
	t.Helper()
	row := &model.Workspace{ID: id, Name: "工作空间", Dir: "/tmp/ocean-ws-" + id, LaunchSettings: launchSettingsRaw}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("预置工作空间: %v", err)
	}
}

// seedProjectRow 预置项目行（launchSettingsRaw 为 JSON 列原文，空串 = 未配置）。
func seedProjectRow(t *testing.T, db *gorm.DB, id, launchSettingsRaw string) {
	t.Helper()
	row := &model.Project{ID: id, Name: "项目", Dir: "/tmp/ocean-proj-" + id, LaunchSettings: launchSettingsRaw}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("预置项目: %v", err)
	}
}

// seedTaskRow 在指定项目下预置任务行（launchSettingsRaw 为 JSON 列原文），返回任务 id。
func seedTaskRow(t *testing.T, db *gorm.DB, projectID, launchSettingsRaw string) string {
	t.Helper()
	row := &model.ProjectTask{ID: uuid.Must(uuid.NewV7()).String(), ProjectID: projectID, Name: "任务", LaunchSettings: launchSettingsRaw}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("预置任务: %v", err)
	}
	return row.ID
}

// seedRouteTask ACP 档路由测试的默认夹具：proj-1 配置 mode=acp，任务行不覆盖，返回任务 id。
func seedRouteTask(t *testing.T, db *gorm.DB) string {
	t.Helper()
	seedProjectRow(t, db, "proj-1", `{"mode":"acp"}`)
	return seedTaskRow(t, db, "proj-1", "")
}

// seedConversation 直插一行会话实体，返回行 id。
func seedConversation(t *testing.T, db *gorm.DB, ownerID, parentID string, mode enums.ConversationMode) string {
	t.Helper()
	row := &model.Conversation{
		ID:        uuid.Must(uuid.NewV7()).String(),
		OwnerType: enums.OWNER_TYPE_TASK,
		OwnerID:   ownerID,
		Title:     "会话",
		Mode:      mode,
		ParentID:  parentID,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("预置会话实体: %v", err)
	}
	return row.ID
}

// taskCfg project 档运行配置。
func taskCfg(taskID string) BotRuntimeConfig {
	return BotRuntimeConfig{BotID: "bot-1", ConversationType: enums.CONVERSATION_TYPE_PROJECT, ProjectID: "proj-1", TaskID: taskID, AccessPolicy: openAccess()}
}
