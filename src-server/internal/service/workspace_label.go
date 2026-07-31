package service

import (
	"errors"

	"gorm.io/gorm"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/model"
	"we-claude-terminal/go-server/internal/dal/query"
	"we-claude-terminal/go-server/internal/dal/types"
)

// WorkspaceLabel 对应 /api/tracker/workspaceLabel 命名空间下的业务逻辑。
// label 只挂 workspace，所有项目共享一套通用标签（无 project 级归属）。
// 嵌入 apis.Service 获得由 controller 灌入的 Context/Orm/Logger。
type WorkspaceLabel struct {
	apis.Service
}

// GetList 返回某 workspace 下全部 label（软删自动过滤），按 sort_order、id 升序。
func (svc WorkspaceLabel) GetList(req *types.WorkspaceLabelGetListRequest) ([]*model.WorkspaceLabel, error) {
	q := query.Use(svc.Orm)
	return q.WorkspaceLabel.WithContext(svc.Context).
		Where(q.WorkspaceLabel.WorkspaceID.Eq(req.WorkspaceID)).
		Order(q.WorkspaceLabel.SortOrder.Asc(), q.WorkspaceLabel.ID.Asc()).
		Find()
}

// GetInfo 按 id 返回单个 label。
func (svc WorkspaceLabel) GetInfo(req *types.WorkspaceLabelGetInfoRequest) (*model.WorkspaceLabel, error) {
	q := query.Use(svc.Orm)
	l, err := q.WorkspaceLabel.WithContext(svc.Context).Where(q.WorkspaceLabel.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("标签不存在")
		}
		return nil, err
	}
	return l, nil
}

// Create 新建 label（无业务唯一键，普通插入）。sort_order 自算：同 workspace MAX(sort_order)+10000，首个 10000。
func (svc WorkspaceLabel) Create(req *types.WorkspaceLabelCreateRequest) (*model.WorkspaceLabel, error) {
	q := query.Use(svc.Orm)
	lq := q.WorkspaceLabel.WithContext(svc.Context)

	// 取同 workspace 当前最大 sort_order（无记录则从 10000 起，按 10000 步进便于后续插值）。
	sortOrder := float64(10000)
	if last, err := lq.Where(q.WorkspaceLabel.WorkspaceID.Eq(req.WorkspaceID)).
		Order(q.WorkspaceLabel.SortOrder.Desc()).First(); err == nil {
		sortOrder = last.SortOrder + 10000
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	created := &model.WorkspaceLabel{
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		Color:       req.Color,
		Description: req.Description,
		SortOrder:   sortOrder,
	}
	if e := lq.Create(created); e != nil {
		return nil, e
	}
	return created, nil
}

// Update 更新 label 的 name/color/description（不动 workspaceId/sortOrder）。
func (svc WorkspaceLabel) Update(req *types.WorkspaceLabelUpdateRequest) (*model.WorkspaceLabel, error) {
	q := query.Use(svc.Orm)
	lq := q.WorkspaceLabel.WithContext(svc.Context)

	l, err := lq.Where(q.WorkspaceLabel.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("标签不存在")
		}
		return nil, err
	}
	l.Name = req.Name
	l.Color = req.Color
	l.Description = req.Description
	if e := lq.Save(l); e != nil {
		return nil, e
	}
	return l, nil
}

// Delete 软删除 label（无 DB 外键），事务内级联软删 t_issue_labels 里该 label 的全部关联，避免悬挂。
func (svc WorkspaceLabel) Delete(req *types.WorkspaceLabelDeleteRequest) error {
	return svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		// 1) 确认 label 存在（软删自动过滤已删行）。
		if _, e := q.WorkspaceLabel.WithContext(svc.Context).
			Where(q.WorkspaceLabel.ID.Eq(req.ID)).First(); e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("标签不存在")
			}
			return e
		}
		// 2) 软删 label。
		if _, e := q.WorkspaceLabel.WithContext(svc.Context).
			Where(q.WorkspaceLabel.ID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		// 3) 级联软删该 label 的 issue 关联（限定 labelId）。
		if _, e := q.IssueLabel.WithContext(svc.Context).
			Where(q.IssueLabel.LabelID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		return nil
	})
}
