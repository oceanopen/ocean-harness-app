package bot

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
)

// launchModeACP workspace 启动模式的 ACP 档（取值 SSOT 是前端 shared/launchSettings.ts；
// acpsession 域同款字面量门禁——跨域各自维护字面量，不做导出耦合）。
const launchModeACP = "acp"

// turnNotAcceptedError 引擎受理拒绝：回合未真正发生，与 headless 会话锚点有效性无关
// （ACP 引擎的同步失败——未绑定引导、配置非法、会话起不来、排队受理被拒——与启动模式
// 解析失败全属此类）。driverRoute 以此标记此类错误，编排器据其跳过 healAfterFailure
// 清锚（errors.As 判别，编排器不感知具体引擎；误清会把 headless 时代的续聊锚点永久丢掉）。
type turnNotAcceptedError struct{ err error }

func (e turnNotAcceptedError) Error() string { return e.err.Error() }

func (e turnNotAcceptedError) Unwrap() error { return e.err }

// routeTarget 路由判定产物：引擎选择 + ACP 档的绑定产物。IssueID 空 = ACP 模式但本会话
// 未绑定（或绑定已挂空），由 ACP 引擎回引导文案；headless 不消费 IssueID。
type routeTarget struct {
	IssueID string
	ACP     bool
}

// effectiveACPMode 启动模式判定（T2.3，mode-only 字段合并）：绑定 issue 的
// launch_settings.mode 非空则覆盖 workspace 级（显式 "none" 亦为有效覆盖值——「明说不要」
// ≠「未配置」，语义对齐 acpsession.mergeLaunchSettings 的字段级合并；bot 路由只消费
// mode 一项，不做整份 launch_settings 拷贝）。issueMode 空 = 未覆盖，回落 workspace 级。
func effectiveACPMode(workspaceMode, issueMode string) bool {
	if issueMode != "" {
		return issueMode == launchModeACP
	}
	return workspaceMode == launchModeACP
}

// driverRoute ClaudeDriver 的引擎路由实现（T2.1 建骨架，T2.3 并入绑定解析）：按 workspace
// 级 launch_settings.mode 与会话显式绑定逐回合在 headless（driver_claude）与 ACP
// （driver_acp）两引擎间切换。路由知识收在引擎层（实现 ClaudeDriver），编排器保持零引擎
// 感知；mode 与绑定每回合读取——workspace 抽屉改启动配置、IM 内 #issue 切换绑定，均对
// 在跑 bot 下一回合生效（bot 不随配置重启）。
type driverRoute struct {
	headless ClaudeDriver
	acp      ClaudeDriver
	// sessions 会话域消费面（审批快路径 Get/RespondPermission 消费；与 acp 引擎同源）。
	sessions acpSessions
	// resolve 一步判定路由目标：读绑定 + issue 存在性/归属校验 + 两级 launch_settings
	// mode 合并。读库失败 fail closed 同步报错（不静默回落 headless——配置读不出时
	// 「以为没配 ACP」是危险默认）；挂空绑定（issue 已删/换工作空间）降级为未绑定。
	resolve func(botID, workspaceID int, conversationKey string) (routeTarget, error)
}

// newDriverRoute 装配路由驱动（返回具体类型：编排器同时把它当 ClaudeDriver 与
// pendingGate 两个角色装配——TryRespondPending 见 pending_gate.go）。绑定行纯读不建行
// （行由编排器受理路径 GetOrCreate 保证在场；路由层不为查询副作用负责）。绑定校验按
// (issue.id, workspace_id) 双键——挂在其他 workspace 的 issue 上的绑定等同未绑定（降级
// 交引导文案，不报错不阻断）。
func newDriverRoute(db *gorm.DB, sessions acpSessions) *driverRoute {
	return &driverRoute{
		headless: NewClaudeDriver(),
		acp:      NewAcpDriver(sessions),
		sessions: sessions,
		resolve: func(botID, workspaceID int, conversationKey string) (routeTarget, error) {
			if workspaceID <= 0 {
				return routeTarget{}, nil // 未选工作空间已被编排器门禁拦截，此处兜底 headless
			}
			q := query.Use(db)
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
			// 未绑定语义 =「是否 ACP 由 workspace 级定，无目标 issue」。绑定与 issue 解析
			// 全部前置完成后统一走 effectiveACPMode 合并判定——issue 级 mode 显式覆盖双向
			// 生效（"none" 否决 / "acp" 启用），不得在绑定解析前按 wsMode 短路（会吞掉
			// issue 显式覆盖；acpsession 域同为合并后判，语义须一致）。
			unbound := routeTarget{ACP: wsMode == launchModeACP}
			conv, err := q.ImBotConversation.WithContext(context.Background()).Where(
				q.ImBotConversation.BotID.Eq(botID),
				q.ImBotConversation.ConversationKey.Eq(conversationKey),
			).First()
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				return unbound, nil // 未绑定：ACP 档时引擎回引导文案
			case err != nil:
				return routeTarget{}, err
			}
			if conv.BoundIssueID == "" {
				return unbound, nil
			}
			issue, err := q.ProjectIssue.WithContext(context.Background()).Where(
				q.ProjectIssue.ID.Eq(conv.BoundIssueID),
				q.ProjectIssue.WorkspaceID.Eq(workspaceID),
			).First()
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				return unbound, nil // 挂空绑定降级未绑定：引擎引导重新 #issue
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
			return routeTarget{IssueID: issue.ID, ACP: true}, nil
		},
	}
}

// RunTurn 实现见 driver.go 契约：一步解析路由目标后转发对应引擎，IssueID 在此回填
// （TurnRequest 为值拷贝）；ACP 引擎的同步失败包 turnNotAcceptedError 标记（回合未发生，
// 编排器不清会话锚点）。
func (r *driverRoute) RunTurn(ctx context.Context, req TurnRequest) (<-chan TurnEvent, error) {
	target, err := r.resolve(req.BotID, req.WorkspaceID, req.ConversationKey)
	if err != nil {
		return nil, turnNotAcceptedError{fmt.Errorf("读取工作空间启动配置失败: %w", err)}
	}
	if !target.ACP {
		return r.headless.RunTurn(ctx, req)
	}
	req.IssueID = target.IssueID
	events, err := r.acp.RunTurn(ctx, req)
	if err != nil {
		return nil, turnNotAcceptedError{err}
	}
	return events, nil
}
