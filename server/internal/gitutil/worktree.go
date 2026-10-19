package gitutil

import (
	"path/filepath"
	"strconv"
	"strings"
)

// WorktreeInfo 是单个 worktree 条目；主仓库也是一项且位于结果首位。
type WorktreeInfo struct {
	Dir    string // worktree 绝对路径
	Branch string // 检出分支（refs/heads/ 剥前缀）；detached HEAD 留空
}

/*
 * Worktrees 列出 dir 的全部 worktree（`git worktree list --porcelain`，每个块以 worktree 行开头，无需按空行分块）；
 * quotePath=false 抑制非 ASCII 路径的八进制转义；失败 / 非 git 目录返回 nil（与包内惯例一致，不阻塞调用方）。
 */
func Worktrees(dir string) []WorktreeInfo {
	out := gitOutput(dir, "-c", "core.quotePath=false", "worktree", "list", "--porcelain")
	if out == "" {
		return nil
	}
	var list []WorktreeInfo
	cur := WorktreeInfo{}
	open := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			if open {
				list = append(list, cur)
			}
			cur, open = WorktreeInfo{Dir: unquoteCPath(strings.TrimPrefix(line, "worktree "))}, true
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	if open {
		list = append(list, cur)
	}
	return list
}

// unquoteCPath 还原 git 输出的 C 风格引号路径（路径含引号/控制字符时 git 仍会加引号）。
func unquoteCPath(p string) string {
	if len(p) < 2 || p[0] != '"' || p[len(p)-1] != '"' {
		return p
	}
	if s, err := strconv.Unquote(p); err == nil {
		return s
	}
	return p
}

/*
 * ExtraWorktrees 返回 dir 的额外 worktree（剔除首项主仓库与 dir 本身，供计数与列表共用）。
 * dir 经符号链接解析后与 git 输出的真实路径比对，避免 localDir 以别名路径存储时误计/漏排。
 */
func ExtraWorktrees(dir string) []WorktreeInfo {
	list := Worktrees(dir)
	if len(list) == 0 {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		resolved = dir
	}
	out := make([]WorktreeInfo, 0, len(list))
	for i, wt := range list {
		if i == 0 || wt.Dir == dir || wt.Dir == resolved {
			continue
		}
		out = append(out, wt)
	}
	return out
}
