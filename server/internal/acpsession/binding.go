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
 * BindingStore 会话锚点存储（会话域唯一 DB 依赖点），双锚形分派：issue uuid →
 * t_issue_acp_sessions（工作台任务域行，GetOrCreate 建行）；conv- 键 →
 * t_im_bot_conversations.acp_session_id（行属 bot 域，只读写锚列，缺失报错不代建）。
 * DB 是锚点 SSOT，运行时注册表（Manager.entries）是投影；写法对齐 bot 域 ConversationStore：
 * 受理建行、锚点回写、失败清锚、无内存副本。
 */
type BindingStore struct {
	DB *gorm.DB
}

// GetOrCreate 确保锚载体就绪：issue 键取行（不存在则建，acp_session_id 空 = 无活跃会话）；
// conv- 键校验会话行在场。
func (s *BindingStore) GetOrCreate(ctx context.Context, sessionKey string) error {
	if _, ok := conversationRowID(sessionKey); ok {
		_, err := s.conversationRow(ctx, sessionKey)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("会话行不存在，无法作为 ACP 会话锚")
		}
		return err
	}
	q := query.Use(s.DB)
	_, err := s.issueRow(ctx, sessionKey)
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	row := &model.IssueAcpSession{IssueID: sessionKey}
	return q.IssueAcpSession.WithContext(ctx).Create(row)
}

/*
 * Peek 只读锚点 acpSessionId（重建路径的 resume 依据：非空即可经适配器 session/load 恢复
 * 会话）。不动行、不建行；载体不存在返回空串（无锚语义与 GetOrCreate 建行的空值一致）。
 */
func (s *BindingStore) Peek(ctx context.Context, sessionKey string) (string, error) {
	if _, ok := conversationRowID(sessionKey); ok {
		conv, err := s.conversationRow(ctx, sessionKey)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return conv.AcpSessionID, nil
	}
	row, err := s.issueRow(ctx, sessionKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.AcpSessionID, nil
}

// SaveReady 会话就绪落锚：issue 键记 agentCode + acpSessionId 并清 last_error（新锚生效即旧错失效）；
// conv- 键只写会话行 acp_session_id（bot 域行无 agent_code/last_error 锚面）。
func (s *BindingStore) SaveReady(ctx context.Context, sessionKey, agentCode, acpSessionID string) error {
	q := query.Use(s.DB)
	if _, ok := conversationRowID(sessionKey); ok {
		_, err := q.ImBotConversation.WithContext(ctx).Where(
			q.ImBotConversation.ID.Eq(mustConversationRowID(sessionKey)),
		).UpdateSimple(
			q.ImBotConversation.AcpSessionID.Value(acpSessionID),
			q.ImBotConversation.UpdatedAt.Value(time.Now()),
		)
		return err
	}
	_, err := q.IssueAcpSession.WithContext(ctx).Where(
		q.IssueAcpSession.IssueID.Eq(sessionKey),
	).UpdateSimple(
		q.IssueAcpSession.AgentCode.Value(enums.AgentCode(agentCode)),
		q.IssueAcpSession.AcpSessionID.Value(acpSessionID),
		q.IssueAcpSession.LastError.Value(""),
		q.IssueAcpSession.UpdatedAt.Value(time.Now()),
	)
	return err
}

/*
 * SaveError 失败/意外死亡落因并清锚（issue 键：agentCode 记创建会话时的实际取值；last_error
 * 跨重启可见，对齐 t_im_bots.last_error。conv- 键：会话行无错误锚面，仅清 acp_session_id）。
 * update-only：载体不存在时 no-op，不复活已被 Discard 级联删除的行。
 */
func (s *BindingStore) SaveError(ctx context.Context, sessionKey, agentCode, reason string) error {
	q := query.Use(s.DB)
	if _, ok := conversationRowID(sessionKey); ok {
		_, err := q.ImBotConversation.WithContext(ctx).Where(
			q.ImBotConversation.ID.Eq(mustConversationRowID(sessionKey)),
		).UpdateSimple(
			q.ImBotConversation.AcpSessionID.Value(""),
			q.ImBotConversation.UpdatedAt.Value(time.Now()),
		)
		return err
	}
	_, err := q.IssueAcpSession.WithContext(ctx).Where(
		q.IssueAcpSession.IssueID.Eq(sessionKey),
	).UpdateSimple(
		q.IssueAcpSession.AgentCode.Value(enums.AgentCode(agentCode)),
		q.IssueAcpSession.AcpSessionID.Value(""),
		q.IssueAcpSession.LastError.Value(reason),
		q.IssueAcpSession.UpdatedAt.Value(time.Now()),
	)
	return err
}

// Delete 删除锚载体（issue 删除级联；conv- 键的会话行属 bot 域不受会话域级联，恒 no-op）。
func (s *BindingStore) Delete(ctx context.Context, sessionKey string) error {
	if _, ok := conversationRowID(sessionKey); ok {
		return nil
	}
	q := query.Use(s.DB)
	_, err := q.IssueAcpSession.WithContext(ctx).Where(
		q.IssueAcpSession.IssueID.Eq(sessionKey),
	).Delete()
	return err
}

/*
 * SweepSessionIDs 启动清扫：sidecar 重启后全部运行时锚失效（无 resume 语义，P3 同向），
 * 双锚形统一清零。载体行保留（配置连续性），仅锚与错误证据归位。恒真条件满足 gorm
 * 全表 UPDATE 的 where 强制。
 */
func (s *BindingStore) SweepSessionIDs(ctx context.Context) error {
	q := query.Use(s.DB)
	if _, err := q.IssueAcpSession.WithContext(ctx).Where(
		q.IssueAcpSession.IssueID.Neq(""),
	).UpdateSimple(
		q.IssueAcpSession.AcpSessionID.Value(""),
		q.IssueAcpSession.UpdatedAt.Value(time.Now()),
	); err != nil {
		return err
	}
	_, err := q.ImBotConversation.WithContext(ctx).Where(
		q.ImBotConversation.ID.Neq(""),
	).UpdateSimple(
		q.ImBotConversation.AcpSessionID.Value(""),
		q.ImBotConversation.UpdatedAt.Value(time.Now()),
	)
	return err
}

// issueRow issue 锚行取行口（issue 键形各方法共用）。
func (s *BindingStore) issueRow(ctx context.Context, sessionKey string) (*model.IssueAcpSession, error) {
	q := query.Use(s.DB)
	return q.IssueAcpSession.WithContext(ctx).Where(
		q.IssueAcpSession.IssueID.Eq(sessionKey),
	).First()
}

// conversationRow IM 会话行取行口（conv- 键形各方法共用）。
func (s *BindingStore) conversationRow(ctx context.Context, sessionKey string) (*model.ImBotConversation, error) {
	q := query.Use(s.DB)
	return q.ImBotConversation.WithContext(ctx).Where(
		q.ImBotConversation.ID.Eq(mustConversationRowID(sessionKey)),
	).First()
}
