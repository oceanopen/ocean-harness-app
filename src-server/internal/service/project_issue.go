package service

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/enums"
	"we-claude-terminal/go-server/internal/dal/model"
	"we-claude-terminal/go-server/internal/dal/query"
	"we-claude-terminal/go-server/internal/dal/types"
)

// ProjectIssue 对应 /api/tracker/projectIssue 命名空间下的业务逻辑。
// issue 用全局自增 id 标识（无 issue key）；completed_at 为 *time.Time（未完成=nil/完成=&time）。
type ProjectIssue struct {
	apis.Service
}

// GetList 按 projectId 查 issue（扁平列表），支持筛选（stateId/priority/keyword/labelId）+ orderBy（默认 sort_order）。
// 批量组装每个 issue 的 label 列表（3 次查询避免 N+1）。groupBy 由前端对扁平列表自行分组。
func (svc ProjectIssue) GetList(req *types.ProjectIssueGetListRequest) ([]*types.ProjectIssueResponseData, error) {
	q := query.Use(svc.Orm)
	iq := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ProjectID.Eq(req.ProjectID))
	if req.StateID > 0 {
		iq = iq.Where(q.ProjectIssue.StateID.Eq(req.StateID))
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
		ids := make([]int, 0, len(issueLabels))
		for _, il := range issueLabels {
			ids = append(ids, il.IssueID)
		}
		iq = iq.Where(q.ProjectIssue.ID.In(ids...))
	}

	switch req.OrderBy {
	case "id":
		iq = iq.Order(q.ProjectIssue.ID.Asc())
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

// Create 新建 issue。state_id 取 project.default_state_id；sort_order 自算（同 project MAX+10000，首个 10000）；
// priority/is_draft 空值规范为 none/N。completed_at 默认 nil（未完成）。
func (svc ProjectIssue) Create(req *types.ProjectIssueCreateRequest) (*types.ProjectIssueResponseData, error) {
	q := query.Use(svc.Orm)

	proj, err := q.WorkspaceProject.WithContext(svc.Context).Where(q.WorkspaceProject.ID.Eq(req.ProjectID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("项目不存在")
		}
		return nil, err
	}

	// sort_order 自算：同 project MAX(sort_order)+10000，首个 10000。
	sortOrder := float64(10000)
	if last, e := q.ProjectIssue.WithContext(svc.Context).
		Where(q.ProjectIssue.ProjectID.Eq(req.ProjectID)).
		Order(q.ProjectIssue.SortOrder.Desc()).First(); e == nil {
		sortOrder = last.SortOrder + 10000
	} else if !errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}

	priority := req.Priority
	if priority == "" {
		priority = enums.PRIORITY_NONE
	}
	isDraft := req.IsDraft
	if isDraft == "" {
		isDraft = enums.YES_NO_N
	}

	created := &model.ProjectIssue{
		ProjectID:   req.ProjectID,
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		Description: req.Description,
		StateID:     proj.DefaultStateID,
		Priority:    priority,
		SortOrder:   sortOrder,
		IsDraft:     isDraft,
		StartDate:   req.StartDate,
		TargetDate:  req.TargetDate,
	}
	if e := q.ProjectIssue.WithContext(svc.Context).Create(created); e != nil {
		return nil, e
	}
	return &types.ProjectIssueResponseData{ProjectIssue: created, Labels: []*model.WorkspaceLabel{}}, nil
}

// Update 更新 issue 业务字段；检测 stateId 变化触发 completed_at 流转：
// 新 state 的 state_group=completed→写 now，否则清 nil（*time.Time 指针，Save 写 NULL）。
func (svc ProjectIssue) Update(req *types.ProjectIssueUpdateRequest) (*types.ProjectIssueResponseData, error) {
	q := query.Use(svc.Orm)
	iq := q.ProjectIssue.WithContext(svc.Context)

	issue, err := iq.Where(q.ProjectIssue.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("issue 不存在")
		}
		return nil, err
	}

	issue.Name = req.Name
	issue.Description = req.Description
	// priority/isDraft 为 typed 枚举：空值保留原值（前端不传即不改，避免 Value() 校验空串报错）。
	if req.Priority != "" {
		issue.Priority = req.Priority
	}
	if req.IsDraft != "" {
		issue.IsDraft = req.IsDraft
	}
	issue.StartDate = req.StartDate
	issue.TargetDate = req.TargetDate

	// stateId 变化 → completed_at 流转。
	if e := svc.applyStateTransition(issue, req.StateID); e != nil {
		return nil, e
	}

	if e := iq.Save(issue); e != nil {
		return nil, e
	}
	list, err := svc.assembleWithLabels([]*model.ProjectIssue{issue})
	if err != nil {
		return nil, err
	}
	return list[0], nil
}

// applyStateTransition 处理 stateId 变化时的 completed_at 流转：新 state 的 state_group=completed→写 now，否则清 nil。
// newStateID<=0 或等于当前值为 no-op；state 不存在返回错误。供 update/move 复用，避免流转逻辑重复。
func (svc ProjectIssue) applyStateTransition(issue *model.ProjectIssue, newStateID int) error {
	if newStateID <= 0 || newStateID == issue.StateID {
		return nil
	}
	q := query.Use(svc.Orm)
	st, e := q.ProjectState.WithContext(svc.Context).Where(q.ProjectState.ID.Eq(newStateID)).First()
	if e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return errors.New("状态不存在")
		}
		return e
	}
	issue.StateID = newStateID
	if st.StateGroup == enums.STATE_GROUP_COMPLETED {
		now := time.Now()
		issue.CompletedAt = &now
	} else {
		issue.CompletedAt = nil // 清空（*time.Time 指针 nil → Save 写 NULL）
	}
	return nil
}

// Move 看板拖拽单卡移动：写 sortOrder（前端按分数插值算好）+ stateId 变化触发 completed_at 流转。
// 不碰其他业务字段（name/description/priority 等由 update 维护）。
func (svc ProjectIssue) Move(req *types.ProjectIssueMoveRequest) (*types.ProjectIssueResponseData, error) {
	q := query.Use(svc.Orm)
	iq := q.ProjectIssue.WithContext(svc.Context)

	issue, err := iq.Where(q.ProjectIssue.ID.Eq(req.ID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("issue 不存在")
		}
		return nil, err
	}

	issue.SortOrder = req.SortOrder
	if e := svc.applyStateTransition(issue, req.StateID); e != nil {
		return nil, e
	}

	if e := iq.Save(issue); e != nil {
		return nil, e
	}
	list, err := svc.assembleWithLabels([]*model.ProjectIssue{issue})
	if err != nil {
		return nil, err
	}
	return list[0], nil
}

// Delete 软删除 issue（无 DB 外键），事务内级联软删其 t_issue_labels 关联，避免悬挂。
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

	issueIDs := make([]int, 0, len(issues))
	for _, i := range issues {
		issueIDs = append(issueIDs, i.ID)
	}
	issueLabels, err := q.IssueLabel.WithContext(svc.Context).Where(q.IssueLabel.IssueID.In(issueIDs...)).Find()
	if err != nil {
		return nil, err
	}

	issueToLabelIDs := make(map[int][]int, len(issues))
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
