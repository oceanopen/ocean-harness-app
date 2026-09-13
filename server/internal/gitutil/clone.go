// gitutil 的 clone/分支写能力（读能力见 gitutil.go）：issueWorkspace cloneRepos 步骤（T1.4）用。
// 与 gitutil.go 的只读风格差异：写操作失败必须携带 stderr 摘要（gitOutput 丢弃 stderr 只适合
// 探测式读取），故独立 gitRunEnv。
package gitutil

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// CloneStrategy 工作空间拉取策略：本期固定 CLONE，worktree 口子留给 T5.3（共享主仓库 +
// git worktree add），届时仅扩展本包、上层接口不变。
type CloneStrategy string

const (
	CLONE_STRATEGY_CLONE    CloneStrategy = "clone"    // 每仓库独立 git clone（本期唯一实现）
	CLONE_STRATEGY_WORKTREE CloneStrategy = "worktree" // 主仓库 + worktree add（T5.3 预留）
)

// gitRunEnv 执行 git args（dir 为空在当前目录跑，clone 到新目录时用），env 为附加环境变量
// （如 GIT_SSH_COMMAND）。失败返回带输出尾部摘要的 error——git 的诊断走 stderr，clone/fetch
// 进度也混在其中，CombinedOutput 一并捕获。
func gitRunEnv(dir string, env []string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s 失败: %v: %s", strings.Join(args, " "), err, outputTail(out))
	}
	return nil
}

// outputTail 截取 git 输出尾部若干行作错误上下文（超长输出截尾防刷屏）。
func outputTail(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// Clone 克隆 url 到 dir（父目录须存在，dir 本身由 git 创建）。branch 非空时 --branch 指定基准
// 分支（远程不存在该分支时 git 自行报错）；depth > 0 时浅克隆。env 附加环境变量（如
// GIT_SSH_COMMAND 指向 workspace 级 ssh config）。
func Clone(url, dir, branch string, depth int, env []string) error {
	args := []string{"clone"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	if depth > 0 {
		args = append(args, "--depth", strconv.Itoa(depth))
	}
	args = append(args, url, dir)
	return gitRunEnv("", env, args...)
}

// RemoteBranchExists 判断 url 远程是否存在 branch 分支（ls-remote --heads，输出非空即存在）。
// 网络/认证失败返回 error——与「分支不存在」区分，调用方分别处理。
func RemoteBranchExists(url, branch string, env []string) (bool, error) {
	cmd := exec.Command("git", "ls-remote", "--heads", url, "refs/heads/"+branch)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git ls-remote %s 失败: %v: %s", url, err, outputTail(out))
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// FetchLatest 拉取 dir 的 origin 最新引用（--prune 清理已删远程分支）。浅克隆仓库 fetch 仍保持
// 浅层，不会自动补全量历史。
func FetchLatest(dir string, env []string) error {
	return gitRunEnv(dir, env, "fetch", "origin", "--prune")
}

// LocalBranchExists 判断 dir 是否存在本地分支 branch（show-ref --verify 静默探测，非 git 目录
// 一律 false）。
func LocalBranchExists(dir, branch string) bool {
	return exec.Command("git", "-C", dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}

// CreateAndCheckoutBranch 从当前 HEAD 创建并切换到新分支（checkout -b，分支已存在时报错）。
func CreateAndCheckoutBranch(dir, branch string) error {
	return gitRunEnv(dir, nil, "checkout", "-b", branch)
}

// CheckoutBranch 切换到已存在的本地分支。
func CheckoutBranch(dir, branch string) error {
	return gitRunEnv(dir, nil, "checkout", branch)
}

// CreateAndCheckoutFromRemote 从 origin/<branch> 创建本地分支并切换（复用半成品目录时，
// 基准分支本地尚不存在但 fetch 后 origin/ 引用已就绪）。
func CreateAndCheckoutFromRemote(dir, branch string) error {
	return gitRunEnv(dir, nil, "checkout", "-b", branch, "origin/"+branch)
}
