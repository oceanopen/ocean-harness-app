package service

import (
	"fmt"
	"time"

	"we-claude-terminal/go-server/internal/dal/types"
)

// issueWorkspace 状态派生/合并/幂等辅助：由 DB 查询输入（issueWorkspaceRepoInput）与旧状态文件
// 构造本轮待执行状态（PENDING），派生 status 顶层结论，并提供幂等对比与响应快照深拷贝。
// 均为纯函数，不碰 DB 与文件系统（状态文件读写见 issue_workspace_state.go）。

// issueWorkspaceDeriveStatusResponse 读状态文件并派生顶层 serverStatus：无文件→NOT_INITIALIZED；
// 解析失败→CORRUPTED；文件 RUNNING 但 registry 无活跃任务→INTERRUPTED（上次进程中断，可重试）；
// 否则原样返回文件状态。init 的「重复触发」路径同样复用本派生。
func issueWorkspaceDeriveStatusResponse(baseDir, issueID string) (*types.IssueWorkspaceStatusResponseData, error) {
	state, err := loadIssueWorkspaceState(baseDir, issueID)
	if err != nil {
		return &types.IssueWorkspaceStatusResponseData{ServerStatus: types.IW_STATUS_CORRUPTED}, nil
	}
	if state == nil {
		return &types.IssueWorkspaceStatusResponseData{ServerStatus: types.IW_STATUS_NOT_INITIALIZED}, nil
	}
	if state.Status == types.IW_STATUS_RUNNING && !issueWorkspaceActive(issueID) {
		return &types.IssueWorkspaceStatusResponseData{ServerStatus: types.IW_STATUS_INTERRUPTED, State: state}, nil
	}
	return &types.IssueWorkspaceStatusResponseData{ServerStatus: state.Status, State: state}, nil
}

// issueWorkspaceCloneState 深拷贝状态（Steps/Repos 逐元素复制，切片 nil 性保持）。
// Init 响应返回快照、原 state 交给后台 goroutine 持续 mutate，两者不共享可变内存。
func issueWorkspaceCloneState(src *types.IssueWorkspaceState) *types.IssueWorkspaceState {
	dst := *src
	if src.Steps != nil {
		dst.Steps = make([]*types.IssueWorkspaceStep, len(src.Steps))
		for i, s := range src.Steps {
			step := *s
			if s.Repos != nil {
				step.Repos = make([]*types.IssueWorkspaceRepoState, len(s.Repos))
				for j, r := range s.Repos {
					repo := *r
					step.Repos[j] = &repo
				}
			}
			dst.Steps[i] = &step
		}
	}
	if src.Manifest != nil {
		dst.Manifest = make([]types.IssueWorkspaceRepoRef, len(src.Manifest))
		copy(dst.Manifest, src.Manifest)
	}
	return &dst
}

// issueWorkspaceHasSkipped 判断状态中是否存在 SKIPPED 步骤/仓库（本期占位结果）。
// 幂等短路据此放行「含占位结果的旧 SUCCESS」走 merge 重跑，后续任务注册实现后自动接续执行。
func issueWorkspaceHasSkipped(state *types.IssueWorkspaceState) bool {
	for _, s := range state.Steps {
		if s.Status == types.IW_STATUS_SKIPPED {
			return true
		}
		for _, r := range s.Repos {
			if r.Status == types.IW_STATUS_SKIPPED {
				return true
			}
		}
	}
	return false
}

// issueWorkspaceRepoRefs 由输入派生幂等 manifest（对比键 localRepositoryId+baseBranch）。
func issueWorkspaceRepoRefs(repos []issueWorkspaceRepoInput) []types.IssueWorkspaceRepoRef {
	refs := make([]types.IssueWorkspaceRepoRef, 0, len(repos))
	for _, r := range repos {
		refs = append(refs, types.IssueWorkspaceRepoRef{
			LocalRepositoryID: r.LocalRepositoryID,
			RemoteURL:         r.RemoteURL,
			BaseBranch:        r.BaseBranch,
		})
	}
	return refs
}

// issueWorkspaceRepoKey 仓库身份键（幂等对比/增量合并共用的单一来源）：localRepositoryId+baseBranch。
func issueWorkspaceRepoKey(repoID int, baseBranch string) string {
	return fmt.Sprintf("%d|%s", repoID, baseBranch)
}

// issueWorkspaceManifestEqual 对比两个 manifest 是否同集合（键见 issueWorkspaceRepoKey，与顺序无关）。
func issueWorkspaceManifestEqual(a, b []types.IssueWorkspaceRepoRef) bool {
	if len(a) != len(b) {
		return false
	}
	keys := make(map[string]struct{}, len(a))
	for _, r := range a {
		keys[issueWorkspaceRepoKey(r.LocalRepositoryID, r.BaseBranch)] = struct{}{}
	}
	for _, r := range b {
		if _, ok := keys[issueWorkspaceRepoKey(r.LocalRepositoryID, r.BaseBranch)]; !ok {
			return false
		}
	}
	return true
}

// issueWorkspaceMergeState 把 issue 当前关联仓库合并进旧状态，产出本轮待执行状态（PENDING）。
// 规则：
//   - 全局步骤：旧状态中 SUCCESS 的步骤保留成功结论（幂等跳过重跑）；SKIPPED 不保留——占位结果
//     每次重跑时重新推导，T1.2/T1.3/T1.4 实现注册后可自动接续执行；
//   - cloneRepos 仓库级：关联与基准分支未变且旧结果 SUCCESS 的仓库保留（增量跳过），新增/分支变更/
//     旧结果非 SUCCESS 的仓库重置 PENDING，已移除的仓库剔除；全部仓库有既有成功结果且步骤曾成功才保留成功；
//   - manifest 全量重建为当前关联集合；顶层 error 清空（旧失败原因已吸收进重试）。
func issueWorkspaceMergeState(old *types.IssueWorkspaceState, baseDir, issueID string, repos []issueWorkspaceRepoInput) *types.IssueWorkspaceState {
	state := &types.IssueWorkspaceState{
		Version:   issueWorkspaceStateVersion,
		IssueID:   issueID,
		BaseDir:   baseDir,
		Status:    types.IW_STATUS_PENDING,
		Steps:     make([]*types.IssueWorkspaceStep, 0, len(issueWorkspaceStepTitles)),
		Manifest:  issueWorkspaceRepoRefs(repos),
		CreatedAt: time.Now(),
	}
	oldSteps := make(map[string]*types.IssueWorkspaceStep)
	if old != nil {
		state.CreatedAt = old.CreatedAt
		for _, s := range old.Steps {
			oldSteps[s.Key] = s
		}
	}

	for _, def := range issueWorkspaceStepTitles {
		step := &types.IssueWorkspaceStep{Key: def.Key, Title: def.Title, Status: types.IW_STATUS_PENDING}
		prev := oldSteps[def.Key]
		if def.Key == types.IW_STEP_KEY_CLONE_REPOS {
			step.Repos = issueWorkspaceRepoStates(repos, issueID)
			if prev != nil {
				step.Repos = issueWorkspaceMergeRepoStates(prev.Repos, step.Repos)
			}
			if !issueWorkspaceReposPending(step.Repos) && prev != nil && prev.Status == types.IW_STATUS_SUCCESS {
				step.Status = types.IW_STATUS_SUCCESS
			}
		} else if prev != nil && prev.Status == types.IW_STATUS_SUCCESS {
			step.Status = types.IW_STATUS_SUCCESS
		}
		state.Steps = append(state.Steps, step)
	}
	return state
}

// issueWorkspaceRepoStates 由输入构造全新仓库级子状态（全部 PENDING，targetBranch 统一 agent_{issueId}）。
func issueWorkspaceRepoStates(repos []issueWorkspaceRepoInput, issueID string) []*types.IssueWorkspaceRepoState {
	out := make([]*types.IssueWorkspaceRepoState, len(repos))
	for i, r := range repos {
		out[i] = &types.IssueWorkspaceRepoState{
			LocalRepositoryID: r.LocalRepositoryID,
			Name:              r.Name,
			RemoteURL:         r.RemoteURL,
			BaseBranch:        r.BaseBranch,
			TargetBranch:      "agent_" + issueID,
			Status:            types.IW_STATUS_PENDING,
		}
	}
	return out
}

// issueWorkspaceMergeRepoStates 把新仓库清单合并进旧结果：键见 issueWorkspaceRepoKey，
// 关联与分支未变且旧结果为 SUCCESS 的仓库保留，其余（新增/分支变更/旧结果非 SUCCESS）重置 PENDING
// 待（重）执行。返回顺序对齐新清单。
func issueWorkspaceMergeRepoStates(prev, next []*types.IssueWorkspaceRepoState) []*types.IssueWorkspaceRepoState {
	prevKeys := make(map[string]*types.IssueWorkspaceRepoState, len(prev))
	for _, p := range prev {
		prevKeys[issueWorkspaceRepoKey(p.LocalRepositoryID, p.BaseBranch)] = p
	}
	merged := make([]*types.IssueWorkspaceRepoState, len(next))
	for i, n := range next {
		if p, ok := prevKeys[issueWorkspaceRepoKey(n.LocalRepositoryID, n.BaseBranch)]; ok && p.Status == types.IW_STATUS_SUCCESS {
			merged[i] = p
		} else {
			merged[i] = n
		}
	}
	return merged
}

// issueWorkspaceReposPending 判断仓库级子状态中是否存在待执行项。
func issueWorkspaceReposPending(repos []*types.IssueWorkspaceRepoState) bool {
	for _, r := range repos {
		if r.Status == types.IW_STATUS_PENDING {
			return true
		}
	}
	return false
}
