package bot

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"ocean-harness/server/internal/acpsession"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
)

/*
 * launchModeACP 容器 launch_settings.mode 的 ACP 档（取值 SSOT 是前端 shared/launchSettings.ts；
 * acpsession 域同款字面量门禁——跨域各自维护字面量，不做导出耦合）。
 */
const launchModeACP = "acp"

/*
 * turnNotAcceptedError 引擎受理拒绝：回合未真正发生（配置非法、会话起不来、排队受理被拒）。
 * driverRoute 以此标记此类错误，编排器据其跳过失败自愈（errors.As 判别，编排器不感知具体引擎；
 * 误清会丢 headless 既有会话锚点）。
 */
type turnNotAcceptedError struct{ err error }

func (e turnNotAcceptedError) Error() string { return e.err.Error() }

func (e turnNotAcceptedError) Unwrap() error { return e.err }

/*
 * routeTarget 路由判定产物：引擎选择 + ACP 档会话锚键（conv-<bot 会话行id>，行即与桌面共享
 * 的会话实体）；headless 档 SessionKey 恒空（锚 = 同行 claude_session_id）。
 */
type routeTarget struct {
	SessionKey string
	ACP        bool
}

/*
 * driverRoute TurnDriver 的引擎路由实现（双驱动并存）：按容器 launch_settings.mode 逐回合路由
 * ——mode='acp' 走 ACP（锚键 conv-<bot 会话行id>），其余档走 headless spawn（锚 = 同行
 * claude_session_id）。路由知识收在引擎层（实现 TurnDriver），编排器零引擎感知；容器配置
 * 每回合读取——改档位/关联任务对在跑 bot 下一回合生效（bot 不随配置重启）。
 */
type driverRoute struct {
	headless TurnDriver
	acp      TurnDriver
	// sessions 会话域消费面（审批快路径 Get/RespondPermission 消费；与 acp 引擎同源）。
	sessions acpSessions
	db       *gorm.DB
	/*
	 * resolve 一步判定路由目标：读容器启动档 + 会话行定位/创建 + 档位覆写。读库失败
	 * fail closed 同步报错（不静默降级）。惯例实现是 resolveTarget 方法（构造器赋值）；
	 * 保留字段形态供白盒测试注入替身。
	 */
	resolve func(cfg BotRuntimeConfig, conversationKey string) (routeTarget, error)
}

/*
 * newDriverRoute 装配路由驱动（返回具体类型：编排器同时把它当 TurnDriver 与 pendingGate
 * 两个角色装配——TryRespondPending 见 pending_gate.go）。
 */
func newDriverRoute(db *gorm.DB, sessions acpSessions) *driverRoute {
	r := &driverRoute{
		headless: NewClaudeDriver(),
		acp:      NewAcpDriver(sessions),
		sessions: sessions,
		db:       db,
	}
	r.resolve = r.resolveTarget
	return r
}

/*
 * resolveTarget 路由判定实现：容器启动档判定 → bot 会话行定位/创建（EnsureConversation）→
 * 行档位按容器 launch 档幂等覆写。ACP 档锚键 = conv-<行id>；headless 档锚 = 同行
 * claude_session_id（读写链在编排器 runTurn）。
 */
func (r *driverRoute) resolveTarget(cfg BotRuntimeConfig, conversationKey string) (routeTarget, error) {
	mode, err := r.containerLaunch(cfg)
	if err != nil {
		return routeTarget{}, err
	}
	store := &ConversationStore{DB: r.db}
	conv, err := store.EnsureConversation(cfg, conversationKey)
	if err != nil {
		return routeTarget{}, err
	}
	targetMode := enums.CONVERSATION_MODE_TERMINAL
	if mode == launchModeACP {
		targetMode = enums.CONVERSATION_MODE_ACP
	}
	if conv.Mode != targetMode {
		if err := store.SetMode(conv.ID, targetMode); err != nil {
			return routeTarget{}, err
		}
	}
	// 非 ACP 档（none/terminal-auto/未配置）：headless，锚 = bot 会话行 claude_session_id。
	if mode != launchModeACP {
		return routeTarget{}, nil
	}
	return routeTarget{SessionKey: acpsession.ConversationSessionKey(conv.ID), ACP: true}, nil
}

/*
 * containerLaunch 读容器启动档（路由判定输入）：workspace 档 = workspace 行单级；project 档 =
 * project+task 双级字段级合并（task 显式键覆盖，语义同 acpsession.mergeLaunchSettings）。
 * 容器行已删按未配置回落 headless；permissionMode 为全局恒定档不在此读取。
 */
func (r *driverRoute) containerLaunch(cfg BotRuntimeConfig) (mode string, err error) {
	q := query.Use(r.db)
	var baseRaw, overrideRaw string
	switch cfg.ConversationType {
	case enums.CONVERSATION_TYPE_WORKSPACE:
		if cfg.WorkspaceID == "" {
			return "", errors.New("工作空间档未选择工作空间")
		}
		ws, e := q.Workspace.WithContext(context.Background()).Where(q.Workspace.ID.Eq(cfg.WorkspaceID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return "", nil
			}
			return "", e
		}
		baseRaw = ws.LaunchSettings
	case enums.CONVERSATION_TYPE_PROJECT:
		if cfg.TaskID == "" {
			return "", errors.New("项目档未关联任务")
		}
		task, e := q.ProjectTask.WithContext(context.Background()).Where(q.ProjectTask.ID.Eq(cfg.TaskID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return "", nil
			}
			return "", e
		}
		project, e := q.Project.WithContext(context.Background()).Where(q.Project.ID.Eq(task.ProjectID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return "", nil
			}
			return "", e
		}
		baseRaw, overrideRaw = project.LaunchSettings, task.LaunchSettings
	default:
		return "", errors.New("会话类型未配置")
	}
	base, override := types.ParseLaunchSettings(baseRaw), types.ParseLaunchSettings(overrideRaw)
	return mergedLaunchKey(base, override, func(ls *types.WorkspaceLaunchSettings) string { return ls.Mode }), nil
}

/*
 * mergedLaunchKey 双级字段级合并取单键（语义同 acpsession.mergeLaunchSettings：override
 * 显式配置过的键（非空串）覆盖 base，显式 none 亦是有效覆盖值）。
 */
func mergedLaunchKey(base, override *types.WorkspaceLaunchSettings, key func(*types.WorkspaceLaunchSettings) string) string {
	if override != nil && key(override) != "" {
		return key(override)
	}
	if base != nil {
		return key(base)
	}
	return ""
}

/*
 * RunTurn 实现见 driver.go 契约：一步解析路由目标后转发对应引擎——headless 原样透传（resume
 * 锚由编排器经 req.SessionID 传入，失效锚自愈在编排器）；ACP 回填 AcpSessionKey（值拷贝），
 * 受理失败包 turnNotAcceptedError 标记（回合未发生，编排器不清锚）。
 */
func (r *driverRoute) RunTurn(ctx context.Context, req TurnRequest) (<-chan TurnEvent, error) {
	target, err := r.resolve(BotRuntimeConfig{
		BotID:            req.BotID,
		WorkspaceID:      req.WorkspaceID,
		ConversationType: req.ConversationType,
		ProjectID:        req.ProjectID,
		TaskID:           req.TaskID,
	}, req.ConversationKey)
	if err != nil {
		return nil, turnNotAcceptedError{fmt.Errorf("路由判定失败: %w", err)}
	}
	if !target.ACP {
		return r.headless.RunTurn(ctx, req)
	}
	if target.SessionKey == "" {
		return nil, turnNotAcceptedError{errors.New("ACP 目标缺少会话锚键（路由判定缺陷）")}
	}
	req.AcpSessionKey = target.SessionKey
	events, err := r.acp.RunTurn(ctx, req)
	if err != nil {
		return nil, turnNotAcceptedError{err}
	}
	return events, nil
}

/*
 * ProbeTarget 路由探针：不改状态地探明本会话下一回合会落的引擎。读库失败原样返回 err，
 * 调用方不拦截——消费方按探测失败回落保守语义。
 */
func (r *driverRoute) ProbeTarget(cfg BotRuntimeConfig, conversationKey string) (acp bool, err error) {
	target, err := r.resolve(cfg, conversationKey)
	if err != nil {
		return false, err
	}
	return target.ACP, nil
}
