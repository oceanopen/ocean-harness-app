package service

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"ocean-harness/src-server/internal/apis"
	"ocean-harness/src-server/internal/dal/model"
	"ocean-harness/src-server/internal/dal/query"
	"ocean-harness/src-server/internal/dal/types"
)

// Project 对应 /api/tracker/project 命名空间下的业务逻辑。
// 嵌入 apis.Service 获得由 controller 灌入的 Context/Orm/Logger；方法只收 req、用 svc.Orm、返响应 DTO。
type Project struct {
	apis.Service
}

// GetList 返回某 workspace 下全部 project，按 id 倒序（新建在前）。
// Preload 关联仓库中间表，装配为 localRepositoryIds 随项目一起返回（前端编辑回显/issue 仓库下拉直接用）。
func (svc Project) GetList(req *types.ProjectGetListRequest) ([]*types.ProjectResponseData, error) {
	q := query.Use(svc.Orm)
	projects, err := q.WorkspaceProject.WithContext(svc.Context).
		Where(q.WorkspaceProject.WorkspaceID.Eq(req.WorkspaceID)).
		Order(q.WorkspaceProject.ID.Desc()).
		Preload(q.WorkspaceProject.ProjectLocalRepositoryList).
		Find()
	if err != nil {
		return nil, err
	}
	return svc.assembleWithRepos(projects), nil
}

// GetInfo 按 id 返回单个 project（含关联仓库 ids）。
func (svc Project) GetInfo(req *types.ProjectGetInfoRequest) (*types.ProjectResponseData, error) {
	q := query.Use(svc.Orm)
	p, err := q.WorkspaceProject.WithContext(svc.Context).
		Where(q.WorkspaceProject.ID.Eq(req.ID)).
		Preload(q.WorkspaceProject.ProjectLocalRepositoryList).
		First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("项目不存在")
		}
		return nil, err
	}
	list := svc.assembleWithRepos([]*model.WorkspaceProject{p})
	return list[0], nil
}

// Create 新建 project（允许重名、无业务唯一键，普通插入）。
// 同一事务内：插入 project → 全量写入关联仓库。返回响应含本批写入的 localRepositoryIds。
func (svc Project) Create(req *types.ProjectCreateRequest) (*types.ProjectResponseData, error) {
	created := &model.WorkspaceProject{}
	var repoIDs []int
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

		// 2) 全量写入关联仓库（随项目信息一起保存），返回有效 id 列表。
		var e error
		repoIDs, e = svc.replaceAssociatedRepositories(tx, created.ID, req.LocalRepositoryIDs)
		return e
	})
	if err != nil {
		return nil, err
	}
	return &types.ProjectResponseData{WorkspaceProject: created, LocalRepositoryIDs: repoIDs}, nil
}

// Update 更新 project 的 name/description/emoji（不动 workspaceId）+ 全量覆盖关联仓库。
// 关联采用「先全量删后全量插」策略：不做 diff，前端只传最终列表。
// 被解绑的仓库同步硬删该项目 issue 关联表中的对应记录（与仓库删除级联口径一致），
// 否则 issue 编辑保存时回显悬挂关联会被 validateIssueRepoList 拒绝（改任何字段都无法保存）。
func (svc Project) Update(req *types.ProjectUpdateRequest) (*types.ProjectResponseData, error) {
	var p *model.WorkspaceProject
	var repoIDs []int
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
		// 全量覆盖前先记旧关联集合，用于解绑 diff。
		oldLinks, e := q.ProjectLocalRepository.WithContext(svc.Context).
			Where(q.ProjectLocalRepository.WorkspaceProjectID.Eq(req.ID)).Find()
		if e != nil {
			return e
		}
		oldSet := make(map[int]struct{}, len(oldLinks))
		for _, l := range oldLinks {
			oldSet[l.LocalRepositoryID] = struct{}{}
		}
		// 全量覆盖关联仓库（无 diff）。
		repoIDs, e = svc.replaceAssociatedRepositories(tx, req.ID, req.LocalRepositoryIDs)
		if e != nil {
			return e
		}
		// 级联清理：被解绑仓库（旧有且新无）从该项目 issue 的关联表 t_issue_local_repositories 中硬删，
		// 否则 issue 编辑保存时回显悬挂关联会被 validateIssueRepoList 拒绝（改任何字段都无法保存）。
		newSet := repoIDsToSet(req.LocalRepositoryIDs)
		unbound := make([]int, 0, len(oldSet))
		for id := range oldSet {
			if _, keep := newSet[id]; !keep {
				unbound = append(unbound, id)
			}
		}
		if len(unbound) == 0 {
			return nil
		}
		// 该项目的 issue，按解绑仓库删其关联记录。
		projIssues, e := q.ProjectIssue.WithContext(svc.Context).
			Where(q.ProjectIssue.ProjectID.Eq(req.ID)).Find()
		if e != nil {
			return e
		}
		if len(projIssues) == 0 {
			return nil
		}
		issueIDs := make([]string, 0, len(projIssues))
		for _, i := range projIssues {
			issueIDs = append(issueIDs, i.ID)
		}
		_, e = q.IssueLocalRepository.WithContext(svc.Context).
			Where(q.IssueLocalRepository.IssueID.In(issueIDs...)).
			Where(q.IssueLocalRepository.LocalRepositoryID.In(unbound...)).Delete()
		return e
	})
	if err != nil {
		return nil, err
	}
	return &types.ProjectResponseData{WorkspaceProject: p, LocalRepositoryIDs: repoIDs}, nil
}

// repoIDsToSet 把请求里的仓库 id 列表转为集合（去重 + 过滤 <=0），用于新旧关联 diff。
func repoIDsToSet(repoIDs []int) map[int]struct{} {
	set := make(map[int]struct{}, len(repoIDs))
	for _, rid := range repoIDs {
		if rid > 0 {
			set[rid] = struct{}{}
		}
	}
	return set
}

// Delete 物理删除 project（无 DB 外键），事务内级联清理其下全部数据（deleteProjectCascade）。
func (svc Project) Delete(req *types.ProjectDeleteRequest) error {
	return svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		// 1) 确认 project 存在。
		if _, e := q.WorkspaceProject.WithContext(svc.Context).
			Where(q.WorkspaceProject.ID.Eq(req.ID)).First(); e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("项目不存在")
			}
			return e
		}
		// 2) 级联删除（project 本体 + issue + 关联表 + 中间表）。
		return deleteProjectCascade(svc.Context, tx, req.ID)
	})
}

// deleteProjectCascade 物理删除 project 及其全部下挂数据（无 DB 外键，service 层手动级联）：
// project 本体 → 其下 issue → 这些 issue 的 t_issue_labels / t_issue_local_repositories 关联
// → 项目↔仓库中间表 t_project_local_repositories。ctx 为调用方 service 的 Context；orm 传 tx 复用事务。
// 供 Project.Delete 与 Workspace.Delete（级联删其下 project）共用。
func deleteProjectCascade(ctx context.Context, orm *gorm.DB, projectID int) error {
	q := query.Use(orm)
	// 1) 删 project 本体。
	if _, e := q.WorkspaceProject.WithContext(ctx).
		Where(q.WorkspaceProject.ID.Eq(projectID)).Delete(); e != nil {
		return e
	}
	// 2) 查其下 issue，删 issue 本体 + 两种关联。
	issues, e := q.ProjectIssue.WithContext(ctx).
		Where(q.ProjectIssue.ProjectID.Eq(projectID)).Find()
	if e != nil {
		return e
	}
	if len(issues) > 0 {
		issueIDs := make([]string, 0, len(issues))
		for _, i := range issues {
			issueIDs = append(issueIDs, i.ID)
		}
		if _, e := q.ProjectIssue.WithContext(ctx).
			Where(q.ProjectIssue.ID.In(issueIDs...)).Delete(); e != nil {
			return e
		}
		if _, e := q.IssueLabel.WithContext(ctx).
			Where(q.IssueLabel.IssueID.In(issueIDs...)).Delete(); e != nil {
			return e
		}
		if _, e := q.IssueLocalRepository.WithContext(ctx).
			Where(q.IssueLocalRepository.IssueID.In(issueIDs...)).Delete(); e != nil {
			return e
		}
	}
	// 3) 删项目↔仓库中间表记录。
	_, e = q.ProjectLocalRepository.WithContext(ctx).
		Where(q.ProjectLocalRepository.WorkspaceProjectID.Eq(projectID)).Delete()
	return e
}

// replaceAssociatedRepositories 全量替换项目的关联仓库：先删该项目全部中间表记录，再插入 repoIDs。
// 约定"无 diff、全量覆盖"——create/update 随项目信息一起保存关联，前端只传最终列表。orm 传 tx 复用事务。
// repoIDs 内部去重 + 过滤 <=0（防唯一索引冲突）；空切片=清空全部关联。返回去重后的有效 id 列表。
func (svc Project) replaceAssociatedRepositories(orm *gorm.DB, projectID int, repoIDs []int) ([]int, error) {
	q := query.Use(orm)
	jq := q.ProjectLocalRepository.WithContext(svc.Context)
	// 1) 先全量删除该项目现有关联。
	if _, e := jq.Where(q.ProjectLocalRepository.WorkspaceProjectID.Eq(projectID)).Delete(); e != nil {
		return nil, e
	}
	// 2) 去重后全量插入。
	seen := make(map[int]struct{}, len(repoIDs))
	effective := make([]int, 0, len(repoIDs))
	links := make([]*model.ProjectLocalRepository, 0, len(repoIDs))
	for _, rid := range repoIDs {
		if rid <= 0 {
			continue
		}
		if _, ok := seen[rid]; ok {
			continue
		}
		seen[rid] = struct{}{}
		effective = append(effective, rid)
		links = append(links, &model.ProjectLocalRepository{WorkspaceProjectID: projectID, LocalRepositoryID: rid})
	}
	if len(links) == 0 {
		return effective, nil
	}
	if e := jq.Create(links...); e != nil {
		return nil, e
	}
	return effective, nil
}

// assembleWithRepos 把预加载的 ProjectLocalRepositoryList 转为 LocalRepositoryIDs 装配进 ResponseData。
// 转换后清空 DO 上的原列表（omitempty），避免 JSON 同时输出 projectLocalRepositoryList 与 localRepositoryIds 冗余。
func (svc Project) assembleWithRepos(projects []*model.WorkspaceProject) []*types.ProjectResponseData {
	out := make([]*types.ProjectResponseData, 0, len(projects))
	for _, p := range projects {
		ids := make([]int, 0, len(p.ProjectLocalRepositoryList))
		for _, l := range p.ProjectLocalRepositoryList {
			ids = append(ids, l.LocalRepositoryID)
		}
		p.ProjectLocalRepositoryList = nil
		out = append(out, &types.ProjectResponseData{WorkspaceProject: p, LocalRepositoryIDs: ids})
	}
	return out
}
