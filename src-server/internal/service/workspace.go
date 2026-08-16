package service

import (
	"errors"

	"gorm.io/gorm"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/model"
	"we-claude-terminal/go-server/internal/dal/query"
	"we-claude-terminal/go-server/internal/dal/types"
)

// Workspace 对应 /api/tracker/workspace 命名空间下的业务逻辑。
// 嵌入 apis.Service 获得由 controller 灌入的 Context/Orm/Logger；方法只收 req、用 svc.Orm、返原始 model。
type Workspace struct {
	apis.Service
}

// GetList 返回全部 workspace，按 id 倒序（新建在前）。
func (svc Workspace) GetList(req *types.WorkspaceGetListRequest) ([]*model.Workspace, error) {
	_ = req // 当前无筛选条件，预留
	q := query.Use(svc.Orm)
	return q.Workspace.WithContext(svc.Context).Order(q.Workspace.ID.Desc()).Find()
}

// GetInfo 按 id 返回单个 workspace。
func (svc Workspace) GetInfo(req *types.WorkspaceGetInfoRequest) (*model.Workspace, error) {
	q := query.Use(svc.Orm)
	ws, err := q.Workspace.WithContext(svc.Context).Where(q.Workspace.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("工作空间不存在")
		}
		return nil, err
	}
	return ws, nil
}

// Create 创建 workspace（slug 查重后插入；物理删除时代 slug 已删即释放，无需恢复分支）。
func (svc Workspace) Create(req *types.WorkspaceCreateRequest) (*model.Workspace, error) {
	q := query.Use(svc.Orm)
	wq := q.Workspace.WithContext(svc.Context)

	if _, err := wq.Where(q.Workspace.Slug.Eq(req.Slug)).First(); err == nil {
		return nil, errors.New("记录重复：slug 已存在")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	ws := &model.Workspace{Name: req.Name, Slug: req.Slug, Description: req.Description}
	if e := wq.Create(ws); e != nil {
		return nil, e
	}
	return ws, nil
}

// Update 更新 workspace：slug 唯一性校验（排除自身）后保存。
func (svc Workspace) Update(req *types.WorkspaceUpdateRequest) (*model.Workspace, error) {
	q := query.Use(svc.Orm)
	wq := q.Workspace.WithContext(svc.Context)

	if _, err := wq.Where(q.Workspace.Slug.Eq(req.Slug), q.Workspace.ID.Neq(req.ID)).First(); err == nil {
		return nil, errors.New("记录重复：slug 已存在")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	ws, err := wq.Where(q.Workspace.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("工作空间不存在")
		}
		return nil, err
	}
	ws.Name = req.Name
	ws.Slug = req.Slug
	ws.Description = req.Description
	if e := wq.Save(ws); e != nil {
		return nil, e
	}
	return ws, nil
}

// Delete 物理删除 workspace（无 DB 外键），事务内级联清理其下全部数据，避免悬挂：
// project（deleteProjectCascade：issue + label/仓库关联 + 项目↔仓库中间表）+ 其下 label（t_workspace_labels）。
// label 属 workspace 维度（所有项目共享），其 issue 关联已随 project 级联清理，此处删 label 本体即可。
func (svc Workspace) Delete(req *types.WorkspaceDeleteRequest) error {
	return svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		if _, err := q.Workspace.WithContext(svc.Context).Where(q.Workspace.ID.Eq(req.ID)).First(); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("工作空间不存在")
			}
			return err
		}
		if _, err := q.Workspace.WithContext(svc.Context).Where(q.Workspace.ID.Eq(req.ID)).Delete(); err != nil {
			return err
		}
		// 级联删其下 label 本体（其 issue 关联随下方 project 级联清理）。
		if _, e := q.WorkspaceLabel.WithContext(svc.Context).
			Where(q.WorkspaceLabel.WorkspaceID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		// 级联删其下 project（含 project 自身的全部级联）。
		projects, pe := q.WorkspaceProject.WithContext(svc.Context).
			Where(q.WorkspaceProject.WorkspaceID.Eq(req.ID)).Find()
		if pe != nil {
			return pe
		}
		for _, p := range projects {
			if e := deleteProjectCascade(svc.Context, tx, p.ID); e != nil {
				return e
			}
		}
		return nil
	})
}
