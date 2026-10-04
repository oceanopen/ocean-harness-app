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

// driverRoute ClaudeDriver 的引擎路由实现（T2.1）：按 workspace 级 launch_settings.mode
// 逐回合在 headless（driver_claude）与 ACP（driver_acp）两引擎间切换。路由知识收在引擎
// 层（实现 ClaudeDriver），编排器保持零引擎感知；mode 每回合读取——workspace 抽屉改启动
// 配置即对在跑 bot 生效（bot 不随 workspace 配置重启）。issue 级 launch_settings 覆盖在
// T2.3 显式绑定落地后再纳入路由判定（bot 侧尚无 issue 归属，issue 级覆盖无从消费）。
type driverRoute struct {
	headless ClaudeDriver
	acp      ClaudeDriver
	acpMode  func(workspaceID int) (bool, error)
}

// newDriverRoute 装配路由驱动：acpMode 直查 t_workspaces.launch_settings（sqlite 本地读，
// 逐回合开销可忽略；workspaceID 无效 / 行缺失 = 未启用 ACP）。读取失败 fail closed 同步
// 报错（不静默回落 headless——配置读不出时「以为没配 ACP」是危险默认）。
func newDriverRoute(db *gorm.DB, sessions acpSessions) ClaudeDriver {
	return driverRoute{
		headless: NewClaudeDriver(),
		acp:      NewAcpDriver(sessions),
		acpMode: func(workspaceID int) (bool, error) {
			if workspaceID <= 0 {
				return false, nil
			}
			q := query.Use(db)
			ws, err := q.Workspace.WithContext(context.Background()).Where(q.Workspace.ID.Eq(workspaceID)).First()
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return false, nil
				}
				return false, err
			}
			ls := types.ParseLaunchSettings(ws.LaunchSettings)
			return ls != nil && ls.Mode == launchModeACP, nil
		},
	}
}

// RunTurn 实现见 driver.go 契约：解析 workspace 启动模式后转发对应引擎；ACP 引擎的
// 同步失败包 turnNotAcceptedError 标记（回合未发生，编排器不清会话锚点）。
func (r driverRoute) RunTurn(ctx context.Context, req TurnRequest) (<-chan TurnEvent, error) {
	acpMode, err := r.acpMode(req.WorkspaceID)
	if err != nil {
		return nil, turnNotAcceptedError{fmt.Errorf("读取工作空间启动配置失败: %w", err)}
	}
	if acpMode {
		events, err := r.acp.RunTurn(ctx, req)
		if err != nil {
			return nil, turnNotAcceptedError{err}
		}
		return events, nil
	}
	return r.headless.RunTurn(ctx, req)
}
