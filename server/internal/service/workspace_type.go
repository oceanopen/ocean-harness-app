package service

import (
	"errors"

	"gorm.io/gorm"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
)

// WorkspaceType 对应 /api/tracker/workspaceType 命名空间下的业务逻辑。
// type 只挂 workspace，所有项目共享一套通用类型（无 project 级归属）；
// issue 经 t_project_issues.type_id 单值引用（每 issue 至多一个类型）。
// 嵌入 apis.Service 获得由 controller 灌入的 Context/Orm/Logger。
type WorkspaceType struct {
	apis.Service
}

// GetList 返回某 workspace 下全部 type，按 sort_order、id 升序。
func (svc WorkspaceType) GetList(req *types.WorkspaceTypeGetListRequest) ([]*model.WorkspaceType, error) {
	q := query.Use(svc.Orm)
	return q.WorkspaceType.WithContext(svc.Context).
		Where(q.WorkspaceType.WorkspaceID.Eq(req.WorkspaceID)).
		Order(q.WorkspaceType.SortOrder.Asc(), q.WorkspaceType.ID.Asc()).
		Find()
}

// GetInfo 按 id 返回单个 type。
func (svc WorkspaceType) GetInfo(req *types.WorkspaceTypeGetInfoRequest) (*model.WorkspaceType, error) {
	q := query.Use(svc.Orm)
	t, err := q.WorkspaceType.WithContext(svc.Context).Where(q.WorkspaceType.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("类型不存在")
		}
		return nil, err
	}
	return t, nil
}

// Create 新建 type（无业务唯一键，普通插入）。sort_order 自算：同 workspace MAX(sort_order)+10000，首个 10000。
func (svc WorkspaceType) Create(req *types.WorkspaceTypeCreateRequest) (*model.WorkspaceType, error) {
	q := query.Use(svc.Orm)
	tq := q.WorkspaceType.WithContext(svc.Context)

	// 取同 workspace 当前最大 sort_order（无记录则从 10000 起，按 10000 步进便于后续插值）。
	sortOrder := float64(10000)
	if last, err := tq.Where(q.WorkspaceType.WorkspaceID.Eq(req.WorkspaceID)).
		Order(q.WorkspaceType.SortOrder.Desc()).First(); err == nil {
		sortOrder = last.SortOrder + 10000
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	created := &model.WorkspaceType{
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		Color:       req.Color,
		Description: req.Description,
		SortOrder:   sortOrder,
	}
	if e := tq.Create(created); e != nil {
		return nil, e
	}
	return created, nil
}

// Update 更新 type 的 name/color/description（不动 workspaceId/sortOrder）。
func (svc WorkspaceType) Update(req *types.WorkspaceTypeUpdateRequest) (*model.WorkspaceType, error) {
	q := query.Use(svc.Orm)
	tq := q.WorkspaceType.WithContext(svc.Context)

	t, err := tq.Where(q.WorkspaceType.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("类型不存在")
		}
		return nil, err
	}
	t.Name = req.Name
	t.Color = req.Color
	t.Description = req.Description
	if e := tq.Save(t); e != nil {
		return nil, e
	}
	return t, nil
}

// Delete 物理删除 type（无 DB 外键），事务内将引用该类型的 issue.type_id 置 0（未分类），避免悬挂。
func (svc WorkspaceType) Delete(req *types.WorkspaceTypeDeleteRequest) error {
	return svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		// 1) 确认 type 存在。
		if _, e := q.WorkspaceType.WithContext(svc.Context).
			Where(q.WorkspaceType.ID.Eq(req.ID)).First(); e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("类型不存在")
			}
			return e
		}
		// 2) 物理删除 type。
		if _, e := q.WorkspaceType.WithContext(svc.Context).
			Where(q.WorkspaceType.ID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		// 3) 引用该类型的 issue 置为未分类（type_id=0）。
		if _, e := q.ProjectIssue.WithContext(svc.Context).
			Where(q.ProjectIssue.TypeID.Eq(req.ID)).
			Update(q.ProjectIssue.TypeID, 0); e != nil {
			return e
		}
		return nil
	})
}
