// gitutil 的工作区安全检查能力（issueWorkspace archive 归档/取消前置检查，T3.2）。
// 与 gitutil.go 的吞错探测风格不同：本文件函数服务于「删目录前的安全检查」——
// 检查失败必须显式报错（把「命令失败」误判为「工作区干净」会导致带未提交变更的
// 目录被静默删除），故错误携带输出摘要（同 clone.go 的 gitRunEnv 风格）。
package gitutil

import (
	"fmt"
	"os/exec"
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
