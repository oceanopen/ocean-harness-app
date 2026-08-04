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
// 同一事务内：插入 project → 种 5 个默认状态（SeedDefaultStates）→ 回填 default_state_id → 全量写入关联仓库。
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
		if _, e = pq.Where(q.WorkspaceProject.ID.Eq(created.ID)).
			UpdateColumn(q.WorkspaceProject.DefaultStateID, created.DefaultStateID); e != nil {
			return e
		}

		// 4) 全量写入关联仓库（随项目信息一起保存）。
		return svc.replaceAssociatedRepositories(tx, created.ID, req.LocalRepositoryIDs)
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Update 更新 project 的 name/description/emoji（不动 workspaceId/defaultStateId）+ 全量覆盖关联仓库。
// 关联采用「先全量删后全量插」策略：不做 diff，前端只传最终列表（含 create 同款语义）。
func (svc Project) Update(req *types.ProjectUpdateRequest) (*model.WorkspaceProject, error) {
	var p *model.WorkspaceProject
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		pq := q.WorkspaceProject.WithContext(svc.Context)

		found, e := pq.Where(q.WorkspaceProject.ID.Eq(req.ID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("项目不存在")
			}
			return e
		}
		p = found
		p.Name = req.Name
		p.Description = req.Description
		p.Emoji = req.Emoji
		if e := pq.Save(p); e != nil {
			return e
		}
		// 全量覆盖关联仓库（无 diff）。
		return svc.replaceAssociatedRepositories(tx, req.ID, req.LocalRepositoryIDs)
	})
	if err != nil {
		return nil, err
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
		// 5) 硬删项目↔仓库中间表记录（中间表无 deleted_at）。
		if _, e := q.ProjectLocalRepository.WithContext(svc.Context).
			Where(q.ProjectLocalRepository.WorkspaceProjectID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		return nil
	})
}

// replaceAssociatedRepositories 全量替换项目的关联仓库：先删该项目全部中间表记录，再插入 repoIDs。
// 约定"无 diff、全量覆盖"——create/update 随项目信息一起保存关联，前端只传最终列表。orm 传 tx 复用事务。
// repoIDs 内部去重 + 过滤 <=0（防唯一索引冲突）；空切片=清空全部关联。
func (svc Project) replaceAssociatedRepositories(orm *gorm.DB, projectID int, repoIDs []int) error {
	q := query.Use(orm)
	jq := q.ProjectLocalRepository.WithContext(svc.Context)
	// 1) 先全量删除该项目现有关联。
	if _, e := jq.Where(q.ProjectLocalRepository.WorkspaceProjectID.Eq(projectID)).Delete(); e != nil {
		return e
	}
	// 2) 去重后全量插入。
	seen := make(map[int]struct{}, len(repoIDs))
	links := make([]*model.ProjectLocalRepository, 0, len(repoIDs))
	for _, rid := range repoIDs {
		if rid <= 0 {
			continue
		}
		if _, ok := seen[rid]; ok {
			continue
		}
		seen[rid] = struct{}{}
		links = append(links, &model.ProjectLocalRepository{WorkspaceProjectID: projectID, LocalRepositoryID: rid})
	}
	if len(links) == 0 {
		return nil
	}
	return jq.Create(links...)
}

// ListRepositories 返回项目已关联的本地仓库（按 last_commit_at 倒序、id 升序，与 localRepository 列表口径一致）。
// 读接口：供项目编辑弹窗回显 + issue 分支选择器的仓库下拉。
func (svc Project) ListRepositories(req *types.ProjectListRepositoriesRequest) ([]types.LocalRepositoryResponseData, error) {
	q := query.Use(svc.Orm)
	links, e := q.ProjectLocalRepository.WithContext(svc.Context).
		Where(q.ProjectLocalRepository.WorkspaceProjectID.Eq(req.ProjectID)).Find()
	if e != nil {
		return nil, e
	}
	if len(links) == 0 {
		return []types.LocalRepositoryResponseData{}, nil
	}
	repoIDs := make([]int, 0, len(links))
	for _, l := range links {
		repoIDs = append(repoIDs, l.LocalRepositoryID)
	}
	repos, e := q.LocalRepository.WithContext(svc.Context).
		Where(q.LocalRepository.ID.In(repoIDs...)).
		Order(q.LocalRepository.LastCommitAt.Desc()).
		Order(q.LocalRepository.ID.Asc()).
		Find()
	if e != nil {
		return nil, e
	}
	out := make([]types.LocalRepositoryResponseData, 0, len(repos))
	for _, r := range repos {
		out = append(out, types.LocalRepositoryResponseData{}.FromModel(r))
	}
	return out, nil
}
