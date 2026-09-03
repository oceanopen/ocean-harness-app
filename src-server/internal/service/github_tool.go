package service

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"ocean-harness/src-server/internal/apis"
	"ocean-harness/src-server/internal/dal/query"
)

// GithubTool 是 github MCP 工具（T4.1）的仓库定位支撑：本地仓库 id → 远程地址与
// issue 关联基准分支。纯 DB 查询组合（无状态），GitHub API 调用在 githubapi 包
// （handler 层组装：本 service 定位仓库 → gitutil 解析 → githubapi 调用）。
type GithubTool struct {
	apis.Service
}

// GithubRepoRef 是仓库定位结果。
type GithubRepoRef struct {
	RemoteURL     string // origin 地址原文（gitutil.ParseRemoteURL 的输入）
	BaseBranch    string // issue 关联此仓库时选定的基准分支；未关联/未选定为空串
	DefaultBranch string // 仓库默认分支（BaseBranch 为空时的回退）
}

// ResolveRepo 定位本地仓库的远程信息。issueID 非空时校验 issue 已关联该仓库并返回
// 关联基准分支（create_pr 的 base 缺省推导依据）；issueID 为空串时跳过关联（list/ci
// 场景，无需 issue 上下文）。
func (svc GithubTool) ResolveRepo(issueID string, localRepositoryID int) (*GithubRepoRef, error) {
	q := query.Use(svc.Orm)
	repo, err := q.LocalRepository.WithContext(svc.Context).Where(q.LocalRepository.ID.Eq(localRepositoryID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("本地仓库不存在（id: %d）", localRepositoryID)
		}
		return nil, err
	}
	if repo.RemoteURL == "" {
		return nil, fmt.Errorf("仓库 %s 无 origin 远程地址（请在仓库管理页刷新重试）", repo.Name)
	}

	ref := &GithubRepoRef{RemoteURL: repo.RemoteURL, DefaultBranch: repo.DefaultBranch}
	if issueID == "" {
		return ref, nil
	}
	link, err := q.IssueLocalRepository.WithContext(svc.Context).
		Where(q.IssueLocalRepository.IssueID.Eq(issueID), q.IssueLocalRepository.LocalRepositoryID.Eq(localRepositoryID)).
		First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("issue 未关联该仓库（%s），无法推导基准分支；请确认 localRepositoryId 或显式传入 base", repo.Name)
		}
		return nil, err
	}
	ref.BaseBranch = link.RepositoryBranch
	return ref, nil
}
