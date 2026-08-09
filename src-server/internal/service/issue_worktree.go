package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/enums"
	"we-claude-terminal/go-server/internal/dal/model"
	"we-claude-terminal/go-server/internal/dal/query"
	"we-claude-terminal/go-server/internal/dal/types"
	"we-claude-terminal/go-server/internal/gitutil"
)

// IssueWorktree 对应 /api/tracker/issueWorktree 命名空间下的业务逻辑（issue 开发流程 worktree 元数据）。
// P1 桩（Module G）：createWorktree 派生假 worktree 路径写记录（不真调 git worktree add）；真实现见 worktree_term.md §6。
type IssueWorktree struct {
	apis.Service
}

// CreateWorktree 为 issue 创建 worktree 记录：派生 per-workspace 真实落盘路径（仍不真调 git worktree add，
// 真实建目录见任务 1.3）。幂等 + 重入安全：按 worktreeId（UNIQUE）查——已存在（active 或 removed）则
// 重置为 active + 更新字段（支持清理后用同一分支重启），避免 UNIQUE 冲突；不存在则创建。
//
// 路径派生（docs/worktree_lifecycle.md §4.3，per-workspace 改造）：
//
//	<workspace.worktreeRoot>/<repoName>/workspace_{wid}-project_{pid}-issue_{iid}
//
// repoName 从 local_repository.remote_url 的 /xxx.git 末段解析；remote_url 为空回退 filepath.Base(local_dir)。
// workspace.worktreeRoot 为空（用户未配）报错，要求先在工作空间设置配置 worktree 存放目录。
// 成功后由前端推进 issue.stateId 到首个开发步骤（developing）。
func (svc IssueWorktree) CreateWorktree(req *types.IssueWorktreeCreateWorktreeRequest) (*types.IssueWorktreeResponseData, error) {
	q := query.Use(svc.Orm)

	// 1) 查 issue：拿 workspaceId/projectId（issue 直接带两字段，无需经 project 中转）。
	issue, err := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ID.Eq(req.IssueID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("事项不存在")
		}
		return nil, err
	}

	// 2) 查 workspace 拿 worktreeRoot（per-workspace 配置，为空报错要求配置）。
	ws, err := q.Workspace.WithContext(svc.Context).Where(q.Workspace.ID.Eq(issue.WorkspaceID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("工作空间不存在")
		}
		return nil, err
	}
	if strings.TrimSpace(ws.WorktreeRoot) == "" {
		return nil, errors.New("请先在工作空间设置中配置 worktree 存放目录")
	}

	// 3) 查 local_repository 拿 remote_url 解析 repoName；remote_url 为空回退本地目录名。
	repo, err := q.LocalRepository.WithContext(svc.Context).Where(q.LocalRepository.ID.Eq(req.LocalRepositoryID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("仓库不存在")
		}
		return nil, err
	}
	repoName := gitutil.RepoNameFromRemoteURL(repo.RemoteURL)
	if repoName == "" {
		repoName = filepath.Base(repo.LocalDir) // remote_url 缺失时用本地目录名兜底
	}

	// 4) 派生落盘路径与跨端共享键（§4.1 worktreeId = repoId::absPath）。
	worktreePath := filepath.Join(
		strings.TrimSpace(ws.WorktreeRoot),
		repoName,
		fmt.Sprintf("workspace_%d-project_%d-issue_%d", ws.ID, issue.ProjectID, req.IssueID),
	)
	worktreeID := fmt.Sprintf("%d::%s", req.LocalRepositoryID, worktreePath)

	var wt *model.IssueWorktree
	err = svc.Orm.Transaction(func(tx *gorm.DB) error {
		q := query.Use(tx)
		wq := q.IssueWorktree.WithContext(svc.Context)
		// 按 worktreeId 查（UNIQUE）：已存在则重置 active + 更新字段，避免 UNIQUE 冲突。
		found, e := wq.Where(q.IssueWorktree.WorktreeID.Eq(worktreeID)).First()
		if e == nil {
			found.Status = enums.ISSUE_WORKTREE_STATUS_ACTIVE
			found.WorktreePath = worktreePath
			found.WorktreeBranch = req.WorktreeBranch
			found.BaseBranch = req.BaseBranch
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
			WorktreeBranch:    req.WorktreeBranch,
			BaseBranch:        req.BaseBranch,
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
