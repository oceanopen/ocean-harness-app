package service

import (
	"ocean-harness/server/internal/acpsession"
	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/global"
)

// AcpSession 对应 /api/acpSession 命名空间下的业务逻辑。无本地状态：SSOT 为会话域门面
// global.AcpSessions（main 装配），本 service 只做受理转发与投影。
type AcpSession struct {
	apis.Service
}

// Ensure 幂等受理会话创建（异步建模）：立即返回受理时刻快照，spawn 链在后台进行，
// 就绪/失败经 SSE sessionStatus 帧或 getInfo 轮询跟进。pickedLaunchMode 临场启动声明透传。
func (svc AcpSession) Ensure(req *types.AcpSessionEnsureRequest) (acpsession.ViewSnapshot, error) {
	return global.AcpSessions.Ensure(svc.Context, req.IssueID, req.PickedLaunchMode)
}

// GetInfo 当前会话视图快照（轮询读端，无副作用；无会话为 idle 空快照）。
func (svc AcpSession) GetInfo(req *types.AcpSessionGetInfoRequest) (acpsession.ViewSnapshot, error) {
	return global.AcpSessions.Get(req.IssueID), nil
}

// Prompt 受理一轮回合（立即返回；回合终态经 SSE turnEnded 帧）。
func (svc AcpSession) Prompt(req *types.AcpSessionPromptRequest) error {
	return global.AcpSessions.Prompt(req.IssueID, req.Text)
}

// Cancel 软取消当前回合（幂等 no-op 安全）。
func (svc AcpSession) Cancel(req *types.AcpSessionCancelRequest) error {
	return global.AcpSessions.Cancel(req.IssueID)
}

// RespondPermission 以 optionId 应答挂起权限审批。
func (svc AcpSession) RespondPermission(req *types.AcpSessionRespondPermissionRequest) error {
	return global.AcpSessions.RespondPermission(req.IssueID, req.PendingID, req.OptionID)
}

// RespondElicitation 应答挂起 elicitation（accept 带 content / decline / cancel）。
func (svc AcpSession) RespondElicitation(req *types.AcpSessionRespondElicitationRequest) error {
	return global.AcpSessions.RespondElicitation(req.IssueID, req.PendingID, req.Action, req.Content)
}
