package service

import (
	"fmt"
	"time"

	"ocean-harness/server/internal/dal/types"
)

/*
 * taskWorkspace 状态派生/合并/幂等辅助：由 DB 查询输入（taskWorkspaceRepoInput）与旧状态文件
 * 构造本轮待执行状态（PENDING），派生 status 顶层结论，并提供幂等对比与响应快照深拷贝。
 * 均为纯函数，不碰 DB 与文件系统（状态文件读写见 task_workspace_state.go）。
 */

/*
 * taskWorkspaceDeriveStatusResponse 读状态文件并派生顶层 serverStatus：无文件→NOT_INITIALIZED；
 * 解析失败→CORRUPTED；文件 RUNNING 但 registry 无活跃任务→INTERRUPTED（上次进程中断，可重试）；
 * 否则原样返回文件状态。init 的「重复触发」路径同样复用本派生。
 */
func taskWorkspaceDeriveStatusResponse(baseDir, taskID string) (*types.TaskWorkspaceStatusResponseData, error) {
	state, err := loadTaskWorkspaceState(baseDir, taskID)
	if err != nil {
		return &types.TaskWorkspaceStatusResponseData{ServerStatus: types.IW_STATUS_CORRUPTED}, nil
	}
	if state == nil {
		return &types.TaskWorkspaceStatusResponseData{ServerStatus: types.IW_STATUS_NOT_INITIALIZED}, nil
	}
	if state.Status == types.IW_STATUS_RUNNING && !taskWorkspaceActive(taskID) {
		return &types.TaskWorkspaceStatusResponseData{ServerStatus: types.IW_STATUS_INTERRUPTED, State: state}, nil
	}
	return &types.TaskWorkspaceStatusResponseData{ServerStatus: state.Status, State: state}, nil
}

/*
 * taskWorkspaceCloneState 深拷贝状态（Steps/Repos 逐元素复制，切片 nil 性保持）。
 * Init 响应返回快照、原 state 交给后台 goroutine 持续 mutate，两者不共享可变内存。
 */
func taskWorkspaceCloneState(src *types.TaskWorkspaceState) *types.TaskWorkspaceState {
	dst := *src
	if src.Steps != nil {
		dst.Steps = make([]*types.TaskWorkspaceStep, len(src.Steps))
		for i, s := range src.Steps {
			step := *s
			if s.Repos != nil {
				step.Repos = make([]*types.TaskWorkspaceRepoState, len(s.Repos))
				for j, r := range s.Repos {
					repo := *r
					step.Repos[j] = &repo
				}
			}
			dst.Steps[i] = &step
		}
	}
	if src.Manifest != nil {
		dst.Manifest = make([]types.TaskWorkspaceRepoRef, len(src.Manifest))
		copy(dst.Manifest, src.Manifest)
	}
	return &dst
}

/*
 * taskWorkspaceHasSkipped 判断状态中是否存在 SKIPPED 步骤/仓库（本期占位结果）。
 * 幂等短路据此放行「含占位结果的旧 SUCCESS」走 merge 重跑，后续任务注册实现后自动接续执行。
 */
func taskWorkspaceHasSkipped(state *types.TaskWorkspaceState) bool {
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

// taskWorkspaceRepoRefs 由输入派生幂等 manifest（对比键 localRepositoryId+baseBranch）。
func taskWorkspaceRepoRefs(repos []taskWorkspaceRepoInput) []types.TaskWorkspaceRepoRef {
	refs := make([]types.TaskWorkspaceRepoRef, 0, len(repos))
	for _, r := range repos {
		refs = append(refs, types.TaskWorkspaceRepoRef{
			LocalRepositoryID: r.LocalRepositoryID,
			RemoteURL:         r.RemoteURL,
			BaseBranch:        r.BaseBranch,
		})
	}
	return refs
}

// taskWorkspaceRepoKey 仓库身份键（幂等对比/增量合并共用的单一来源）：localRepositoryId+baseBranch。
func taskWorkspaceRepoKey(repoID int, baseBranch string) string {
	return fmt.Sprintf("%d|%s", repoID, baseBranch)
}

// taskWorkspaceManifestEqual 对比两个 manifest 是否同集合（键见 taskWorkspaceRepoKey，与顺序无关）。
func taskWorkspaceManifestEqual(a, b []types.TaskWorkspaceRepoRef) bool {
	if len(a) != len(b) {
		return false
	}
	keys := make(map[string]struct{}, len(a))
	for _, r := range a {
		keys[taskWorkspaceRepoKey(r.LocalRepositoryID, r.BaseBranch)] = struct{}{}
	}
	for _, r := range b {
		if _, ok := keys[taskWorkspaceRepoKey(r.LocalRepositoryID, r.BaseBranch)]; !ok {
			return false
		}
	}
	return true
}

/*
 * taskWorkspaceMergeState 把 issue 当前关联仓库合并进旧状态，产出本轮待执行状态（PENDING）。
 * 规则：全局步骤——旧状态 SUCCESS/SKIPPED 均保留（SUCCESS 幂等跳过重跑；SKIPPED 为条件跳过结论，
 * 保留展示避免「前步待执行、后步已完成」矛盾态，执行器每轮仍会重推导）；cloneRepos 仓库级——
 * 关联与基准分支未变且旧结果 SUCCESS 的仓库保留（增量跳过），新增/分支变更/旧结果非 SUCCESS 的仓库重置 PENDING，已移除的仓库剔除；全部仓库有既有成功结果且步骤曾成功才保留成功。
 * manifest 全量重建为当前关联集合；顶层 error 清空（旧失败原因已吸收进重试）。
 */
func taskWorkspaceMergeState(old *types.TaskWorkspaceState, baseDir, taskID string, repos []taskWorkspaceRepoInput) *types.TaskWorkspaceState {
	state := &types.TaskWorkspaceState{
		Version:   taskWorkspaceStateVersion,
		TaskID:    taskID,
		BaseDir:   baseDir,
		Status:    types.IW_STATUS_PENDING,
		Steps:     make([]*types.TaskWorkspaceStep, 0, len(taskWorkspaceStepTitles)),
		Manifest:  taskWorkspaceRepoRefs(repos),
		CreatedAt: time.Now(),
	}
	oldSteps := make(map[string]*types.TaskWorkspaceStep)
	if old != nil {
		state.CreatedAt = old.CreatedAt
		for _, s := range old.Steps {
			oldSteps[s.Key] = s
		}
	}

	for _, def := range taskWorkspaceStepTitles {
		step := &types.TaskWorkspaceStep{Key: def.Key, Title: def.Title, Status: types.IW_STATUS_PENDING}
		prev := oldSteps[def.Key]
		if def.Key == types.IW_STEP_KEY_CLONE_REPOS {
			step.Repos = taskWorkspaceRepoStates(repos, taskID)
			if prev != nil {
				step.Repos = taskWorkspaceMergeRepoStates(prev.Repos, step.Repos)
			}
			if !taskWorkspaceReposPending(step.Repos) && prev != nil && prev.Status == types.IW_STATUS_SUCCESS {
				step.Status = types.IW_STATUS_SUCCESS
			}
		} else if prev != nil && (prev.Status == types.IW_STATUS_SUCCESS || prev.Status == types.IW_STATUS_SKIPPED) {
			step.Status = prev.Status
		}
		state.Steps = append(state.Steps, step)
	}
	return state
}

// taskWorkspaceRepoStates 由输入构造全新仓库级子状态（全部 PENDING，targetBranch 统一 agent_{taskId}）。
func taskWorkspaceRepoStates(repos []taskWorkspaceRepoInput, taskID string) []*types.TaskWorkspaceRepoState {
	out := make([]*types.TaskWorkspaceRepoState, len(repos))
	for i, r := range repos {
		out[i] = &types.TaskWorkspaceRepoState{
			LocalRepositoryID: r.LocalRepositoryID,
			Name:              r.Name,
			RemoteURL:         r.RemoteURL,
			BaseBranch:        r.BaseBranch,
			TargetBranch:      "agent_" + taskID,
			Status:            types.IW_STATUS_PENDING,
		}
	}
	return out
}

/*
 * taskWorkspaceMergeRepoStates 把新仓库清单合并进旧结果：键见 taskWorkspaceRepoKey，
 * 关联与分支未变且旧结果为 SUCCESS 的仓库保留，其余（新增/分支变更/旧结果非 SUCCESS）重置 PENDING
 * 待（重）执行。返回顺序对齐新清单。
 */
func taskWorkspaceMergeRepoStates(prev, next []*types.TaskWorkspaceRepoState) []*types.TaskWorkspaceRepoState {
	prevKeys := make(map[string]*types.TaskWorkspaceRepoState, len(prev))
	for _, p := range prev {
		prevKeys[taskWorkspaceRepoKey(p.LocalRepositoryID, p.BaseBranch)] = p
	}
	merged := make([]*types.TaskWorkspaceRepoState, len(next))
	for i, n := range next {
		if p, ok := prevKeys[taskWorkspaceRepoKey(n.LocalRepositoryID, n.BaseBranch)]; ok && p.Status == types.IW_STATUS_SUCCESS {
			merged[i] = p
		} else {
			merged[i] = n
		}
	}
	return merged
}

// taskWorkspaceReposPending 判断仓库级子状态中是否存在待执行项。
func taskWorkspaceReposPending(repos []*types.TaskWorkspaceRepoState) bool {
	for _, r := range repos {
		if r.Status == types.IW_STATUS_PENDING {
			return true
		}
	}
	return false
}
