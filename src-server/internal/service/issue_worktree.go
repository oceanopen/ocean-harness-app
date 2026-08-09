package service

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/enums"
	"we-claude-terminal/go-server/internal/dal/model"
	"we-claude-terminal/go-server/internal/dal/query"
	"we-claude-terminal/go-server/internal/dal/types"
)

// IssueWorktree 对应 /api/tracker/issueWorktree 命名空间下的业务逻辑（issue 开发流程 worktree 元数据）。
// P1 桩（Module G）：createWorktree 派生假 worktree 路径写记录（不真调 git worktree add）；真实现见 worktree_term.md §6。
type IssueWorktree struct {
	apis.Service
}

// worktreeRoot P1 桩根目录占位（worktree_term.md §5.3 真派生含 repoName/sanitizedName，待 P2 接 gitutil 落地）。
const worktreeRoot = "<worktree-root-placeholder>"

// CreateWorktree 为 issue 创建 worktree 记录（P1 桩：派生假路径，不真调 git）。
// 幂等 + 重入安全：按 worktreeId（UNIQUE）查——已存在（active 或 removed）则重置为 active + 更新字段
// （支持清理后用同一分支重启），避免 UNIQUE 冲突；不存在则创建。
// 成功后由前端推进 issue.stateId 到首个开发步骤（developing）。
func (svc IssueWorktree) CreateWorktree(req *types.IssueWorktreeCreateWorktreeRequest) (*types.IssueWorktreeResponseData, error) {
	worktreePath := fmt.Sprintf("%s/issue-%d-%s", worktreeRoot, req.IssueID, req.Branch)
	worktreeID := fmt.Sprintf("%d::%s", req.LocalRepositoryID, worktreePath)
	var wt *model.IssueWorktree
	err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		wq := q.IssueWorktree.WithContext(svc.Context)
		// 按 worktreeId 查（UNIQUE）：已存在则重置 active + 更新字段，避免 UNIQUE 冲突。
		found, e := wq.Where(q.IssueWorktree.WorktreeID.Eq(worktreeID)).First()
		if e == nil {
			found.Status = enums.ISSUE_WORKTREE_STATUS_ACTIVE
			found.WorktreePath = worktreePath
			found.Branch = req.Branch
			found.BaseRef = req.BaseRef
			if e := wq.Save(found); e != nil {
				return e
			}
			wt = found
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		wt = &model.IssueWorktree{
			WorktreeID:        worktreeID,
			IssueID:           req.IssueID,
			LocalRepositoryID: req.LocalRepositoryID,
			WorktreePath:      worktreePath,
			Branch:            req.Branch,
			BaseRef:           req.BaseRef,
			Status:            enums.ISSUE_WORKTREE_STATUS_ACTIVE,
		}
		return wq.Create(wt)
	})
	if err != nil {
		return nil, err
	}
	return &types.IssueWorktreeResponseData{IssueWorktree: wt}, nil
}

// RemoveWorktree 软删 worktree 记录（status=removed）。前置：前端已停 PTY（worktree_term.md §9.3）。
// P1 桩：不真删 worktree 目录（真实现调 gitutil.WorktreeRemove + prune）。
func (svc IssueWorktree) RemoveWorktree(req *types.IssueWorktreeRemoveWorktreeRequest) error {
	return svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		wq := q.IssueWorktree.WithContext(svc.Context)
		if _, e := wq.Where(q.IssueWorktree.WorktreeID.Eq(req.WorktreeID)).First(); e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("worktree 记录不存在")
			}
			return e
		}
		_, e := wq.Where(q.IssueWorktree.WorktreeID.Eq(req.WorktreeID)).
			Update(q.IssueWorktree.Status, enums.ISSUE_WORKTREE_STATUS_REMOVED)
		return e
	})
}

// GetList 按 issueId 查 active worktree 记录（前端作 worktreePath/worktreeId 的 SSOT）。
func (svc IssueWorktree) GetList(req *types.IssueWorktreeGetListRequest) ([]*types.IssueWorktreeResponseData, error) {
	q := query.Use(svc.Orm)
	list, err := q.IssueWorktree.WithContext(svc.Context).
		Where(q.IssueWorktree.IssueID.Eq(req.IssueID)).
		Where(q.IssueWorktree.Status.Eq(enums.ISSUE_WORKTREE_STATUS_ACTIVE)).
		Find()
	if err != nil {
		return nil, err
	}
	out := make([]*types.IssueWorktreeResponseData, 0, len(list))
	for _, w := range list {
		out = append(out, &types.IssueWorktreeResponseData{IssueWorktree: w})
	}
	return out, nil
}
