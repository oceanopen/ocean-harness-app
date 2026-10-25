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
 * conversationSessionKeyPrefix 会话锚键前缀（conv-<id>）。键形 SSOT：全部会话锚键带此前缀，
 * 前缀后的 id 按行存在性消歧——t_conversations.id（会话实体，真实锚载体）优先，
 * t_im_bot_conversations.id（bot 未关联会话，窄桩载体）回落；两空间随 T4.3 bot 收敛归一。
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
 * BindingStore 会话锚点存储（会话域唯一 DB 依赖点）。锚值 = t_conversations.claude_session_id
 * （终端/ACP 统一锚，跨重启 resume 语义，agent_code 记创建会话时的实际取值）；conv-<id> 按
 * 行存在性消歧：会话实体行读写锚列，bot 未关联会话行窄桩（仅 GetOrCreate 校验在场，锚读写
 * no-op，其锚语义随 T4.3 收敛）。DB 是锚点 SSOT，运行时注册表（Manager.entries）是投影。
 */
type BindingStore struct {
	DB *gorm.DB
}

// GetOrCreate 确保锚载体就绪：键须有任一载体行（会话实体行或 bot 会话行），皆无即报错。
func (s *BindingStore) GetOrCreate(ctx context.Context, sessionKey string) error {
	id, ok := conversationRowID(sessionKey)
	if !ok {
		return nil
	}
	q := query.Use(s.DB)
	if _, err := q.Conversation.WithContext(ctx).Where(q.Conversation.ID.Eq(id)).First(); err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	_, err := q.ImBotConversation.WithContext(ctx).Where(q.ImBotConversation.ID.Eq(id)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("会话行不存在，无法作为 ACP 会话锚")
	}
	return err
}

// Peek 只读锚点 claudeSessionId（闲置重建与跨重启路径的 resume 依据）；
// bot 窄桩载体与无载体恒空串（无锚语义 = 走全新会话）。
func (s *BindingStore) Peek(ctx context.Context, sessionKey string) (string, error) {
	id, ok := conversationRowID(sessionKey)
	if !ok {
		return "", nil
	}
	conv, err := s.conversationEntityRow(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return conv.ClaudeSessionID, nil
}

// SaveReady 会话就绪落锚：会话实体行写 agent_code + claude_session_id（新锚生效即覆盖旧值）；
// bot 窄桩载体与无载体 no-op。
func (s *BindingStore) SaveReady(ctx context.Context, sessionKey, agentCode, acpSessionID string) error {
	id, ok := conversationRowID(sessionKey)
	if !ok {
		return nil
	}
	q := query.Use(s.DB)
	_, err := q.Conversation.WithContext(ctx).Where(q.Conversation.ID.Eq(id)).UpdateSimple(
		q.Conversation.AgentCode.Value(enums.AgentCode(agentCode)),
		q.Conversation.ClaudeSessionID.Value(acpSessionID),
		q.Conversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

/*
 * SaveError 失败/意外死亡清锚（会话实体行无 last_error 锚面，失败证据经状态帧与快照透出）；
 * bot 窄桩载体与无载体 no-op。update-only：载体不存在时 no-op。
 */
func (s *BindingStore) SaveError(ctx context.Context, sessionKey, agentCode, reason string) error {
	id, ok := conversationRowID(sessionKey)
	if !ok {
		return nil
	}
	q := query.Use(s.DB)
	_, err := q.Conversation.WithContext(ctx).Where(q.Conversation.ID.Eq(id)).UpdateSimple(
		q.Conversation.ClaudeSessionID.Value(""),
		q.Conversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

// Delete 清锚保行（会话实体归 CRUD 管理不删行，Discard 级联仅失效锚；bot 载体行归属 bot 域同样不动）。
func (s *BindingStore) Delete(ctx context.Context, sessionKey string) error {
	return s.SaveError(ctx, sessionKey, "", "")
}

// conversationEntityRow 会话实体行取行口（锚读写载体；行不存在返回 gorm.ErrRecordNotFound）。
func (s *BindingStore) conversationEntityRow(ctx context.Context, id string) (*model.Conversation, error) {
	q := query.Use(s.DB)
	return q.Conversation.WithContext(ctx).Where(q.Conversation.ID.Eq(id)).First()
}
