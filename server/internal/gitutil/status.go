// gitutil 的工作区安全检查能力（issueWorkspace archive 归档/取消前置检查，T3.2）。
// 与 gitutil.go 的吞错探测风格不同：本文件函数服务于「删目录前的安全检查」——
// 检查失败必须显式报错（把「命令失败」误判为「工作区干净」会导致带未提交变更的
// 目录被静默删除），故错误携带输出摘要（同 clone.go 的 gitRunEnv 风格）。
package gitutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// gitOutputErr 跑 `git -C <dir> <args...>`，成功返回去尾换行的 stdout；失败返回携带输出
// 尾部摘要的 error（gitutil.go 的 gitOutput 吞错只适合探测式读取，不适用于安全检查语义）。
func gitOutputErr(dir string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", cmdArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s 失败: %v: %s", strings.Join(args, " "), err, outputTail(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// StatusPorcelain 列出 dir 的未提交变更（`git status --porcelain`，每行一条；空输出 = 工作区
// 干净）。非 git 目录 / git 异常返回 error——调用方须把「检查失败」与「干净」区分处理。
func StatusPorcelain(dir string) (string, error) {
	return gitOutputErr(dir, "status", "--porcelain")
}

// RemoteRefExists 判断 dir 是否存在远程跟踪引用 remoteRef（形如 "origin/main"，refs/remotes/
// 前缀由本函数拼接）。show-ref --verify 静默探测，非 git 目录 / 引用不存在一律 false
// （LocalBranchExists 同款范式）。
func RemoteRefExists(dir, remoteRef string) bool {
	return exec.Command("git", "-C", dir, "show-ref", "--verify", "--quiet", "refs/remotes/"+remoteRef).Run() == nil
}

// LogAhead 列出 toRef 领先于 fromRef 的提交（`git log --oneline fromRef..toRef`，每行一条；
// 空输出 = 无领先提交）。引用不存在等错误返回 error。本地 ref 快照对照（不联网 fetch，
// T3.2 归档安全检查的定稿基准——快，且「从未推送」由 RemoteRefExists 单独探测）。
func LogAhead(dir, fromRef, toRef string) (string, error) {
	return gitOutputErr(dir, "log", "--oneline", fromRef+".."+toRef)
}

// —— 工作区 diff 能力（issueWorkspace 文件工具「Git 变更」模式，T5.1）——
// 口径定稿：只看未 commit 的变更（HEAD 对比暂存区+工作区，用户分步 commit、每次只关注
// 当前未提交的部分）；untracked 新文件经 StatusPorcelain 的 "??" 行补充。不对比基准分支、
// 不探测 upstream——已提交历史一概不算。

// GitChangeEntry 是单个文件的未提交变更摘要（DiffNumstat/DiffNameStatus/UntrackedFiles 三方合并后的行）。
type GitChangeEntry struct {
	Path        string // 仓库内相对路径（正斜杠）
	Status      string // A=新增 M=修改 D=删除 U=untracked（前端翻译为徽标）
	Ins         int    // 新增行数（binary 为 -1）
	Del         int    // 删除行数（binary 为 -1）
	IsUntracked bool
}

// gitOutputNoQuote 同 gitOutputErr，但加 `-c core.quotePath=false`：非 ASCII 路径按原样
// 输出（默认的 C 八进制转义形态会让变更表 path 与展示/git show/ReadFile 全链路失配）。
func gitOutputNoQuote(dir string, args ...string) (string, error) {
	cmdArgs := append([]string{"-c", "core.quotePath=false", "-C", dir}, args...)
	out, err := exec.Command("git", cmdArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s 失败: %v: %s", strings.Join(args, " "), err, outputTail(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// headExists 探测 HEAD 是否存在（unborn HEAD：刚 init 未 commit 的仓库，git diff HEAD 会
// 以退出码 128 失败——此时只有 untracked 变更，跳过 diff 段只走 porcelain 采集）。
func headExists(dir string) bool {
	return exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", "HEAD").Run() == nil
}

// UncommittedChanges 返回 dir 仓库的全部未提交变更（已跟踪部分 `git diff HEAD` 的
// numstat + name-status 合并，untracked 部分取 `git status --porcelain -uall` 的 "??" 行
// —— -uall 逐文件展开未跟踪目录，默认形态会把整个新目录折叠成一行 `?? dir/` 导致漏列）。
// 空切片 = 工作区干净（无未提交变更）。
func UncommittedChanges(dir string) ([]GitChangeEntry, error) {
	// unborn HEAD（无任何提交）：跳过两段 diff，只采集 untracked。
	numstat, nameStatus := "", ""
	if headExists(dir) {
		// numstat：每行 `ins\tdel\tpath`（binary 为 `-\t-\tpath`），覆盖暂存+工作区 vs HEAD。
		var err error
		numstat, err = gitOutputNoQuote(dir, "diff", "HEAD", "--numstat", "--no-renames")
		if err != nil {
			return nil, err
		}
		// name-status：每行 `X\tpath`（X 为 A/M/D），与 numstat 按 path 对齐补状态字母。
		nameStatus, err = gitOutputNoQuote(dir, "diff", "HEAD", "--name-status", "--no-renames")
		if err != nil {
			return nil, err
		}
	}
	statusByPath := make(map[string]string)
	for _, line := range strings.Split(nameStatus, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		statusByPath[parts[1]] = parts[0]
	}

	entries := make([]GitChangeEntry, 0)
	seen := make(map[string]struct{})
	for _, line := range strings.Split(numstat, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		ins, del := -1, -1 // binary（"-"）保留 -1
		if n, e := strconv.Atoi(parts[0]); e == nil {
			ins = n
		}
		if n, e := strconv.Atoi(parts[1]); e == nil {
			del = n
		}
		path := parts[2]
		entries = append(entries, GitChangeEntry{
			Path:   path,
			Status: statusByPath[path],
			Ins:    ins,
			Del:    del,
		})
		seen[path] = struct{}{}
	}

	// untracked：porcelain -uall 的 "??" 行（git diff 不含未跟踪文件）。行数未知（-uall 展开
	// 目录后无对应 numstat），置 -1 哨兵——前端与 binary 同口径隐藏 `+N/-N` 数字。
	porcelain, err := gitOutputNoQuote(dir, "status", "--porcelain", "-uall")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(porcelain, "\n") {
		if len(line) < 4 || line[:2] != "??" {
			continue
		}
		path := strings.TrimPrefix(line[3:], "\"") // 引号包裹的非常规文件名（含空格/引号等）
		path = strings.TrimSuffix(path, "\"")
		if _, dup := seen[path]; dup {
			continue
		}
		entries = append(entries, GitChangeEntry{Path: path, Status: "U", Ins: -1, Del: -1, IsUntracked: true})
	}
	return entries, nil
}

// ShowHeadFile 读 HEAD 版本的单个文件内容（`git show HEAD:<path>`，quotePath 关闭保证
// 非 ASCII 路径按原样匹配）。文件在 HEAD 中不存在（新增/untracked）返回空串与 false，
// 非错误；其余失败（如非 git 目录）返回 error。
func ShowHeadFile(dir, path string) (string, bool, error) {
	cmdArgs := []string{"-c", "core.quotePath=false", "-C", dir, "show", "HEAD:" + path}
	out, err := exec.Command("git", cmdArgs...).Output()
	if err != nil {
		// git show 对不存在的路径输出 "fatal: path ... does not exist in 'HEAD'" 且退出码 128——
		// 探测式吞错（gitOutput 同款语义）：文件不在 HEAD 即「新增」，返回空。
		return "", false, nil
	}
	return string(out), true, nil
}

// IsGitDir 探测 dir 是否为 git 仓库工作区（.git 子目录存在）。
func IsGitDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}
