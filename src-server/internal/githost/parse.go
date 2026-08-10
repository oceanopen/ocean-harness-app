// parse.go 放 git remote URL 的纯字符串解析（不调网络、无 IO）。
// 忠实复刻前端 src/shared/gitRemote.ts parseRemoteUrl（含 GitLab subgroup）。

package githost

import (
	"net/url"
	"regexp"
	"strings"
)

// sshRemoteRE 匹配 SSH 形式 remote：git@host:owner/repo(.git)?
// 非贪婪 ownerRepo（.+?）+ 末尾 $ 锚定：引擎让捕获组尽量短，但 $ 强制完整匹配，
// 故 ownerRepo 最终扩张到「末尾 .git 之前」的全部内容，GitLab subgroup（group/sub/repo）完整保留。
var sshRemoteRE = regexp.MustCompile(`^git@([^:]+):(.+?)(?:\.git)?$`)

// ParseRemoteURL 解析 git remote URL（SSH/HTTPS）为 host + ownerRepo。
//   - SSH:   git@github.com:org/repo.git        → host="github.com", ownerRepo="org/repo"
//   - HTTPS: https://github.com/org/repo.git    → 同上
//   - GitLab subgroup: git@gitlab.com:group/sub/repo.git → ownerRepo="group/sub/repo"
//
// 无法解析（空串、纯本地路径、非 URL）返回 ok=false。
// HTTPS 末尾斜杠被清除（修前端 gitRemote.ts 未清尾斜杠的瑕疵）。
func ParseRemoteURL(remoteURL string) (host, ownerRepo string, ok bool) {
	s := strings.TrimSpace(remoteURL)
	if s == "" {
		return "", "", false
	}
	// SSH 形式：git@host:owner/repo(.git)?
	if m := sshRemoteRE.FindStringSubmatch(s); m != nil {
		return m[1], m[2], true
	}
	// HTTPS 形式：scheme://host/owner/repo(.git)?
	u, err := url.Parse(s)
	if err == nil && u.Scheme != "" && u.Host != "" {
		ownerRepo := strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/")
		if ownerRepo != "" {
			return u.Host, ownerRepo, true
		}
	}
	return "", "", false
}
