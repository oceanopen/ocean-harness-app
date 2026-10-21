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
	"ocean-harness/server/internal/global"
)

/*
 * WorkspaceIssue 对应 /api/tracker/workspaceIssue 命名空间下的业务逻辑。issue 主键 id 为 uuid 字符串（与 claude session_id 同格式，Create 时生成 uuid v7（时间有序），后续作为工作空间运行任务目录标识）；state_code 为固定 5 值枚举（无 state_id）；completed_at 为 *time.Time（未完成=nil/完成=&time）。
 * 状态流转口径：各 issue 状态独立变更（无父子联动，已无子任务概念），DONE→写 completed_at，离开 DONE→清 NULL。
 */
type WorkspaceIssue struct {
	apis.Service
}

/*
 * GetList 按 workspaceId 查 issue（扁平列表），支持筛选（stateCode/keyword/typeId）+ orderBy（默认 sort_order）。
 * 批量组装每个 issue 的类型与仓库+分支列表（避免 N+1）。分组由前端对扁平列表自行分组。
 */
func (svc WorkspaceIssue) GetList(req *types.WorkspaceIssueGetListRequest) ([]*types.WorkspaceIssueResponseData, error) {
	q := query.Use(svc.Orm)
	iq := q.WorkspaceIssue.WithContext(svc.Context).Where(q.WorkspaceIssue.WorkspaceID.Eq(req.WorkspaceID))
	if req.StateCode != "" {
		iq = iq.Where(q.WorkspaceIssue.StateCode.Eq(req.StateCode))
	}
	if req.Keyword != "" {
		iq = iq.Where(q.WorkspaceIssue.Name.Like("%" + req.Keyword + "%"))
	}
	if req.TypeID > 0 {
		iq = iq.Where(q.WorkspaceIssue.TypeID.Eq(req.TypeID))
	}

	switch req.OrderBy {
	case "created_at":
		iq = iq.Order(q.WorkspaceIssue.CreatedAt.Desc())
	default: // sort_order 或空
		iq = iq.Order(q.WorkspaceIssue.SortOrder.Asc(), q.WorkspaceIssue.ID.Asc())
	}

	issues, err := iq.Find()
	if err != nil {
		return nil, err
	}
	return svc.assembleWithType(issues)
}

// GetInfo 按 id 返回单个 issue（含类型）。
func (svc WorkspaceIssue) GetInfo(req *types.WorkspaceIssueGetInfoRequest) (*types.WorkspaceIssueResponseData, error) {
	q := query.Use(svc.Orm)
	issue, err := q.WorkspaceIssue.WithContext(svc.Context).Where(q.WorkspaceIssue.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("issue 不存在")
		}
		return nil, err
	}
	list, err := svc.assembleWithType([]*model.WorkspaceIssue{issue})
	if err != nil {
		return nil, err
	}
	return list[0], nil
}

/*
 * Create 新建 issue。stateCode 空值取默认 BACKLOG；sort_order 自算（同 workspace MAX+10000，首个 10000）；
 * typeId 单值直写（0=未分类）。事务内创建 issue + 同步仓库分支关联，completed_at 默认 nil（未完成）。
 */
func (svc WorkspaceIssue) Create(req *types.WorkspaceIssueCreateRequest) (*types.WorkspaceIssueResponseData, error) {
	var created *model.WorkspaceIssue
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)

		// sort_order 自算：同 workspace MAX(sort_order)+10000，首个 10000。
		sortOrder := float64(10000)
		if last, le := q.WorkspaceIssue.WithContext(svc.Context).
			Where(q.WorkspaceIssue.WorkspaceID.Eq(req.WorkspaceID)).
			Order(q.WorkspaceIssue.SortOrder.Desc()).First(); le == nil {
			sortOrder = last.SortOrder + 10000
		} else if !errors.Is(le, gorm.ErrRecordNotFound) {
			return le
		}

		/*
		 * stateCode 空值取默认；非法值报错。DONE 时同步写 completed_at（与 applyStateTransition 口径一致，
		 * 否则留下 state=DONE 但 completed_at=nil 的记录）。
		 */
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

		// 关联仓库+分支列表：逐项校验（仓库必选、不重复、须属于工作空间关联仓库）后写关联表。
		repoBranchList, ve := svc.validateIssueRepoList(tx, req.WorkspaceID, req.RepositoryBranchList)
		if ve != nil {
			return ve
		}

		created = &model.WorkspaceIssue{
			ID:             uuid.Must(uuid.NewV7()).String(),
			WorkspaceID:    req.WorkspaceID,
			Name:           req.Name,
			Description:    req.Description,
			LaunchSettings: launchSettingsJSON(req.LaunchSettings),
			StateCode:      stateCode,
			SortOrder:      sortOrder,
			TypeID:         req.TypeID,
			StartDate:      req.StartDate,
			TargetDate:     req.TargetDate,
			CompletedAt:    completedAt,
		}
		if ce := q.WorkspaceIssue.WithContext(svc.Context).Create(created); ce != nil {
			return ce
		}
		return svc.syncIssueRepos(tx, created.ID, repoBranchList)
	})
	if err != nil {
		return nil, err
	}
	list, e := svc.assembleWithType([]*model.WorkspaceIssue{created})
	if e != nil {
		return nil, e
	}
	return list[0], nil
}

/*
 * Update 更新 issue 业务字段；检测 stateCode 变化触发 completed_at 流转：
 * 新 stateCode=DONE→写 now，否则清 nil（*time.Time 指针，Save 写 NULL）。
 * 事务内 Save + 全量同步仓库分支关联；typeId 为 *int：nil=保留原值（MCP 部分更新），0=未分类。
 */
func (svc WorkspaceIssue) Update(req *types.WorkspaceIssueUpdateRequest) (*types.WorkspaceIssueResponseData, error) {
	var issue *model.WorkspaceIssue
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		iq := q.WorkspaceIssue.WithContext(svc.Context)

		found, e := iq.Where(q.WorkspaceIssue.ID.Eq(req.ID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("issue 不存在")
			}
			return e
		}
		issue = found

		issue.Name = req.Name
		issue.Description = req.Description
		issue.LaunchSettings = launchSettingsJSON(req.LaunchSettings)
		issue.StartDate = req.StartDate
		issue.TargetDate = req.TargetDate
		// typeId 单值：nil=保留原值（MCP 部分更新），非 nil 覆写（0=未分类）。
		if req.TypeID != nil {
			issue.TypeID = *req.TypeID
		}

		// 关联仓库+分支列表：逐项校验（仓库必选、不重复、须属于工作空间关联仓库）后全量替换关联表。
		repoBranchList, ve := svc.validateIssueRepoList(tx, issue.WorkspaceID, req.RepositoryBranchList)
		if ve != nil {
			return ve
		}

		if e := svc.applyStateTransition(issue, req.StateCode); e != nil {
			return e
		}
		if e := iq.Save(issue); e != nil {
			return e
		}
		return svc.syncIssueRepos(tx, issue.ID, repoBranchList)
	})
	if err != nil {
		return nil, err
	}
	list, e := svc.assembleWithType([]*model.WorkspaceIssue{issue})
	if e != nil {
		return nil, e
	}
	return list[0], nil
}

/*
 * applyStateTransition 处理 stateCode 变化时的 completed_at 流转：新 stateCode=DONE→写 now，否则清 nil。
 * 只改内存结构体，不落库（Save 由调用方在事务内执行）。newStateCode 为空或等于当前值为 no-op；非法值返回错误。
 */
func (svc WorkspaceIssue) applyStateTransition(issue *model.WorkspaceIssue, newStateCode enums.StateCode) error {
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

/*
 * Move 看板拖拽单卡移动：写 sortOrder（前端按分数插值算好）+ stateCode 变化触发 completed_at 流转。
 * 不碰其他业务字段。
 */
func (svc WorkspaceIssue) Move(req *types.WorkspaceIssueMoveRequest) (*types.WorkspaceIssueResponseData, error) {
	var issue *model.WorkspaceIssue
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		iq := q.WorkspaceIssue.WithContext(svc.Context)

		found, e := iq.Where(q.WorkspaceIssue.ID.Eq(req.ID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("issue 不存在")
			}
			return e
		}
		issue = found

		issue.SortOrder = req.SortOrder
		if e := svc.applyStateTransition(issue, req.StateCode); e != nil {
			return e
		}
		return iq.Save(issue)
	})
	if err != nil {
		return nil, err
	}
	list, err := svc.assembleWithType([]*model.WorkspaceIssue{issue})
	if err != nil {
		return nil, err
	}
	return list[0], nil
}

/*
 * Delete 物理删除 issue（无 DB 外键），事务内级联删其 t_issue_local_repositories 关联，避免悬挂（type_id 为单值列，无独立关联可清）。
 * 事务内同步清 IM 会话绑定锚（issue 关联移除后 bot 回未绑定态）；事务成功后进程内级联 ACP 会话域：
 * 关活会话 + 删绑定锚点行（放事务外因会话收敛是进程内操作，不参与 DB 事务，服务未装配（工具进程）时跳过）。
 */
func (svc WorkspaceIssue) Delete(req *types.WorkspaceIssueDeleteRequest) error {
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		if _, e := q.WorkspaceIssue.WithContext(svc.Context).Where(q.WorkspaceIssue.ID.Eq(req.ID)).First(); e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("issue 不存在")
			}
			return e
		}
		if _, e := q.WorkspaceIssue.WithContext(svc.Context).Where(q.WorkspaceIssue.ID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		if _, e := q.IssueLocalRepository.WithContext(svc.Context).Where(q.IssueLocalRepository.IssueID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		/*
		 * 级联清绑：清该 issue 的 IM 会话绑定锚（清列保行——claude_session_id 等 headless 续聊锚
		 * 不动，绑定回未绑定态，下一回合由路由层降级为未绑定引导）。
		 */
		if _, e := q.ImBotConversation.WithContext(svc.Context).Where(
			q.ImBotConversation.BoundIssueID.Eq(req.ID),
		).UpdateSimple(
			q.ImBotConversation.BoundIssueID.Value(""),
			q.ImBotConversation.UpdatedAt.Value(time.Now()),
		); e != nil {
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
 * assembleWithType 批量组装 issue 的类型与关联仓库+分支列表（3 次查询避免 N+1）：
 * issues → t_workspace_types（按 type_id 批查）→ t_issue_local_repositories（按 issue_id 批查）
 * → 按 issue 组装（type 为单值，type_id=0 或类型已被删时为 null）。
 */
func (svc WorkspaceIssue) assembleWithType(issues []*model.WorkspaceIssue) ([]*types.WorkspaceIssueResponseData, error) {
	result := make([]*types.WorkspaceIssueResponseData, 0, len(issues))
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
		result = append(result, &types.WorkspaceIssueResponseData{
			WorkspaceIssue:       i,
			Type:                 typeMap[i.TypeID], // map 取不到为 nil（未分类/类型已删）
			RepositoryBranchList: repoBranchList,
			LaunchSettings:       types.ParseLaunchSettings(i.LaunchSettings),
		})
	}
	return result, nil
}

/*
 * syncIssueRepos 全量同步某 issue 的关联仓库+分支为 list（写 t_issue_local_repositories，事务内调用，orm 传 tx）。
 * 先删该 issue 全部关联再插入（列表经 validateIssueRepoList 已校验，量小无需 diff）。
 */
func (svc WorkspaceIssue) syncIssueRepos(orm *gorm.DB, issueID string, list []types.IssueRepositoryBranch) error {
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

/*
 * validateIssueRepoList 校验 issue 关联的仓库+分支列表：逐项 localRepositoryId > 0（前端空行未选仓库报错）、
 * 同一仓库不重复（每仓库至多一行）且属于 workspaceID 的关联仓库集合（一次批量查中间表，避免逐项 Count）。
 * 返回原列表（校验通过即原样落库）。orm 传 tx 以复用调用方事务。
 */
func (svc WorkspaceIssue) validateIssueRepoList(orm *gorm.DB, workspaceID string, list []types.IssueRepositoryBranch) ([]types.IssueRepositoryBranch, error) {
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
	links, e := q.WorkspaceLocalRepository.WithContext(svc.Context).
		Where(q.WorkspaceLocalRepository.WorkspaceID.Eq(workspaceID)).
		Where(q.WorkspaceLocalRepository.LocalRepositoryID.In(repoIDs...)).Find()
	if e != nil {
		return nil, e
	}
	linked := make(map[int]struct{}, len(links))
	for _, l := range links {
		linked[l.LocalRepositoryID] = struct{}{}
	}
	for id := range wanted {
		if _, ok := linked[id]; !ok {
			return nil, errors.New("该仓库未关联到当前工作空间")
		}
	}
	return list, nil
}
