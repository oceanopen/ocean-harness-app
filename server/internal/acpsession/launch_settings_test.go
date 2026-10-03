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

// newTestDB 临时 sqlite（glebarez 纯 Go 驱动）+ 测试涉及的三张表（AutoMigrate 按 model
// tag 建列，与 goose 迁移的 DSN 差异不影响本域查询语义）。测试收尾关连接池（后台收尾
// goroutine 的 best-effort 写库在其后失败无害，仅日志）。
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&model.Workspace{}, &model.ProjectIssue{}, &model.IssueAcpSession{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

func queryOf(db *gorm.DB) *query.Query {
	return query.Use(db)
}

// seedIssueSeq 同测试内多次种 issue 的主键去重序号（issueId 全局唯一约束）。
var seedIssueSeq atomic.Int64

// seedIssue 建 workspace + issue 种子（launch_settings 为 JSON 列原文；返回 issueId）。
func seedIssue(t *testing.T, db *gorm.DB, wsDir, wsLaunchSettings, issueLaunchSettings string) string {
	t.Helper()
	q := queryOf(db)
	ws := &model.Workspace{Name: "ws", Dir: wsDir, LaunchSettings: wsLaunchSettings}
	if err := q.Workspace.WithContext(context.Background()).Create(ws); err != nil {
		t.Fatalf("建 workspace: %v", err)
	}
	issueID := fmt.Sprintf("issue-%s-%d", strings.ReplaceAll(t.Name(), "/", "-"), seedIssueSeq.Add(1))
	issue := &model.ProjectIssue{
		ID:             issueID,
		ProjectID:      1,
		WorkspaceID:    ws.ID,
		Name:           "测试 issue",
		StateCode:      enums.STATE_CODE_BACKLOG,
		Priority:       enums.PRIORITY_NONE,
		LaunchSettings: issueLaunchSettings,
	}
	if err := q.ProjectIssue.WithContext(context.Background()).Create(issue); err != nil {
		t.Fatalf("建 issue: %v", err)
	}
	return issueID
}

func TestResolveSessionConfig(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	wsDir := t.TempDir()

	// 用例 1：workspace 配 acp、issue 未配置 → 全回落（agentCode 回落首个 enabled 条目，
	// permissionMode 回落 acceptEdits），cwd = <wsDir>/<issueId> 且已兜底建目录。
	issueID := seedIssue(t, db, wsDir, `{"mode":"acp"}`, "")
	cfg, err := resolveSessionConfig(ctx, db, issueID)
	if err != nil {
		t.Fatalf("resolveSessionConfig: %v", err)
	}
	enabled := agentcatalog.Enabled()
	if len(enabled) == 0 || cfg.AgentCode != enabled[0].Code {
		t.Fatalf("agentCode 应回落首个 enabled 条目，got %q", cfg.AgentCode)
	}
	if cfg.PermissionMode != acp.PermissionModeAcceptEdits {
		t.Fatalf("permissionMode 应回落 acceptEdits，got %q", cfg.PermissionMode)
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
	cfg, err = resolveSessionConfig(ctx, db, issueID)
	if err != nil {
		t.Fatalf("resolveSessionConfig(override): %v", err)
	}
	if cfg.PermissionMode != acp.PermissionModeBypassPermissions || cfg.AgentCode != "claude-acp" {
		t.Fatalf("issue 显式键应覆盖，got agent=%q mode=%q", cfg.AgentCode, cfg.PermissionMode)
	}

	// 用例 3：mode 非 acp 拒绝（terminal-manual / 显式 none 同拒）。
	for _, mode := range []string{"terminal-manual", "none"} {
		issueID = seedIssue(t, db, wsDir, `{"mode":"acp"}`, `{"mode":"`+mode+`"}`)
		if _, err := resolveSessionConfig(ctx, db, issueID); err == nil {
			t.Fatalf("mode=%q 应拒绝", mode)
		}
	}

	// 用例 4：双侧均未配置（merged nil）拒绝。
	issueID = seedIssue(t, db, wsDir, "", "")
	if _, err := resolveSessionConfig(ctx, db, issueID); err == nil {
		t.Fatal("无 launch_settings 应拒绝")
	}

	// 用例 5：非法 permissionMode / agentCode 拒绝。
	issueID = seedIssue(t, db, wsDir, `{"mode":"acp"}`, `{"mode":"acp","permissionMode":"yolo"}`)
	if _, err := resolveSessionConfig(ctx, db, issueID); err == nil || !strings.Contains(err.Error(), "权限模式") {
		t.Fatalf("非法 permissionMode 应拒绝，got %v", err)
	}
	issueID = seedIssue(t, db, wsDir, `{"mode":"acp"}`, `{"mode":"acp","agentCode":"no-such-agent"}`)
	if _, err := resolveSessionConfig(ctx, db, issueID); err == nil || !strings.Contains(err.Error(), "no-such-agent") {
		t.Fatalf("非法 agentCode 应拒绝，got %v", err)
	}

	// 用例 6：issue 不存在 / issueId 非法。
	if _, err := resolveSessionConfig(ctx, db, "no-such-issue"); err == nil {
		t.Fatal("issue 不存在应拒绝")
	}
	if _, err := resolveSessionConfig(ctx, db, "../x"); err == nil {
		t.Fatal("路径穿越 issueId 应拒绝")
	}
}

// TestResolveSessionConfigRelDir 工作空间相对目录拒绝（绝对路径校验）。
func TestResolveSessionConfigRelDir(t *testing.T) {
	db := newTestDB(t)
	q := queryOf(db)
	ws := &model.Workspace{Name: "ws", Dir: "relative/dir", LaunchSettings: `{"mode":"acp"}`}
	if err := q.Workspace.WithContext(context.Background()).Create(ws); err != nil {
		t.Fatalf("建 workspace: %v", err)
	}
	issue := &model.ProjectIssue{ID: "issue-rel", ProjectID: 1, WorkspaceID: ws.ID, Name: "x", StateCode: enums.STATE_CODE_BACKLOG, Priority: enums.PRIORITY_NONE}
	if err := q.ProjectIssue.WithContext(context.Background()).Create(issue); err != nil {
		t.Fatalf("建 issue: %v", err)
	}
	if _, err := resolveSessionConfig(context.Background(), db, issue.ID); err == nil || !strings.Contains(err.Error(), "绝对路径") {
		t.Fatalf("相对目录应拒绝，got %v", err)
	}
}
