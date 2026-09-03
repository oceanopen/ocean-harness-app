package gitutil

import (
	"fmt"
	"net/url"
	"strings"
)

// RemoteRef 是远程 URL 解析产物（T4.1 GitHub MCP 工具的 owner/repo 映射基础）。
// Host 统一小写（URL host 大小写不敏感）；Repo 已剥 .git 后缀。
type RemoteRef struct {
	Host  string
	Owner string
	Repo  string
}

// ParseRemoteURL 从 origin remote URL 原文（t_local_repositories.remote_url 快照）解析
// host/owner/repo。覆盖三种常见形态：
//
//	git@host:owner/repo.git        （scp 风格，实际场景最常见）
//	https://host[:port]/owner/repo.git
//	ssh://git@host[:port]/owner/repo.git
//
// 与 issueWorkspaceSSHHost（service 层，仅出 host、拒协议前缀）定位不同：本函数面向
// 平台适配（需要完整 owner/repo），故三者皆解析。不识别的格式返回中文错误（MCP 工具
// 直接透传给 AI 自我纠正）。owner/repo 之外的路径段（如嵌套 group）不支持——GitHub
// 两段式足够，GitLab 嵌套 group 待后续平台适配时另行扩展。
func ParseRemoteURL(remoteURL string) (RemoteRef, error) {
	raw := strings.TrimSpace(remoteURL)
	if raw == "" {
		return RemoteRef{}, fmt.Errorf("远程 URL 为空（仓库无 origin 远程）")
	}

	var host, path string
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return RemoteRef{}, fmt.Errorf("解析远程 URL 失败: %q: %w", raw, err)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "ssh", "git":
		default:
			return RemoteRef{}, fmt.Errorf("不支持的远程 URL 协议: %q（支持 https/ssh/git）", u.Scheme)
		}
		// u.Hostname() 已剥 port 与 userinfo；scp 风格单独处理（见下）。
		host, path = u.Hostname(), strings.Trim(u.Path, "/")
	} else {
		// scp 风格 user@host:owner/repo.git（与 issueWorkspaceSSHHost 同款切片手法，
		// 但额外产出 owner/repo）。首段 @ 前是 user，@ 后到首个 : 是 host。
		at := strings.IndexByte(raw, '@')
		if at < 0 {
			return RemoteRef{}, fmt.Errorf("无法解析远程 URL: %q（期望 git@host:owner/repo 或 https://host/owner/repo 形态）", raw)
		}
		rest := raw[at+1:]
		colon := strings.IndexByte(rest, ':')
		if colon <= 0 {
			return RemoteRef{}, fmt.Errorf("无法解析远程 URL: %q（scp 风格缺少 host:path 冒号分隔）", raw)
		}
		host, path = rest[:colon], rest[colon+1:]
	}

	host = strings.ToLower(strings.TrimSpace(host))
	owner, repo, ok := strings.Cut(strings.TrimSuffix(strings.Trim(path, "/"), ".git"), "/")
	if host == "" || !ok || owner == "" || repo == "" {
		return RemoteRef{}, fmt.Errorf("无法从远程 URL 解析 owner/repo: %q", raw)
	}
	// 残留分隔符拦截：scp 双冒号（git@host:22:o/r → owner "22:o"）与嵌套 group
	//（gitlab 形态 a/b/c → repo "b/c"）在此给出中文错误而非让远端报莫名的 404。
	if strings.ContainsAny(owner, ":/") || strings.ContainsAny(repo, ":/") {
		return RemoteRef{}, fmt.Errorf("无法从远程 URL 解析 owner/repo: %q（不支持的路径形态：scp 带端口或嵌套 group，后者待后续平台适配支持）", raw)
	}
	return RemoteRef{Host: host, Owner: owner, Repo: repo}, nil
}
