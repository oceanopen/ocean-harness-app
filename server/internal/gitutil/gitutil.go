// Package gitutil 封装本地仓库的 git 读取能力：纯 os/exec 调系统 git，无外部 git 库依赖。
//
// 复刻 src-tauri 旧 shared/repositories.rs 的 git 解析风格：`git -C <dir> ...`、
// stderr 丢弃（非 git 目录的 fatal: 不污染日志）、失败字段留空。供 local_repository service 调用。
package gitutil

import (
	"os/exec"
	"strconv"
	"strings"
)

// Info 是从 git 解析得到的仓库信息。
type Info struct {
	RemoteURL         string // origin remote；无 origin 留空
	Branch            string // 当前分支；detached HEAD 留空
	DefaultBranch     string // 默认分支（origin/HEAD 指向的远程默认分支）；未克隆/无 origin 留空
	LastCommitAt      int64  // 最近提交时间（毫秒时间戳）；无提交为 0
	LastCommitMessage string // 最近提交标题；无提交留空
}

// gitOutput 跑 `git -C <dir> <args...>`，成功返回去尾换行的 stdout；失败 / 非 git 目录返回 ""。
// exec.Cmd 的 Stderr 默认连 /dev/null，故 git 的 fatal: 不会泄露到本进程日志（与旧 Rust 实现一致）。
func gitOutput(dir string, args ...string) string {
	cmdArgs := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", cmdArgs...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// IsRepo 判断 dir 是否为 git 工作区（add/update 严格校验用）。
// `git rev-parse --is-inside-work-tree` 在 git 工作区内退出码 0；非 git 目录退出非 0 → Run 返回 err。
func IsRepo(dir string) bool {
	return exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Run() == nil
}

// ParseInfo 解析 dir 的 git 信息。任一字段失败留空 / 零值（与旧 Rust parse_repo_info 语义一致）。
func ParseInfo(dir string) Info {
	remoteURL := gitOutput(dir, "remote", "get-url", "origin")

	// detached HEAD 时 --abbrev-ref 返回 "HEAD"，前端无意义，统一留空。
	// 注意：--abbrev-ref 与 HEAD 必须是两个独立 argv（同 Rust 注释），单参数会被原样回显。
	branch := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "HEAD" {
		branch = ""
	}

	defaultBranch := DefaultBranch(dir)

	// 一次 log 调用同时取提交时间(%ct)与标题(%s)，换行分隔；subject 不含换行，SplitN 安全。
	var lastCommitAt int64
	var lastCommitMsg string
	if s := gitOutput(dir, "log", "-1", "--format=%ct%n%s"); s != "" {
		parts := strings.SplitN(s, "\n", 2)
		if secs, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64); err == nil {
			lastCommitAt = secs * 1000 // 秒 → 毫秒，与 ClaudeSessionInfo 时间戳口径对齐
		}
		if len(parts) > 1 {
			lastCommitMsg = parts[1]
		}
	}

	return Info{
		RemoteURL:         remoteURL,
		Branch:            branch,
		DefaultBranch:     defaultBranch,
		LastCommitAt:      lastCommitAt,
		LastCommitMessage: lastCommitMsg,
	}
}

// LocalBranches 列出 dir 的本地分支名（`git branch` 默认仅本地分支），按 git 输出顺序返回。
// --format=%(refname:short) 同时抑制当前分支的 `*` 标记，每行一个干净分支名。
// 后续若需远程分支列表，另写 RemoteBranches（git branch -r），不复用本函数以免语义混淆。
// 失败 / 非 git 目录 / 无分支返回 nil。
func LocalBranches(dir string) []string {
	out := gitOutput(dir, "branch", "--format=%(refname:short)")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// DefaultBranch 解析 dir 的默认分支（origin/HEAD 指向的远程默认分支）。
// `git symbolic-ref --short refs/remotes/origin/HEAD` 输出形如 "origin/main"，剥 "origin/" 前缀得 "main"。
// 克隆仓库由 git 自动设置 origin/HEAD；本地 init 未克隆 / 无 origin / 未设置时返回 ""（调用方回退）。
func DefaultBranch(dir string) string {
	out := gitOutput(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if out == "" {
		return ""
	}
	return strings.TrimPrefix(out, "origin/")
}
