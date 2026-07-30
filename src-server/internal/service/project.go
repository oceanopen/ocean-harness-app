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

// Project 对应 /api/tracker/project 命名空间下的业务逻辑。
// 嵌入 apis.Service 获得由 controller 灌入的 Context/Orm/Logger；方法只收 req、用 svc.Orm、返原始 model。
type Project struct {
	apis.Service
}

// GetList 返回某 workspace 下全部 project（软删自动过滤），按 id 倒序（新建在前）。
func (svc Project) GetList(req *types.ProjectGetListRequest) ([]*model.WorkspaceProject, error) {
	q := query.Use(svc.Orm)
	return q.WorkspaceProject.WithContext(svc.Context).
		Where(q.WorkspaceProject.WorkspaceID.Eq(req.WorkspaceID)).
		Order(q.WorkspaceProject.ID.Desc()).
		Find()
}

// GetInfo 按 id 返回单个 project。
func (svc Project) GetInfo(req *types.ProjectGetInfoRequest) (*model.WorkspaceProject, error) {
	q := query.Use(svc.Orm)
	p, err := q.WorkspaceProject.WithContext(svc.Context).Where(q.WorkspaceProject.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("项目不存在")
		}
		return nil, err
	}
	return p, nil
}

// Create 新建 project（允许重名、无业务唯一键，普通插入）。
// 同一事务内：插入 project → 种 5 个默认状态（SeedDefaultStates）→ 回填 default_state_id（取 is_default 那条）。
func (svc Project) Create(req *types.ProjectCreateRequest) (*model.WorkspaceProject, error) {
	created := &model.WorkspaceProject{}
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		pq := q.WorkspaceProject.WithContext(svc.Context)

		// 1) 普通插入 project（无 upsert，允许重名）。
		created = &model.WorkspaceProject{
			WorkspaceID: req.WorkspaceID,
			Name:        req.Name,
			Description: req.Description,
			Emoji:       req.Emoji,
		}
		if e := pq.Create(created); e != nil {
			return e
		}

		// 2) 种 5 个默认状态：把 tx 透传给 ProjectState service，纳入同一事务。
		stateSvc := ProjectState{}
		svc.MakeService(&stateSvc.Service)
		stateSvc.Orm = tx
		states, e := stateSvc.SeedDefaultStates(created.ID, req.WorkspaceID)
		if e != nil {
			return e
		}

		// 3) 回填 default_state_id（取 is_default=YES_NO_Y 那条，即 Backlog）。
		for _, s := range states {
			if s.IsDefault == enums.YES_NO_Y {
				created.DefaultStateID = s.ID
				break
			}
		}
		_, e = pq.Where(q.WorkspaceProject.ID.Eq(created.ID)).
			UpdateColumn(q.WorkspaceProject.DefaultStateID, created.DefaultStateID)
		return e
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Update 更新 project 的 name/description/emoji（不动 workspaceId/defaultStateId；无业务唯一键，无需唯一性校验）。
func (svc Project) Update(req *types.ProjectUpdateRequest) (*model.WorkspaceProject, error) {
	q := query.Use(svc.Orm)
	pq := q.WorkspaceProject.WithContext(svc.Context)

	p, err := pq.Where(q.WorkspaceProject.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("项目不存在")
		}
		return nil, err
	}
	p.Name = req.Name
	p.Description = req.Description
	p.Emoji = req.Emoji
	if e := pq.Save(p); e != nil {
		return nil, e
	}
	return p, nil
}

// Delete 软删除 project（无 DB 外键），事务内级联软删其下 state 与 issue。
// issue 下挂的 t_issue_labels 不在此清理，留给 label/issue 模块统一处理（本期该表无数据）。
func (svc Project) Delete(req *types.ProjectDeleteRequest) error {
	return svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		// 1) 确认 project 存在（软删自动过滤已删行）。
		if _, e := q.WorkspaceProject.WithContext(svc.Context).
			Where(q.WorkspaceProject.ID.Eq(req.ID)).First(); e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("项目不存在")
			}
			return e
		}
		// 2) 软删 project（gorm 自动写 deleted_at）。
		if _, e := q.WorkspaceProject.WithContext(svc.Context).
			Where(q.WorkspaceProject.ID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		// 3) 级联软删其下 state（限定 projectId 防跨项目）。
		if _, e := q.ProjectState.WithContext(svc.Context).
			Where(q.ProjectState.ProjectID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		// 4) 级联软删其下 issue。
		if _, e := q.ProjectIssue.WithContext(svc.Context).
			Where(q.ProjectIssue.ProjectID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		return nil
	})
}
