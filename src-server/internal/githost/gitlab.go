// gitlab.go 实现 GitLabProvider（含自建 GitLab via 自定义 baseURL），调 GitLab REST API v4。
// 认证：PRIVATE-TOKEN: <token>。project id 用 URL-encoded ownerRepo（含 subgroup 全路径）。

package githost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// gitlabProvider 实现 Provider，调 GitLab REST API v4。
// baseURL 生产用 "https://gitlab.com/api/v4"；自建 GitLab 用 "<host>/api/v4"。
type gitlabProvider struct {
	baseURL   string
	host      string
	ownerRepo string // group/sub/repo（含 subgroup），project id 用 PathEscape 编码
}

// NewGitLabProvider 构造 GitLabProvider。baseURL 为 API 根（如 https://gitlab.com/api/v4）。
func NewGitLabProvider(baseURL, host, ownerRepo string) Provider {
	return &gitlabProvider{baseURL: baseURL, host: host, ownerRepo: ownerRepo}
}

func (p *gitlabProvider) Kind() ProviderKind { return PROVIDER_KIND_GITLAB }
func (p *gitlabProvider) Host() string       { return p.host }
func (p *gitlabProvider) OwnerRepo() string  { return p.ownerRepo }

// projectID 把 ownerRepo（group/sub/repo）编码为 GitLab project id（URL-encoded 全路径：group%2Fsub%2Frepo）。
func (p *gitlabProvider) projectID() string {
	return url.PathEscape(p.ownerRepo)
}

// gitlabMRReq GitLab create MR 请求体。
type gitlabMRReq struct {
	Title        string `json:"title"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	Description  string `json:"description,omitempty"`
}

// gitlabMRResp GitLab create MR 响应（仅取需要的字段）。
type gitlabMRResp struct {
	IID    int    `json:"iid"`
	WebURL string `json:"web_url"`
}

func (p *gitlabProvider) CreatePullRequest(ctx context.Context, req CreatePRRequest) (*PR, error) {
	body, err := json.Marshal(gitlabMRReq{
		Title:        req.Title,
		SourceBranch: req.Head,
		TargetBranch: req.Base,
		Description:  req.Body,
	})
	if err != nil {
		return nil, fmt.Errorf("githost 构造请求体失败: %w", err)
	}
	u := fmt.Sprintf("%s/projects/%s/merge_requests", p.baseURL, p.projectID())
	headers := map[string]string{
		"PRIVATE-TOKEN": req.Token,
		"Accept":        "application/json",
		"Content-Type":  "application/json",
	}
	var resp gitlabMRResp
	if err := doJSON(ctx, http.MethodPost, u, headers, bytes.NewReader(body), &resp); err != nil {
		return nil, err
	}
	return &PR{Number: resp.IID, URL: resp.WebURL}, nil
}

func (p *gitlabProvider) MergePullRequest(ctx context.Context, req MergePRRequest) error {
	// GitLab merge：PUT /projects/{id}/merge_requests/{iid}/merge，无请求体。
	u := fmt.Sprintf("%s/projects/%s/merge_requests/%d/merge", p.baseURL, p.projectID(), req.Number)
	headers := map[string]string{
		"PRIVATE-TOKEN": req.Token,
		"Accept":        "application/json",
	}
	return doJSON(ctx, http.MethodPut, u, headers, nil, nil)
}
