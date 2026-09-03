package mcpdto

// —— GitHub 工具（T4.1）入参 Args ——
// 仓库定位链（三工具共用）：localRepositoryId → t_local_repositories.remote_url →
// gitutil.ParseRemoteURL 解析 host/owner/repo → host 适配分流（本期仅支持 github.com，
// 其余 host 报「暂未支持」，判定集中 mcp_tool.githubTarget）。

// GitHubCreatePRArgs 是 github_create_pr 的入参。head/base 缺省按 agent 开发工作流推导：
// head = agent_{issueId}（工作空间统一目标分支），base = 该 issue 关联的此仓库基准分支。
type GitHubCreatePRArgs struct {
	LocalRepositoryID int    `json:"localRepositoryId" jsonschema:"本地仓库 id（PR 目标仓库，按其 remote_url 定位）" vd:"@:$>0; msg:'localRepositoryId 必须为正整数'"`
	IssueID           string `json:"issueId" jsonschema:"issue 主键（uuid 文本，用于 head/base 缺省推导：head=agent_{issueId}，base=该 issue 关联的此仓库基准分支）" vd:"@:$!=''; msg:'issueId 不能为空'"`
	Title             string `json:"title" jsonschema:"PR 标题（建议基于变更内容生成，动词开头简述意图）" vd:"@:$!=''; msg:'title 不能为空'"`
	Body              string `json:"body,omitempty" jsonschema:"PR 描述（Markdown：变更点列表 + 测试计划），可留空"`
	Head              string `json:"head,omitempty" jsonschema:"源分支（head），留空默认 agent_{issueId}"`
	Base              string `json:"base,omitempty" jsonschema:"目标分支（base），留空默认该 issue 关联的此仓库基准分支"`
}

// GitHubListPRsArgs 是 github_list_prs 的入参。
type GitHubListPRsArgs struct {
	LocalRepositoryID int    `json:"localRepositoryId" jsonschema:"本地仓库 id（按其 remote_url 定位 GitHub 仓库）" vd:"@:$>0; msg:'localRepositoryId 必须为正整数'"`
	State             string `json:"state,omitempty" jsonschema:"PR 状态过滤，留空默认 open。可选 open/closed/all"`
}

// GitHubCIStatusArgs 是 github_ci_status 的入参。
type GitHubCIStatusArgs struct {
	LocalRepositoryID int `json:"localRepositoryId" jsonschema:"本地仓库 id（按其 remote_url 定位 GitHub 仓库）" vd:"@:$>0; msg:'localRepositoryId 必须为正整数'"`
	PullNumber        int `json:"pullNumber" jsonschema:"PR 编号（github_create_pr 返回的 number，或 github_list_prs 列表项的 number）" vd:"@:$>0; msg:'pullNumber 必须为正整数'"`
}

// —— GitHub 工具出参 Content（平铺镜像，见包注释） ——

// GitHubPullRequestContent 是单个 PR 的出参（github_create_pr 返回）。
type GitHubPullRequestContent struct {
	Number       int    `json:"number" jsonschema:"PR 编号"`
	Title        string `json:"title" jsonschema:"PR 标题"`
	State        string `json:"state" jsonschema:"状态：open/closed"`
	HeadRef      string `json:"headRef" jsonschema:"源分支"`
	BaseRef      string `json:"baseRef" jsonschema:"目标分支"`
	HTMLURL      string `json:"htmlUrl" jsonschema:"PR 浏览器地址"`
	RepositoryID int    `json:"localRepositoryId" jsonschema:"对应本地仓库 id"`
}

// GitHubPullRequestListContent 是 github_list_prs 的出参（顶层对象，列表包在字段内）。
type GitHubPullRequestListContent struct {
	Pulls        []*GitHubPullRequestContent `json:"pulls" jsonschema:"PR 列表（最多 50 条）；无 PR 为空数组"`
	RepositoryID int                         `json:"localRepositoryId" jsonschema:"对应本地仓库 id"`
}

// GitHubCIStatusContent 是 github_ci_status 的出参。
type GitHubCIStatusContent struct {
	PullNumber   int                     `json:"pullNumber" jsonschema:"PR 编号"`
	State        string                  `json:"state" jsonschema:"CI 汇总结论：success(全部通过)/failure(有失败)/pending(进行中或无 CI)"`
	Checks       []GitHubCIRecordContent `json:"checks" jsonschema:"逐项检查列表（GitHub Actions check runs 与旧 commit status 归并）；无 CI 为空数组"`
	RepositoryID int                     `json:"localRepositoryId" jsonschema:"对应本地仓库 id"`
}

// GitHubCIRecordContent 是单条 CI 检查出参项。
type GitHubCIRecordContent struct {
	Name       string `json:"name" jsonschema:"检查名（如 workflow / job 名）"`
	Status     string `json:"status" jsonschema:"执行状态：queued/in_progress/completed"`
	Conclusion string `json:"conclusion,omitempty" jsonschema:"结论（status=completed 时有值）：success/failure/neutral/cancelled/skipped/timed_out"`
	HTMLURL    string `json:"htmlUrl,omitempty" jsonschema:"检查详情地址"`
}
