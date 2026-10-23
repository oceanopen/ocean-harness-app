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
 * Conversation 对应 /api/conversation 命名空间下的业务逻辑。会话第一性实体：仅存元数据，消息内容直读 claude 本地 jsonl。
 * claude_session_id 为终端/ACP 统一锚：ACP 链路经 acpsession 域覆盖写，终端捕获经 SetClaudeSessionID 首捕获胜出。
 * mode 为必填入参（前端编排按归属实体 mergeLaunchSettings 算好）。
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
func (svc Conversation) Create(req *types.ConversationCreateRequest) (*model.Conversation, error) {
	if err := svc.ensureOwnerExists(req.OwnerType, req.OwnerID); err != nil {
		return nil, err
	}
	conv := &model.Conversation{
		ID:        uuid.Must(uuid.NewV7()).String(),
		OwnerType: req.OwnerType,
		OwnerID:   req.OwnerID,
		Title:     req.Title,
		Mode:      req.Mode,
		ParentID:  req.ParentID,
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

// SetClaudeSessionID 终端捕获回写统一锚：条件更新仅在锚为空时落值（首捕获胜出，并发败方
// 与幂等复设同走读回现行的返回契约），updated_at 随更新刷新。
func (svc Conversation) SetClaudeSessionID(req *types.ConversationSetClaudeSessionIdRequest) (*model.Conversation, error) {
	q := query.Use(svc.Orm)
	_, err := q.Conversation.WithContext(svc.Context).
		Where(q.Conversation.ID.Eq(req.ID), q.Conversation.ClaudeSessionID.Eq("")).
		UpdateSimple(
			q.Conversation.ClaudeSessionID.Value(req.ClaudeSessionID),
			q.Conversation.UpdatedAt.Value(time.Now()),
		)
	if err != nil {
		return nil, err
	}
	return svc.GetInfo(&types.ConversationGetInfoRequest{ID: req.ID})
}

/*
 * Delete 物理删除会话行（无 DB 外键）。消息内容在 claude 本地 jsonl，不随行删除；
 * bot 会话行的 conversation_id 引用清列级联随 T4.3 接入。
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
