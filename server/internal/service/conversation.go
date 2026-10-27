package service

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
)

/*
 * Conversation 对应 /api/conversation 命名空间下的业务逻辑。会话第一性实体（唯一会话源）：
 * 仅存元数据，消息内容直读 claude 本地 jsonl；锚语义（预置/二择/绝对一致）见 EnsureConversation
 * 同款约定。mode 为必填入参（前端编排按归属实体 mergeLaunchSettings 算好）。
 */
type Conversation struct {
	apis.Service
}

// GetList 按归属实体查会话（扁平全量），created_at 升序。
func (svc Conversation) GetList(req *types.ConversationGetListRequest) ([]*model.Conversation, error) {
	q := query.Use(svc.Orm)
	return q.Conversation.WithContext(svc.Context).
		Where(q.Conversation.OwnerType.Eq(req.OwnerType), q.Conversation.OwnerID.Eq(req.OwnerID)).
		Order(q.Conversation.CreatedAt.Asc(), q.Conversation.ID.Asc()).
		Find()
}

// GetInfo 按 id 返回单个会话。
func (svc Conversation) GetInfo(req *types.ConversationGetInfoRequest) (*model.Conversation, error) {
	q := query.Use(svc.Orm)
	conv, err := q.Conversation.WithContext(svc.Context).Where(q.Conversation.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("会话不存在")
		}
		return nil, err
	}
	return conv, nil
}

// Create 新建会话：校验归属实体存在；title 空默认「新会话」；parentId 空 = 主会话。
// 建行即预置 claude 会话锚（驱动注入 --session-id / ACP _meta，与 bot 域建行同规则）。
func (svc Conversation) Create(req *types.ConversationCreateRequest) (*model.Conversation, error) {
	if err := svc.ensureOwnerExists(req.OwnerType, req.OwnerID); err != nil {
		return nil, err
	}
	sessionID, status := model.NewPresetClaudeSession()
	conv := &model.Conversation{
		ID:                  uuid.Must(uuid.NewV7()).String(),
		OwnerType:           req.OwnerType,
		OwnerID:             req.OwnerID,
		Title:               req.Title,
		Mode:                req.Mode,
		ParentID:            req.ParentID,
		ClaudeSessionID:     sessionID,
		ClaudeSessionStatus: status,
	}
	if conv.Title == "" {
		conv.Title = "新会话"
	}
	q := query.Use(svc.Orm)
	if err := q.Conversation.WithContext(svc.Context).Create(conv); err != nil {
		return nil, err
	}
	return conv, nil
}

// Update 更新会话标题。
func (svc Conversation) Update(req *types.ConversationUpdateRequest) (*model.Conversation, error) {
	q := query.Use(svc.Orm)
	cq := q.Conversation.WithContext(svc.Context)
	conv, err := cq.Where(q.Conversation.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("会话不存在")
		}
		return nil, err
	}
	conv.Title = req.Title
	if err := cq.Save(conv); err != nil {
		return nil, err
	}
	return conv, nil
}

/*
 * SetClaudeSessionID 终端捕获回写锚（持续同步语义，绝对一致铁律）：观测值变化即覆写并置
 * active——预置 id（首启 --session-id 后回捕同值天然幂等）与终端内 id 轮换（/clear 新会话）
 * 都以实际观测值为准。
 */
func (svc Conversation) SetClaudeSessionID(req *types.ConversationSetClaudeSessionIdRequest) (*model.Conversation, error) {
	q := query.Use(svc.Orm)
	_, err := q.Conversation.WithContext(svc.Context).
		Where(q.Conversation.ID.Eq(req.ID)).
		UpdateSimple(
			q.Conversation.ClaudeSessionID.Value(req.ClaudeSessionID),
			q.Conversation.ClaudeSessionStatus.Value(enums.CLAUDE_SESSION_STATUS_ACTIVE),
			q.Conversation.UpdatedAt.Value(time.Now()),
		)
	if err != nil {
		return nil, err
	}
	return svc.GetInfo(&types.ConversationGetInfoRequest{ID: req.ID})
}

/*
 * Delete 物理删除会话行（无 DB 外键）。消息内容在 claude 本地 jsonl，不随行删除；
 * bot 会话行的 conversation_id 悬挂引用由路由回落自愈收敛（下条消息重新定位/创建）。
 */
func (svc Conversation) Delete(req *types.ConversationDeleteRequest) error {
	q := query.Use(svc.Orm)
	res, err := q.Conversation.WithContext(svc.Context).Where(q.Conversation.ID.Eq(req.ID)).Delete()
	if err != nil {
		return err
	}
	if res.RowsAffected == 0 {
		return errors.New("会话不存在")
	}
	return nil
}

// ensureOwnerExists 归属实体存在性校验（workspace → t_workspaces / task → t_project_tasks）。
func (svc Conversation) ensureOwnerExists(ownerType enums.ConversationOwnerType, ownerID string) error {
	q := query.Use(svc.Orm)
	var err error
	switch ownerType {
	case enums.OWNER_TYPE_WORKSPACE:
		_, err = q.Workspace.WithContext(svc.Context).Where(q.Workspace.ID.Eq(ownerID)).First()
	case enums.OWNER_TYPE_TASK:
		_, err = q.ProjectTask.WithContext(svc.Context).Where(q.ProjectTask.ID.Eq(ownerID)).First()
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("归属实体不存在")
	}
	return err
}
