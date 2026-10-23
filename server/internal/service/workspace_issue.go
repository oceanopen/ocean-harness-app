package service

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/global"
)

/*
 * WorkspaceIssue 对应 /api/tracker/workspaceIssue 命名空间下的业务逻辑。任务主键 id 为 uuid 字符串
 * （与 claude session_id 同格式，Create 时生成 uuid v7（时间有序），后续作为任务运行目录标识）。
 */
type WorkspaceIssue struct {
	apis.Service
}

/*
 * GetList 按 projectId 查任务（扁平列表），支持筛选（keyword/typeId），恒按 created_at 升序。
 * 批量组装每个任务的类型与仓库+分支列表（避免 N+1）。分组由前端对扁平列表自行分组。
 */
func (svc WorkspaceIssue) GetList(req *types.WorkspaceIssueGetListRequest) ([]*types.WorkspaceIssueResponseData, error) {
	q := query.Use(svc.Orm)
	iq := q.ProjectTask.WithContext(svc.Context).Where(q.ProjectTask.ProjectID.Eq(req.ProjectID))
	if req.Keyword != "" {
		iq = iq.Where(q.ProjectTask.Name.Like("%" + req.Keyword + "%"))
	}
	if req.TypeID > 0 {
		iq = iq.Where(q.ProjectTask.TypeID.Eq(req.TypeID))
	}
	iq = iq.Order(q.ProjectTask.CreatedAt.Asc(), q.ProjectTask.ID.Asc())

	tasks, err := iq.Find()
	if err != nil {
		return nil, err
	}
	return svc.assembleWithType(tasks)
}

// GetInfo 按 id 返回单个任务（含类型）。
func (svc WorkspaceIssue) GetInfo(req *types.WorkspaceIssueGetInfoRequest) (*types.WorkspaceIssueResponseData, error) {
	q := query.Use(svc.Orm)
	task, err := q.ProjectTask.WithContext(svc.Context).Where(q.ProjectTask.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("任务不存在")
		}
		return nil, err
	}
	list, err := svc.assembleWithType([]*model.ProjectTask{task})
	if err != nil {
		return nil, err
	}
	return list[0], nil
}

/*
 * Create 新建任务：id 生成 uuid v7；typeId 单值直写（0=未分类）。
 * 事务内创建任务 + 同步仓库分支关联。
 */
func (svc WorkspaceIssue) Create(req *types.WorkspaceIssueCreateRequest) (*types.WorkspaceIssueResponseData, error) {
	var created *model.ProjectTask
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)

		// 关联仓库+分支列表：逐项校验（仓库必选、不重复、仓库存在）后写关联表。
		repoBranchList, ve := svc.validateTaskRepoList(tx, req.RepositoryBranchList)
		if ve != nil {
			return ve
		}

		created = &model.ProjectTask{
			ID:             uuid.Must(uuid.NewV7()).String(),
			ProjectID:      req.ProjectID,
			Name:           req.Name,
			Description:    req.Description,
			LaunchSettings: launchSettingsJSON(req.LaunchSettings),
			TypeID:         req.TypeID,
		}
		if ce := q.ProjectTask.WithContext(svc.Context).Create(created); ce != nil {
			return ce
		}
		return svc.syncTaskRepos(tx, created.ID, repoBranchList)
	})
	if err != nil {
		return nil, err
	}
	list, e := svc.assembleWithType([]*model.ProjectTask{created})
	if e != nil {
		return nil, e
	}
	return list[0], nil
}

/*
 * Update 更新任务业务字段；事务内 Save + 全量同步仓库分支关联；
 * typeId 为 *int：nil=保留原值（MCP 部分更新），0=未分类。
 */
func (svc WorkspaceIssue) Update(req *types.WorkspaceIssueUpdateRequest) (*types.WorkspaceIssueResponseData, error) {
	var task *model.ProjectTask
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		tq := q.ProjectTask.WithContext(svc.Context)

		found, e := tq.Where(q.ProjectTask.ID.Eq(req.ID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("任务不存在")
			}
			return e
		}
		task = found

		task.Name = req.Name
		task.Description = req.Description
		task.LaunchSettings = launchSettingsJSON(req.LaunchSettings)
		// typeId 单值：nil=保留原值（MCP 部分更新），非 nil 覆写（0=未分类）。
		if req.TypeID != nil {
			task.TypeID = *req.TypeID
		}

		// 关联仓库+分支列表：逐项校验（仓库必选、不重复、仓库存在）后全量替换关联表。
		repoBranchList, ve := svc.validateTaskRepoList(tx, req.RepositoryBranchList)
		if ve != nil {
			return ve
		}

		if e := tq.Save(task); e != nil {
			return e
		}
		return svc.syncTaskRepos(tx, task.ID, repoBranchList)
	})
	if err != nil {
		return nil, err
	}
	list, e := svc.assembleWithType([]*model.ProjectTask{task})
	if e != nil {
		return nil, e
	}
	return list[0], nil
}

/*
 * Delete 物理删除任务（无 DB 外键），事务内级联删其 t_task_local_repositories 关联，避免悬挂。
 * 事务成功后进程内级联 ACP 会话域关活会话（放事务外因会话收敛是进程内操作，
 * 不参与 DB 事务，服务未装配（工具进程）时跳过）。
 */
func (svc WorkspaceIssue) Delete(req *types.WorkspaceIssueDeleteRequest) error {
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		if _, e := q.ProjectTask.WithContext(svc.Context).Where(q.ProjectTask.ID.Eq(req.ID)).First(); e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("任务不存在")
			}
			return e
		}
		if _, e := q.ProjectTask.WithContext(svc.Context).Where(q.ProjectTask.ID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		if _, e := q.TaskLocalRepository.WithContext(svc.Context).Where(q.TaskLocalRepository.TaskID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		return err
	}
	if global.AcpSessions != nil {
		global.AcpSessions.Discard(req.ID)
	}
	return nil
}

/*
 * assembleWithType 批量组装任务的类型与关联仓库+分支列表（3 次查询避免 N+1）：
 * tasks → t_project_types（按 type_id 批查）→ t_task_local_repositories（按 task_id 批查）
 * → 按任务组装（type 为单值，type_id=0 或类型已被删时为 null）。
 */
func (svc WorkspaceIssue) assembleWithType(tasks []*model.ProjectTask) ([]*types.WorkspaceIssueResponseData, error) {
	result := make([]*types.WorkspaceIssueResponseData, 0, len(tasks))
	if len(tasks) == 0 {
		return result, nil
	}
	q := query.Use(svc.Orm)

	taskIDs := make([]string, 0, len(tasks))
	typeIDSet := make(map[int]struct{})
	for _, t := range tasks {
		taskIDs = append(taskIDs, t.ID)
		if t.TypeID > 0 {
			typeIDSet[t.TypeID] = struct{}{}
		}
	}
	taskRepos, err := q.TaskLocalRepository.WithContext(svc.Context).Where(q.TaskLocalRepository.TaskID.In(taskIDs...)).Find()
	if err != nil {
		return nil, err
	}

	taskToRepoBranches := make(map[string][]types.IssueRepositoryBranch, len(tasks))
	for _, r := range taskRepos {
		taskToRepoBranches[r.TaskID] = append(taskToRepoBranches[r.TaskID], types.IssueRepositoryBranch{
			LocalRepositoryID: r.LocalRepositoryID,
			RepositoryBranch:  r.RepositoryBranch,
		})
	}

	typeMap := make(map[int]*model.ProjectType, len(typeIDSet))
	if len(typeIDSet) > 0 {
		typeIDs := make([]int, 0, len(typeIDSet))
		for id := range typeIDSet {
			typeIDs = append(typeIDs, id)
		}
		ts, e := q.ProjectType.WithContext(svc.Context).Where(q.ProjectType.ID.In(typeIDs...)).Find()
		if e != nil {
			return nil, e
		}
		for _, t := range ts {
			typeMap[t.ID] = t
		}
	}

	for _, t := range tasks {
		repoBranchList, ok := taskToRepoBranches[t.ID]
		if !ok {
			repoBranchList = []types.IssueRepositoryBranch{}
		}
		result = append(result, &types.WorkspaceIssueResponseData{
			ProjectTask:          t,
			Type:                 typeMap[t.TypeID], // map 取不到为 nil（未分类/类型已删）
			RepositoryBranchList: repoBranchList,
			LaunchSettings:       types.ParseLaunchSettings(t.LaunchSettings),
		})
	}
	return result, nil
}

/*
 * syncTaskRepos 全量同步某任务的关联仓库+分支为 list（写 t_task_local_repositories，事务内调用，orm 传 tx）。
 * 先删该任务全部关联再插入（列表经 validateTaskRepoList 已校验，量小无需 diff）。
 */
func (svc WorkspaceIssue) syncTaskRepos(orm *gorm.DB, taskID string, list []types.IssueRepositoryBranch) error {
	q := query.Use(orm)
	if _, e := q.TaskLocalRepository.WithContext(svc.Context).
		Where(q.TaskLocalRepository.TaskID.Eq(taskID)).Delete(); e != nil {
		return e
	}
	if len(list) == 0 {
		return nil
	}
	links := make([]*model.TaskLocalRepository, 0, len(list))
	for _, rb := range list {
		links = append(links, &model.TaskLocalRepository{
			TaskID:            taskID,
			LocalRepositoryID: rb.LocalRepositoryID,
			RepositoryBranch:  rb.RepositoryBranch,
		})
	}
	return q.TaskLocalRepository.WithContext(svc.Context).Create(links...)
}

/*
 * validateTaskRepoList 校验任务关联的仓库+分支列表：逐项 localRepositoryId > 0（前端空行未选仓库报错）、
 * 同一仓库不重复（每仓库至多一行）且仓库存在（归属项目关联集合的校验随 T3.2 项目域接入恢复）。
 * 返回原列表（校验通过即原样落库）。orm 传 tx 以复用调用方事务。
 */
func (svc WorkspaceIssue) validateTaskRepoList(orm *gorm.DB, list []types.IssueRepositoryBranch) ([]types.IssueRepositoryBranch, error) {
	if len(list) == 0 {
		return nil, nil
	}
	q := query.Use(orm)
	wanted := make(map[int]struct{}, len(list))
	for _, rb := range list {
		if rb.LocalRepositoryID <= 0 {
			return nil, errors.New("关联仓库不能为空")
		}
		if _, dup := wanted[rb.LocalRepositoryID]; dup {
			return nil, errors.New("仓库重复关联")
		}
		wanted[rb.LocalRepositoryID] = struct{}{}
	}
	repoIDs := make([]int, 0, len(wanted))
	for id := range wanted {
		repoIDs = append(repoIDs, id)
	}
	repos, e := q.LocalRepository.WithContext(svc.Context).Where(q.LocalRepository.ID.In(repoIDs...)).Find()
	if e != nil {
		return nil, e
	}
	if len(repos) != len(repoIDs) {
		return nil, errors.New("关联仓库不存在")
	}
	return list, nil
}
