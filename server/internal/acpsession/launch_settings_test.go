package acpsession

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/agentcatalog"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
)

func TestMergeLaunchSettings(t *testing.T) {
	if merged := mergeLaunchSettings(nil, nil); merged != nil {
		t.Fatalf("双侧 nil 应为 nil，got %+v", merged)
	}
	base := &types.WorkspaceLaunchSettings{
		Mode:        "acp",
		AgentCode:   enums.AGENT_CODE_CLAUDE_ACP,
		AutoCommand: "claude",
	}
	// override 空 = 全回落 base。
	merged := mergeLaunchSettings(base, &types.WorkspaceLaunchSettings{})
	if merged.Mode != "acp" || merged.AgentCode != enums.AGENT_CODE_CLAUDE_ACP || merged.AutoCommand != "claude" {
		t.Fatalf("空 override 应全回落 base，got %+v", merged)
	}
	// 显式键覆盖；未配置键回落。
	merged = mergeLaunchSettings(base, &types.WorkspaceLaunchSettings{
		Mode:           "acp",
		PermissionMode: "bypassPermissions",
	})
	if merged.AutoCommand != "claude" {
		t.Fatalf("未配置键应回落 base，got %+v", merged)
	}
	if merged.PermissionMode != "bypassPermissions" {
		t.Fatalf("显式键应覆盖，got %+v", merged)
	}
	// mode 显式 "none" 是有效覆盖值（明说不要 ≠ 未配置）。
	merged = mergeLaunchSettings(base, &types.WorkspaceLaunchSettings{Mode: "none"})
	if merged.Mode != "none" {
		t.Fatalf("mode 显式 none 应保留，got %+v", merged)
	}
	// 仅 override 侧（base 未配置）。
	merged = mergeLaunchSettings(nil, &types.WorkspaceLaunchSettings{Mode: "acp", AgentCode: enums.AGENT_CODE_CLAUDE_ACP})
	if merged == nil || merged.Mode != "acp" {
		t.Fatalf("nil base 应透传 override，got %+v", merged)
	}
}

func TestValidIssueID(t *testing.T) {
	for _, id := range []string{"01JABCDEF", "issue-1", "a_b.c"} {
		if !ValidIssueID(id) {
			t.Fatalf("%q 应合法", id)
		}
	}
	for _, id := range []string{"", ".", "..", "a/b", `a\b`, "../x", "a/..", "./x"} {
		if ValidIssueID(id) {
			t.Fatalf("%q 应拒绝", id)
		}
	}
}

/*
 * newTestDB 临时 sqlite（glebarez 纯 Go 驱动）+ 测试涉及的三张表（AutoMigrate 按 model
 * tag 建列，与 goose 迁移的 DSN 差异不影响本域查询语义）。测试收尾关连接池（后台收尾
 * goroutine 的 best-effort 写库在其后失败无害，仅日志）。
 */
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.Project{}, &model.ProjectTask{}, &model.ImBot{}, &model.ImBotConversation{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

func queryOf(db *gorm.DB) *query.Query {
	return query.Use(db)
}

// seedIssueSeq 同测试内多次种行的主键去重序号。
var seedIssueSeq atomic.Int64

// seedIssue 建 project + task 种子（launch_settings 为 JSON 列原文；返回任务 id）。
func seedIssue(t *testing.T, db *gorm.DB, wsDir, wsLaunchSettings, issueLaunchSettings string) string {
	t.Helper()
	q := queryOf(db)
	// project 为 uuid 文本主键（无自增），显式赋唯一 id。
	proj := &model.Project{ID: fmt.Sprintf("proj-%d", seedIssueSeq.Add(1)), Name: "proj", Dir: wsDir, LaunchSettings: wsLaunchSettings}
	if err := q.Project.WithContext(context.Background()).Create(proj); err != nil {
		t.Fatalf("建 project: %v", err)
	}
	taskID := fmt.Sprintf("issue-%s-%d", strings.ReplaceAll(t.Name(), "/", "-"), seedIssueSeq.Add(1))
	task := &model.ProjectTask{
		ID:             taskID,
		ProjectID:      proj.ID,
		Name:           "测试任务",
		LaunchSettings: issueLaunchSettings,
	}
	if err := q.ProjectTask.WithContext(context.Background()).Create(task); err != nil {
		t.Fatalf("建 task: %v", err)
	}
	return taskID
}

// seedBotConversation 建 bot 行（绑 workspace）+ 会话行（conv- 锚载体），返回会话行 id。
func seedBotConversation(t *testing.T, db *gorm.DB, wsID string) string {
	t.Helper()
	q := queryOf(db)
	botID := fmt.Sprintf("bot-%d", seedIssueSeq.Add(1))
	if err := q.ImBot.WithContext(context.Background()).Create(&model.ImBot{
		ID: botID, Name: "bot", Channel: enums.CHANNEL_WECOM, WorkspaceID: wsID,
		AccessPolicy: `{"mode":"open"}`, Enabled: enums.YES_NO_YES,
	}); err != nil {
		t.Fatalf("建 bot: %v", err)
	}
	convID := fmt.Sprintf("conv-row-%d", seedIssueSeq.Add(1))
	if err := q.ImBotConversation.WithContext(context.Background()).Create(&model.ImBotConversation{
		ID: convID, BotID: botID, ConversationKey: fmt.Sprintf("single:u-%s", convID), SeenMessageIds: "[]",
	}); err != nil {
		t.Fatalf("建会话行: %v", err)
	}
	return convID
}

func TestResolveSessionConfig(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	wsDir := t.TempDir()

	/*
	 * 用例 1：workspace 配 acp、issue 未配置 → 全回落（agentCode 回落首个 enabled 条目，
	 * permissionMode 回落 bypassPermissions），cwd = <wsDir>/<issueId> 且已兜底建目录。
	 */
	issueID := seedIssue(t, db, wsDir, `{"mode":"acp"}`, "")
	cfg, err := resolveSessionConfig(ctx, db, issueID, "")
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}
	enabled := agentcatalog.Enabled()
	if len(enabled) == 0 || cfg.AgentCode != enabled[0].Code {
		t.Fatalf("agentCode 应回落首个 enabled 条目，got %q", cfg.AgentCode)
	}
	if cfg.PermissionMode != acp.PermissionModeBypassPermissions {
		t.Fatalf("permissionMode 应回落 bypassPermissions，got %q", cfg.PermissionMode)
	}
	if want := filepath.Join(wsDir, issueID); cfg.Cwd != want || cfg.WorkspaceDir != wsDir {
		t.Fatalf("cwd = %q, want %q", cfg.Cwd, want)
	}
	if info, err := os.Stat(cfg.Cwd); err != nil || !info.IsDir() {
		t.Fatalf("cwd 应已兜底建目录: %v", err)
	}

	// 用例 2：issue 显式覆盖 permissionMode + agentCode。
	issueID = seedIssue(t, db, wsDir, `{"mode":"acp"}`,
		`{"mode":"acp","agentCode":"claude-acp","permissionMode":"bypassPermissions"}`)
	cfg, err = resolveSessionConfig(ctx, db, issueID, "")
	if err != nil {
		t.Fatalf("resolveSessionConfig(override): %v", err)
	}
	if cfg.PermissionMode != acp.PermissionModeBypassPermissions || cfg.AgentCode != "claude-acp" {
		t.Fatalf("issue 显式键应覆盖，got agent=%q mode=%q", cfg.AgentCode, cfg.PermissionMode)
	}

	// 用例 3：mode 非 acp 拒绝（terminal-manual / 显式 none 同拒）。
	for _, mode := range []string{"terminal-manual", "none"} {
		issueID = seedIssue(t, db, wsDir, `{"mode":"acp"}`, `{"mode":"`+mode+`"}`)
		if _, err := resolveSessionConfig(ctx, db, issueID, ""); err == nil {
			t.Fatalf("mode=%q 应拒绝", mode)
		}
	}

	// 用例 4：双侧均未配置（merged nil）拒绝。
	issueID = seedIssue(t, db, wsDir, "", "")
	if _, err := resolveSessionConfig(ctx, db, issueID, ""); err == nil {
		t.Fatal("无 launch_settings 应拒绝")
	}

	/*
	 * 用例 4b：持久化配置显式 none（LaunchModePicker 场景实况）+ 临场声明 acp 放行；
	 * 无声明仍拒绝（临场声明仅本次会话有效，不落库不记忆）。
	 */
	issueID = seedIssue(t, db, wsDir, `{"mode":"none"}`, "")
	if _, err := resolveSessionConfig(ctx, db, issueID, "acp"); err != nil {
		t.Fatalf("临场声明 acp 应放行: %v", err)
	}
	if _, err := resolveSessionConfig(ctx, db, issueID, ""); err == nil {
		t.Fatal("mode=none 无临场声明仍应拒绝")
	}

	// 用例 5：非法 permissionMode / agentCode 拒绝。
	issueID = seedIssue(t, db, wsDir, `{"mode":"acp"}`, `{"mode":"acp","permissionMode":"yolo"}`)
	if _, err := resolveSessionConfig(ctx, db, issueID, ""); err == nil || !strings.Contains(err.Error(), "权限模式") {
		t.Fatalf("非法 permissionMode 应拒绝，got %v", err)
	}
	issueID = seedIssue(t, db, wsDir, `{"mode":"acp"}`, `{"mode":"acp","agentCode":"no-such-agent"}`)
	if _, err := resolveSessionConfig(ctx, db, issueID, ""); err == nil || !strings.Contains(err.Error(), "no-such-agent") {
		t.Fatalf("非法 agentCode 应拒绝，got %v", err)
	}

	// 用例 6：issue 不存在 / issueId 非法。
	if _, err := resolveSessionConfig(ctx, db, "no-such-issue", ""); err == nil {
		t.Fatal("issue 不存在应拒绝")
	}
	if _, err := resolveSessionConfig(ctx, db, "../x", ""); err == nil {
		t.Fatal("路径穿越 issueId 应拒绝")
	}
}

// TestResolveSessionConfigRelDir 工作空间相对目录拒绝（绝对路径校验）。
func TestResolveSessionConfigRelDir(t *testing.T) {
	db := newTestDB(t)
	q := queryOf(db)
	proj := &model.Project{ID: "proj-rel", Name: "proj", Dir: "relative/dir", LaunchSettings: `{"mode":"acp"}`}
	if err := q.Project.WithContext(context.Background()).Create(proj); err != nil {
		t.Fatalf("建 project: %v", err)
	}
	task := &model.ProjectTask{ID: "issue-rel", ProjectID: proj.ID, Name: "x"}
	if err := q.ProjectTask.WithContext(context.Background()).Create(task); err != nil {
		t.Fatalf("建 task: %v", err)
	}
	if _, err := resolveSessionConfig(context.Background(), db, task.ID, ""); err == nil || !strings.Contains(err.Error(), "绝对路径") {
		t.Fatalf("相对目录应拒绝，got %v", err)
	}
}

/*
 * TestResolveSessionConfigConversationAnchor conv- 锚解析（IM 未关联任务的独立会话）：
 * 三跳解析（会话行 → bot → workspace）、launch_settings 只消费 workspace 单级、cwd 即
 * 工空间根目录；workspace 非 acp / 会话行缺失拒绝。
 */
func TestResolveSessionConfigConversationAnchor(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	q := queryOf(db)

	// workspace 级显式 agentCode/permissionMode 供 conv 会话继承。
	wsDir := t.TempDir()
	mkWS := func(launchSettings string) string {
		id := fmt.Sprintf("ws-conv-%d", seedIssueSeq.Add(1))
		if err := q.Workspace.WithContext(ctx).Create(&model.Workspace{
			ID: id, Name: "ws", Dir: wsDir, LaunchSettings: launchSettings,
		}); err != nil {
			t.Fatalf("建 workspace: %v", err)
		}
		return id
	}

	wsID := mkWS(`{"mode":"acp","agentCode":"claude-acp","permissionMode":"acceptEdits"}`)
	key := ConversationSessionKey(seedBotConversation(t, db, wsID))
	cfg, err := resolveSessionConfig(ctx, db, key, "")
	if err != nil {
		t.Fatalf("resolveSessionConfig(conv): %v", err)
	}
	if cfg.SessionKey != key || cfg.WorkspaceDir != wsDir || cfg.Cwd != wsDir {
		t.Fatalf("conv 锚 cwd 应为工作空间根目录: %+v", cfg)
	}
	if cfg.AgentCode != "claude-acp" || cfg.PermissionMode != acp.PermissionModeAcceptEdits {
		t.Fatalf("应继承 workspace 级 agent/permission 配置: %+v", cfg)
	}

	// workspace 非 acp 拒绝。
	keyTerminal := ConversationSessionKey(seedBotConversation(t, db, mkWS(`{"mode":"terminal-manual"}`)))
	if _, err := resolveSessionConfig(ctx, db, keyTerminal, ""); err == nil || !strings.Contains(err.Error(), "未配置为 ACP") {
		t.Fatalf("workspace 非 acp 应拒绝: %v", err)
	}

	// 会话行缺失拒绝。
	if _, err := resolveSessionConfig(ctx, db, ConversationSessionKey("no-such-row"), ""); err == nil {
		t.Fatal("会话行缺失应拒绝")
	}
}
