package acpsession

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

/*
 * conversationSessionKeyPrefix 会话锚键前缀（conv-<t_conversations.id>，唯一会话源——全部
 * 会话实体行在 t_conversations，无第二载体）。
 */
const conversationSessionKeyPrefix = "conv-"

// ConversationSessionKey 拼装会话锚键（conv-<t_conversations.id> 形）。
func ConversationSessionKey(conversationRowID string) string {
	return conversationSessionKeyPrefix + conversationRowID
}

// IsConversationSessionKey 判定会话锚键是否带 conv- 前缀（非空即键形非法调用方的防御位）。
func IsConversationSessionKey(sessionKey string) bool {
	_, ok := conversationRowID(sessionKey)
	return ok
}

// conversationRowID 拆出 conv- 键的行 id（非本形 ok=false）。
func conversationRowID(sessionKey string) (string, bool) {
	id, ok := strings.CutPrefix(sessionKey, conversationSessionKeyPrefix)
	return id, ok && id != ""
}

/*
 * BindingStore 会话锚点存储（锚点唯一读写点）。锚值 = t_conversations.claude_session_id（建行
 * 即预生成，以实际 claude 会话 id 为准回写/同步），status pending=预置未驱动、active=已驱动；
 * DB 是锚点 SSOT，运行时注册表（Manager.entries）是投影。
 */
type BindingStore struct {
	DB *gorm.DB
}

// GetOrCreate 确保锚载体就绪：键须有会话实体行，皆无即报错。
func (s *BindingStore) GetOrCreate(ctx context.Context, sessionKey string) error {
	id, ok := conversationRowID(sessionKey)
	if !ok {
		return nil
	}
	q := query.Use(s.DB)
	if _, err := q.Conversation.WithContext(ctx).Where(q.Conversation.ID.Eq(id)).First(); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("会话行不存在，无法作为 ACP 会话锚")
		}
		return err
	}
	return nil
}

/*
 * Anchor 会话锚点快照：ClaudeSessionID 为 claude 会话 id（空 = 无锚走全新会话），
 * Status 为驱动状态（pending = 预置未驱动——以预置 id 开新会话；active/存量 = LoadSession 恢复）。
 */
type Anchor struct {
	ClaudeSessionID string
	Status          enums.ClaudeSessionStatus
}

// Peek 只读锚点（闲置重建与跨重启路径的 resume 依据）；行不存在恒零值（无锚语义）。
func (s *BindingStore) Peek(ctx context.Context, sessionKey string) (Anchor, error) {
	id, ok := conversationRowID(sessionKey)
	if !ok {
		return Anchor{}, nil
	}
	conv, err := s.conversationEntityRow(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Anchor{}, nil
		}
		return Anchor{}, err
	}
	return Anchor{ClaudeSessionID: conv.ClaudeSessionID, Status: conv.ClaudeSessionStatus}, nil
}

/*
 * SaveReady 会话就绪落锚：agent_code + 实际 claude 会话 id + 置 active（新锚生效即覆盖旧值
 * ——降级/轮换后以实际值为准，绝对一致优先于预置值）。
 */
func (s *BindingStore) SaveReady(ctx context.Context, sessionKey, agentCode, acpSessionID string) error {
	id, ok := conversationRowID(sessionKey)
	if !ok {
		return nil
	}
	q := query.Use(s.DB)
	_, err := q.Conversation.WithContext(ctx).Where(q.Conversation.ID.Eq(id)).UpdateSimple(
		q.Conversation.AgentCode.Value(enums.AgentCode(agentCode)),
		q.Conversation.ClaudeSessionID.Value(acpSessionID),
		q.Conversation.ClaudeSessionStatus.Value(enums.CLAUDE_SESSION_STATUS_ACTIVE),
		q.Conversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

/*
 * SaveError 失败/意外死亡锚重置（会话实体行无 last_error 锚面，失败证据经状态帧与快照透出）：
 * 换新预置 id 并回 pending——下次受理以新 id 开新会话。update-only：载体不存在时 no-op。
 */
func (s *BindingStore) SaveError(ctx context.Context, sessionKey, agentCode, reason string) error {
	id, ok := conversationRowID(sessionKey)
	if !ok {
		return nil
	}
	sessionID, status := model.NewPresetClaudeSession()
	q := query.Use(s.DB)
	_, err := q.Conversation.WithContext(ctx).Where(q.Conversation.ID.Eq(id)).UpdateSimple(
		q.Conversation.ClaudeSessionID.Value(sessionID),
		q.Conversation.ClaudeSessionStatus.Value(status),
		q.Conversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

// Delete 清锚保行（会话实体归 CRUD 管理不删行，Discard 级联仅重置锚）。
func (s *BindingStore) Delete(ctx context.Context, sessionKey string) error {
	return s.SaveError(ctx, sessionKey, "", "")
}

/*
 * SyncClaudeSessionID 实际 claude 会话 id 持续同步（绝对一致主通道：adapter 侧 id 轮换或
 * 降级随机 id 时以发布值为准即时覆锚）。空 id no-op；值未变不写。
 */
func (s *BindingStore) SyncClaudeSessionID(ctx context.Context, sessionKey, claudeSessionID string) error {
	if claudeSessionID == "" {
		return nil
	}
	id, ok := conversationRowID(sessionKey)
	if !ok {
		return nil
	}
	conv, err := s.conversationEntityRow(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if conv.ClaudeSessionID == claudeSessionID {
		return nil
	}
	q := query.Use(s.DB)
	_, err = q.Conversation.WithContext(ctx).Where(q.Conversation.ID.Eq(id)).UpdateSimple(
		q.Conversation.ClaudeSessionID.Value(claudeSessionID),
		q.Conversation.ClaudeSessionStatus.Value(enums.CLAUDE_SESSION_STATUS_ACTIVE),
		q.Conversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

// conversationEntityRow 会话实体行取行口（锚读写载体；行不存在返回 gorm.ErrRecordNotFound）。
func (s *BindingStore) conversationEntityRow(ctx context.Context, id string) (*model.Conversation, error) {
	q := query.Use(s.DB)
	return q.Conversation.WithContext(ctx).Where(q.Conversation.ID.Eq(id)).First()
}
