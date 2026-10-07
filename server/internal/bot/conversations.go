package bot

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

// seenWindowLimit 消息幂等滚窗上限：企微 WS 无补投语义，幂等仅防瞬时重推，100 条远超重推窗口。
const seenWindowLimit = 100

// ConversationStore t_im_bot_conversations 行的唯一读写点（bot 核心层；路由层纯读与
// #任务 候选匹配另按需裸查各自表，不经过本 store）。调用有两类上下文：会话串行 worker
// goroutine（回合链）与编排器拦截步所在的适配器回调 goroutine（幂等查询/卡片交互，
// 见 orchestrator.HandleInbound）——两类上下文写列正交、单语句 UPDATE 原子，无并发
// 丢更新路径，不需要行级锁。DB 是 SSOT：sidecar 重启后下一条消息自然续上，运行时不做
// 内存副本。
type ConversationStore struct {
	DB *gorm.DB
}

// GetOrCreate 取会话映射行，不存在则建（空会话 = 未开场：claude_session_id 空、seen 空表）。
func (s *ConversationStore) GetOrCreate(botID int, conversationKey string) (*model.ImBotConversation, error) {
	q := query.Use(s.DB)
	conv, err := q.ImBotConversation.WithContext(context.Background()).Where(
		q.ImBotConversation.BotID.Eq(botID),
		q.ImBotConversation.ConversationKey.Eq(conversationKey),
	).First()
	if err == nil {
		return conv, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	conv = &model.ImBotConversation{
		BotID:           botID,
		ConversationKey: conversationKey,
		ClaudeSessionID: "",
		SeenMessageIds:  "[]",
	}
	if err := q.ImBotConversation.WithContext(context.Background()).Create(conv); err != nil {
		return nil, err
	}
	return conv, nil
}

// SessionID 读当前 claude 会话 id（空 = 下条消息开新会话）。
func (s *ConversationStore) SessionID(botID int, conversationKey string) (string, error) {
	conv, err := s.GetOrCreate(botID, conversationKey)
	if err != nil {
		return "", err
	}
	return conv.ClaudeSessionID, nil
}

// SaveSessionID 回写 claude 会话 id（每回合以 init/result 捕获值为准——--resume 后 id 可能
// 刷新，每回合回写保证下一轮 -r 正确）。
func (s *ConversationStore) SaveSessionID(botID int, conversationKey, sessionID string) error {
	q := query.Use(s.DB)
	_, err := q.ImBotConversation.WithContext(context.Background()).Where(
		q.ImBotConversation.BotID.Eq(botID),
		q.ImBotConversation.ConversationKey.Eq(conversationKey),
	).UpdateSimple(
		q.ImBotConversation.ClaudeSessionID.Value(sessionID),
		q.ImBotConversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

// FinishTurn 回合收尾持久化：会话 id + 最近消息时间（一次性落两项，语义原子）。
func (s *ConversationStore) FinishTurn(botID int, conversationKey, sessionID string, at time.Time) error {
	q := query.Use(s.DB)
	_, err := q.ImBotConversation.WithContext(context.Background()).Where(
		q.ImBotConversation.BotID.Eq(botID),
		q.ImBotConversation.ConversationKey.Eq(conversationKey),
	).UpdateSimple(
		q.ImBotConversation.ClaudeSessionID.Value(sessionID),
		q.ImBotConversation.LastMessageAt.Value(at),
		q.ImBotConversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

// BoundIssueID 读会话显式绑定的目标 issue id（空串 = 未绑定）。读失败交由调用方定降级
// （消费方按未绑定处理，与 SessionID/HasSeen 同款「尽力读」惯例）。
func (s *ConversationStore) BoundIssueID(botID int, conversationKey string) (string, error) {
	conv, err := s.GetOrCreate(botID, conversationKey)
	if err != nil {
		return "", err
	}
	return conv.BoundIssueID, nil
}

// SaveBoundIssueID 写绑定锚（issueID 空串 = 解绑；幂等覆盖），返回受影响行数——UPDATE
// 无行匹配不是 error（行不在场 0 行静默成功曾是隐患），绑定路径调用方须按 0 行失败收口；
// 解绑路径幂等语义天然忽略行数。与 claude_session_id 正交：headless 续聊锚与任务绑定无
// 耦合，绑定/解绑均不动它；ACP 模式本就不写会话锚。调用横跨 worker（#任务 指令）与拦截
// 步回调（taskbind 卡点击）两上下文——只写绑定列，与 worker 侧写列正交（见类型注释）。
func (s *ConversationStore) SaveBoundIssueID(botID int, conversationKey, issueID string) (int, error) {
	q := query.Use(s.DB)
	res, err := q.ImBotConversation.WithContext(context.Background()).Where(
		q.ImBotConversation.BotID.Eq(botID),
		q.ImBotConversation.ConversationKey.Eq(conversationKey),
	).UpdateSimple(
		q.ImBotConversation.BoundIssueID.Value(issueID),
		q.ImBotConversation.UpdatedAt.Value(time.Now()),
	)
	return int(res.RowsAffected), err
}

// ClearSessionID resume 失效自愈：清空会话 id（下条消息开新会话）。
func (s *ConversationStore) ClearSessionID(botID int, conversationKey string) error {
	return s.SaveSessionID(botID, conversationKey, "")
}

// HasSeen 幂等查询：msgid 是否在滚窗内。
func (s *ConversationStore) HasSeen(botID int, conversationKey, msgID string) (bool, error) {
	conv, err := s.GetOrCreate(botID, conversationKey)
	if err != nil {
		return false, err
	}
	for _, id := range decodeSeenIDs(conv.SeenMessageIds) {
		if id == msgID {
			return true, nil
		}
	}
	return false, nil
}

// MarkSeen 受理时落定（不回滚）：回合失败也已用错误终帧回复了用户，视为已处理；
// 避免失败重推永远卡死的墓碑问题。滚窗超限丢最旧。
func (s *ConversationStore) MarkSeen(botID int, conversationKey, msgID string) error {
	q := query.Use(s.DB)
	conv, err := s.GetOrCreate(botID, conversationKey)
	if err != nil {
		return err
	}
	ids := append(decodeSeenIDs(conv.SeenMessageIds), msgID)
	if len(ids) > seenWindowLimit {
		ids = ids[len(ids)-seenWindowLimit:]
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	_, err = q.ImBotConversation.WithContext(context.Background()).Where(q.ImBotConversation.ID.Eq(conv.ID)).UpdateSimple(
		q.ImBotConversation.SeenMessageIds.Value(string(encoded)),
		q.ImBotConversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

// decodeSeenIDs 解析 seen JSON；损坏按空表处理（幂等是尽力语义，不因列损坏阻断消息）。
func decodeSeenIDs(raw string) []string {
	if raw == "" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil
	}
	return ids
}

// last_message_card 列的处理状态（绑定族卡登记槽）。
const (
	lastMessageCardPending   = "pending"
	lastMessageCardProcessed = "processed"
)

// lastMessageCard 会话行 last_message_card 列的 JSON 形态：最近一张绑定族卡（wsbind/taskbind/taskunbind）
// 的消费状态 + 出卡时的完整 CardSpec 快照。快照仅供排查与将来「灰化旧卡」重建帧用——消费
// 链不得从快照取数（绑定关系走点击时指纹重导，快照不作数据源）。出卡即覆盖本槽，旧卡点击
// 自然按已失效收口（supersession 过期，无需 TTL）。
type lastMessageCard struct {
	Kind   string   `json:"kind"`   // 出卡族：wsbind / taskbind / taskunbind
	Status string   `json:"status"` // pending 未消费 / processed 已消费
	Spec   CardSpec `json:"spec"`   // 出卡时完整快照（Spec.TaskID 即点击对照锚）
}

// SaveLastMessageCard 出卡登记（status=pending 覆盖旧槽）。时序约定：先写库后发卡——写库失败
// 调用方不得出卡（防「刚收到的卡一点就提示已失效」）；写库成功而发卡失败则槽指向未送达
// 卡，旧卡点击按已失效收口，用户重发指令即可自愈。
func (s *ConversationStore) SaveLastMessageCard(botID int, conversationKey, kind string, spec CardSpec) error {
	q := query.Use(s.DB)
	conv, err := s.GetOrCreate(botID, conversationKey)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(lastMessageCard{Kind: kind, Status: lastMessageCardPending, Spec: spec})
	if err != nil {
		return err
	}
	_, err = q.ImBotConversation.WithContext(context.Background()).Where(q.ImBotConversation.ID.Eq(conv.ID)).UpdateSimple(
		q.ImBotConversation.LastMessageCard.Value(string(encoded)),
		q.ImBotConversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

// LastMessageCard 读最近卡登记；未建行/空列/损坏一律按无登记处理（点击消费 fail open 走原链，
// 与 decodeSeenIDs 同款尽力语义）。纯读不建行——点击路径不得因登记查询顺手创建会话行
// （生产上行由受理步 GetOrCreate 保证在场，缺席即防御分支）。
func (s *ConversationStore) LastMessageCard(botID int, conversationKey string) (lastMessageCard, bool) {
	q := query.Use(s.DB)
	conv, err := q.ImBotConversation.WithContext(context.Background()).Where(
		q.ImBotConversation.BotID.Eq(botID),
		q.ImBotConversation.ConversationKey.Eq(conversationKey),
	).First()
	if err != nil {
		return lastMessageCard{}, false
	}
	return decodeLastMessageCard(conv.LastMessageCard)
}

// MarkLastMessageCardProcessed 消费置位（绑定族点击消费成功后调用）。乐观锁：重读对照锚仍为
// taskID 且列原文未变才写——置位与并发出卡（指令步 vs 拦截步回调）互相顶掉的窗口下不误标
// 新卡；不匹配/0 行静默（卡已被新卡取代，重复点击走已失效收口）。返回是否实际置位。
func (s *ConversationStore) MarkLastMessageCardProcessed(botID int, conversationKey, taskID string) bool {
	q := query.Use(s.DB)
	conv, err := q.ImBotConversation.WithContext(context.Background()).Where(
		q.ImBotConversation.BotID.Eq(botID),
		q.ImBotConversation.ConversationKey.Eq(conversationKey),
	).First()
	if err != nil {
		return false
	}
	lc, ok := decodeLastMessageCard(conv.LastMessageCard)
	if !ok || lc.Spec.TaskID != taskID || lc.Status == lastMessageCardProcessed {
		return false
	}
	lc.Status = lastMessageCardProcessed
	encoded, err := json.Marshal(lc)
	if err != nil {
		return false
	}
	res, err := q.ImBotConversation.WithContext(context.Background()).Where(
		q.ImBotConversation.ID.Eq(conv.ID),
		q.ImBotConversation.LastMessageCard.Eq(conv.LastMessageCard), // 原文比对：并发写则 0 行放弃
	).UpdateSimple(
		q.ImBotConversation.LastMessageCard.Value(string(encoded)),
		q.ImBotConversation.UpdatedAt.Value(time.Now()),
	)
	return err == nil && res.RowsAffected == 1
}

// decodeLastMessageCard 解析 last_message_card JSON；空/损坏/无锚按无登记处理。
func decodeLastMessageCard(raw string) (lastMessageCard, bool) {
	if raw == "" {
		return lastMessageCard{}, false
	}
	var lc lastMessageCard
	if err := json.Unmarshal([]byte(raw), &lc); err != nil || lc.Spec.TaskID == "" {
		return lastMessageCard{}, false
	}
	return lc, true
}
