package githubapi

import (
	"fmt"
	"net/http"
	"net/url"
)

// PullRequest 是 GitHub PR 响应的最小字段集（**wire format**：snake_case + head/base
// 嵌套对象——tag 必须与 GitHub REST v3 实际响应逐字段对齐，出参平铺由 mcp_tool 转换层
// 承担）。HeadRef/BaseRef 是 head.ref / base.ref 的平铺只读视图（MCP 出参消费）。
type PullRequest struct {
	Number  int      `json:"number"`
	Title   string   `json:"title"`
	State   string   `json:"state"` // open / closed
	HTMLURL string   `json:"html_url"`
	Head    PRBranch `json:"head"`
	Base    PRBranch `json:"base"`
}

// PRBranch 是 PR 的 head/base 分支对象（wire 嵌套形态）。
type PRBranch struct {
	Ref string `json:"ref"`
}

// HeadRef / BaseRef 是嵌套 ref 的平铺视图。
func (pr PullRequest) HeadRef() string { return pr.Head.Ref }
func (pr PullRequest) BaseRef() string { return pr.Base.Ref }

// CreatePullRequestInput 是创建 PR 入参（head/base 方向：head → base）。
type CreatePullRequestInput struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

// CreatePullRequest 创建 PR（POST /repos/{owner}/{repo}/pulls）。
func (c *Client) CreatePullRequest(owner, repo string, in CreatePullRequestInput) (*PullRequest, error) {
	var out PullRequest
	if err := c.doRequest(http.MethodPost, fmt.Sprintf("/repos/%s/%s/pulls", owner, repo), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListPullRequests 列出仓库 PR（GET /repos/{owner}/{repo}/pulls）。state 取 open/closed/all
// （空串按 GitHub 默认 open）；最多返回 50 条（MCP 出参裁剪，防大仓库刷屏）。
func (c *Client) ListPullRequests(owner, repo, state string) ([]PullRequest, error) {
	if state == "" {
		state = "open"
	}
	var out []PullRequest
	path := fmt.Sprintf("/repos/%s/%s/pulls?state=%s&per_page=50", owner, repo, url.QueryEscape(state))
	if err := c.doRequest(http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CIStatus 是 PR 提交点的 CI 汇总结论：State 为两体系归并结果
// （success/failure/pending），Checks 为逐项检查（GitHub Actions check runs 与旧 commit
// status 归并为同一形态）。归并规则见 mergeCIState。
type CIStatus struct {
	PullNumber int
	State      string     `json:"state"` // success / failure / pending（pending 含「无任何 CI」）
	Checks     []CIRecord `json:"checks"`
}

// CIRecord 是单条检查项（check run 与旧 status 归并：Status 取 check run 的
// queued/in_progress/completed，Conclusion 取其结论；旧体系恒 completed + state 作结论）。
type CIRecord struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"htmlUrl"`
}

// GetPullRequestCIStatus 获取指定 PR 的 CI 检查状态。链路：PR → head sha →
// combined status（旧体系）+ check runs（新体系，GitHub Actions）并行取并集。
// 单体系查询失败不阻断（另一体系仍可用）；两体系都失败返回错误（不把「查询失败」
// 伪装成「CI 进行中」）。
func (c *Client) GetPullRequestCIStatus(owner, repo string, pullNumber int) (*CIStatus, error) {
	// ① PR 详情取 head sha（head.sha 不在 PullRequest 最小集，单独声明）。
	var prDetail struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := c.doRequest(http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, pullNumber), nil, &prDetail); err != nil {
		return nil, err
	}
	sha := prDetail.Head.SHA
	if sha == "" {
		return nil, fmt.Errorf("PR #%d 无 head 提交（可能刚创建尚未同步），稍后重试", pullNumber)
	}

	status := &CIStatus{PullNumber: pullNumber}

	// ② combined status（旧体系）：{state, statuses[]}。失败记为无数据（不参与归并）。
	var combinedState string
	var combined struct {
		State    string `json:"state"`
		Statuses []struct {
			Context   string `json:"context"`
			State     string `json:"state"`
			TargetURL string `json:"target_url"`
		} `json:"statuses"`
	}
	combinedErr := c.doRequest(http.MethodGet, fmt.Sprintf("/repos/%s/%s/commits/%s/status", owner, repo, sha), nil, &combined)
	if combinedErr == nil {
		combinedState = combined.State
		if len(combined.Statuses) == 0 {
			// 空集归一为无数据：GitHub 对无任何 commit status 的提交也返回 pending，
			// 但「旧体系无 CI」不应拖累纯 Actions 仓库的 success 判定。
			combinedState = ""
		}
		for _, s := range combined.Statuses {
			status.Checks = append(status.Checks, CIRecord{Name: s.Context, Status: "completed", Conclusion: s.State, HTMLURL: s.TargetURL})
		}
	}

	// ③ check runs（新体系）：{check_runs[]}。
	var checks struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
		} `json:"check_runs"`
	}
	checksErr := c.doRequest(http.MethodGet, fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", owner, repo, sha), nil, &checks)
	if checksErr == nil {
		for _, cr := range checks.CheckRuns {
			status.Checks = append(status.Checks, CIRecord{Name: cr.Name, Status: cr.Status, Conclusion: cr.Conclusion, HTMLURL: cr.HTMLURL})
		}
	}

	// 两体系都失败：如实报错（含各自原因）。
	if combinedErr != nil && checksErr != nil {
		return nil, fmt.Errorf("查询 CI 状态失败（commit status: %v; check runs: %v）", combinedErr, checksErr)
	}
	status.State = mergeCIState(combinedState, status.Checks)
	return status, nil
}

// mergeCIState 归并两体系的 CI 结论（combined status 的 state + check runs 逐项）：
//
//	两体系均无数据（combinedState 空串 = 查询失败或空集归一，且 checks 为空）→ pending（无 CI）
//	failure/error 任一命中（旧体系 state 或任一 check run 非中性失败结论）→ failure
//	存在未完成 check run（queued/in_progress），或旧体系有数据的 pending → pending
//	全部完成且无失败 → success（旧体系无数据不拖累纯 Actions 仓库）
//
// neutral/skipped 视为中性不阻断 success。
func mergeCIState(combinedState string, checks []CIRecord) string {
	if combinedState == "" && len(checks) == 0 {
		return "pending" // 无任何 CI
	}
	failure := combinedState == "failure" || combinedState == "error"
	allCompleted := true
	for _, c := range checks {
		if c.Status != "completed" {
			allCompleted = false
			continue
		}
		switch c.Conclusion {
		case "success", "neutral", "skipped":
		default:
			failure = true
		}
	}
	switch {
	case failure:
		return "failure"
	case !allCompleted || combinedState == "pending":
		return "pending"
	default:
		return "success"
	}
}
