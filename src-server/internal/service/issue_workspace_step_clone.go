package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"ocean-harness/src-server/internal/dal/types"
	"ocean-harness/src-server/internal/gitutil"
)

// issueWorkspace cloneRepos step（T1.4）：按状态文件中 cloneRepos 步骤的仓库清单（init 受理时
// 已从 DB 解析入文件，执行期不碰 DB）逐仓库浅克隆（--depth 1，T1.4 决策）到
// {issueId}/repo/{name} 并创建 agent_{issueId} 分支。
// 环境变量：sshConfig 步骤产物（workspace 级 .ssh/config）存在时注入 GIT_SSH_COMMAND 指向它，
// 不存在（T1.2 SKIPPED）走默认 SSH。
// 幂等（T1.4 决策）：merge 保留的 SUCCESS 仓库跳过；半成品目录（已是 git 仓库）复用+fetch 后
// 直接补分支；agent 分支已存在仅 checkout 不重置。单仓库失败记 Message 继续下一个，跑完任一
// 失败则步骤 FAILED——重试时 merge 只重跑失败仓库。

func init() {
	issueWorkspaceStepRunners[types.IW_STEP_KEY_CLONE_REPOS] = issueWorkspaceRunCloneRepos
}

// issueWorkspaceRunCloneRepos 逐仓库执行 clone/分支，每仓库状态变化即时落盘（单写者 goroutine
// 内独占 state，前端轮询可见仓库级进度）。全部跑完任一失败则返回 error（编排置步骤/顶层 FAILED）。
func issueWorkspaceRunCloneRepos(state *types.IssueWorkspaceState, step *types.IssueWorkspaceStep, logger *zap.Logger) error {
	env := issueWorkspaceCloneGitEnv(state)
	persist := func() {
		if err := saveIssueWorkspaceState(state); err != nil {
			logger.Error("[issueWorkspace] persist state file failed", zap.String("issueId", state.IssueID), zap.Error(err))
		}
	}
	failed := 0
	for _, repo := range step.Repos {
		if repo.Status != types.IW_STATUS_PENDING {
			continue // 增量合并保留的既有成功结果，不重跑
		}
		repo.Status = types.IW_STATUS_RUNNING
		persist()
		if err := issueWorkspaceCloneRepo(state, repo, env); err != nil {
			repo.Status = types.IW_STATUS_FAILED
			repo.Message = err.Error()
			failed++
		} else {
			repo.Status = types.IW_STATUS_SUCCESS
			repo.Message = ""
		}
		persist()
	}
	if failed > 0 {
		return fmt.Errorf("%d 个仓库 clone/分支失败", failed)
	}
	return nil
}

// issueWorkspaceCloneGitEnv 构造 clone/fetch/ls-remote 的附加环境变量：workspace 级 .ssh/config
// 存在时注入 GIT_SSH_COMMAND（%q 包裹路径，git 经 shell 解析该变量）；不存在返回 nil（默认 SSH）。
func issueWorkspaceCloneGitEnv(state *types.IssueWorkspaceState) []string {
	cfg := filepath.Join(state.BaseDir, state.IssueID, ".ssh", "config")
	if _, err := os.Stat(cfg); err != nil {
		return nil
	}
	return []string{fmt.Sprintf("GIT_SSH_COMMAND=ssh -F %q", cfg)}
}

// issueWorkspaceCloneRepo 单仓库 clone + agent 分支：
//   - 目标目录已是 git 仓库 → 复用 + FetchLatest（重试半成品，不重新 clone）；
//   - 目标目录存在但非 git 仓库 → 报错（不覆盖非 git 内容，人工介入）；
//   - 不存在 → baseBranch 非空先 ls-remote 验证远程分支存在（T1.4 决策：写错分支名应修关联
//     而非静默回退，不存在即该仓库失败），Clone --branch base --depth 1；baseBranch 为空回退
//     默认分支浅克隆；
//   - 分支：agent 分支已存在仅 Checkout（重试不重置既有分支）；不存在则定位基准（fresh clone
//     已在基准分支；复用场景 checkout 本地基准或从 origin/基准 建）后 CreateAndCheckoutBranch。
func issueWorkspaceCloneRepo(state *types.IssueWorkspaceState, repo *types.IssueWorkspaceRepoState, env []string) error {
	target := filepath.Join(state.BaseDir, state.IssueID, "repo", repo.Name)
	reused := false
	if gitutil.IsRepo(target) {
		reused = true
		if err := gitutil.FetchLatest(target, env); err != nil {
			return fmt.Errorf("增量拉取失败: %w", err)
		}
	} else if _, err := os.Stat(target); err == nil {
		return errors.New("目标目录已存在且非 git 仓库，请人工处理")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("检查目标目录失败: %w", err)
	} else if repo.BaseBranch != "" {
		exists, err := gitutil.RemoteBranchExists(repo.RemoteURL, repo.BaseBranch, env)
		if err != nil {
			return fmt.Errorf("验证基准分支失败: %w", err)
		}
		if !exists {
			return fmt.Errorf("基准分支 %q 在远程不存在", repo.BaseBranch)
		}
	}

	if !reused {
		if err := gitutil.Clone(repo.RemoteURL, target, repo.BaseBranch, 1, env); err != nil {
			return fmt.Errorf("clone 失败: %w", err)
		}
	}

	if gitutil.LocalBranchExists(target, repo.TargetBranch) {
		if err := gitutil.CheckoutBranch(target, repo.TargetBranch); err != nil {
			return fmt.Errorf("切换 agent 分支失败: %w", err)
		}
		return nil
	}
	if repo.BaseBranch != "" {
		if gitutil.LocalBranchExists(target, repo.BaseBranch) {
			if err := gitutil.CheckoutBranch(target, repo.BaseBranch); err != nil {
				return fmt.Errorf("切换基准分支失败: %w", err)
			}
		} else if err := gitutil.CreateAndCheckoutFromRemote(target, repo.BaseBranch); err != nil {
			return fmt.Errorf("从远程创建基准分支失败: %w", err)
		}
	}
	if err := gitutil.CreateAndCheckoutBranch(target, repo.TargetBranch); err != nil {
		return fmt.Errorf("创建 agent 分支失败: %w", err)
	}
	return nil
}
