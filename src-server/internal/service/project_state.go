package service

import (
	"errors"
	"fmt"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/enums"
	"we-claude-terminal/go-server/internal/dal/model"
	"we-claude-terminal/go-server/internal/dal/query"
	"we-claude-terminal/go-server/internal/dal/types"
)

// ProjectState 对应 /api/tracker/projectState 命名空间下的业务逻辑。
// 嵌入 apis.Service 获得由 controller 灌入的 Context/Orm/Logger；方法只收 req、用 svc.Orm、返原始 model。
//
// 状态为引用模型（docs/issue.md §3）：数据行只存 (state_group_code, state_code)，展示元数据由目录提供。
// 状态管理不再有独立 CRUD/reorder 接口——随项目 create/update 全量提交，由 ApplyStates 统一处理。
type ProjectState struct {
	apis.Service
}

// GetList 返回某 project 下全部状态（软删自动过滤），按 sort_order 升序、id 升序。
func (svc ProjectState) GetList(req *types.ProjectStateGetListRequest) ([]*model.ProjectState, error) {
	q := query.Use(svc.Orm)
	return q.ProjectState.WithContext(svc.Context).
		Where(q.ProjectState.ProjectID.Eq(req.ProjectID)).
		Order(q.ProjectState.SortOrder.Asc(), q.ProjectState.ID.Asc()).
		Find()
}

// GetCatalog 返回固定的状态目录常量（第 1+2 层）：分组展示元数据 + 全部子状态定义。
// 全局唯一、与项目无关；前端用它渲染配置模块、状态徽章、步骤条、看板列头。
func (svc ProjectState) GetCatalog() (*types.CatalogResponse, error) {
	return &types.CatalogResponse{
		Groups: enums.StateGroupCatalog,
		States: enums.StateCatalog,
	}, nil
}

// ApplyStates 全量替换某 project 的状态：硬删该 project 全部 state 行 → 校验入参 → 批量插入。
// 用 svc.Orm——调用方（project create/update）在事务内把 svc.Orm 覆写为 tx 以纳入同一事务。
// 校验：每条 (stateGroupCode, stateCode) 命中 StateCatalog 且不重复；每个 group ≥1 项；is_default=Y 恰好一条。
// 返回 is_default 那条的 id，供调用方回填 project.default_state_id。
func (svc ProjectState) ApplyStates(projectID, workspaceID int, items []types.ProjectStateItem) (int, error) {
	// 1) 校验。
	if len(items) == 0 {
		return 0, errors.New("状态列表不能为空")
	}
	seen := make(map[string]struct{}, len(items))
	byGroup := make(map[enums.StateGroup]int, len(enums.StateGroupCatalog))
	defaultCnt := 0
	for _, it := range items {
		if _, ok := enums.FindStateMeta(it.StateGroupCode, it.StateCode); !ok {
			return 0, fmt.Errorf("非法状态：%s/%s 不在目录内", it.StateGroupCode, it.StateCode)
		}
		key := string(it.StateGroupCode) + "|" + it.StateCode
		if _, dup := seen[key]; dup {
			return 0, fmt.Errorf("重复状态：%s/%s", it.StateGroupCode, it.StateCode)
		}
		seen[key] = struct{}{}
		byGroup[it.StateGroupCode]++
		if it.IsDefault == enums.YES_NO_Y {
			defaultCnt++
		}
	}
	if defaultCnt != 1 {
		return 0, errors.New("须恰好指定一个默认状态")
	}
	for _, g := range enums.StateGroupCatalog {
		if byGroup[g.Code] == 0 {
			return 0, fmt.Errorf("分组「%s」至少需 1 个状态", g.Name)
		}
	}

	q := query.Use(svc.Orm)
	wq := q.ProjectState.WithContext(svc.Context)

	// 2) 硬删该 project 全部 state 行（Unscoped：避免软删行占用全局唯一索引 udx_project_states_proj_group_state）。
	if _, e := wq.Unscoped().Where(q.ProjectState.ProjectID.Eq(projectID)).Delete(); e != nil {
		return 0, e
	}

	// 3) 批量插入。
	states := make([]*model.ProjectState, 0, len(items))
	for _, it := range items {
		states = append(states, &model.ProjectState{
			ProjectID:      projectID,
			WorkspaceID:    workspaceID,
			StateGroupCode: it.StateGroupCode,
			StateCode:      it.StateCode,
			SortOrder:      it.SortOrder,
			IsDefault:      it.IsDefault,
		})
	}
	if e := wq.Create(states...); e != nil {
		return 0, e
	}

	// 4) 取 is_default 那条的 id（defaultCnt==1 已校验，必然命中）。
	for _, s := range states {
		if s.IsDefault == enums.YES_NO_Y {
			return s.ID, nil
		}
	}
	return 0, nil
}
