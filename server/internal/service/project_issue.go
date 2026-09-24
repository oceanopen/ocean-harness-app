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

// ProjectIssue 对应 /api/tracker/projectIssue 命名空间下的业务逻辑。
// issue 主键 id 为 uuid 字符串（与 claude session_id 同格式，Create 时生成 uuid v7（时间有序），
// 后续作为工作空间运行任务目录标识）；state_code 为固定 5 值枚举（无 state_id/项目级状态行）；
// completed_at 为 *time.Time（未完成=nil/完成=&time）。
//
// 状态联动口径（2026-09-02 解耦定稿，T3.2 归档/取消随同收口）：父子状态独立变更——
// 父状态流转不级联子任务（原 maybeSyncChildrenState 已删，含看板拖拽/Update 全链路），
// 新建子任务默认 BACKLOG（STATE_CODE_DEFAULT）；唯一保留的单向联动是「全部子任务
// 完成 → 父自动 DONE」（maybeAutoCompleteParent）。
type ProjectIssue struct {
	apis.Service
}

// GetList 按 projectId 查 issue（扁平列表），支持筛选（stateCode/priority/keyword/typeId）+ orderBy（默认 sort_order）。
// 批量组装每个 issue 的类型与仓库+分支列表（避免 N+1）。分组由前端对扁平列表自行分组。
func (svc ProjectIssue) GetList(req *types.ProjectIssueGetListRequest) ([]*types.ProjectIssueResponseData, error) {
	q := query.Use(svc.Orm)
	iq := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ProjectID.Eq(req.ProjectID))
	if req.StateCode != "" {
		iq = iq.Where(q.ProjectIssue.StateCode.Eq(req.StateCode))
	}
	if req.Priority != "" {
		iq = iq.Where(q.ProjectIssue.Priority.Eq(req.Priority))
	}
	if req.Keyword != "" {
		iq = iq.Where(q.ProjectIssue.Name.Like("%" + req.Keyword + "%"))
	}
	if req.TypeID > 0 {
		// type_id 为 issue 表单值列，直接等值匹配（原 label 多对多关联表已废）。
		iq = iq.Where(q.ProjectIssue.TypeID.Eq(req.TypeID))
	}

	switch req.OrderBy {
	case "created_at":
		iq = iq.Order(q.ProjectIssue.CreatedAt.Desc())
	case "priority":
		// MVP：priority 按文本值排序（业务严格顺序 urgent>high>medium>low>none 由前端按权重重排）。
		iq = iq.Order(q.ProjectIssue.Priority.Asc())
	default: // sort_order 或空
		iq = iq.Order(q.ProjectIssue.SortOrder.Asc(), q.ProjectIssue.ID.Asc())
	}

	issues, err := iq.Find()
	if err != nil {
		return nil, err
	}
	return svc.assembleWithType(issues)
}

// GetInfo 按 id 返回单个 issue（含类型）。
func (svc ProjectIssue) GetInfo(req *types.ProjectIssueGetInfoRequest) (*types.ProjectIssueResponseData, error) {
	q := query.Use(svc.Orm)
	issue, err := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("issue 不存在")
		}
		return nil, err
	}
	list, err := svc.assembleWithType([]*model.ProjectIssue{issue})
	if err != nil {
		return nil, err
	}
	return list[0], nil
}

// Create 新建 issue。stateCode 空值取默认 BACKLOG；sort_order 自算（同 project MAX+10000，首个 10000）；
// priority/is_draft 空值规范为 none/N；typeId 单值直写（0=未分类）。事务内创建 issue + 同步仓库分支关联，
// completed_at 默认 nil（未完成）。
func (svc ProjectIssue) Create(req *types.ProjectIssueCreateRequest) (*types.ProjectIssueResponseData, error) {
	var created *model.ProjectIssue
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)

		// parent_id：非空时为子任务，校验父存在 + 同 project + 仅一层（父自身不能是子任务）。
		parentID := req.ParentID
		if parentID != "" {
			parent, pe := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ID.Eq(parentID)).First()
			if errors.Is(pe, gorm.ErrRecordNotFound) {
				return errors.New("父任务不存在")
			}
			if pe != nil {
				return pe
			}
			if parent.ProjectID != req.ProjectID {
				return errors.New("子任务须与父任务同项目")
			}
			if parent.ParentID != "" {
				return errors.New("仅支持一层子任务")
			}
		}

		// sort_order 自算：同 project MAX(sort_order)+10000，首个 10000。
		sortOrder := float64(10000)
		if last, le := q.ProjectIssue.WithContext(svc.Context).
			Where(q.ProjectIssue.ProjectID.Eq(req.ProjectID)).
			Order(q.ProjectIssue.SortOrder.Desc()).First(); le == nil {
			sortOrder = last.SortOrder + 10000
		} else if !errors.Is(le, gorm.ErrRecordNotFound) {
			return le
		}

		priority := req.Priority
		if priority == "" {
			priority = enums.PRIORITY_NONE
		}
		isDraft := req.IsDraft
		if isDraft == "" {
			isDraft = enums.YES_NO_N
		}

		// stateCode 空值取默认；非法值报错。DONE 时同步写 completed_at（与 applyStateTransition 口径一致，
		// 否则留下 state=DONE 但 completed_at=nil 的记录，会阻断父任务自动完成）。
		stateCode := req.StateCode
		if stateCode == "" {
			stateCode = enums.STATE_CODE_DEFAULT
		}
		if _, ok := enums.FindStateMeta(stateCode); !ok {
			return errors.New("非法状态：" + string(stateCode))
		}
		var completedAt *time.Time
		if stateCode == enums.STATE_CODE_DONE {
			now := time.Now()
			completedAt = &now
		}

		// 关联仓库+分支列表：逐项校验（仓库必选、不重复、须属于项目关联仓库）后写关联表。
		repoBranchList, ve := svc.validateIssueRepoList(tx, req.ProjectID, req.RepositoryBranchList)
		if ve != nil {
			return ve
		}

		created = &model.ProjectIssue{
			ID:          uuid.Must(uuid.NewV7()).String(),
			ProjectID:   req.ProjectID,
			WorkspaceID: req.WorkspaceID,
			Name:        req.Name,
			Description: req.Description,
			StateCode:   stateCode,
			Priority:    priority,
			SortOrder:   sortOrder,
			ParentID:    parentID,
			IsDraft:     isDraft,
			TypeID:      req.TypeID,
			StartDate:   req.StartDate,
			TargetDate:  req.TargetDate,
			CompletedAt: completedAt,
		}
		if ce := q.ProjectIssue.WithContext(svc.Context).Create(created); ce != nil {
			return ce
		}
		return svc.syncIssueRepos(tx, created.ID, repoBranchList)
	})
	if err != nil {
		return nil, err
	}
	list, e := svc.assembleWithType([]*model.ProjectIssue{created})
	if e != nil {
		return nil, e
	}
	return list[0], nil
}

// Update 更新 issue 业务字段；检测 stateCode 变化触发 completed_at 流转：
// 新 stateCode=DONE→写 now，否则清 nil（*time.Time 指针，Save 写 NULL）。
// 事务内 Save + 全量同步仓库分支关联；typeId 为 *int：nil=保留原值（MCP 部分更新），0=未分类。
func (svc ProjectIssue) Update(req *types.ProjectIssueUpdateRequest) (*types.ProjectIssueResponseData, error) {
	var issue *model.ProjectIssue
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		iq := q.ProjectIssue.WithContext(svc.Context)

		found, e := iq.Where(q.ProjectIssue.ID.Eq(req.ID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("issue 不存在")
			}
			return e
		}
		issue = found

		issue.Name = req.Name
		issue.Description = req.Description
		issue.StartDate = req.StartDate
		issue.TargetDate = req.TargetDate
		// priority/isDraft/stateCode 为 typed 枚举：空值保留原值（前端不传即不改，避免 Value() 校验空串报错）。
		if req.Priority != "" {
			issue.Priority = req.Priority
		}
		if req.IsDraft != "" {
			issue.IsDraft = req.IsDraft
		}
		// typeId 单值：nil=保留原值（MCP 部分更新），非 nil 覆写（0=未分类）。
		if req.TypeID != nil {
			issue.TypeID = *req.TypeID
		}

		// 关联仓库+分支列表：逐项校验（仓库必选、不重复、须属于项目关联仓库）后全量替换关联表。
		repoBranchList, ve := svc.validateIssueRepoList(tx, issue.ProjectID, req.RepositoryBranchList)
		if ve != nil {
			return ve
		}

		// stateCode 变化 → completed_at 流转（父子解耦：不级联子任务，见文件头注释）。
		wasCompleted := issue.CompletedAt != nil
		if e := svc.applyStateTransition(issue, req.StateCode); e != nil {
			return e
		}
		if e := iq.Save(issue); e != nil {
			return e
		}
		// 仅当本任务本次"变为完成"时触发父联动（须在自身完成态落库后，否则兄弟查询读到旧值导致永不联动）。
		if !wasCompleted && issue.CompletedAt != nil {
			if e := svc.maybeAutoCompleteParent(tx, issue); e != nil {
				return e
			}
		}
		if se := svc.syncIssueRepos(tx, issue.ID, repoBranchList); se != nil {
			return se
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	list, e := svc.assembleWithType([]*model.ProjectIssue{issue})
	if e != nil {
		return nil, e
	}
	return list[0], nil
}

// applyStateTransition 处理 stateCode 变化时的 completed_at 流转：新 stateCode=DONE→写 now，否则清 nil。
// 只改内存结构体，不落库（Save 由调用方在事务内执行）。newStateCode 为空或等于当前值为 no-op；非法值返回错误。
func (svc ProjectIssue) applyStateTransition(issue *model.ProjectIssue, newStateCode enums.StateCode) error {
	if newStateCode == "" || newStateCode == issue.StateCode {
		return nil
	}
	if _, ok := enums.FindStateMeta(newStateCode); !ok {
		return errors.New("非法状态：" + string(newStateCode))
	}
	issue.StateCode = newStateCode
	if newStateCode == enums.STATE_CODE_DONE {
		now := time.Now()
		issue.CompletedAt = &now
	} else {
		issue.CompletedAt = nil // 清空（*time.Time 指针 nil → Save 写 NULL）
	}
	return nil
}

// maybeAutoCompleteParent 状态联动：若 issue 是子任务（ParentID 非空）且其全部兄弟均已完成，
// 则把父任务流转到 DONE。仅做"全完成→完成父"单方向；父已完成/不存在时静默跳过（不阻断主流程）。orm 传 tx 以复用事务。
func (svc ProjectIssue) maybeAutoCompleteParent(orm *gorm.DB, issue *model.ProjectIssue) error {
	if issue.ParentID == "" {
		return nil
	}
	q := query.Use(orm)
	// 兄弟（含自身）：同 parent_id；任一未完成则不联动。
	siblings, e := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ParentID.Eq(issue.ParentID)).Find()
	if e != nil {
		return e
	}
	for _, s := range siblings {
		if s.CompletedAt == nil {
			return nil
		}
	}
	// 父任务。
	parent, pe := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ID.Eq(issue.ParentID)).First()
	if pe != nil {
		if errors.Is(pe, gorm.ErrRecordNotFound) {
			return nil // 父已删，忽略
		}
		return pe
	}
	if parent.CompletedAt != nil {
		return nil // 父已完成
	}
	if e := svc.applyStateTransition(parent, enums.STATE_CODE_DONE); e != nil {
		return e
	}
	return q.ProjectIssue.WithContext(svc.Context).Save(parent)
}

// Move 看板拖拽单卡移动：写 sortOrder（前端按分数插值算好）+ stateCode 变化触发 completed_at 流转。
// 子任务全完成时联动完成父（事务内：自身完成态先落库再查兄弟，避免部分失败不一致）。不碰其他业务字段。
func (svc ProjectIssue) Move(req *types.ProjectIssueMoveRequest) (*types.ProjectIssueResponseData, error) {
	var issue *model.ProjectIssue
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		iq := q.ProjectIssue.WithContext(svc.Context)

		found, e := iq.Where(q.ProjectIssue.ID.Eq(req.ID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("issue 不存在")
			}
			return e
		}
		issue = found

		issue.SortOrder = req.SortOrder
		wasCompleted := issue.CompletedAt != nil
		if e := svc.applyStateTransition(issue, req.StateCode); e != nil {
			return e
		}
		if e := iq.Save(issue); e != nil {
			return e
		}
		// 看板拖入 DONE 列：子任务全完成 → 父自动完成（须在自身完成态落库后）。
		if !wasCompleted && issue.CompletedAt != nil {
			if e := svc.maybeAutoCompleteParent(tx, issue); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	list, err := svc.assembleWithType([]*model.ProjectIssue{issue})
	if err != nil {
		return nil, err
	}
	return list[0], nil
}

// Delete 物理删除 issue（无 DB 外键），事务内级联：删其 t_issue_local_repositories
// 关联与子任务（+ 子任务的仓库分支关联），避免悬挂（type_id 为单值列，无独立关联可清）。
func (svc ProjectIssue) Delete(req *types.ProjectIssueDeleteRequest) error {
	return svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		if _, e := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ID.Eq(req.ID)).First(); e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("issue 不存在")
			}
			return e
		}
		if _, e := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		if _, e := q.IssueLocalRepository.WithContext(svc.Context).Where(q.IssueLocalRepository.IssueID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		// 级联删子任务（parent_id 指向本 issue）+ 子任务的仓库分支关联。
		children, ce := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ParentID.Eq(req.ID)).Find()
		if ce != nil {
			return ce
		}
		if len(children) > 0 {
			childIDs := make([]string, 0, len(children))
			for _, c := range children {
				childIDs = append(childIDs, c.ID)
			}
			if _, e := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ID.In(childIDs...)).Delete(); e != nil {
				return e
			}
			if _, e := q.IssueLocalRepository.WithContext(svc.Context).Where(q.IssueLocalRepository.IssueID.In(childIDs...)).Delete(); e != nil {
				return e
			}
		}
		return nil
	})
}

// assembleWithType 批量组装 issue 的类型与关联仓库+分支列表（3 次查询避免 N+1）：
// issues → t_workspace_types（按 type_id 批查）→ t_issue_local_repositories（按 issue_id 批查）
// → 按 issue 组装（type 为单值，type_id=0 或类型已被删时为 null）。
func (svc ProjectIssue) assembleWithType(issues []*model.ProjectIssue) ([]*types.ProjectIssueResponseData, error) {
	result := make([]*types.ProjectIssueResponseData, 0, len(issues))
	if len(issues) == 0 {
		return result, nil
	}
	q := query.Use(svc.Orm)

	issueIDs := make([]string, 0, len(issues))
	typeIDSet := make(map[int]struct{})
	for _, i := range issues {
		issueIDs = append(issueIDs, i.ID)
		if i.TypeID > 0 {
			typeIDSet[i.TypeID] = struct{}{}
		}
	}
	issueRepos, err := q.IssueLocalRepository.WithContext(svc.Context).Where(q.IssueLocalRepository.IssueID.In(issueIDs...)).Find()
	if err != nil {
		return nil, err
	}

	issueToRepoBranches := make(map[string][]types.IssueRepositoryBranch, len(issues))
	for _, r := range issueRepos {
		issueToRepoBranches[r.IssueID] = append(issueToRepoBranches[r.IssueID], types.IssueRepositoryBranch{
			LocalRepositoryID: r.LocalRepositoryID,
			RepositoryBranch:  r.RepositoryBranch,
		})
	}

	typeMap := make(map[int]*model.WorkspaceType, len(typeIDSet))
	if len(typeIDSet) > 0 {
		typeIDs := make([]int, 0, len(typeIDSet))
		for id := range typeIDSet {
			typeIDs = append(typeIDs, id)
		}
		ts, e := q.WorkspaceType.WithContext(svc.Context).Where(q.WorkspaceType.ID.In(typeIDs...)).Find()
		if e != nil {
			return nil, e
		}
		for _, t := range ts {
			typeMap[t.ID] = t
		}
	}

	for _, i := range issues {
		repoBranchList, ok := issueToRepoBranches[i.ID]
		if !ok {
			repoBranchList = []types.IssueRepositoryBranch{}
		}
		result = append(result, &types.ProjectIssueResponseData{
			ProjectIssue:         i,
			Type:                 typeMap[i.TypeID], // map 取不到为 nil（未分类/类型已删）
			RepositoryBranchList: repoBranchList,
		})
	}
	return result, nil
}

// syncIssueRepos 全量同步某 issue 的关联仓库+分支为 list（写 t_issue_local_repositories，事务内调用，orm 传 tx）。
// 先删该 issue 全部关联再插入（列表经 validateIssueRepoList 已校验，量小无需 diff）。
func (svc ProjectIssue) syncIssueRepos(orm *gorm.DB, issueID string, list []types.IssueRepositoryBranch) error {
	q := query.Use(orm)
	if _, e := q.IssueLocalRepository.WithContext(svc.Context).
		Where(q.IssueLocalRepository.IssueID.Eq(issueID)).Delete(); e != nil {
		return e
	}
	if len(list) == 0 {
		return nil
	}
	links := make([]*model.IssueLocalRepository, 0, len(list))
	for _, rb := range list {
		links = append(links, &model.IssueLocalRepository{
			IssueID:           issueID,
			LocalRepositoryID: rb.LocalRepositoryID,
			RepositoryBranch:  rb.RepositoryBranch,
		})
	}
	return q.IssueLocalRepository.WithContext(svc.Context).Create(links...)
}

// validateIssueRepoList 校验 issue 关联的仓库+分支列表：逐项 localRepositoryId > 0（前端空行未选仓库报错）、
// 同一仓库不重复（每仓库至多一行）且属于 projectID 的关联仓库集合（一次批量查中间表，避免逐项 Count）。
// 返回原列表（校验通过即原样落库）。orm 传 tx 以复用调用方事务。
func (svc ProjectIssue) validateIssueRepoList(orm *gorm.DB, projectID int, list []types.IssueRepositoryBranch) ([]types.IssueRepositoryBranch, error) {
	if len(list) == 0 {
		return nil, nil
	}
	q := query.Use(orm)
	projectRepoIDs := make(map[int]struct{}, len(list))
	for _, rb := range list {
		if rb.LocalRepositoryID <= 0 {
			return nil, errors.New("关联仓库不能为空")
		}
		if _, dup := projectRepoIDs[rb.LocalRepositoryID]; dup {
			return nil, errors.New("仓库重复关联")
		}
		projectRepoIDs[rb.LocalRepositoryID] = struct{}{}
	}
	repoIDs := make([]int, 0, len(projectRepoIDs))
	for id := range projectRepoIDs {
		repoIDs = append(repoIDs, id)
	}
	links, e := q.ProjectLocalRepository.WithContext(svc.Context).
		Where(q.ProjectLocalRepository.WorkspaceProjectID.Eq(projectID)).
		Where(q.ProjectLocalRepository.LocalRepositoryID.In(repoIDs...)).Find()
	if e != nil {
		return nil, e
	}
	linked := make(map[int]struct{}, len(links))
	for _, l := range links {
		linked[l.LocalRepositoryID] = struct{}{}
	}
	for id := range projectRepoIDs {
		if _, ok := linked[id]; !ok {
			return nil, errors.New("该仓库未关联到当前项目")
		}
	}
	return list, nil
}
