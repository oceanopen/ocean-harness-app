package acp

import (
	"context"
	"fmt"
	"strings"

	"github.com/ironpark/acp-go/acp1"
)

/*
 * PermissionMode launch_settings.permissionMode 的两档取值（P6）：经 session/set_mode
 * 下发。值域校验由本层对 agent 上报目录执行（本地预检），agent 侧拒绝原样透传。
 */
type PermissionMode string

const (
	// PermissionModeAcceptEdits 需要审批：编辑自动放行，其余（Bash 等）弹审批。
	PermissionModeAcceptEdits PermissionMode = "acceptEdits"
	// PermissionModeBypassPermissions 自动：agent 免问。
	PermissionModeBypassPermissions PermissionMode = "bypassPermissions"
)

/*
 * SetPermissionMode 下发权限模式：目录外的值本地拒绝（列出可用值），成功后确认值合并
 * 回本地模式视图（current_mode_update 通知随后幂等回写）。
 * 锁序红线：不得持 stateMu 发网络调用——先快照送验，确认后合并。
 */
func (c *AgentClient) SetPermissionMode(ctx context.Context, s *Session, mode PermissionMode) error {
	runtime := s.runtime
	modes := runtime.modesSnapshot()
	/*
	 * 本地目录预检：未知值不发请求（agent 侧报错不带目录，这里补齐「可用值列表」语义）。
	 */
	if modes != nil {
		available := make([]string, 0, len(modes.AvailableModes))
		found := false
		for _, availableMode := range modes.AvailableModes {
			available = append(available, string(availableMode.ID))
			if availableMode.ID == acp1.SessionModeID(mode) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("权限模式 %q 不被当前 agent 支持（可用：%s）", mode, strings.Join(available, ", "))
		}
	}
	if _, err := c.agent.SetSessionMode(ctx, &acp1.SetSessionModeRequest{
		SessionID: runtime.id,
		ModeID:    acp1.SessionModeID(mode),
	}); err != nil {
		return fmt.Errorf("ACP 下发权限模式失败: %w", err)
	}
	runtime.setModeID(acp1.SessionModeID(mode))
	return nil
}
