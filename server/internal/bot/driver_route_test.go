package bot

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/google/uuid"

	"ocean-harness/server/internal/acpsession"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
)

/*
 * TestDriverRouteWorkspaceEntitySession workspace 档路由（mode=acp）：bot 会话行即会话实体行
 * （owner=workspace、bot 字落在行、档位覆写 acp），锚键指向该行——行进工作空间会话列表。
 */
func TestDriverRouteWorkspaceEntitySession(t *testing.T) {
	db := routeTestDB(t)
	seedWorkspaceRow(t, db, "ws-1", `{"mode":"acp"}`)
	r := newDriverRoute(db, nil)
	cfg := workspaceBotCfg("bot-1", "ws-1")

	target, err := r.resolve(cfg, "single:u1")
	if err != nil {
		t.Fatalf("workspace 档路由: %v", err)
	}
	if !target.ACP {
		t.Fatalf("acp 档应路由 ACP 引擎: %+v", target)
	}
	var convs []model.Conversation
	if err := db.Where(&model.Conversation{OwnerType: enums.OWNER_TYPE_WORKSPACE, OwnerID: "ws-1"}).Find(&convs).Error; err != nil {
		t.Fatalf("查工作空间会话行: %v", err)
	}
	if len(convs) != 1 || convs[0].Mode != enums.CONVERSATION_MODE_ACP {
		t.Fatalf("应取/建 bot 会话行并覆写 acp 档: %+v", convs)
	}
	if convs[0].BotID != "bot-1" || convs[0].BotConversationKey != "single:u1" {
		t.Fatalf("bot 字段应落会话行: %+v", convs[0])
	}
	if target.SessionKey != acpsession.ConversationSessionKey(convs[0].ID) {
		t.Fatalf("锚键应指向 bot 会话行: %q", target.SessionKey)
	}
}

/*
 * TestDriverRouteWorkspaceHeadless workspace 档非 acp 档（terminal-auto）：headless 目标
 * （ACP=false、无会话锚键），bot 会话行档位对齐 terminal。
 */
func TestDriverRouteWorkspaceHeadless(t *testing.T) {
	db := routeTestDB(t)
	seedWorkspaceRow(t, db, "ws-1", `{"mode":"terminal-auto"}`)
	r := newDriverRoute(db, nil)
	cfg := workspaceBotCfg("bot-1", "ws-1")

	target, err := r.resolve(cfg, "single:u1")
	if err != nil {
		t.Fatalf("workspace 档路由: %v", err)
	}
	if target.ACP || target.SessionKey != "" {
		t.Fatalf("非 acp 档应路由 headless: %+v", target)
	}
	var convs []model.Conversation
	if err := db.Where(&model.Conversation{BotID: "bot-1"}).Find(&convs).Error; err != nil {
		t.Fatalf("查 bot 会话行: %v", err)
	}
	if len(convs) != 1 || convs[0].Mode != enums.CONVERSATION_MODE_TERMINAL {
		t.Fatalf("bot 会话行档位应对齐 terminal: %+v", convs)
	}
}

/*
 * TestDriverRouteWorkspaceUnconfiguredHeadless 未配置（launch_settings 空）与容器行已删：
 * 均按未配置回落 headless（fail safe，不误入 ACP）。
 */
func TestDriverRouteWorkspaceUnconfiguredHeadless(t *testing.T) {
	db := routeTestDB(t)
	r := newDriverRoute(db, nil)

	cfg := workspaceBotCfg("bot-1", "ws-1")
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
 * 路由层触碰：active 态锚预置后 resolve，锚保持原值（读写链在编排器 runTurn）。
 */
func TestDriverRouteHeadlessAnchorUntouched(t *testing.T) {
	db := routeTestDB(t)
	seedWorkspaceRow(t, db, "ws-1", "")
	store := &ConversationStore{DB: db}
	cfg := workspaceBotCfg("bot-1", "ws-1")
	r := newDriverRoute(db, nil)

	if _, err := store.EnsureConversation(cfg, "single:u1"); err != nil {
		t.Fatalf("预置会话行: %v", err)
	}
	if err := store.FinishTurn(cfg, "single:u1", "s-prev", time.Now()); err != nil {
		t.Fatalf("预置续聊锚: %v", err)
	}
	if _, err := r.resolve(cfg, "single:u1"); err != nil {
		t.Fatalf("headless 路由: %v", err)
	}
	if resume, preset, err := store.HeadlessAnchor(cfg, "single:u1"); err != nil || resume != "s-prev" || preset != "" {
		t.Fatalf("路由层不得触碰 headless 锚: resume=%q preset=%q err=%v", resume, preset, err)
	}
}

// TestDriverRouteWorkspaceMissing workspace 档未选工作空间：fail closed 报错。
func TestDriverRouteWorkspaceMissing(t *testing.T) {
	db := routeTestDB(t)
	r := newDriverRoute(db, nil)

	if _, err := r.resolve(BotRuntimeConfig{BotID: "bot-1", ConversationType: enums.CONVERSATION_TYPE_WORKSPACE}, "single:u1"); err == nil {
		t.Fatalf("未选工作空间应报错")
	}
}

/*
 * TestDriverRouteProjectCreatesMain project 档·任务无主会话（容器合并档 = acp）：bot 会话行即
 * 任务主会话（owner=task、parent 空、档位覆写 acp），锚键指向该行。
 */
func TestDriverRouteProjectCreatesMain(t *testing.T) {
	db := routeTestDB(t)
	taskID := seedRouteTask(t, db)
	r := newDriverRoute(db, nil)
	cfg := projectBotCfg("bot-1", taskID)

	target, err := r.resolve(cfg, "single:u1")
	if err != nil {
		t.Fatalf("project 档路由: %v", err)
	}
	if !target.ACP {
		t.Fatalf("acp 档应路由 ACP 引擎: %+v", target)
	}
	var convs []model.Conversation
	if err := db.Where(&model.Conversation{OwnerType: enums.OWNER_TYPE_TASK, OwnerID: taskID}).Find(&convs).Error; err != nil {
		t.Fatalf("查会话行: %v", err)
	}
	if len(convs) != 1 || convs[0].Mode != enums.CONVERSATION_MODE_ACP || convs[0].ParentID != "" {
		t.Fatalf("bot 会话行应即任务主会话: %+v", convs)
	}
	if target.SessionKey != acpsession.ConversationSessionKey(convs[0].ID) {
		t.Fatalf("锚键应指向主会话行: %q", target.SessionKey)
	}
}

/*
 * TestDriverRouteProjectReusesAcpMain project 档·桌面先建主会话：bot 认领既有主会话行
 * （bot 字段迁移其上，共享同会话），不新建行。
 */
func TestDriverRouteProjectReusesAcpMain(t *testing.T) {
	db := routeTestDB(t)
	taskID := seedRouteTask(t, db)
	mainID := seedConversation(t, db, taskID, "", enums.CONVERSATION_MODE_ACP)
	r := newDriverRoute(db, nil)
	cfg := projectBotCfg("bot-1", taskID)

	target, err := r.resolve(cfg, "single:u1")
	if err != nil {
		t.Fatalf("project 档路由: %v", err)
	}
	if target.SessionKey != acpsession.ConversationSessionKey(mainID) {
		t.Fatalf("应认领既有主会话行: %q", target.SessionKey)
	}
	var convs []model.Conversation
	if err := db.Where(&model.Conversation{OwnerType: enums.OWNER_TYPE_TASK, OwnerID: taskID}).Find(&convs).Error; err != nil {
		t.Fatalf("查会话行: %v", err)
	}
	if len(convs) != 1 {
		t.Fatalf("认领不得新建会话行: %d", len(convs))
	}
	if convs[0].BotID != "bot-1" || convs[0].BotConversationKey != "single:u1" {
		t.Fatalf("认领应迁移 bot 字段到主会话行: %+v", convs[0])
	}
}

/*
 * TestDriverRouteProjectTerminalMainClaimed project 档·主会话为终端档且容器档 = acp：bot 认领
 * 主会话行并覆写档位 acp（档位跟随容器 launch 档，无子会话分建）。
 */
func TestDriverRouteProjectTerminalMainClaimed(t *testing.T) {
	db := routeTestDB(t)
	taskID := seedRouteTask(t, db)
	mainID := seedConversation(t, db, taskID, "", enums.CONVERSATION_MODE_TERMINAL)
	r := newDriverRoute(db, nil)
	cfg := projectBotCfg("bot-1", taskID)

	target, err := r.resolve(cfg, "single:u1")
	if err != nil {
		t.Fatalf("project 档路由: %v", err)
	}
	if target.SessionKey != acpsession.ConversationSessionKey(mainID) {
		t.Fatalf("应认领终端主会话行: %q", target.SessionKey)
	}
	var main model.Conversation
	if err := db.Where(&model.Conversation{ID: mainID}).First(&main).Error; err != nil {
		t.Fatalf("查主会话行: %v", err)
	}
	if main.Mode != enums.CONVERSATION_MODE_ACP || main.BotID != "bot-1" {
		t.Fatalf("主会话档位应覆写 acp 且 bot 字段落行: %+v", main)
	}
	var subs []model.Conversation
	if err := db.Where(&model.Conversation{ParentID: mainID}).Find(&subs).Error; err != nil {
		t.Fatalf("查子会话: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("档位跟随下不得分建子会话: %+v", subs)
	}
}

/*
 * TestDriverRouteProjectHeadless project 档非 acp 档（terminal-auto）：headless 目标，bot 会话行
 * 认领任务主会话（parent 空、档位对齐 terminal），锚 = 同行 claude_session_id（resume 锚）。
 */
func TestDriverRouteProjectHeadless(t *testing.T) {
	db := routeTestDB(t)
	seedProjectRow(t, db, "proj-1", `{"mode":"terminal-auto"}`)
	taskID := seedTaskRow(t, db, "proj-1", "")
	r := newDriverRoute(db, nil)
	cfg := projectBotCfg("bot-1", taskID)

	target, err := r.resolve(cfg, "single:u1")
	if err != nil {
		t.Fatalf("project 档路由: %v", err)
	}
	if target.ACP || target.SessionKey != "" {
		t.Fatalf("非 acp 档应路由 headless: %+v", target)
	}
	var convs []model.Conversation
	if err := db.Where(&model.Conversation{OwnerType: enums.OWNER_TYPE_TASK, OwnerID: taskID}).Find(&convs).Error; err != nil {
		t.Fatalf("查会话行: %v", err)
	}
	if len(convs) != 1 || convs[0].ParentID != "" || convs[0].Mode != enums.CONVERSATION_MODE_TERMINAL {
		t.Fatalf("headless 档 bot 会话行应即任务主会话: %+v", convs)
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
	r := newDriverRoute(db, nil)

	if target, err := r.resolve(projectBotCfg("bot-1", overridden), "single:u1"); err != nil || target.ACP || target.SessionKey != "" {
		t.Fatalf("task 显式 none 应覆盖为 headless: %+v err=%v", target, err)
	}
	target, err := r.resolve(projectBotCfg("bot-1", coverage), "single:u1")
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
		t.Fatalf("bot 会话行应即 ACP 主会话: %+v", mains)
	}
}

// TestDriverRouteProjectUnboundTaskRejected project 档未关联任务：路由层兜底拒绝（编排器门禁已先行拦截）。
func TestDriverRouteProjectUnboundTaskRejected(t *testing.T) {
	db := routeTestDB(t)
	r := newDriverRoute(db, nil)

	cfg := BotRuntimeConfig{BotID: "bot-1", Channel: enums.CHANNEL_WEIXIN, ConversationType: enums.CONVERSATION_TYPE_PROJECT, ProjectID: "proj-1", AccessPolicy: openAccess()}
	if _, err := r.resolve(cfg, "single:u1"); err == nil {
		t.Fatalf("未关联任务应拒绝")
	}
}

// TestDriverRouteTypeUnconfigured 会话类型未配置（遗留哨兵行）：fail closed 报错。
func TestDriverRouteTypeUnconfigured(t *testing.T) {
	db := routeTestDB(t)
	r := newDriverRoute(db, nil)

	cfg := BotRuntimeConfig{BotID: "bot-1", AccessPolicy: openAccess()}
	if _, err := r.resolve(cfg, "single:u1"); err == nil {
		t.Fatalf("会话类型未配置应报错")
	}
}

// routeTestDB 任务路由测试库：会话行 + 任务/项目/工作空间容器表。
func routeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newBotTestDB(t, &model.Conversation{}, &model.ProjectTask{}, &model.Project{}, &model.Workspace{})
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

// seedConversation 直插一行会话实体（无 bot 字段 = 桌面建行形态），返回行 id。
func seedConversation(t *testing.T, db *gorm.DB, ownerID, parentID string, mode enums.ConversationMode) string {
	t.Helper()
	row := &model.Conversation{
		ID:                  uuid.Must(uuid.NewV7()).String(),
		OwnerType:           enums.OWNER_TYPE_TASK,
		OwnerID:             ownerID,
		Title:               "会话",
		Mode:                mode,
		ParentID:            parentID,
		ClaudeSessionStatus: enums.CLAUDE_SESSION_STATUS_PENDING,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("预置会话实体: %v", err)
	}
	return row.ID
}
