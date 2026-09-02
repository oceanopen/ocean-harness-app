package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"ocean-harness/src-server/internal/dal/types"
)

// newCloneBareRemote 构造 cloneRepos 测试源（与 gitutil 包测试同构，test helper 不跨包复用）：
// work 仓库 main 分支 + feature/base 分支，bare（HEAD=main）作 remote。
func newCloneBareRemote(t *testing.T) (work, bare string) {
	t.Helper()
	work, bare = t.TempDir(), t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(work, "init", "-b", "main")
	run(work, "config", "user.email", "test@we.local")
	run(work, "config", "user.name", "test")
	run(work, "commit", "--allow-empty", "-m", "init")
	run(work, "checkout", "-b", "feature/base")
	run(work, "commit", "--allow-empty", "-m", "base-branch-commit")
	run(work, "checkout", "main")
	run(work, "clone", "--bare", work, bare)
	return work, bare
}

// newCloneState 构造含给定仓库清单的最小 state/step，并预建 {issueId}/repo 目录（createDirs 前置）。
func newCloneState(t *testing.T, repos ...*types.IssueWorkspaceRepoState) (*types.IssueWorkspaceState, *types.IssueWorkspaceStep) {
	t.Helper()
	baseDir := t.TempDir()
	issueID := "01111111-1111-7111-1111-111111111111"
	if err := os.MkdirAll(filepath.Join(baseDir, issueID, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, r := range repos {
		if r.Status == "" {
			r.Status = types.IW_STATUS_PENDING // 真实流程由 mergeState 构造为 PENDING
		}
	}
	return &types.IssueWorkspaceState{
			Version: issueWorkspaceStateVersion, IssueID: issueID, BaseDir: baseDir,
		}, &types.IssueWorkspaceStep{
			Key: types.IW_STEP_KEY_CLONE_REPOS, Title: "Clone 仓库与分支",
			Status: types.IW_STATUS_RUNNING, Repos: repos,
		}
}

func TestIssueWorkspaceRunCloneRepos(t *testing.T) {
	if issueWorkspaceStepRunners[types.IW_STEP_KEY_CLONE_REPOS] == nil {
		t.Fatal("cloneRepos runner 未注册（init 自注册失效）")
	}
	_, bare := newCloneBareRemote(t)

	// 路径 1：全新 clone 成功——repo SUCCESS、agent 分支存在、HEAD 在 agent 分支。
	state, step := newCloneState(t, &types.IssueWorkspaceRepoState{
		Name: "repoA", RemoteURL: bare, BaseBranch: "feature/base", TargetBranch: "agent_x",
	})
	if err := issueWorkspaceRunCloneRepos(state, step, zap.NewNop()); err != nil {
		t.Fatalf("runner 出错: %v", err)
	}
	if step.Repos[0].Status != types.IW_STATUS_SUCCESS {
		t.Fatalf("repo 状态 = %q, want SUCCESS（%s）", step.Repos[0].Status, step.Repos[0].Message)
	}
	dir := filepath.Join(state.BaseDir, state.IssueID, "repo", "repoA")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(out)) != "agent_x" {
		t.Fatalf("HEAD 应在 agent_x 分支: out=%q err=%v", out, err)
	}

	// 路径 2：基准分支不存在 → 该仓库 FAILED+Message，其余仓库继续 SUCCESS，runner 返回 error。
	state, step = newCloneState(t,
		&types.IssueWorkspaceRepoState{Name: "bad", RemoteURL: bare, BaseBranch: "no-such", TargetBranch: "agent_x"},
		&types.IssueWorkspaceRepoState{Name: "good", RemoteURL: bare, BaseBranch: "feature/base", TargetBranch: "agent_x"},
	)
	err = issueWorkspaceRunCloneRepos(state, step, zap.NewNop())
	if err == nil {
		t.Fatal("存在失败仓库时 runner 应返回 error")
	}
	if step.Repos[0].Status != types.IW_STATUS_FAILED || !strings.Contains(step.Repos[0].Message, "基准分支") {
		t.Fatalf("bad 仓库应 FAILED 且 Message 含原因: %+v", step.Repos[0])
	}
	if step.Repos[1].Status != types.IW_STATUS_SUCCESS {
		t.Fatalf("good 仓库应继续执行并 SUCCESS: %+v", step.Repos[1])
	}

	// 路径 3：半成品复用——预置已 clone 目录（停在旧提交），remote 推进后重跑 → fetch 生效 + 分支就绪。
	work, bare2 := newCloneBareRemote(t)
	target := filepath.Join(t.TempDir(), "half")
	if out, e := exec.Command("git", "clone", "--depth", "1", "--branch", "feature/base", bare2, target).CombinedOutput(); e != nil {
		t.Fatalf("预置半成品 clone 失败: %v\n%s", e, out)
	}
	state, step = newCloneState(t, &types.IssueWorkspaceRepoState{
		Name: "half", RemoteURL: bare2, BaseBranch: "feature/base", TargetBranch: "agent_x",
	})
	// 把半成品目录挪到 state 期望位置（模拟上次中断遗留）。
	if err := os.Rename(target, filepath.Join(state.BaseDir, state.IssueID, "repo", "half")); err != nil {
		t.Fatal(err)
	}
	// remote 侧在 feature/base 追加提交（helper 结束时 work 停在 main，须先切回基准分支）。
	if out, e := exec.Command("git", "-C", work, "checkout", "feature/base").CombinedOutput(); e != nil {
		t.Fatalf("切换分支失败: %v\n%s", e, out)
	}
	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "second")
	cmd.Dir = work
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("追加提交失败: %v\n%s", e, out)
	}
	if out, e := exec.Command("git", "-C", work, "push", bare2, "feature/base").CombinedOutput(); e != nil {
		t.Fatalf("推送失败: %v\n%s", e, out)
	}
	if err := issueWorkspaceRunCloneRepos(state, step, zap.NewNop()); err != nil {
		t.Fatalf("runner 出错: %v", err)
	}
	if step.Repos[0].Status != types.IW_STATUS_SUCCESS {
		t.Fatalf("半成品复用应 SUCCESS: %+v", step.Repos[0])
	}
	dir = filepath.Join(state.BaseDir, state.IssueID, "repo", "half")
	out, err = exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(out)) != "agent_x" {
		t.Fatalf("复用后 HEAD 应在 agent_x: out=%q err=%v", out, err)
	}
	remoteRef := exec.Command("git", "-C", dir, "rev-parse", "origin/feature/base")
	remoteRefOut, _ := remoteRef.Output()
	workHead, _ := exec.Command("git", "-C", work, "rev-parse", "HEAD").Output()
	if strings.TrimSpace(string(remoteRefOut)) != strings.TrimSpace(string(workHead)) {
		t.Fatalf("复用后应 fetch 到 remote 最新: origin=%s work=%s", remoteRefOut, workHead)
	}
}
