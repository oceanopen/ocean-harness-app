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

// ConversationStore t_im_bot_conversations 读写（核心层唯一 DB 依赖点）。所有调用都发生在
// 单个会话的串行 worker goroutine 内（见 orchestrator），天然无并发写，不需要行级锁。
// DB 是 SSOT：sidecar 重启后下一条消息自然续上，运行时不做内存副本。
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
