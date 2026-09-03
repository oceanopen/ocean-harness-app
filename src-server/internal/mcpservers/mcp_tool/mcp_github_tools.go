package mcptool

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"ocean-harness/src-server/internal/githubapi"
	"ocean-harness/src-server/internal/gitutil"
	mcpdto "ocean-harness/src-server/internal/mcpservers/mcp_dto"
	"ocean-harness/src-server/internal/mcpservers/mcp_util"
	"ocean-harness/src-server/internal/service"
)

// githubHost 是本期唯一支持的托管平台 host（「各 host 单独适配」原则）。多平台扩展点
// 单点收敛在 githubTarget：后续 gitee/gitlab 落地时在此按 host 分流到各自 api 客户端
// （各平台 PAT key 的读取分流同在此点），三个 handler 面向返回的 client 编排、无需感知平台。
const githubHost = "github.com"

// McpGithubTool 是 github server 的工具集合（T4.1）。
type McpGithubTool struct {
	mcputil.McpTool
}

// githubTarget 是三工具共用的定位链：仓库 id → remote_url → URL 解析 → host 适配
// 判定 → PAT 读取 → API 客户端构造。issueID 非空时顺带做 issue 关联校验并返回
// 分支推导上下文（create_pr 的 base 两级回退：issue 关联基准分支 → 仓库默认分支）。
func (mt McpGithubTool) githubTarget(repoSvc *service.GithubTool, issueID string, localRepositoryID int) (
	ref gitutil.RemoteRef, client *githubapi.Client, branches service.GithubRepoRef, err error) {

	repo, err := repoSvc.ResolveRepo(issueID, localRepositoryID)
	if err != nil {
		return ref, nil, branches, err
	}
	parsed, err := gitutil.ParseRemoteURL(repo.RemoteURL)
	if err != nil {
		return ref, nil, branches, err
	}
	if parsed.Host != githubHost {
		return ref, nil, branches, fmt.Errorf(
			"github 工具暂仅支持 github.com 仓库（当前 host: %s），gitee/gitlab 等平台待后续支持", parsed.Host)
	}
	pat, err := mcputil.ReadGithubPAT()
	if err != nil {
		return ref, nil, branches, err
	}
	return parsed, githubapi.NewClient(pat), *repo, nil
}

// CreatePullRequest 创建 PR（github_create_pr）。head/base 缺省按 agent 工作流推导：
// head = agent_{issueId}（工作空间统一目标分支），base = issue 关联基准分支 → 仓库默认分支。
func (mt McpGithubTool) CreatePullRequest(ctx context.Context, _ *mcp.ServerSession,
	params *mcp.CallToolParamsFor[mcpdto.GitHubCreatePRArgs]) (*mcp.CallToolResultFor[mcpdto.GitHubPullRequestContent], error) {

	args := params.Arguments
	repoSvc := service.GithubTool{}
	if err := mt.MakeContext(ctx).Validate(&args).MakeService(&repoSvc.Service).Errors; err != nil {
		return mcputil.McpFail[mcpdto.GitHubPullRequestContent](err)
	}
	ref, client, branches, err := mt.githubTarget(&repoSvc, args.IssueID, args.LocalRepositoryID)
	if err != nil {
		return mcputil.McpFail[mcpdto.GitHubPullRequestContent](err)
	}
	head := args.Head
	if head == "" {
		head = "agent_" + args.IssueID
	}
	// base 两级回退（与 clone 步骤同款语义：关联基准分支为空 = 仓库默认分支）。
	base := args.Base
	if base == "" {
		base = branches.BaseBranch
	}
	if base == "" {
		base = branches.DefaultBranch
	}
	if base == "" {
		return mcputil.McpFail[mcpdto.GitHubPullRequestContent](
			errors.New("无法推导 base 分支（issue 关联与仓库默认分支均为空），请显式传入 base"))
	}

	pr, err := client.CreatePullRequest(ref.Owner, ref.Repo, githubapi.CreatePullRequestInput{
		Title: args.Title,
		Body:  args.Body,
		Head:  head,
		Base:  base,
	})
	if err != nil {
		return mcputil.McpFail[mcpdto.GitHubPullRequestContent](err)
	}
	return mcputil.McpOK(newGithubPullRequestContent(pr, args.LocalRepositoryID))
}

// ListPullRequests 列出仓库 PR（github_list_prs，state 缺省 open，最多 50 条）。
func (mt McpGithubTool) ListPullRequests(ctx context.Context, _ *mcp.ServerSession,
	params *mcp.CallToolParamsFor[mcpdto.GitHubListPRsArgs]) (*mcp.CallToolResultFor[mcpdto.GitHubPullRequestListContent], error) {

	args := params.Arguments
	repoSvc := service.GithubTool{}
	if err := mt.MakeContext(ctx).Validate(&args).MakeService(&repoSvc.Service).Errors; err != nil {
		return mcputil.McpFail[mcpdto.GitHubPullRequestListContent](err)
	}
	ref, client, _, err := mt.githubTarget(&repoSvc, "", args.LocalRepositoryID)
	if err != nil {
		return mcputil.McpFail[mcpdto.GitHubPullRequestListContent](err)
	}

	prs, err := client.ListPullRequests(ref.Owner, ref.Repo, args.State)
	if err != nil {
		return mcputil.McpFail[mcpdto.GitHubPullRequestListContent](err)
	}
	pulls := make([]*mcpdto.GitHubPullRequestContent, 0, len(prs))
	for i := range prs {
		pulls = append(pulls, prContentPtr(newGithubPullRequestContent(&prs[i], args.LocalRepositoryID)))
	}
	return mcputil.McpOK(mcpdto.GitHubPullRequestListContent{Pulls: pulls, RepositoryID: args.LocalRepositoryID})
}

// PullRequestCIStatus 获取 PR 的 CI 检查状态（github_ci_status：combined status +
// check runs 归并，规则见 githubapi.mergeCIState）。
func (mt McpGithubTool) PullRequestCIStatus(ctx context.Context, _ *mcp.ServerSession,
	params *mcp.CallToolParamsFor[mcpdto.GitHubCIStatusArgs]) (*mcp.CallToolResultFor[mcpdto.GitHubCIStatusContent], error) {

	args := params.Arguments
	repoSvc := service.GithubTool{}
	if err := mt.MakeContext(ctx).Validate(&args).MakeService(&repoSvc.Service).Errors; err != nil {
		return mcputil.McpFail[mcpdto.GitHubCIStatusContent](err)
	}
	ref, client, _, err := mt.githubTarget(&repoSvc, "", args.LocalRepositoryID)
	if err != nil {
		return mcputil.McpFail[mcpdto.GitHubCIStatusContent](err)
	}

	st, err := client.GetPullRequestCIStatus(ref.Owner, ref.Repo, args.PullNumber)
	if err != nil {
		return mcputil.McpFail[mcpdto.GitHubCIStatusContent](err)
	}
	checks := make([]mcpdto.GitHubCIRecordContent, 0, len(st.Checks))
	for _, c := range st.Checks {
		checks = append(checks, mcpdto.GitHubCIRecordContent{
			Name:       c.Name,
			Status:     c.Status,
			Conclusion: c.Conclusion,
			HTMLURL:    c.HTMLURL,
		})
	}
	return mcputil.McpOK(mcpdto.GitHubCIStatusContent{
		PullNumber:   st.PullNumber,
		State:        st.State,
		Checks:       checks,
		RepositoryID: args.LocalRepositoryID,
	})
}

// —— Content 转换（githubapi 出参 → mcp_dto 平铺镜像）——

func newGithubPullRequestContent(pr *githubapi.PullRequest, repositoryID int) mcpdto.GitHubPullRequestContent {
	return mcpdto.GitHubPullRequestContent{
		Number:       pr.Number,
		Title:        pr.Title,
		State:        pr.State,
		HeadRef:      pr.HeadRef(),
		BaseRef:      pr.BaseRef(),
		HTMLURL:      pr.HTMLURL,
		RepositoryID: repositoryID,
	}
}

func prContentPtr(c mcpdto.GitHubPullRequestContent) *mcpdto.GitHubPullRequestContent {
	return &c
}
