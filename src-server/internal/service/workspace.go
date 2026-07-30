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

// GetList 返回全部 workspace（软删自动过滤），按 id 倒序（新建在前）。
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

// Create 创建 workspace，采用「恢复式 upsert」：按 slug 含软删记录（Unscoped）查询——
//   - 未删同 slug → 报「记录重复」；
//   - 已删同 slug → 恢复：业务字段覆盖 + deleted_at 清空 + 保留 id/created_at（Save 自动刷新 updated_at）；
//   - 不存在 → 正常插入。
//
// gorm 对含 gorm.DeletedAt 的模型自动给所有查询加 WHERE deleted_at IS NULL（只看未删行）。
// .Unscoped() 关掉这个自动过滤，让查询/写入把已软删的行也包括进来。
func (svc Workspace) Create(req *types.WorkspaceCreateRequest) (*model.Workspace, error) {
	q := query.Use(svc.Orm)
	wq := q.Workspace.WithContext(svc.Context)

	existing, err := wq.Unscoped().Where(q.Workspace.Slug.Eq(req.Slug)).First()
	if err == nil {
		if existing.DeletedAt.Valid {
			existing.Name = req.Name
			existing.Description = req.Description
			existing.DeletedAt = gorm.DeletedAt{} // 清空 → deleted_at = NULL（须 Unscoped 才能改写）
			if e := wq.Unscoped().Save(existing); e != nil {
				return nil, e
			}
			return existing, nil
		}
		return nil, errors.New("记录重复：slug 已存在")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	ws := &model.Workspace{Name: req.Name, Slug: req.Slug, Description: req.Description}
	if e := wq.Create(ws); e != nil {
		return nil, e
	}
	return ws, nil
}

// Update 更新 workspace：slug 唯一性校验（排除自身、仅未删）后保存。
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

// Delete 软删除 workspace（gorm 自动写 deleted_at）。先确认存在，避免静默忽略。
func (svc Workspace) Delete(req *types.WorkspaceDeleteRequest) error {
	q := query.Use(svc.Orm)
	wq := q.Workspace.WithContext(svc.Context)
	if _, err := wq.Where(q.Workspace.ID.Eq(req.ID)).First(); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("工作空间不存在")
		}
		return err
	}
	_, err := wq.Where(q.Workspace.ID.Eq(req.ID)).Delete()
	return err
}
