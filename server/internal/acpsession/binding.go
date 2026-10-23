package acpsession

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

/*
 * conversationSessionKeyPrefix 会话锚键的 IM 会话形前缀（conv-<t_im_bot_conversations.id>）。
 * 键形分派 SSOT：bot 域经 ConversationSessionKey 生成、本域各处按前缀消费；与 issue uuid
 * 键形互斥——uuid hex 字符集拼不出该前缀，无命名空间冲突。
 */
const conversationSessionKeyPrefix = "conv-"

// ConversationSessionKey 拼装 IM 会话锚键（未关联任务的 bot 会话以会话行本身为会话载体）。
func ConversationSessionKey(conversationRowID string) string {
	return conversationSessionKeyPrefix + conversationRowID
}

// IsConversationSessionKey 判定会话锚键是否 IM 会话形（非空即 issue uuid 形）。
func IsConversationSessionKey(sessionKey string) bool {
	_, ok := conversationRowID(sessionKey)
	return ok
}

// conversationRowID 拆出 conv- 键的会话行 id（非本形 ok=false）。
func conversationRowID(sessionKey string) (string, bool) {
	id, ok := strings.CutPrefix(sessionKey, conversationSessionKeyPrefix)
	return id, ok && id != ""
}

// mustConversationRowID 键形已判定的方法内取行 id（缺失即调用方未分派，程序缺陷）。
func mustConversationRowID(sessionKey string) string {
	id, _ := conversationRowID(sessionKey)
	return id
}

/*
 * BindingStore 会话锚点存储（会话域唯一 DB 依赖点）。锚载体随 t_issue_acp_sessions 删除与
 * t_im_bot_conversations.acp_session_id 列下线暂为窄桩：conv- 键仅校验会话行在场，
 * 锚读写全部 no-op（闲置重建退化为新会话）；锚语义随 T1.2 迁 t_conversations.claude_session_id 恢复。
 */
type BindingStore struct {
	DB *gorm.DB
}

// GetOrCreate 确保锚载体就绪：conv- 键校验会话行在场（不存在报错）；issue 键暂无载体（no-op）。
func (s *BindingStore) GetOrCreate(ctx context.Context, sessionKey string) error {
	if _, ok := conversationRowID(sessionKey); !ok {
		return nil
	}
	_, err := s.conversationRow(ctx, sessionKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("会话行不存在，无法作为 ACP 会话锚")
	}
	return err
}

// Peek 只读锚点 acpSessionId（重建路径的 resume 依据）：窄桩期恒空串（无锚 = Ensure 走新会话）。
func (s *BindingStore) Peek(ctx context.Context, sessionKey string) (string, error) {
	return "", nil
}

// SaveReady 会话就绪落锚：窄桩期 no-op。
func (s *BindingStore) SaveReady(ctx context.Context, sessionKey, agentCode, acpSessionID string) error {
	return nil
}

// SaveError 失败/意外死亡落因并清锚：窄桩期 no-op。
func (s *BindingStore) SaveError(ctx context.Context, sessionKey, agentCode, reason string) error {
	return nil
}

// Delete 删除锚载体：窄桩期 no-op。
func (s *BindingStore) Delete(ctx context.Context, sessionKey string) error {
	return nil
}

// conversationRow IM 会话行取行口（conv- 键载体存在性校验用）。
func (s *BindingStore) conversationRow(ctx context.Context, sessionKey string) (*model.ImBotConversation, error) {
	q := query.Use(s.DB)
	return q.ImBotConversation.WithContext(ctx).Where(
		q.ImBotConversation.ID.Eq(mustConversationRowID(sessionKey)),
	).First()
}
