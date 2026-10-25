package service

import (
	"time"

	"ocean-harness/server/internal/acpsession"
	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/global"
)

/*
 * AcpSession 对应 /api/acpSession 命名空间下的业务逻辑。无本地状态：SSOT 为会话域门面
 * global.AcpSessions（main 装配），本 service 只做受理转发与投影。
 * HTTP 面收裸 conversationId，会话锚键（conv-<id> 形）包装统一在本层完成。
 */
type AcpSession struct {
	apis.Service
}

/*
 * Ensure 幂等受理会话创建（异步建模）：立即返回受理时刻快照，spawn 链在后台进行，
 * 就绪/失败经 SSE sessionStatus 帧或 getInfo 轮询跟进。pickedLaunchMode 临场启动声明透传。
 */
func (svc AcpSession) Ensure(req *types.AcpSessionEnsureRequest) (acpsession.ViewSnapshot, error) {
	return global.AcpSessions.Ensure(svc.Context, acpsession.ConversationSessionKey(req.ConversationID), req.PickedLaunchMode)
}

// GetInfo 当前会话视图快照（轮询读端，无副作用；无会话为 idle 空快照）。
func (svc AcpSession) GetInfo(req *types.AcpSessionGetInfoRequest) (acpsession.ViewSnapshot, error) {
	return global.AcpSessions.Get(acpsession.ConversationSessionKey(req.ConversationID)), nil
}

// Prompt 受理一轮回合（立即返回；回合终态经 SSE turnEnded 帧）。
// Prompt 受理一轮回合，并回写会话 updated_at（会话行「相对时间」的数据源：
// bot 路径由 HandleInbound 的 TouchLastMessage 承担，桌面 ACP 路径由此处对等补齐）。
func (svc AcpSession) Prompt(req *types.AcpSessionPromptRequest) error {
	if err := global.AcpSessions.Prompt(acpsession.ConversationSessionKey(req.ConversationID), req.Text); err != nil {
		return err
	}
	q := query.Use(svc.Orm)
	_, err := q.Conversation.WithContext(svc.Context).
		Where(q.Conversation.ID.Eq(req.ConversationID)).
		UpdateSimple(q.Conversation.UpdatedAt.Value(time.Now()))
	return err
}

// Cancel 软取消当前回合（幂等 no-op 安全）。
func (svc AcpSession) Cancel(req *types.AcpSessionCancelRequest) error {
	return global.AcpSessions.Cancel(acpsession.ConversationSessionKey(req.ConversationID))
}

/*
 * RespondPermission 以 optionId 应答挂起权限审批。HTTP 面即桌面 issue 主窗口，source
 * 恒 panel（T2.4 收敛语义的入口标记；bot 快路径不经本层）。
 */
func (svc AcpSession) RespondPermission(req *types.AcpSessionRespondPermissionRequest) error {
	return global.AcpSessions.RespondPermission(acpsession.ConversationSessionKey(req.ConversationID), req.PendingID, req.OptionID, acpsession.PendingSourcePanel)
}

/*
 * RespondElicitation 应答挂起 elicitation（accept 带 content / decline / cancel）；
 * source 同 RespondPermission 恒 panel。
 */
func (svc AcpSession) RespondElicitation(req *types.AcpSessionRespondElicitationRequest) error {
	return global.AcpSessions.RespondElicitation(acpsession.ConversationSessionKey(req.ConversationID), req.PendingID, req.Action, req.Content, acpsession.PendingSourcePanel)
}
