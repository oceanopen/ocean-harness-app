package acpsession

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

// BindingStore t_issue_acp_sessions 绑定锚点的读写（会话域唯一 DB 依赖点）。DB 是锚点
// SSOT，运行时注册表（Manager.entries）是投影；写法对齐 bot 域 ConversationStore：
// GetOrCreate 受理建行、锚点回写、失败清锚、无内存副本。
type BindingStore struct {
	DB *gorm.DB
}

// GetOrCreate 取绑定行，不存在则建（acp_session_id 空 = 无活跃会话）。
func (s *BindingStore) GetOrCreate(ctx context.Context, issueID string) (*model.IssueAcpSession, error) {
	q := query.Use(s.DB)
	row, err := q.IssueAcpSession.WithContext(ctx).Where(
		q.IssueAcpSession.IssueID.Eq(issueID),
	).First()
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	row = &model.IssueAcpSession{IssueID: issueID}
	if err := q.IssueAcpSession.WithContext(ctx).Create(row); err != nil {
		return nil, err
	}
	return row, nil
}

// SaveReady 会话就绪落锚：记 agentCode + acpSessionId 并清 last_error（新锚生效即旧错失效）。
func (s *BindingStore) SaveReady(ctx context.Context, issueID, agentCode, acpSessionID string) error {
	q := query.Use(s.DB)
	_, err := q.IssueAcpSession.WithContext(ctx).Where(
		q.IssueAcpSession.IssueID.Eq(issueID),
	).UpdateSimple(
		q.IssueAcpSession.AgentCode.Value(enums.AgentCode(agentCode)),
		q.IssueAcpSession.AcpSessionID.Value(acpSessionID),
		q.IssueAcpSession.LastError.Value(""),
		q.IssueAcpSession.UpdatedAt.Value(time.Now()),
	)
	return err
}

// SaveError 失败/意外死亡落因并清锚（agentCode 记创建会话时的实际取值；last_error 跨
// 重启可见，对齐 t_im_bots.last_error）。update-only：绑定行不存在时 no-op，不复活已被
// Discard 级联删除的行。
func (s *BindingStore) SaveError(ctx context.Context, issueID, agentCode, reason string) error {
	q := query.Use(s.DB)
	_, err := q.IssueAcpSession.WithContext(ctx).Where(
		q.IssueAcpSession.IssueID.Eq(issueID),
	).UpdateSimple(
		q.IssueAcpSession.AgentCode.Value(enums.AgentCode(agentCode)),
		q.IssueAcpSession.AcpSessionID.Value(""),
		q.IssueAcpSession.LastError.Value(reason),
		q.IssueAcpSession.UpdatedAt.Value(time.Now()),
	)
	return err
}

// Delete 删除绑定行（issue 删除级联；不存在为 no-op）。
func (s *BindingStore) Delete(ctx context.Context, issueID string) error {
	q := query.Use(s.DB)
	_, err := q.IssueAcpSession.WithContext(ctx).Where(
		q.IssueAcpSession.IssueID.Eq(issueID),
	).Delete()
	return err
}

// SweepSessionIDs 启动清扫：sidecar 重启后全部运行时锚失效（无 resume 语义，P3 同向），
// 统一清零。绑定行保留（issue 配置连续性），仅锚与错误证据归位。恒真条件（issue_id
// NOT NULL）满足 gorm 全表 UPDATE 的 where 强制。
func (s *BindingStore) SweepSessionIDs(ctx context.Context) error {
	q := query.Use(s.DB)
	_, err := q.IssueAcpSession.WithContext(ctx).Where(
		q.IssueAcpSession.IssueID.Neq(""),
	).UpdateSimple(
		q.IssueAcpSession.AcpSessionID.Value(""),
		q.IssueAcpSession.UpdatedAt.Value(time.Now()),
	)
	return err
}
