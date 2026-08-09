// naming.go 放 worktree 目录名/分支名的纯字符串解析工具（不调 git、无 IO）。
// 与 gitutil.go（调 git 的读取能力）同包，便于 worktree service 复用。

package gitutil

import "strings"

// RepoNameFromRemoteURL 从 git remote URL 解析仓库名（路径末段，剥 .git 后缀）。
// 覆盖 SSH / HTTPS / GitLab subgroup / 本地 file 路径，统一取最后一段：
//   - git@github.com:org/repo.git            → repo
//   - https://github.com/org/repo.git         → repo
//   - https://gitlab.com/group/sub/repo.git   → repo（subgroup 取末段）
//   - /path/to/repo.git                       → repo
//
// 空串或无法识别返回 ""（调用方决定回退，如 filepath.Base(localDir)）。
func RepoNameFromRemoteURL(remoteURL string) string {
	s := strings.TrimSpace(remoteURL)
	s = strings.TrimSuffix(s, ".git")
	// SSH 的 org/repo 与 HTTPS 的 /org/repo、本地路径 /path/repo 统一按 / 与 : 切分，
	// 取最后一段非空（HTTPS 头的 // 产生的空段被 FieldsFunc 丢弃）。
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == ':'
	})
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}
