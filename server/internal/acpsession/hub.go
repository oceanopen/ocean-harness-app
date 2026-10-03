package acpsession

import (
	"sync"

	"github.com/BrokkAi/acp-go/schema"
)

// subscriberBuffer 单订户有界缓冲：满即断开（溢出策略见 broadcast）——pump 永不阻塞于
// 慢 SSE 客户端（保住 acp 事件流的背压契约：Session.Events() 必须被持续排空，慢消费会
// 拖住整条连接的读循环）。断开的客户端由 EventSource 自动重连取全新快照（对齐 PTY
// scrollback 重挂载语义；不做 Last-Event-ID 续传）。
const subscriberBuffer = 256

// FrameType SSE 帧型全集（信封 type 字段取值域）。
type FrameType string

const (
	FrameSnapshot        FrameType = "snapshot"        // 建连首帧：当前 ViewSnapshot
	FrameSessionStatus   FrameType = "sessionStatus"   // 会话状态迁移（starting/ready/failed）
	FrameEntry           FrameType = "entry"           // 会话记录条目 upsert（覆写式）
	FramePlanUpdated     FrameType = "planUpdated"     // 快照类：最新替换
	FrameUsageUpdated    FrameType = "usageUpdated"    // 快照类：最新替换
	FrameCommandsUpdated FrameType = "commandsUpdated" // 快照类：最新替换
	FrameModeUpdated     FrameType = "modeUpdated"     // 快照类：最新替换
	FramePendingOpened   FrameType = "pendingOpened"   // 挂起审批/表单开启
	FramePendingClosed   FrameType = "pendingClosed"   // 挂起关闭（应答/结算）
	FrameTurnStarted     FrameType = "turnStarted"     // 回合受理
	FrameTurnEnded       FrameType = "turnEnded"       // 回合终态（stopReason/错误）
	FrameTerminated      FrameType = "terminated"      // 会话终结（终态帧，订阅端收到即可断开）
)

// Frame SSE 帧信封：seq 按 issue 单调递增（前端检测 gap 即重连取快照），type 判别载荷
// （至多一个非零载荷字段，与 Type 对应）。JSON 序列化即 wire 形态。
type Frame struct {
	Seq     uint64    `json:"seq"`
	IssueID string    `json:"issueId"`
	Type    FrameType `json:"type"`

	Snapshot  *ViewSnapshot             `json:"snapshot,omitempty"`
	Status    *StatusPayload            `json:"status,omitempty"`
	Entry     *ConversationEntry        `json:"entry,omitempty"`
	Plan      *schema.Plan              `json:"plan,omitempty"`
	Usage     *schema.UsageUpdate       `json:"usage,omitempty"`
	Commands  []schema.AvailableCommand `json:"commands,omitempty"`
	Mode      *schema.SessionModeState  `json:"mode,omitempty"`
	Pending   *PendingView              `json:"pending,omitempty"`
	PendingID uint64                    `json:"pendingId,omitempty"`
	Turn      *TurnPayload              `json:"turn,omitempty"`
}

// StatusPayload sessionStatus / terminated 帧载荷。
type StatusPayload struct {
	Status       SessionStatus `json:"status"`
	AgentCode    string        `json:"agentCode,omitempty"`
	AcpSessionID string        `json:"acpSessionId,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// TurnPayload turnStarted / turnEnded 帧载荷。
type TurnPayload struct {
	Active     bool   `json:"active"`
	StopReason string `json:"stopReason,omitempty"`
	Error      string `json:"error,omitempty"`
}

// hub issue 粒度的 SSE 订阅注册表：seq 单调分配 + 建连即快照（首帧）+ 广播扇出。
// 所有方法内部加锁；帧发送永不阻塞（溢出断开慢订户）。
type hub struct {
	mu     sync.Mutex
	subs   map[string]map[*subscriber]struct{}
	seq    map[string]uint64
	closed bool
}

type subscriber struct {
	issueID string
	ch      chan Frame
}

func newHub() *hub {
	return &hub{
		subs: map[string]map[*subscriber]struct{}{},
		seq:  map[string]uint64{},
	}
}

// subscribe 订阅并装配首帧：快照在 hub 锁内生成入订户通道（seq 连续分配，杜绝快照与
// 增量帧之间的缝隙）。snapshot 由调用方提供投影（Manager.Get）。返回帧通道与退订函数。
func (h *hub) subscribe(issueID string, snapshot func() *ViewSnapshot) (<-chan Frame, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sub := &subscriber{issueID: issueID, ch: make(chan Frame, subscriberBuffer)}
	if h.subs[issueID] == nil {
		h.subs[issueID] = map[*subscriber]struct{}{}
	}
	h.subs[issueID][sub] = struct{}{}
	if snapshot != nil && !h.closed {
		h.seq[issueID]++
		snap := snapshot()
		sub.ch <- Frame{Seq: h.seq[issueID], IssueID: issueID, Type: FrameSnapshot, Snapshot: snap}
	}
	return sub.ch, func() { h.unsubscribe(sub) }
}

// broadcast 帧扇出：seq 逐帧分配后投递。慢订户（缓冲满）即断开——断开的是订阅而非
// HTTP 连接，客户端 EventSource 自动重连（重连即新快照）。
func (h *hub) broadcast(issueID string, frames []Frame) {
	if len(frames) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for i := range frames {
		h.seq[issueID]++
		frame := frames[i]
		frame.Seq = h.seq[issueID]
		frame.IssueID = issueID
		for sub := range h.subs[issueID] {
			select {
			case sub.ch <- frame:
			default:
				delete(h.subs[issueID], sub)
				close(sub.ch)
			}
		}
	}
}

// unsubscribe 退订（SSE handler defer 调用；幂等）。
func (h *hub) unsubscribe(sub *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs, ok := h.subs[sub.issueID]
	if !ok {
		return
	}
	if _, ok := subs[sub]; !ok {
		return
	}
	delete(subs, sub)
	close(sub.ch)
}

// closeAll 关停全部订阅（SIGTERM 收尾：SSE handler 的通道关闭即返回，不拖 HTTP shutdown）。
func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for issueID, subs := range h.subs {
		for sub := range subs {
			close(sub.ch)
		}
		delete(h.subs, issueID)
	}
}
