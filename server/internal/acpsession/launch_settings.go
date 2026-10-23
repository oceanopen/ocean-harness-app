package acpsession

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/dal/enums"
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
 * errNotConversationEntity 会话实体锚未命中的判别哨兵（conv-<id> 按行存在性消歧：
 * 非 t_conversations 行即回落 bot 未关联会话旧链，T4.3 收敛后移除回落分支）。
 */
var errNotConversationEntity = errors.New("非会话实体锚")

/*
 * resolveSessionConfig 由会话锚键解析会话配置（统一 conv-<id> 键形，按行存在性消歧，
 * 前缀判别见 binding.go）：t_conversations 行走会话实体上下文（见
 * resolveConversationEntityConfig）；未命中回落 bot 未关联会话旧链（T4.3 收敛后移除）
 * → mode 门禁 → agentCode / permissionMode 回落与校验 → cwd 兜底建目录；同步调用
 * （Ensure 受理期），校验失败立即反馈 HTTP 调用方。
 */
func resolveSessionConfig(ctx context.Context, db *gorm.DB, sessionKey, pickedLaunchMode string) (SessionConfig, error) {
	if !IsConversationSessionKey(sessionKey) {
		return SessionConfig{}, errors.New("会话键非法（须为 conv-<会话id> 形）")
	}
	cfg, err := resolveConversationEntityConfig(ctx, db, sessionKey, pickedLaunchMode)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, errNotConversationEntity) {
		return SessionConfig{}, err
	}
	return resolveConversationSessionConfig(ctx, db, sessionKey, pickedLaunchMode)
}

/*
 * resolveConversationEntityConfig 会话实体锚（conv-<t_conversations.id>）的配置解析：
 * 会话行 → 归属实体两档（workspace 会话 = 工作空间根目录 + workspace 单级配置；task 会话 =
 * {project.dir}/{ownerId}/ + project+task 双级合并）→ mode 门禁（持久化配置显式 acp，
 * 或请求显式声明临场 acp——LaunchModePicker 仅本次有效不落库，否则拒绝）→ agentCode /
 * permissionMode 回落与校验 → cwd 兜底建目录。行不存在返回 errNotConversationEntity。
 */
func resolveConversationEntityConfig(ctx context.Context, db *gorm.DB, sessionKey, pickedLaunchMode string) (SessionConfig, error) {
	convID, _ := conversationRowID(sessionKey)
	q := query.Use(db)
	conv, err := q.Conversation.WithContext(ctx).Where(q.Conversation.ID.Eq(convID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SessionConfig{}, errNotConversationEntity
		}
		return SessionConfig{}, err
	}
	var baseDir string
	var merged *types.WorkspaceLaunchSettings
	switch conv.OwnerType {
	case enums.OWNER_TYPE_WORKSPACE:
		ws, e := q.Workspace.WithContext(ctx).Where(q.Workspace.ID.Eq(conv.OwnerID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return SessionConfig{}, errors.New("会话归属工作空间不存在")
			}
			return SessionConfig{}, e
		}
		if !filepath.IsAbs(ws.Dir) {
			return SessionConfig{}, errors.New("工作空间目录须为绝对路径")
		}
		baseDir = ws.Dir
		merged = types.ParseLaunchSettings(ws.LaunchSettings)
	case enums.OWNER_TYPE_TASK:
		task, e := q.ProjectTask.WithContext(ctx).Where(q.ProjectTask.ID.Eq(conv.OwnerID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return SessionConfig{}, errors.New("会话归属任务不存在")
			}
			return SessionConfig{}, e
		}
		project, e := q.Project.WithContext(ctx).Where(q.Project.ID.Eq(task.ProjectID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return SessionConfig{}, errors.New("会话归属项目不存在")
			}
			return SessionConfig{}, e
		}
		if !filepath.IsAbs(project.Dir) {
			return SessionConfig{}, errors.New("项目目录须为绝对路径")
		}
		baseDir = project.Dir
		merged = mergeLaunchSettings(types.ParseLaunchSettings(project.LaunchSettings), types.ParseLaunchSettings(task.LaunchSettings))
	default:
		return SessionConfig{}, errors.New("会话归属类型非法")
	}
	if pickedLaunchMode != launchModeAcp && (merged == nil || merged.Mode != launchModeAcp) {
		return SessionConfig{}, errors.New("会话启动模式未配置为 ACP，无法创建 ACP 会话")
	}
	agentCode, mode, err := resolveAgentSettings(merged)
	if err != nil {
		return SessionConfig{}, err
	}
	cwd := baseDir
	if conv.OwnerType == enums.OWNER_TYPE_TASK {
		cwd = filepath.Join(baseDir, conv.OwnerID)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		return SessionConfig{}, fmt.Errorf("创建会话工作目录失败: %w", err)
	}
	return SessionConfig{
		SessionKey:     sessionKey,
		WorkspaceDir:   baseDir,
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

