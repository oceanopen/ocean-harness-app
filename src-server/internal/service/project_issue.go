package service

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/enums"
	"we-claude-terminal/go-server/internal/dal/model"
	"we-claude-terminal/go-server/internal/dal/query"
	"we-claude-terminal/go-server/internal/dal/types"
)

// ProjectIssue 对应 /api/tracker/projectIssue 命名空间下的业务逻辑。
// issue 主键 id 为 uuid 字符串（与 claude session_id 同格式，Create 时生成 uuid v7（时间有序），
// 后续作为工作空间运行任务目录标识）；state_code 为固定 5 值枚举（无 state_id/项目级状态行）；
// completed_at 为 *time.Time（未完成=nil/完成=&time）。
type ProjectIssue struct {
	apis.Service
}

// GetList 按 projectId 查 issue（扁平列表），支持筛选（stateCode/priority/keyword/labelId）+ orderBy（默认 sort_order）。
// 批量组装每个 issue 的 label 列表（3 次查询避免 N+1）。分组由前端对扁平列表自行分组。
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
	if req.LabelID > 0 {
		// 按关联表查 issueIds（两步，避免子查询类型不匹配）。
		issueLabels, e := q.IssueLabel.WithContext(svc.Context).Where(q.IssueLabel.LabelID.Eq(req.LabelID)).Find()
		if e != nil {
			return nil, e
		}
		if len(issueLabels) == 0 {
			return []*types.ProjectIssueResponseData{}, nil
		}
		ids := make([]string, 0, len(issueLabels))
		for _, il := range issueLabels {
			ids = append(ids, il.IssueID)
		}
		iq = iq.Where(q.ProjectIssue.ID.In(ids...))
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
	return svc.assembleWithLabels(issues)
}

// GetInfo 按 id 返回单个 issue（含 label 列表）。
func (svc ProjectIssue) GetInfo(req *types.ProjectIssueGetInfoRequest) (*types.ProjectIssueResponseData, error) {
	q := query.Use(svc.Orm)
	issue, err := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("issue 不存在")
		}
		return nil, err
	}
	list, err := svc.assembleWithLabels([]*model.ProjectIssue{issue})
	if err != nil {
		return nil, err
	}
	return list[0], nil
}

// Create 新建 issue。stateCode 空值取默认 BACKLOG；sort_order 自算（同 project MAX+10000，首个 10000）；
// priority/is_draft 空值规范为 none/N。事务内创建 issue + 全量同步 label 关联，completed_at 默认 nil（未完成）。
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

		// local_repository_id > 0 须属于当前项目关联仓库；=0 时连 branch 一并清空（无仓库则分支无意义）。
		repoID, repoBranch, ve := svc.validateIssueRepo(tx, req.ProjectID, req.LocalRepositoryID, req.RepositoryBranch)
		if ve != nil {
			return ve
		}

		created = &model.ProjectIssue{
			ID:                uuid.Must(uuid.NewV7()).String(),
			ProjectID:         req.ProjectID,
			WorkspaceID:       req.WorkspaceID,
			Name:              req.Name,
			Description:       req.Description,
			StateCode:         stateCode,
			Priority:          priority,
			SortOrder:         sortOrder,
			ParentID:          parentID,
			IsDraft:           isDraft,
			StartDate:         req.StartDate,
			TargetDate:        req.TargetDate,
			CompletedAt:       completedAt,
			LocalRepositoryID: repoID,
			RepositoryBranch:  repoBranch,
		}
		if ce := q.ProjectIssue.WithContext(svc.Context).Create(created); ce != nil {
			return ce
		}
		return svc.syncIssueLabels(tx, created.ID, req.LabelIDs)
	})
	if err != nil {
		return nil, err
	}
	list, e := svc.assembleWithLabels([]*model.ProjectIssue{created})
	if e != nil {
		return nil, e
	}
	return list[0], nil
}

// Update 更新 issue 业务字段；检测 stateCode 变化触发 completed_at 流转：
// 新 stateCode=DONE→写 now，否则清 nil（*time.Time 指针，Save 写 NULL）。
// 事务内 Save + 全量同步 label 关联（labelIds）。
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

		// local_repository_id > 0 须属于 issue 所属项目关联仓库；=0 清除关联（连 branch 一并清空）。
		repoID, repoBranch, ve := svc.validateIssueRepo(tx, issue.ProjectID, req.LocalRepositoryID, req.RepositoryBranch)
		if ve != nil {
			return ve
		}
		issue.LocalRepositoryID = repoID
		issue.RepositoryBranch = repoBranch

		// stateCode 变化 → completed_at 流转。
		oldStateCode := issue.StateCode
		wasCompleted := issue.CompletedAt != nil
		if e := svc.applyStateTransition(tx, issue, req.StateCode); e != nil {
			return e
		}
		if e := iq.Save(issue); e != nil {
			return e
		}
		// 父→子状态联动：父状态变更时子继承父新 stateCode。
		if e := svc.maybeSyncChildrenState(tx, issue, oldStateCode); e != nil {
			return e
		}
		// 仅当本任务本次"变为完成"时触发父联动（须在自身完成态落库后，否则兄弟查询读到旧值导致永不联动）。
		if !wasCompleted && issue.CompletedAt != nil {
			if e := svc.maybeAutoCompleteParent(tx, issue); e != nil {
				return e
			}
		}
		return svc.syncIssueLabels(tx, issue.ID, req.LabelIDs)
	})
	if err != nil {
		return nil, err
	}
	list, e := svc.assembleWithLabels([]*model.ProjectIssue{issue})
	if e != nil {
		return nil, e
	}
	return list[0], nil
}

// applyStateTransition 处理 stateCode 变化时的 completed_at 流转：新 stateCode=DONE→写 now，否则清 nil。
// newStateCode 为空或等于当前值为 no-op；非法值返回错误。orm 参数支持事务内复用（传 tx）或独立调用（传 svc.Orm）。
func (svc ProjectIssue) applyStateTransition(orm *gorm.DB, issue *model.ProjectIssue, newStateCode enums.StateCode) error {
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
	// 兄弟（含自身）：同 parent_id、未软删；任一未完成则不联动。
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
	if e := svc.applyStateTransition(orm, parent, enums.STATE_CODE_DONE); e != nil {
		return e
	}
	return q.ProjectIssue.WithContext(svc.Context).Save(parent)
}

// maybeSyncChildrenState 父→子状态联动：若 issue 是父（ParentID 为空串）且本次 stateCode 发生变化，
// 把其全部子任务的 stateCode 同步为父的新 stateCode（复用 applyStateTransition 口径写 completedAt）。
// orm 传 tx 以复用事务。
func (svc ProjectIssue) maybeSyncChildrenState(orm *gorm.DB, parent *model.ProjectIssue, oldStateCode enums.StateCode) error {
	if parent.ParentID != "" || oldStateCode == parent.StateCode {
		return nil // 不是父，或状态未变（不同步）
	}
	q := query.Use(orm)
	children, e := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ParentID.Eq(parent.ID)).Find()
	if e != nil {
		return e
	}
	for _, c := range children {
		if c.StateCode == parent.StateCode {
			continue // 已同状态
		}
		if e := svc.applyStateTransition(orm, c, parent.StateCode); e != nil {
			return e
		}
		if e := q.ProjectIssue.WithContext(svc.Context).Save(c); e != nil {
			return e
		}
	}
	return nil
}

// syncIssueLabels 全量同步某 issue 的 label 关联为 labelIDs（事务内调用，orm 传 tx）。
// diff 策略（保留软删记录，与原 toggleIssue 恢复式语义一致）：
//   - 现有(含软删)且在目标中 + 已软删 → 恢复（清 deleted_at）；
//   - 现有(未删)且不在目标中 → 软删；
//   - 目标中且无任何现有记录 → 插入。
//
// labelIDs 在内部去重。
func (svc ProjectIssue) syncIssueLabels(orm *gorm.DB, issueID string, labelIDs []int) error {
	q := query.Use(orm)
	ilq := q.IssueLabel.WithContext(svc.Context)

	target := make(map[int]struct{}, len(labelIDs))
	for _, id := range labelIDs {
		target[id] = struct{}{}
	}

	existings, err := ilq.Unscoped().Where(q.IssueLabel.IssueID.Eq(issueID)).Find()
	if err != nil {
		return err
	}
	existingSet := make(map[int]struct{}, len(existings))
	for _, il := range existings {
		existingSet[il.LabelID] = struct{}{}
		_, want := target[il.LabelID]
		switch {
		case want && il.DeletedAt.Valid:
			il.DeletedAt = gorm.DeletedAt{} // 恢复
			if e := ilq.Unscoped().Save(il); e != nil {
				return e
			}
		case !want && !il.DeletedAt.Valid:
			if _, e := ilq.Where(q.IssueLabel.ID.Eq(il.ID)).Delete(); e != nil { // 软删
				return e
			}
		}
	}

	for lid := range target {
		if _, ok := existingSet[lid]; !ok {
			if e := ilq.Create(&model.IssueLabel{IssueID: issueID, LabelID: lid}); e != nil {
				return e
			}
		}
	}
	return nil
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
		oldStateCode := issue.StateCode
		wasCompleted := issue.CompletedAt != nil
		if e := svc.applyStateTransition(tx, issue, req.StateCode); e != nil {
			return e
		}
		if e := iq.Save(issue); e != nil {
			return e
		}
		// 父→子状态联动：父状态变更时子继承父新 stateCode。
		if e := svc.maybeSyncChildrenState(tx, issue, oldStateCode); e != nil {
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
	list, err := svc.assembleWithLabels([]*model.ProjectIssue{issue})
	if err != nil {
		return nil, err
	}
	return list[0], nil
}

// Delete 软删除 issue（无 DB 外键），事务内级联软删其 t_issue_labels 关联与子任务（+ 子任务 label 关联），避免悬挂。
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
		if _, e := q.IssueLabel.WithContext(svc.Context).Where(q.IssueLabel.IssueID.Eq(req.ID)).Delete(); e != nil {
			return e
		}
		// 级联软删子任务（parent_id 指向本 issue）+ 子任务的 label 关联。
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
			if _, e := q.IssueLabel.WithContext(svc.Context).Where(q.IssueLabel.IssueID.In(childIDs...)).Delete(); e != nil {
				return e
			}
		}
		return nil
	})
}

// assembleWithLabels 批量组装 issue 的 label 列表（3 次查询避免 N+1）：
// issues → t_issue_labels（按 issue_id 批查）→ t_workspace_labels（按 label_id 批查）→ 按 issue 分组。
func (svc ProjectIssue) assembleWithLabels(issues []*model.ProjectIssue) ([]*types.ProjectIssueResponseData, error) {
	result := make([]*types.ProjectIssueResponseData, 0, len(issues))
	if len(issues) == 0 {
		return result, nil
	}
	q := query.Use(svc.Orm)

	issueIDs := make([]string, 0, len(issues))
	for _, i := range issues {
		issueIDs = append(issueIDs, i.ID)
	}
	issueLabels, err := q.IssueLabel.WithContext(svc.Context).Where(q.IssueLabel.IssueID.In(issueIDs...)).Find()
	if err != nil {
		return nil, err
	}

	issueToLabelIDs := make(map[string][]int, len(issues))
	labelIDSet := make(map[int]struct{})
	for _, il := range issueLabels {
		issueToLabelIDs[il.IssueID] = append(issueToLabelIDs[il.IssueID], il.LabelID)
		labelIDSet[il.LabelID] = struct{}{}
	}

	labelMap := make(map[int]*model.WorkspaceLabel, len(labelIDSet))
	if len(labelIDSet) > 0 {
		labelIDs := make([]int, 0, len(labelIDSet))
		for id := range labelIDSet {
			labelIDs = append(labelIDs, id)
		}
		labels, e := q.WorkspaceLabel.WithContext(svc.Context).Where(q.WorkspaceLabel.ID.In(labelIDs...)).Find()
		if e != nil {
			return nil, e
		}
		for _, l := range labels {
			labelMap[l.ID] = l
		}
	}

	for _, i := range issues {
		labels := make([]*model.WorkspaceLabel, 0)
		for _, lid := range issueToLabelIDs[i.ID] {
			if l, ok := labelMap[lid]; ok {
				labels = append(labels, l)
			}
		}
		result = append(result, &types.ProjectIssueResponseData{ProjectIssue: i, Labels: labels})
	}
	return result, nil
}

// validateIssueRepo 校验 issue 关联的本地仓库（localRepositoryId > 0 时）属于 projectID 的关联仓库集合。
// 返回规范后的 (localRepositoryID, repositoryBranch)：repoID<=0 时强制返回 (0,"")（无仓库则分支无意义）。
// orm 传 tx 以复用调用方事务。
func (svc ProjectIssue) validateIssueRepo(orm *gorm.DB, projectID, repoID int, branch string) (int, string, error) {
	if repoID <= 0 {
		return 0, "", nil
	}
	q := query.Use(orm)
	count, e := q.ProjectLocalRepository.WithContext(svc.Context).
		Where(q.ProjectLocalRepository.WorkspaceProjectID.Eq(projectID)).
		Where(q.ProjectLocalRepository.LocalRepositoryID.Eq(repoID)).Count()
	if e != nil {
		return 0, "", e
	}
	if count == 0 {
		return 0, "", errors.New("该仓库未关联到当前项目")
	}
	return repoID, branch, nil
}
