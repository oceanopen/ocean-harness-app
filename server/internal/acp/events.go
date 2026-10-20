package acp

import (
	"github.com/ironpark/acp-go/acp1"
)

/*
 * sessionEventQueueSize 会话事件通道容量：满则入队阻塞（背压传导至连接层通知分发）。
 * 消费方（T1.5 会话域的 SSE hub）必须持续排空，慢消费会拖住整条连接的所有会话。
 */
const sessionEventQueueSize = 256

/*
 * SessionEvent 会话事件的联合投递单元：per-session 订阅通道按 wire 顺序送达消费方。Update / Permission / Elicitation 至多一个非 nil；
 * Terminated 为终结哨兵——收到即会话终结（进程死亡或会话关闭），
 * 此后通道不再有任何事件（通道本身不关闭），死亡原因经 AgentClient.Err() 查询。
 */
type SessionEvent struct {
	Update      *acp1.SessionNotification // session/update 通知（typed 判别联合）
	Permission  *PendingPermission        // 权限审批请求：等消费方 Respond，不响应将阻塞回合
	Elicitation *PendingElicitation       // elicitation 请求：等消费方 Respond
	Terminated  bool                      // 终结哨兵
}
