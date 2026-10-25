package bot

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"gorm.io/gorm"

	"github.com/google/uuid"

	"ocean-harness/server/internal/acpsession"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
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
 * routeTarget 路由判定产物：引擎选择 + ACP 档的会话锚键。SessionKey 双锚形——
 * conv-<主会话行id>（任务主/子会话，与桌面工作台共享同一会话视图与审批）或 conv-<bot 会话行id>
 * （workspace 档独立会话，见 acpsession.BindingStore）；headless 档 SessionKey 恒空
 * （锚 = bot 会话行 claude_session_id）。
 */
type routeTarget struct {
	SessionKey string
	ACP        bool
}

/*
 * driverRoute TurnDriver 的引擎路由实现（双驱动并存）：按容器 launch_settings.mode 逐回合路由
 * ——mode='acp' 走 ACP 三分支（workspace 档 bot 会话行锚独立会话；project 档任务主会话复用/
 * ACP 主会话创建/终端档并建 ACP 子会话，与桌面共享同一会话视图）；其余档（none/terminal-auto/
 * 未配置）走 headless spawn（driver_claude）。路由知识收在引擎层（实现 TurnDriver），编排器
 * 保持零引擎感知；容器配置每回合读取——改档位/关联任务，均对在跑 bot 下一回合生效（bot 不随配置重启）。
 */
type driverRoute struct {
	headless TurnDriver
	acp      TurnDriver
	// sessions 会话域消费面（审批快路径 Get/RespondPermission 消费；与 acp 引擎同源）。
	sessions acpSessions
	db       *gorm.DB
	/*
	 * resolve 一步判定路由目标：读容器启动档 + 会话行 + 任务主会话定位/创建。读库失败
	 * fail closed 同步报错（不静默降级）。惯例实现是 resolveTarget 方法（构造器赋值）；
	 * 保留字段形态供白盒测试注入替身。
	 */
	resolve func(cfg BotRuntimeConfig, conversationKey string) (routeTarget, error)
	// mu 串行化主会话定位/创建（同任务并发消息防重复建主会话；纳秒级临界区）。
	mu sync.Mutex
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
 * resolveTarget 路由判定实现。容器启动档判定（containerLaunch）先行——headless 档直接返回，
 * 不触碰 ACP 会话实体；bot 会话行经 ConversationStore 读取（受理链已 GetOrCreate 保证在场，
 * 此处兜底建行）。ACP 档：已绑会话且归属匹配、仍为 ACP 实体行直接复用（幂等）；否则按档
 * 定位/创建（workspace 档实体化工作空间会话；project 档任务主会话三分支）并回绑
 * conversation_id——归属变更对在跑 bot 下一回合生效的承诺由此兑现。
 */
func (r *driverRoute) resolveTarget(cfg BotRuntimeConfig, conversationKey string) (routeTarget, error) {
	mode, err := r.containerLaunch(cfg)
	if err != nil {
		return routeTarget{}, err
	}
	conv, err := (&ConversationStore{DB: r.db}).GetOrCreate(cfg.BotID, conversationKey)
	if err != nil {
		return routeTarget{}, err
	}
	// 非 ACP 档（none/terminal-auto/未配置）：headless，锚 = bot 会话行 claude_session_id。
	if mode != launchModeACP {
		return routeTarget{}, nil
	}
	q := query.Use(r.db)
	// 已绑会话且归属匹配本次配置、仍为 ACP 实体行：直接复用（幂等）；归属漂移/被删/降级
	// 穿透到下方定位/创建分支并覆写回绑。
	if conv.ConversationID != "" {
		row, e := q.Conversation.WithContext(context.Background()).Where(q.Conversation.ID.Eq(conv.ConversationID)).First()
		if e == nil && row.Mode == enums.CONVERSATION_MODE_ACP && conversationOwnedBy(cfg, row) {
			return routeTarget{SessionKey: acpsession.ConversationSessionKey(row.ID), ACP: true}, nil
		}
	}
	var anchorID string
	switch cfg.ConversationType {
	case enums.CONVERSATION_TYPE_WORKSPACE:
		if cfg.WorkspaceID == "" {
			return routeTarget{}, errors.New("工作空间档未选择工作空间")
		}
		// 取/建该 bot 在该工作空间的 ACP 会话实体行（进工作空间会话列表，归属可倒推）。
		row, e := r.createAcpConversation(enums.OWNER_TYPE_WORKSPACE, cfg.WorkspaceID, "")
		if e != nil {
			return routeTarget{}, e
		}
		anchorID = row.ID
	case enums.CONVERSATION_TYPE_PROJECT:
		if cfg.TaskID == "" {
			return routeTarget{}, errors.New("项目档未关联任务")
		}
		r.mu.Lock()
		main, me := q.Conversation.WithContext(context.Background()).
			Where(q.Conversation.OwnerType.Eq(enums.OWNER_TYPE_TASK),
				q.Conversation.OwnerID.Eq(cfg.TaskID),
				q.Conversation.ParentID.Eq("")).
			First()
		switch {
		case me != nil && !errors.Is(me, gorm.ErrRecordNotFound):
			r.mu.Unlock()
			return routeTarget{}, me
		case errors.Is(me, gorm.ErrRecordNotFound):
			// 无主会话：建 ACP 主会话。
			anchorRow, ce := r.createAcpConversation(enums.OWNER_TYPE_TASK, cfg.TaskID, "")
			if ce != nil {
				r.mu.Unlock()
				return routeTarget{}, ce
			}
			anchorID = anchorRow.ID
		case main.Mode == enums.CONVERSATION_MODE_ACP:
			anchorID = main.ID // 主会话已为 ACP 档：复用（与桌面共享同一会话）。
		default:
			// 主会话为终端档：并建 ACP 子会话（桌面主会话不受影响）。
			sub, ce := r.createAcpConversation(enums.OWNER_TYPE_TASK, cfg.TaskID, main.ID)
			if ce != nil {
				r.mu.Unlock()
				return routeTarget{}, ce
			}
			anchorID = sub.ID
		}
		r.mu.Unlock()
	default:
		return routeTarget{}, errors.New("会话类型未配置")
	}
	// 回绑 bot 会话行 conversation_id（归属可倒推；幂等覆写）。
	if _, e := q.ImBotConversation.WithContext(context.Background()).
		Where(q.ImBotConversation.ID.Eq(conv.ID)).
		UpdateSimple(q.ImBotConversation.ConversationID.Value(anchorID)); e != nil {
		return routeTarget{}, e
	}
	return routeTarget{SessionKey: acpsession.ConversationSessionKey(anchorID), ACP: true}, nil
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
 * conversationOwnedBy 已绑会话行的归属是否与本次配置一致：workspace 档要求
 * owner=workspace/<workspaceID>；project 档要求 owner=task/<taskID>（主/子会话同归属维度）。
 */
func conversationOwnedBy(cfg BotRuntimeConfig, row *model.Conversation) bool {
	switch cfg.ConversationType {
	case enums.CONVERSATION_TYPE_WORKSPACE:
		return row.OwnerType == enums.OWNER_TYPE_WORKSPACE && row.OwnerID == cfg.WorkspaceID
	case enums.CONVERSATION_TYPE_PROJECT:
		return row.OwnerType == enums.OWNER_TYPE_TASK && row.OwnerID == cfg.TaskID
	default:
		return false
	}
}

/*
 * createAcpConversation 落一行 ACP 会话实体（ownerType/ownerID 为归属维度；parentID 空 = 主会话，
 * 非空 = 子会话），返回新行。workspace 档 bot 会话亦实体化（进会话列表，归属可倒推）。
 */
func (r *driverRoute) createAcpConversation(ownerType enums.ConversationOwnerType, ownerID, parentID string) (*model.Conversation, error) {
	row := &model.Conversation{
		ID:        uuid.Must(uuid.NewV7()).String(),
		OwnerType: ownerType,
		OwnerID:   ownerID,
		Title:     "主会话",
		Mode:      enums.CONVERSATION_MODE_ACP,
		ParentID:  parentID,
	}
	if parentID != "" {
		row.Title = "子会话"
	}
	q := query.Use(r.db)
	if err := q.Conversation.WithContext(context.Background()).Create(row); err != nil {
		return nil, err
	}
	return row, nil
}

/*
 * RunTurn 实现见 driver.go 契约：一步解析路由目标后转发对应引擎——headless 原样透传
 * （resume 锚由编排器经 req.SessionID 传入；引擎同步失败不包 turnNotAcceptedError，编排器
 * 对失效锚做自愈）；ACP 在此回填 AcpSessionKey（TurnRequest 为值拷贝），同步失败包
 * turnNotAcceptedError 标记（回合未发生，编排器不清锚）。
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
