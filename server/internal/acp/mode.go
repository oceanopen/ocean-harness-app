package acp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	acpgo "github.com/BrokkAi/acp-go"
	"github.com/BrokkAi/acp-go/schema"
)

// PermissionMode launch_settings.permissionMode 的两档取值（P6）：经 session/set_mode
// 下发。值域校验由库对 agent 上报目录执行（legacy modes 优先，configOptions 回落，
// 目录校验内建）。
type PermissionMode string

const (
	// PermissionModeAcceptEdits 需要审批：编辑自动放行，其余（Bash 等）弹审批。
	PermissionModeAcceptEdits PermissionMode = "acceptEdits"
	// PermissionModeBypassPermissions 自动：agent 免问。
	PermissionModeBypassPermissions PermissionMode = "bypassPermissions"
)

// SetPermissionMode 下发权限模式。值不在 agent 上报目录时库返回 UnknownSelectionError
// （不发请求、本地拒），转译为中文错误并列出可用值；成功后把库就地写入快照的
// CurrentModeID 合并回会话视图（Session.Modes() 可见）。
//
// 锁序红线：不得持 stateMu 发起网络调用——通知回调（current_mode_update 回写）跑在
// 连接读循环 goroutine 上且需要同一把锁，持锁等响应会与读循环互锁（读循环被卡住就读
// 不到本次请求的响应）。故先快照送验，确认后合并。
func (c *AgentClient) SetPermissionMode(ctx context.Context, s *Session, mode PermissionMode) error {
	runtime := s.runtime
	snapshot := runtime.sessionValue()
	// legacy modes 目录本地预检：库对 legacy 未知值返回不带目录的无类型错误，
	// 这里预检补齐「可用值列表」语义（configOptions 轨道的目录校验仍由库承担）。
	if snapshot.Modes != nil {
		available := make([]string, 0, len(snapshot.Modes.AvailableModes))
		found := false
		for _, availableMode := range snapshot.Modes.AvailableModes {
			available = append(available, string(availableMode.ID))
			if availableMode.ID == schema.SessionModeId(mode) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("权限模式 %q 不被当前 agent 支持（可用：%s）", mode, strings.Join(available, ", "))
		}
	}
	err := c.conn.SetMode(ctx, &snapshot, string(mode))
	var unknown *acpgo.UnknownSelectionError
	if errors.As(err, &unknown) {
		return fmt.Errorf("权限模式 %q 不被当前 agent 支持（可用：%s）", mode, strings.Join(unknown.Available, ", "))
	}
	var unsupported *acpgo.UnsupportedSelectionError
	if errors.As(err, &unsupported) {
		return fmt.Errorf("当前 agent 未暴露权限模式（mode）配置面，无法下发 %q", mode)
	}
	if err != nil {
		return err
	}
	if snapshot.Modes != nil {
		runtime.stateMu.Lock()
		if runtime.session.Modes != nil {
			runtime.session.Modes.CurrentModeID = snapshot.Modes.CurrentModeID
		}
		runtime.stateMu.Unlock()
	}
	return nil
}
