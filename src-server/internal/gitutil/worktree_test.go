package gitutil

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// skipIfNoGit 在系统未安装 git 时跳过测试（worktree 用例需真实 git 仓库）。
func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 未安装，跳过 worktree 测试")
	}
}

// newTestRepo 在临时沙箱目录下创建带初始提交的 git 仓库（worktree add 需至少一次提交）。
// 返回 repoDir（仓库根）与 sandbox（repo 与 worktree 的共同父目录；worktree 路径取其下未存在的兄弟目录，
// 避免 git worktree add 拒绝已存在路径）。关闭 gpg 签名、配置本地 user，避免 commit 报错。
func newTestRepo(t *testing.T) (repoDir, sandbox string) {
	t.Helper()
	sandbox = t.TempDir()
	// macOS 下 t.TempDir() 返回 /var/...（实为 /private/var/... 符号链接），而 git worktree list
	// 存解析后的真实路径。统一 EvalSymlinks，使派生路径与 git 返回路径一致，避免对比错位。
	if resolved, err := filepath.EvalSymlinks(sandbox); err == nil {
		sandbox = resolved
	}
	repoDir = filepath.Join(sandbox, "repo")
	require.NoError(t, os.Mkdir(repoDir, 0o755))
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		require.NoErrorf(t, cmd.Run(), "git %v: %s", args, stderr.String())
	}
	git("init")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "test")
	git("config", "commit.gpgsign", "false")
	git("commit", "--allow-empty", "-m", "init")
	return repoDir, sandbox
}

// containsPath 报告 list 中是否存在指定路径的 worktree。
func containsPath(list []Worktree, path string) bool {
	for _, w := range list {
		if w.Path == path {
			return true
		}
	}
	return false
}

func TestWorktreeAdd_Success(t *testing.T) {
	skipIfNoGit(t)
	repoDir, sandbox := newTestRepo(t)
	wtPath := filepath.Join(sandbox, "wt1")

	require.NoError(t, WorktreeAdd(repoDir, wtPath, "feature", ""))
	require.DirExists(t, wtPath)                     // 磁盘真生成
	require.True(t, WorktreeBranchExists(repoDir, "feature"))
}

func TestWorktreeAdd_BranchConflict(t *testing.T) {
	skipIfNoGit(t)
	repoDir, sandbox := newTestRepo(t)
	// 预建同名分支，add -b 应失败
	require.NoError(t, exec.Command("git", "-C", repoDir, "branch", "dup").Run())

	err := WorktreeAdd(repoDir, filepath.Join(sandbox, "wt1"), "dup", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "dup") // git: a branch named 'dup' already exists
}

func TestWorktreeAdd_PathOccupied(t *testing.T) {
	skipIfNoGit(t)
	repoDir, sandbox := newTestRepo(t)
	// 目标路径已存在且非空
	wtPath := filepath.Join(sandbox, "wt1")
	require.NoError(t, os.MkdirAll(wtPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wtPath, "x"), []byte("x"), 0o644))

	err := WorktreeAdd(repoDir, wtPath, "feature", "")
	require.Error(t, err)
}

func TestWorktreeAdd_NotARepo(t *testing.T) {
	skipIfNoGit(t)
	dir := t.TempDir()
	err := WorktreeAdd(dir, filepath.Join(dir, "wt"), "feature", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "非 git 仓库")
}

func TestWorktreeList(t *testing.T) {
	skipIfNoGit(t)
	repoDir, sandbox := newTestRepo(t)
	require.NoError(t, WorktreeAdd(repoDir, filepath.Join(sandbox, "wt1"), "a", ""))
	require.NoError(t, WorktreeAdd(repoDir, filepath.Join(sandbox, "wt2"), "b", ""))

	list, err := WorktreeList(repoDir)
	require.NoError(t, err)
	require.Len(t, list, 3) // 主仓 + 2 worktree
	require.NotEmpty(t, list[0].Head)

	branchByPath := map[string]string{}
	for _, w := range list {
		branchByPath[w.Path] = w.Branch
	}
	require.Equal(t, "a", branchByPath[filepath.Join(sandbox, "wt1")])
	require.Equal(t, "b", branchByPath[filepath.Join(sandbox, "wt2")])
}

func TestWorktreeRemove_Success(t *testing.T) {
	skipIfNoGit(t)
	repoDir, sandbox := newTestRepo(t)
	wtPath := filepath.Join(sandbox, "wt1")
	require.NoError(t, WorktreeAdd(repoDir, wtPath, "feature", ""))

	require.NoError(t, WorktreeRemove(repoDir, wtPath, false))
	require.NoDirExists(t, wtPath)
}

func TestWorktreeRemove_DirtyForce(t *testing.T) {
	skipIfNoGit(t)
	repoDir, sandbox := newTestRepo(t)
	wtPath := filepath.Join(sandbox, "wt1")
	require.NoError(t, WorktreeAdd(repoDir, wtPath, "feature", ""))
	// 制造未提交改动 → 安全删除应失败、目录仍在
	require.NoError(t, os.WriteFile(filepath.Join(wtPath, "uncommitted"), []byte("dirty"), 0o644))

	require.Error(t, WorktreeRemove(repoDir, wtPath, false))
	require.DirExists(t, wtPath)

	// 强制删除成功
	require.NoError(t, WorktreeRemove(repoDir, wtPath, true))
	require.NoDirExists(t, wtPath)
}

func TestWorktreeRemove_NotARepo(t *testing.T) {
	skipIfNoGit(t)
	dir := t.TempDir()
	err := WorktreeRemove(dir, filepath.Join(dir, "wt"), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "非 git 仓库")
}

func TestWorktreePrune(t *testing.T) {
	skipIfNoGit(t)
	repoDir, sandbox := newTestRepo(t)
	wtPath := filepath.Join(sandbox, "wt1")
	require.NoError(t, WorktreeAdd(repoDir, wtPath, "feature", ""))
	// 添加后能列出
	list, err := WorktreeList(repoDir)
	require.NoError(t, err)
	require.True(t, containsPath(list, wtPath))

	// 绕过 git 直接删目录，制造孤儿元数据
	require.NoError(t, os.RemoveAll(wtPath))
	// prune 清理后不再列出
	require.NoError(t, WorktreePrune(repoDir))
	list, err = WorktreeList(repoDir)
	require.NoError(t, err)
	require.False(t, containsPath(list, wtPath))
}

func TestWorktreeBranchExists(t *testing.T) {
	skipIfNoGit(t)
	repoDir, sandbox := newTestRepo(t)
	require.False(t, WorktreeBranchExists(repoDir, "feature"))

	require.NoError(t, WorktreeAdd(repoDir, filepath.Join(sandbox, "wt1"), "feature", ""))
	require.True(t, WorktreeBranchExists(repoDir, "feature"))
}

func TestParseWorktreeList(t *testing.T) {
	// 纯单元：覆盖主仓 + 普通分支 worktree + detached（无 branch 行），及 refs/heads/ 剥离。
	in := "worktree /r\nHEAD abc123\nbranch refs/heads/main\n\n" +
		"worktree /r/wt\nHEAD def456\nbranch refs/heads/feature\n\n" +
		"worktree /r/det\nHEAD 999\ndetached\n"
	got := parseWorktreeList(in)
	require.Len(t, got, 3)
	require.Equal(t, Worktree{Path: "/r", Head: "abc123", Branch: "main"}, got[0])
	require.Equal(t, Worktree{Path: "/r/wt", Head: "def456", Branch: "feature"}, got[1])
	require.Equal(t, Worktree{Path: "/r/det", Head: "999", Branch: ""}, got[2])
}
