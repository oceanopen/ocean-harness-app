package bot

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"ocean-harness/server/internal/acpsession"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
)

/*
 * launchModeACP workspace 启动模式的 ACP 档（取值 SSOT 是前端 shared/launchSettings.ts；
 * acpsession 域同款字面量门禁——跨域各自维护字面量，不做导出耦合）。
 */
const launchModeACP = "acp"

/*
 * turnNotAcceptedError 引擎受理拒绝：回合未真正发生，与 headless 会话锚点有效性无关（ACP 引擎的同步失败——配置非法、会话起不来、排队受理被拒——与启动模式解析失败全属此类）。
 * driverRoute 以此标记此类错误，编排器据其跳过 healAfterFailure 清锚（errors.As 判别，编排器不感知具体引擎；
 * 误清会把 headless 时代的续聊锚点永久丢掉）。
 */
type turnNotAcceptedError struct{ err error }

func (e turnNotAcceptedError) Error() string { return e.err.Error() }

func (e turnNotAcceptedError) Unwrap() error { return e.err }

/*
 * routeTarget 路由判定产物：引擎选择 + ACP 档的会话锚键。SessionKey 双锚形——issue uuid
 * （关联任务，与桌面工作台共享同一会话）或 conv-<会话行id>（未关联任务，以会话行为载体的
 * 独立会话，见 acpsession.BindingStore）；headless 不消费。
 */
type routeTarget struct {
	SessionKey string
	ACP        bool
}

/*
 * effectiveACPMode 启动模式判定（mode-only 字段合并）：绑定 issue 的 launch_settings.mode
 * 非空则覆盖 workspace 级（显式 "none" 亦为有效覆盖值——「明说不要」≠「未配置」，语义对齐
 * acpsession.mergeLaunchSettings 的字段级合并；bot 路由只消费 mode 一项）。
 * issueMode 空 = 未覆盖，回落 workspace 级。
 */
func effectiveACPMode(workspaceMode, issueMode string) bool {
	if issueMode != "" {
		return issueMode == launchModeACP
	}
	return workspaceMode == launchModeACP
}

/*
 * driverRoute ClaudeDriver 的引擎路由实现：按 workspace 级 launch_settings.mode 与任务关联
 * 逐回合在 headless（driver_claude）与 ACP（driver_acp）两引擎间切换。
 * 路由知识收在引擎层（实现 ClaudeDriver），编排器保持零引擎感知；
 * mode 与关联每回合读取——workspace 抽屉改启动配置、面板侧改任务关联，均对在跑 bot 下一回合生效（bot 不随配置重启）。
 */
type driverRoute struct {
	headless ClaudeDriver
	acp      ClaudeDriver
	// sessions 会话域消费面（审批快路径 Get/RespondPermission 消费；与 acp 引擎同源）。
	sessions acpSessions
	db       *gorm.DB
	/*
	 * resolve 一步判定路由目标：读会话行 + issue 存在性/归属校验 + 两级 launch_settings mode
	 * 合并。读库失败 fail closed 同步报错（不静默回落 headless——配置读不出时「以为没配 ACP」
	 * 是危险默认）。惯例实现是 resolveTarget 方法（构造器赋值）；保留字段形态供白盒测试注入替身。
	 */
	resolve func(botID, workspaceID string, conversationKey string) (routeTarget, error)
}

/*
 * newDriverRoute 装配路由驱动（返回具体类型：编排器同时把它当 ClaudeDriver 与 pendingGate
 * 两个角色装配——TryRespondPending 见 pending_gate.go）。
 * 关联校验按 (issue.id, workspace_id) 双键——关联到其他 workspace 任务的会话等同未关联（降级
 * conv 锚，不报错不阻断）。
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
 * resolveTarget 路由判定实现（RunTurn 与 ProbeTarget 共用）。会话行经 ConversationStore
 * 读取（受理链已 GetOrCreate 保证在场，此处兜底建行——各可达路径在建行后执行，实际不触发创建）。
 * 关联与 issue 解析全部前置完成后统一走 effectiveACPMode 合并判定——issue 级 mode 显式覆盖
 * 双向生效（"none" 否决 / "acp" 启用），不得在关联解析前按 wsMode 短路（会吞掉 issue 显式
 * 覆盖；acpsession 域同为合并后判，语义须一致）。
 */
func (r *driverRoute) resolveTarget(botID, workspaceID string, conversationKey string) (routeTarget, error) {
	if workspaceID == "" {
		return routeTarget{}, nil // 未选工作空间已被编排器门禁拦截，此处兜底 headless
	}
	q := query.Use(r.db)
	ws, err := q.Workspace.WithContext(context.Background()).Where(q.Workspace.ID.Eq(workspaceID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return routeTarget{}, nil
		}
		return routeTarget{}, err
	}
	wsMode := ""
	if ls := types.ParseLaunchSettings(ws.LaunchSettings); ls != nil {
		wsMode = ls.Mode
	}
	conv, err := (&ConversationStore{DB: r.db}).GetOrCreate(botID, conversationKey)
	if err != nil {
		return routeTarget{}, err
	}
	if conv.BoundIssueID == "" {
		return unboundTarget(wsMode, conv.ID), nil
	}
	issue, err := q.WorkspaceIssue.WithContext(context.Background()).Where(
		q.WorkspaceIssue.ID.Eq(conv.BoundIssueID),
		q.WorkspaceIssue.WorkspaceID.Eq(workspaceID),
	).First()
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return unboundTarget(wsMode, conv.ID), nil // 挂空关联降级未关联：同 conv 锚形
	case err != nil:
		return routeTarget{}, err
	}
	issueMode := ""
	if ls := types.ParseLaunchSettings(issue.LaunchSettings); ls != nil {
		issueMode = ls.Mode
	}
	if !effectiveACPMode(wsMode, issueMode) {
		return routeTarget{}, nil // issue 显式否决 ACP（如 mode="none"）：回合落回 headless
	}
	return routeTarget{SessionKey: issue.ID, ACP: true}, nil
}

// unboundTarget 未关联任务的目标（ACP 档以会话行为锚走工作空间根目录独立会话；否则 headless）。
func unboundTarget(wsMode, conversationRowID string) routeTarget {
	if wsMode != launchModeACP {
		return routeTarget{}
	}
	return routeTarget{SessionKey: acpsession.ConversationSessionKey(conversationRowID), ACP: true}
}

/*
 * RunTurn 实现见 driver.go 契约：一步解析路由目标后转发对应引擎，AcpSessionKey 在此回填
 * （TurnRequest 为值拷贝）；ACP 引擎的同步失败包 turnNotAcceptedError 标记（回合未发生，
 * 编排器不清会话锚点）。
 */
func (r *driverRoute) RunTurn(ctx context.Context, req TurnRequest) (<-chan TurnEvent, error) {
	target, err := r.resolve(req.BotID, req.WorkspaceID, req.ConversationKey)
	if err != nil {
		return nil, turnNotAcceptedError{fmt.Errorf("读取工作空间启动配置失败: %w", err)}
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
	target, err := r.resolve(cfg.BotID, cfg.WorkspaceID, conversationKey)
	if err != nil {
		return false, err
	}
	return target.ACP, nil
}
