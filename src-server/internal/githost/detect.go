// detect.go 放 DetectProvider：按 remote host 识别 git 托管平台并返回绑定了该 repo 的 Provider。
// 精确匹配 github.com / gitlab.com；未识别 host 或无法解析 remote 返回错误。
// 自建 GHE/GitLab 由调用方（1.5）按用户配置手动 NewGitHubProvider/NewGitLabProvider + 显式 baseURL 构造。

package githost

import (
	"errors"
	"fmt"
)

// 生产默认 API baseURL。
const (
	githubDefaultBaseURL = "https://api.github.com"
	gitlabDefaultBaseURL = "https://gitlab.com/api/v4"
)

// DetectProvider 按 remoteURL 的 host 识别平台并返回绑定了该 repo 的 Provider。
//   - github.com → GitHubProvider（api.github.com）
//   - gitlab.com → GitLabProvider（gitlab.com/api/v4）
//
// 其他 host（自建 GHE/GitLab）返回错误：从 host 无法确定平台类型（GitHub 与 GitLab API 形态不同），
// 由调用方按用户配置手动构造。无法解析 remote（空串/本地路径）同样返回错误。
func DetectProvider(remoteURL string) (Provider, error) {
	host, ownerRepo, ok := ParseRemoteURL(remoteURL)
	if !ok {
		return nil, errors.New("无法解析 git remote URL")
	}
	switch host {
	case "github.com":
		return NewGitHubProvider(githubDefaultBaseURL, host, ownerRepo), nil
	case "gitlab.com":
		return NewGitLabProvider(gitlabDefaultBaseURL, host, ownerRepo), nil
	default:
		return nil, fmt.Errorf("不支持的平台 host：%s（仅支持 github.com / gitlab.com，自建请在设置中指定平台与 API 地址）", host)
	}
}
