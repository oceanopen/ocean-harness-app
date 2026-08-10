// 本文件封装本地仓库的 worktree 写操作（add/remove/prune）与列举，与 gitutil.go 的只读能力互补。
// 写操作的失败原因（分支冲突、路径占用、脏工作区）须回传上层生成可读错误，故新增 gitRun 收集
// stderr（区别于丢弃 stderr、仅返回空串的 gitOutput）。

package gitutil

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitRun 跑 `git -C <dir> <args...>`，成功返回去尾换行的 stdout；失败返回 error（含 stderr 原文）。
// 与 gitOutput 的区别：worktree add/remove 的失败原因在 stderr，必须回传给上层生成可读错误，故用
// cmd.Stderr 收集而非丢弃；stderr 为空（如命令不存在）时回退用 err.Error()。
func gitRun(dir string, args ...string) (string, error) {
	var stderr bytes.Buffer
	cmdArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", cmdArgs...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// Worktree 是 `git worktree list --porcelain` 解析出的一条工作区记录。
type Worktree struct {
	Head   string // HEAD 提交 SHA（porcelain 的 HEAD 行）；bare 主仓库无此行留空
	Path   string // 工作区绝对路径（porcelain 的 worktree 行）
	Branch string // 所在分支短名（已剥 refs/heads/ 前缀）；detached/bare 无此行留空
}

// parseWorktreeList 解析 `git worktree list --porcelain` 输出为 []Worktree。
// 输出以空行分段，每段首行 `worktree <path>`，后跟 `HEAD <sha>`、`branch refs/heads/<name>`
// （detached/bare 段缺 branch 行）。逐行扫描：遇 worktree 行开新记录、遇 HEAD/branch 行填字段，
// 其余行（detached/bare/locked/prunable）忽略；空行分隔段。
func parseWorktreeList(out string) []Worktree {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	var list []Worktree
	var cur *Worktree
	flush := func() {
		if cur != nil {
			list = append(list, *cur)
			cur = nil
		}
	}
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case cur != nil && strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case cur != nil && strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return list
}

// WorktreeList 列出 dir 所属仓库的全部工作区（含主工作区），按 git 输出顺序返回。
// 读操作：非 git 目录时 git worktree list 本身报错，由 gitRun 透传，不前置 IsRepo
// （与 ParseInfo/LocalBranches 语义一致：读失败交给 git 报错）。
func WorktreeList(dir string) ([]Worktree, error) {
	out, err := gitRun(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktreeList(out), nil
}

// WorktreeExists 判断 path 是否是 dir 所属仓库的现有工作区（含主工作区）。
// worktree 创建前判断目标路径是否已占用：幂等/重试场景目录已存在则跳过 add，避免重复 add 失败。
func WorktreeExists(dir, path string) (bool, error) {
	list, err := WorktreeList(dir)
	if err != nil {
		return false, err
	}
	// git worktree list 输出符号链接解析后的真实路径（macOS /tmp→/private/tmp 等）；
	// 解析输入 path 使比对一致。路径不存在（首次创建）时 EvalSymlinks 报错，保留原值——
	// 此时 list 中也不会有它，正确返回 false。
	if resolved, e := filepath.EvalSymlinks(path); e == nil {
		path = resolved
	}
	for _, w := range list {
		if w.Path == path {
			return true, nil
		}
	}
	return false, nil
}

// WorktreeAdd 在 dir 所属仓库创建工作区：于 <path> 处基于 <baseBranch> 检出新分支 <branch>。
// --no-track：新分支不建立上游追踪关系（本期本地开发，无需 push/track）。
// baseBranch 为空时从当前 HEAD 派生。前置（调用方负责）：IsRepo 已校验；
// 欲防分支冲突，调用前用 WorktreeBranchExists 检查（git worktree add -b 对已存在分支会失败）。
func WorktreeAdd(dir, path, branch, baseBranch string) error {
	if !IsRepo(dir) {
		return fmt.Errorf("非 git 仓库：%s", dir)
	}
	args := []string{"worktree", "add", "--no-track", "-b", branch, path}
	if baseBranch != "" {
		args = append(args, baseBranch)
	}
	_, err := gitRun(dir, args...)
	return err
}

// WorktreeRemove 删除 dir 所属仓库的指定工作区（按路径）。
// force=false：安全删除，工作区须干净（有未提交改动时失败）；
// force=true：加 --force，脏工作区也删（清理兜底用）。删目录后建议调用 WorktreePrune 清理 git 元数据。
func WorktreeRemove(dir, path string, force bool) error {
	if !IsRepo(dir) {
		return fmt.Errorf("非 git 仓库：%s", dir)
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := gitRun(dir, args...)
	return err
}

// WorktreePrune 清理已删除目录的工作区元数据（git worktree prune）。
// WorktreeRemove 删目录后调一次，使 git worktree list 不再列出已不存在的路径（reconcile 兜底）。
func WorktreePrune(dir string) error {
	if !IsRepo(dir) {
		return fmt.Errorf("非 git 仓库：%s", dir)
	}
	_, err := gitRun(dir, "worktree", "prune")
	return err
}

// WorktreeBranchExists 判断 dir 所属仓库是否存在本地分支 branch（防 worktree add -b 分支冲突）。
// `git show-ref --verify --quiet refs/heads/<branch>`：退出码 0 存在、非 0 不存在；仅查本地分支，不含远程追踪。
func WorktreeBranchExists(dir, branch string) bool {
	return exec.Command("git", "-C", dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}
