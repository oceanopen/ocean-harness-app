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

/*
 * launchModeAcp launch_settings.mode 的 ACP 档（T1.6 接管点：LaunchModePicker 的
 * mode==='acp' 兜底处改走本域 ensure）。
 */
const launchModeAcp = "acp"

// SessionConfig 会话创建所需配置（Ensure 受理时解析一次，spawn 链全程只读消费）。
type SessionConfig struct {
	SessionKey     string
	WorkspaceDir   string // 工作空间根目录（绝对路径，校验见 resolveSessionConfig）
	Cwd            string // 会话工作目录：任务锚 = <WorkspaceDir>/<taskId>；conv- 锚 = 工作空间根目录
	AgentCode      string // 已回落（空配置回落首个 enabled catalog 条目）
	PermissionMode acp.PermissionMode
}

/*
 * mergeLaunchSettings 字段级合并（Go 版，语义对齐前端 packages/web/src/shared/launchSettings.ts）：override 显式配置过的键（非空串）覆盖 base、未配置键回落 base，mode 显式 "none" 亦为有效覆盖值（「明说不要」≠「未配置」，故以非空串判显式而非按取值域）；任一侧可为 nil（= JSON 列空串/损坏的未配置语义，见 types.ParseLaunchSettings）。
 */
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

/*
 * resolveSessionConfig 由会话锚键解析会话配置（任务 uuid / conv-<会话行id> 双锚形分派，
 * 前缀判别见 binding.go）：路径安全校验 → 锚载体存在性 → launch_settings 字段级合并
 * （任务锚 = project 基线 + task 覆盖；conv- 锚 = workspace 单级）→ mode 门禁（持久化配置
 * 显式 acp，或请求显式声明临场 acp——LaunchModePicker 三选一仅本次有效不落库，否则拒绝）
 * → agentCode / permissionMode 回落与校验 → cwd 兜底建目录；DB 只读 + 目录兜底创建，
 * 同步调用（Ensure 受理期），校验失败立即反馈 HTTP 调用方。
 */
func resolveSessionConfig(ctx context.Context, db *gorm.DB, sessionKey, pickedLaunchMode string) (SessionConfig, error) {
	if IsConversationSessionKey(sessionKey) {
		return resolveConversationSessionConfig(ctx, db, sessionKey, pickedLaunchMode)
	}
	if !ValidIssueID(sessionKey) {
		return SessionConfig{}, errors.New("issueId 非法")
	}
	q := query.Use(db)
	task, err := q.ProjectTask.WithContext(ctx).Where(q.ProjectTask.ID.Eq(sessionKey)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SessionConfig{}, errors.New("任务不存在")
		}
		return SessionConfig{}, err
	}
	project, err := q.Project.WithContext(ctx).Where(q.Project.ID.Eq(task.ProjectID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SessionConfig{}, errors.New("任务所属项目不存在")
		}
		return SessionConfig{}, err
	}
	if !filepath.IsAbs(project.Dir) {
		return SessionConfig{}, errors.New("项目目录须为绝对路径")
	}
	merged := mergeLaunchSettings(
		types.ParseLaunchSettings(project.LaunchSettings),
		types.ParseLaunchSettings(task.LaunchSettings),
	)
	if pickedLaunchMode != launchModeAcp && (merged == nil || merged.Mode != launchModeAcp) {
		return SessionConfig{}, errors.New("任务启动模式未配置为 ACP，无法创建 ACP 会话")
	}
	agentCode, mode, err := resolveAgentSettings(merged)
	if err != nil {
		return SessionConfig{}, err
	}
	cwd := filepath.Join(project.Dir, sessionKey)
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		return SessionConfig{}, fmt.Errorf("创建会话工作目录失败: %w", err)
	}
	return SessionConfig{
		SessionKey:     sessionKey,
		WorkspaceDir:   project.Dir,
		Cwd:            cwd,
		AgentCode:      agentCode,
		PermissionMode: mode,
	}, nil
}

/*
 * resolveConversationSessionConfig conv- 锚（IM 未关联任务的独立会话）的配置解析：
 * 会话行 → bot 行 → workspace 三跳；launch_settings 只消费 workspace 单级（无 issue 覆盖面），
 * cwd 即工作空间根目录（与关联任务会话的 <dir>/<issueId> 任务目录区分开）。
 */
func resolveConversationSessionConfig(ctx context.Context, db *gorm.DB, sessionKey, pickedLaunchMode string) (SessionConfig, error) {
	q := query.Use(db)
	convID, _ := conversationRowID(sessionKey)
	conv, err := q.ImBotConversation.WithContext(ctx).Where(q.ImBotConversation.ID.Eq(convID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SessionConfig{}, errors.New("会话不存在")
		}
		return SessionConfig{}, err
	}
	bot, err := q.ImBot.WithContext(ctx).Where(q.ImBot.ID.Eq(conv.BotID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SessionConfig{}, errors.New("会话所属 bot 不存在")
		}
		return SessionConfig{}, err
	}
	base, err := workspaceSessionBase(ctx, q, bot.WorkspaceID, pickedLaunchMode, "会话所属工作空间不存在", "工作空间启动模式未配置为 ACP，无法创建 ACP 会话")
	if err != nil {
		return SessionConfig{}, err
	}
	agentCode, mode, err := resolveAgentSettings(types.ParseLaunchSettings(base.workspaceSettings))
	if err != nil {
		return SessionConfig{}, err
	}
	if err := os.MkdirAll(base.dir, 0o755); err != nil {
		return SessionConfig{}, fmt.Errorf("创建会话工作目录失败: %w", err)
	}
	return SessionConfig{
		SessionKey:     sessionKey,
		WorkspaceDir:   base.dir,
		Cwd:            base.dir,
		AgentCode:      agentCode,
		PermissionMode: mode,
	}, nil
}

/*
 * workspaceSessionBase 两锚形共用的 workspace 段解析：目录绝对路径校验 + mode 门禁
 * （门禁文案随锚形分义）。返回目录与 workspace 级 launch_settings 原文（issue 锚的覆盖
 * 合并由调用方续做）。
 */
func workspaceSessionBase(ctx context.Context, q *query.Query, workspaceID, pickedLaunchMode, missingText, modeText string) (sessionBase, error) {
	if workspaceID == "" {
		return sessionBase{}, errors.New(missingText)
	}
	ws, err := q.Workspace.WithContext(ctx).Where(q.Workspace.ID.Eq(workspaceID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return sessionBase{}, errors.New(missingText)
		}
		return sessionBase{}, err
	}
	if !filepath.IsAbs(ws.Dir) {
		return sessionBase{}, errors.New("工作空间目录须为绝对路径")
	}
	if pickedLaunchMode != launchModeAcp {
		if ls := types.ParseLaunchSettings(ws.LaunchSettings); ls == nil || ls.Mode != launchModeAcp {
			return sessionBase{}, errors.New(modeText)
		}
	}
	return sessionBase{dir: ws.Dir, workspaceSettings: ws.LaunchSettings}, nil
}

// sessionBase workspace 段解析产物（目录 + 级联配置原文）。
type sessionBase struct {
	dir               string
	workspaceSettings string
}

/*
 * resolveAgentSettings agentCode / permissionMode 回落与校验（两锚形共用尾段）：
 * agentCode 空回落首个 enabled catalog 条目；permissionMode 空回落 bypassPermissions，
 * 仅 acceptEdits / bypassPermissions 合法。
 */
func resolveAgentSettings(merged *types.WorkspaceLaunchSettings) (string, acp.PermissionMode, error) {
	agentCode := ""
	if merged != nil {
		agentCode = string(merged.AgentCode)
	}
	if agentCode == "" {
		enabled := agentcatalog.Enabled()
		if len(enabled) == 0 {
			return "", "", errors.New("agent catalog 无 enabled 条目，无法回落 agentCode")
		}
		agentCode = enabled[0].Code
	}
	if _, ok := agentcatalog.GetAgentCatalogInfoByCode(agentCode); !ok {
		return "", "", fmt.Errorf("agent catalog 无 %q 条目", agentCode)
	}
	mode := acp.PermissionMode("")
	if merged != nil {
		mode = acp.PermissionMode(merged.PermissionMode)
	}
	if mode == "" {
		mode = acp.PermissionModeBypassPermissions
	}
	if mode != acp.PermissionModeAcceptEdits && mode != acp.PermissionModeBypassPermissions {
		return "", "", fmt.Errorf("权限模式 %q 非法（可选 acceptEdits / bypassPermissions）", mode)
	}
	return agentCode, mode, nil
}

/*
 * ValidIssueID 校验 issueId 可安全拼入文件路径（与 service 层 issueWorkspaceValidIssueID
 * 同语义互为参照：拒绝空串、路径分隔符与 Clean 后会变化的值，防 "../x" 路径穿越）。
 */
func ValidIssueID(issueID string) bool {
	return issueID != "" &&
		issueID != "." && issueID != ".." &&
		!strings.ContainsAny(issueID, `/\`) &&
		filepath.Clean(issueID) == issueID
}
