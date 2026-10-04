package acpsession

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/agentcatalog"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
)

// launchModeAcp launch_settings.mode 的 ACP 档（T1.6 接管点：LaunchModePicker 的
// mode==='acp' 兜底处改走本域 ensure）。
const launchModeAcp = "acp"

// SessionConfig 会话创建所需配置（Ensure 受理时解析一次，spawn 链全程只读消费）。
type SessionConfig struct {
	IssueID        string
	WorkspaceDir   string // 工作空间根目录（绝对路径，校验见 resolveSessionConfig）
	Cwd            string // 会话工作目录 = <WorkspaceDir>/<issueId>（与终端 cwd 同口径，见 issue_workspace_state.go）
	AgentCode      string // 已回落（空配置回落首个 enabled catalog 条目）
	PermissionMode acp.PermissionMode
}

// mergeLaunchSettings 字段级合并（Go 版，语义对齐前端 packages/web/src/shared/launchSettings.ts）：
// override 显式配置过的键（非空串）覆盖 base，未配置键回落 base；mode 显式 "none" 亦为
// 有效覆盖值（「明说不要」≠「未配置」，故以非空串判显式而非按取值域）。任一侧可为 nil
// （= JSON 列空串/损坏的未配置语义，见 types.ParseLaunchSettings）。
func mergeLaunchSettings(base, override *types.WorkspaceLaunchSettings) *types.WorkspaceLaunchSettings {
	if base == nil && override == nil {
		return nil
	}
	merged := &types.WorkspaceLaunchSettings{}
	if base != nil {
		*merged = *base
	}
	if override != nil {
		if override.Mode != "" {
			merged.Mode = override.Mode
		}
		if override.AgentCode != "" {
			merged.AgentCode = override.AgentCode
		}
		if override.AutoCommand != "" {
			merged.AutoCommand = override.AutoCommand
		}
		if override.PermissionMode != "" {
			merged.PermissionMode = override.PermissionMode
		}
	}
	return merged
}

// resolveSessionConfig 由 issueId 解析会话配置：路径安全校验 → issue/workspace 存在性
// （resolveIssueBaseDir 同款查询链）→ launch_settings 字段级合并（issue 覆盖 workspace）→
// mode 门禁（持久化配置显式 acp，或请求显式声明临场 acp——LaunchModePicker 三选一仅本次
// 有效不落库，否则拒绝）→ agentCode / permissionMode 回落与校验 → cwd 兜底建目录。
// DB 只读 + 目录兜底创建；同步调用（Ensure 受理期），校验失败立即反馈 HTTP 调用方。
func resolveSessionConfig(ctx context.Context, db *gorm.DB, issueID, pickedLaunchMode string) (SessionConfig, error) {
	if !ValidIssueID(issueID) {
		return SessionConfig{}, errors.New("issueId 非法")
	}
	q := query.Use(db)
	issue, err := q.ProjectIssue.WithContext(ctx).Where(q.ProjectIssue.ID.Eq(issueID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SessionConfig{}, errors.New("issue 不存在")
		}
		return SessionConfig{}, err
	}
	ws, err := q.Workspace.WithContext(ctx).Where(q.Workspace.ID.Eq(issue.WorkspaceID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SessionConfig{}, errors.New("issue 所属工作空间不存在")
		}
		return SessionConfig{}, err
	}
	if !filepath.IsAbs(ws.Dir) {
		return SessionConfig{}, errors.New("工作空间目录须为绝对路径")
	}
	merged := mergeLaunchSettings(
		types.ParseLaunchSettings(ws.LaunchSettings),
		types.ParseLaunchSettings(issue.LaunchSettings),
	)
	if pickedLaunchMode != launchModeAcp && (merged == nil || merged.Mode != launchModeAcp) {
		return SessionConfig{}, errors.New("issue 启动模式未配置为 ACP，无法创建 ACP 会话")
	}
	agentCode := string(merged.AgentCode)
	if agentCode == "" {
		enabled := agentcatalog.Enabled()
		if len(enabled) == 0 {
			return SessionConfig{}, errors.New("agent catalog 无 enabled 条目，无法回落 agentCode")
		}
		agentCode = enabled[0].Code
	}
	if _, ok := agentcatalog.GetAgentCatalogInfoByCode(agentCode); !ok {
		return SessionConfig{}, fmt.Errorf("agent catalog 无 %q 条目", agentCode)
	}
	mode := acp.PermissionMode(merged.PermissionMode)
	if mode == "" {
		mode = acp.PermissionModeAcceptEdits
	}
	if mode != acp.PermissionModeAcceptEdits && mode != acp.PermissionModeBypassPermissions {
		return SessionConfig{}, fmt.Errorf("权限模式 %q 非法（可选 acceptEdits / bypassPermissions）", mode)
	}
	cwd := filepath.Join(ws.Dir, issueID)
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		return SessionConfig{}, fmt.Errorf("创建会话工作目录失败: %w", err)
	}
	return SessionConfig{
		IssueID:        issueID,
		WorkspaceDir:   ws.Dir,
		Cwd:            cwd,
		AgentCode:      agentCode,
		PermissionMode: mode,
	}, nil
}

// ValidIssueID 校验 issueId 可安全拼入文件路径（与 service 层 issueWorkspaceValidIssueID
// 同语义互为参照：拒绝空串、路径分隔符与 Clean 后会变化的值，防 "../x" 路径穿越）。
func ValidIssueID(issueID string) bool {
	return issueID != "" &&
		issueID != "." && issueID != ".." &&
		!strings.ContainsAny(issueID, `/\`) &&
		filepath.Clean(issueID) == issueID
}
