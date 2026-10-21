package bot

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

// seenWindowLimit 消息幂等滚窗上限：企微 WS 无补投语义，幂等仅防瞬时重推，100 条远超重推窗口。
const seenWindowLimit = 100

/*
 * ConversationStore t_im_bot_conversations 行的唯一读写点（bot 核心层；路由层纯读、HTTP service 层（getConversations/bindIssue）与删除/tracker 级联清绑另按需裸查直写各自列，不经过本 store——均为单语句读写，写列与本 store 正交）。
 * 调用有两类上下文：会话串行 worker goroutine（回合链）与编排器拦截步所在的适配器回调 goroutine（幂等查询/卡片交互，见 orchestrator.HandleInbound）——两类上下文写列正交、单语句 UPDATE 原子，无并发丢更新路径，不需要行级锁。
 * DB 是 SSOT：sidecar 重启后下一条消息自然续上，运行时不做内存副本。
 */
type ConversationStore struct {
	DB *gorm.DB
}

// GetOrCreate 取会话映射行，不存在则建（空会话 = 未开场：claude_session_id 空、seen 空表）。
func (s *ConversationStore) GetOrCreate(botID string, conversationKey string) (*model.ImBotConversation, error) {
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
		ID:              uuid.Must(uuid.NewV7()).String(),
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
func (s *ConversationStore) SessionID(botID string, conversationKey string) (string, error) {
	conv, err := s.GetOrCreate(botID, conversationKey)
	if err != nil {
		return "", err
	}
	return conv.ClaudeSessionID, nil
}

/*
 * SaveSessionID 回写 claude 会话 id（每回合以 init/result 捕获值为准——--resume 后 id 可能
 * 刷新，每回合回写保证下一轮 -r 正确）。
 */
func (s *ConversationStore) SaveSessionID(botID string, conversationKey, sessionID string) error {
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
func (s *ConversationStore) FinishTurn(botID string, conversationKey, sessionID string, at time.Time) error {
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

/*
 * TouchLastMessage 活动时间戳顺延（受理即触碰）：会话时基的兜底写入——ACP 等无锚回写
 * 链路（FinishTurn 不触发）由此获得准确的 last_message_at，闲置判定不受引擎差异失真。
 */
func (s *ConversationStore) TouchLastMessage(botID string, conversationKey string, at time.Time) error {
	q := query.Use(s.DB)
	_, err := q.ImBotConversation.WithContext(context.Background()).Where(
		q.ImBotConversation.BotID.Eq(botID),
		q.ImBotConversation.ConversationKey.Eq(conversationKey),
	).UpdateSimple(
		q.ImBotConversation.LastMessageAt.Value(at),
		q.ImBotConversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

// ClearSessionID resume 失效自愈：清空会话 id（下条消息开新会话）。
func (s *ConversationStore) ClearSessionID(botID string, conversationKey string) error {
	return s.SaveSessionID(botID, conversationKey, "")
}

// sessionIdleExpiry 会话闲置边界：超过即视为上一会话结束（下条消息开新会话，锚点备份供 /继续 接回）。
const sessionIdleExpiry = 2 * time.Hour

/*
 * ExpireIdleSession 闲置过期判定 + 乐观占位写（主锚仍为读到的值才写——并发回合已换锚
 * 则放弃，宁可漏判不错杀）。expired=true 的轮次以新会话启动（headless 消费，ACP 不写
 * 主锚天然不命中）。
 */
func (s *ConversationStore) ExpireIdleSession(botID string, conversationKey string, now time.Time) (expired bool, err error) {
	conv, err := s.GetOrCreate(botID, conversationKey)
	if err != nil {
		return false, err
	}
	if conv.ClaudeSessionID == "" || conv.LastMessageAt == nil || now.Sub(*conv.LastMessageAt) <= sessionIdleExpiry {
		return false, nil
	}
	q := query.Use(s.DB)
	res, err := q.ImBotConversation.WithContext(context.Background()).Where(
		q.ImBotConversation.ID.Eq(conv.ID),
		q.ImBotConversation.ClaudeSessionID.Eq(conv.ClaudeSessionID),
	).UpdateSimple(
		q.ImBotConversation.UpdatedAt.Value(now),
	)
	if err != nil {
		return false, err
	}
	return res.RowsAffected == 1, nil
}

// HasSeen 幂等查询：msgid 是否在滚窗内。
func (s *ConversationStore) HasSeen(botID string, conversationKey, msgID string) (bool, error) {
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

/*
 * MarkSeen 受理时落定（不回滚）：回合失败也已用错误终帧回复了用户，视为已处理；
 * 避免失败重推永远卡死的墓碑问题。滚窗超限丢最旧。
 */
func (s *ConversationStore) MarkSeen(botID string, conversationKey, msgID string) error {
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
