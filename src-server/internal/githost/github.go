// github.go 实现 GitHubProvider（含 GitHub Enterprise via 自定义 baseURL），调 GitHub REST API v3。
// 认证：Authorization: Bearer <token>；Accept: application/vnd.github+json（v3 规范）。

package githost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// githubProvider 实现 Provider，调 GitHub REST API。
// baseURL 生产用 "https://api.github.com"；GitHub Enterprise 用 "<ghe>/api/v3"（1.5 自建配置传入）。
type githubProvider struct {
	baseURL   string // API 根（无尾斜杠）
	host      string // 展示用 remote host（github.com / ghe.company.com）
	ownerRepo string // owner/repo（URL path 直接拼接）
}

// NewGitHubProvider 构造 GitHubProvider。baseURL 为 API 根（如 https://api.github.com）。
func NewGitHubProvider(baseURL, host, ownerRepo string) Provider {
	return &githubProvider{baseURL: baseURL, host: host, ownerRepo: ownerRepo}
}

func (p *githubProvider) Kind() ProviderKind { return PROVIDER_KIND_GITHUB }
func (p *githubProvider) Host() string       { return p.host }
func (p *githubProvider) OwnerRepo() string  { return p.ownerRepo }

// githubPRReq GitHub create PR 请求体。
type githubPRReq struct {
	Title string `json:"title"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Body  string `json:"body,omitempty"`
}

// githubPRResp GitHub create PR 响应（仅取需要的字段）。
type githubPRResp struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

func (p *githubProvider) CreatePullRequest(ctx context.Context, req CreatePRRequest) (*PR, error) {
	body, err := json.Marshal(githubPRReq{Title: req.Title, Head: req.Head, Base: req.Base, Body: req.Body})
	if err != nil {
		return nil, fmt.Errorf("githost 构造请求体失败: %w", err)
	}
	u := fmt.Sprintf("%s/repos/%s/pulls", p.baseURL, p.ownerRepo)
	headers := map[string]string{
		"Authorization": "Bearer " + req.Token,
		"Accept":        "application/vnd.github+json",
		"Content-Type":  "application/json",
	}
	var resp githubPRResp
	if err := doJSON(ctx, http.MethodPost, u, headers, bytes.NewReader(body), &resp); err != nil {
		return nil, err
	}
	return &PR{Number: resp.Number, URL: resp.HTMLURL}, nil
}

func (p *githubProvider) MergePullRequest(ctx context.Context, req MergePRRequest) error {
	// GitHub merge：PUT /repos/{owner}/{repo}/pulls/{number}/merge，默认 merge commit，无请求体。
	u := fmt.Sprintf("%s/repos/%s/pulls/%d/merge", p.baseURL, p.ownerRepo, req.Number)
	headers := map[string]string{
		"Authorization": "Bearer " + req.Token,
		"Accept":        "application/vnd.github+json",
	}
	return doJSON(ctx, http.MethodPut, u, headers, nil, nil)
}
