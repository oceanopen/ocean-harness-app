package gitutil

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// mustEvalSymlinks 解析路径的符号链接（失败直接终止测试）。
func mustEvalSymlinks(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s) 失败: %v", p, err)
	}
	return resolved
}

/*
 * TestWorktrees 用临时真实仓库覆盖：主仓库解析、linked worktree 的分支解析、detached HEAD 留空；
 * 首项恒为主仓库（上层据此剔除主仓库计额外数量）。
 */
func TestWorktrees(t *testing.T) {
	dir := t.TempDir()
	wtDir := t.TempDir()
	// macOS 的 TempDir 返回 /var/... 符号链接路径，git 输出恒为解析后的真实路径，期望值须同样归一。
	dir = mustEvalSymlinks(t, dir)
	wtDir = mustEvalSymlinks(t, wtDir)
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v 失败: %v\n%s", args, err, out)
		}
	}
	git("init", "-b", "main")
	// 测试环境可能无全局 git 身份，临时仓库内补最小配置。
	git("config", "user.name", "test")
	git("config", "user.email", "test@example.com")
	git("commit", "--allow-empty", "-m", "init")
	// linked worktree 检出新分支（主仓库保持在 main）。
	git("worktree", "add", "-b", "feature/wt", wtDir)
	// detached HEAD 场景：另一 linked worktree 直接挂到提交上。
	git("worktree", "add", "--detach", dir+"-detached", "HEAD")

	list := Worktrees(dir)
	if len(list) != 3 {
		t.Fatalf("期望 3 个 worktree（主 + 2 linked），实际 %d: %+v", len(list), list)
	}
	if list[0].Dir != dir || list[0].Branch != "main" {
		t.Fatalf("首项应为主仓库 %s@main，实际 %+v", dir, list[0])
	}
	// linked worktree 间排序无文档保证，按目录查找断言。
	byDir := map[string]WorktreeInfo{}
	for _, wt := range list[1:] {
		byDir[wt.Dir] = wt
	}
	if wt, ok := byDir[wtDir]; !ok || wt.Branch != "feature/wt" {
		t.Fatalf("linked worktree 解析错误，期望 %s@feature/wt，实际 %+v", wtDir, byDir)
	}
	for _, wt := range list[1:] {
		if wt.Dir != wtDir && wt.Branch != "" {
			t.Fatalf("detached HEAD 分支应留空，实际 %+v", wt)
		}
	}

	// 非 git 目录返回 nil（失败静默惯例）。
	if got := Worktrees(t.TempDir()); got != nil {
		t.Fatalf("非 git 目录应返回 nil，实际 %+v", got)
	}
}
