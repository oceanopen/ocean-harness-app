package service

import (
	"errors"

	"gorm.io/gorm"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/enums"
	"we-claude-terminal/go-server/internal/dal/model"
	"we-claude-terminal/go-server/internal/dal/query"
	"we-claude-terminal/go-server/internal/dal/types"
)

// defaultStateSpec 描述一个默认状态的不可变配置（供 SeedDefaultStates 批量建表用）。
type defaultStateSpec struct {
	Name       string
	Color      string
	Slug       string
	StateGroup enums.StateGroup
	SortOrder  float64
	IsDefault  enums.YesNo
	IsTriage   enums.YesNo
}

// DefaultStates 是新建 project 时自动种入的 5 个默认状态（与 plane 默认工作流对齐）。
// 仅 Backlog 标记 is_default（即 project.default_state_id 指向、新建 issue 初始状态）；
// sort_order 按 10000 步进，便于后续 reorder 插值。
var DefaultStates = []defaultStateSpec{
	{Name: "Backlog", Color: "#94a3b8", StateGroup: enums.STATE_GROUP_BACKLOG, SortOrder: 10000, IsDefault: enums.YES_NO_Y, IsTriage: enums.YES_NO_N},
	{Name: "Todo", Color: "#475569", StateGroup: enums.STATE_GROUP_UNSTARTED, SortOrder: 20000, IsDefault: enums.YES_NO_N, IsTriage: enums.YES_NO_N},
	{Name: "In Progress", Color: "#f59e0b", StateGroup: enums.STATE_GROUP_STARTED, SortOrder: 30000, IsDefault: enums.YES_NO_N, IsTriage: enums.YES_NO_N},
	{Name: "Done", Color: "#16a34a", StateGroup: enums.STATE_GROUP_COMPLETED, SortOrder: 40000, IsDefault: enums.YES_NO_N, IsTriage: enums.YES_NO_N},
	{Name: "Cancelled", Color: "#ef4444", StateGroup: enums.STATE_GROUP_CANCELLED, SortOrder: 50000, IsDefault: enums.YES_NO_N, IsTriage: enums.YES_NO_N},
}

// ProjectState 对应 /api/tracker/projectState 命名空间下的业务逻辑。
// 嵌入 apis.Service 获得由 controller 灌入的 Context/Orm/Logger；方法只收 req、用 svc.Orm、返原始 model。
type ProjectState struct {
	apis.Service
}

// GetList 返回某 project 下全部状态（软删自动过滤），按 sort_order 升序、id 升序。
func (svc ProjectState) GetList(req *types.ProjectStateGetListRequest) ([]*model.ProjectState, error) {
	q := query.Use(svc.Orm)
	return q.ProjectState.WithContext(svc.Context).
		Where(q.ProjectState.ProjectID.Eq(req.ProjectID)).
		Order(q.ProjectState.SortOrder.Asc(), q.ProjectState.ID.Asc()).
		Find()
}

// Create 新建状态（无业务唯一索引，普通插入）。sort_order 服务端按「同 project MAX+10000」自算；
// 若 is_default=true，事务内先清掉同 project 其他默认，保证每项目仅一个默认状态。
func (svc ProjectState) Create(req *types.ProjectStateCreateRequest) (*model.ProjectState, error) {
	created := &model.ProjectState{}
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		wq := q.ProjectState.WithContext(svc.Context)

		// is_default 互斥：置默认前先清同 project 其他默认。
		if req.IsDefault == enums.YES_NO_Y {
			if _, e := wq.Where(q.ProjectState.ProjectID.Eq(req.ProjectID)).
				Update(q.ProjectState.IsDefault, enums.YES_NO_N); e != nil {
				return e
			}
		}

		created = &model.ProjectState{
			ProjectID:   req.ProjectID,
			WorkspaceID: req.WorkspaceID,
			Name:        req.Name,
			Color:       req.Color,
			Slug:        req.Slug,
			StateGroup:  req.StateGroup,
			SortOrder:   req.SortOrder,
			IsDefault:   req.IsDefault,
			IsTriage:    req.IsTriage,
		}
		return wq.Create(created)
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Update 更新状态业务字段（不动 projectId/sortOrder）。若 is_default=true，事务内先清同 project 其他默认（排除自身）。
func (svc ProjectState) Update(req *types.ProjectStateUpdateRequest) (*model.ProjectState, error) {
	saved := &model.ProjectState{}
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		wq := q.ProjectState.WithContext(svc.Context)

		st, e := wq.Where(q.ProjectState.ID.Eq(req.ID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("状态不存在")
			}
			return e
		}

		// is_default 互斥：置默认前先清同 project 其他默认（排除自身）。
		if req.IsDefault == enums.YES_NO_Y {
			if _, e := wq.Where(q.ProjectState.ProjectID.Eq(st.ProjectID), q.ProjectState.ID.Neq(req.ID)).
				Update(q.ProjectState.IsDefault, enums.YES_NO_N); e != nil {
				return e
			}
		}

		st.Name = req.Name
		st.Color = req.Color
		st.Slug = req.Slug
		st.StateGroup = req.StateGroup
		st.IsDefault = req.IsDefault
		st.IsTriage = req.IsTriage
		if e := wq.Save(st); e != nil {
			return e
		}
		saved = st
		return nil
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// Delete 软删除状态。禁止删除项目默认状态（is_default=true），避免默认指向悬空。
func (svc ProjectState) Delete(req *types.ProjectStateDeleteRequest) error {
	q := query.Use(svc.Orm)
	wq := q.ProjectState.WithContext(svc.Context)
	st, err := wq.Where(q.ProjectState.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("状态不存在")
		}
		return err
	}
	if st.IsDefault == enums.YES_NO_Y {
		return errors.New("无法删除默认状态，请先将其他状态设为默认")
	}
	_, err = wq.Where(q.ProjectState.ID.Eq(req.ID)).Delete()
	return err
}

// Reorder 按入参 items 批量重置 sort_order（事务），均限定 projectId 防跨项目篡改。
func (svc ProjectState) Reorder(req *types.ProjectStateReorderRequest) error {
	return svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		wq := q.ProjectState.WithContext(svc.Context)
		for _, item := range req.Items {
			if _, e := wq.Where(q.ProjectState.ID.Eq(item.ID), q.ProjectState.ProjectID.Eq(req.ProjectID)).
				UpdateColumn(q.ProjectState.SortOrder, item.SortOrder); e != nil {
				return e
			}
		}
		return nil
	})
}

// SeedDefaultStates 按 DefaultStates 批量种入某 project 的默认状态。用 svc.Orm——调用方可在 project create
// 事务内把 svc.Orm 覆写为 tx 以纳入同一事务。返回创建后的状态列表，调用方取 is_default 那条的 ID 回填
// project.default_state_id。
func (svc ProjectState) SeedDefaultStates(projectID, workspaceID int) ([]*model.ProjectState, error) {
	q := query.Use(svc.Orm)
	wq := q.ProjectState.WithContext(svc.Context)
	states := make([]*model.ProjectState, 0, len(DefaultStates))
	for _, s := range DefaultStates {
		states = append(states, &model.ProjectState{
			ProjectID:   projectID,
			WorkspaceID: workspaceID,
			Name:        s.Name,
			Color:       s.Color,
			Slug:        s.Slug,
			StateGroup:  s.StateGroup,
			SortOrder:   s.SortOrder,
			IsDefault:   s.IsDefault,
			IsTriage:    s.IsTriage,
		})
	}
	if e := wq.Create(states...); e != nil {
		return nil, e
	}
	return states, nil
}
