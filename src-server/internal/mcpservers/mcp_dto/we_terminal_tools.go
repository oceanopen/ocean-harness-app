// Package mcpdto 定义 MCP 工具的入参（Args）与出参（Content）DTO。
//
// 三 tag 范式（承 pros-admin-server 惯例）：json（协议字段名，camelCase）+ jsonschema
// （字段中文描述——SDK 从泛型反射推导 Input/Output Schema 时写入 description，AI 可见）
// + vd（go-tagexpr 校验，跨字段/复杂规则用）。
//
// 出参 Content 为 AI 消费视角的平铺镜像（不复用 types.XxxResponseData：其嵌入 DO 与
// time.Time 字段反射推导出的 schema 失真——嵌入结构不平铺、时间类型变 object），时间一律
// 转 RFC3339 字符串，全字段带中文描述。
package mcpdto

import "we-claude-terminal/go-server/internal/dal/enums"

// —— 入参 Args ——

// IssueIDArgs 仅需 issueId 的工具共用（issue_get_info / workspace_status）。
// required 由反射规则推导：无 omitempty 的字段自动进 required，故无需手写 InputSchema。
type IssueIDArgs struct {
	IssueID string `json:"issueId" jsonschema:"issue 主键（uuid 文本）" vd:"@:$!=''; msg:'issueId 不能为空'"`
}

// IssueUpdateArgs 部分更新入参（issue_update / issue_child_update 共用，契约一致）：
// 三业务字段全可选，空 = 保留原值（首期不支持经 MCP 置空字段）。
type IssueUpdateArgs struct {
	IssueID     string          `json:"issueId" jsonschema:"目标 issue 主键（uuid 文本）" vd:"@:$!=''; msg:'issueId 不能为空'"`
	StateCode   enums.StateCode `json:"stateCode,omitempty" jsonschema:"目标状态，留空不改。可选 BACKLOG/TODO/IN_PROGRESS/DONE/CANCELLED；DONE 记录完成时间，全部子任务完成后父任务自动完成，父任务状态变化会级联同步子任务"`
	Name        string          `json:"name,omitempty" jsonschema:"新标题，留空不改（不支持置空）"`
	Description string          `json:"description,omitempty" jsonschema:"新描述（Markdown），留空不改（不支持置空）"`
}

// IssueChildListArgs 是 issue_child_list 的入参。
type IssueChildListArgs struct {
	IssueID string `json:"issueId" jsonschema:"父 issue 主键（uuid 文本）" vd:"@:$!=''; msg:'issueId 不能为空'"`
}

// IssueChildCreateArgs 是 issue_child_create 的入参（项目/工作空间归属自动继承父任务）。
type IssueChildCreateArgs struct {
	IssueID     string          `json:"issueId" jsonschema:"父 issue 主键（uuid 文本）" vd:"@:$!=''; msg:'issueId 不能为空'"`
	Name        string          `json:"name" jsonschema:"子任务标题" vd:"@:$!=''; msg:'name 不能为空'"`
	Description string          `json:"description,omitempty" jsonschema:"子任务描述（Markdown），可留空"`
	StateCode   enums.StateCode `json:"stateCode,omitempty" jsonschema:"初始状态，留空默认 BACKLOG"`
}

// WorkspaceStatusArgs 是 workspace_status 的入参（baseDir 不入参，取应用设置）。
type WorkspaceStatusArgs struct {
	IssueID string `json:"issueId" jsonschema:"issue 主键（uuid 文本）" vd:"@:$!=''; msg:'issueId 不能为空'"`
}

// —— 出参 Content（平铺镜像，见包注释） ——

// IssueContent 是单个 issue 的出参（issue_get_info 返回单对象；issue_child_create 返回新建对象）。
type IssueContent struct {
	ID                   string                 `json:"id" jsonschema:"issue 主键（uuid）"`
	Name                 string                 `json:"name" jsonschema:"标题"`
	Description          string                 `json:"description,omitempty" jsonschema:"描述（Markdown）"`
	StateCode            string                 `json:"stateCode" jsonschema:"状态：BACKLOG/TODO/IN_PROGRESS/DONE/CANCELLED"`
	Priority             string                 `json:"priority" jsonschema:"优先级：urgent/high/medium/low/none"`
	ParentID             string                 `json:"parentId,omitempty" jsonschema:"父 issue 主键；留空表示顶级任务"`
	IsDraft              string                 `json:"isDraft" jsonschema:"是否草稿：Y/N"`
	StartDate            string                 `json:"startDate,omitempty" jsonschema:"开始日期（yyyy-mm-dd），留空表示未设置"`
	TargetDate           string                 `json:"targetDate,omitempty" jsonschema:"目标日期（yyyy-mm-dd），留空表示未设置"`
	CompletedAt          string                 `json:"completedAt,omitempty" jsonschema:"完成时间（RFC3339），留空表示未完成"`
	SortOrder            float64                `json:"sortOrder" jsonschema:"看板排序权重（升序）"`
	Labels               []IssueLabelContent    `json:"labels" jsonschema:"标签列表"`
	RepositoryBranchList []IssueRepoBranchContent `json:"repositoryBranchList" jsonschema:"关联仓库与基准分支列表"`
}

// IssueLabelContent 是 issue 标签的出参项。
type IssueLabelContent struct {
	ID    int    `json:"id" jsonschema:"标签 id"`
	Name  string `json:"name" jsonschema:"标签名"`
	Color string `json:"color,omitempty" jsonschema:"标签颜色（如 #ff5050，留空表示无色）"`
}

// IssueRepoBranchContent 是 issue 关联仓库+分支的出参项。
type IssueRepoBranchContent struct {
	LocalRepositoryID int    `json:"localRepositoryId" jsonschema:"本地仓库 id"`
	RepositoryBranch  string `json:"repositoryBranch,omitempty" jsonschema:"基准分支名（工作空间初始化按此 clone），留空表示仓库默认分支"`
}

// IssueChildListContent 是 issue_child_list 的出参（顶层须为对象，列表包在字段内）。
type IssueChildListContent struct {
	Children []*IssueContent `json:"children" jsonschema:"子任务列表（按看板顺序）；无子任务为空数组"`
}

// WorkspaceStatusContent 是 workspace_status 的出参。
type WorkspaceStatusContent struct {
	ServerStatus string                `json:"serverStatus" jsonschema:"顶层结论：NOT_INITIALIZED(未初始化)/PENDING/RUNNING(初始化进行中)/SUCCESS(就绪)/FAILED(失败可重试)/CORRUPTED(状态文件损坏)/INTERRUPTED(进程中断遗留可重试)"`
	State        *WorkspaceStateContent `json:"state,omitempty" jsonschema:"状态文件全文（serverStatus 为 NOT_INITIALIZED 时缺失）"`
}

// WorkspaceStateContent 是 .workspace-init-state.json 的镜像。
type WorkspaceStateContent struct {
	Version   int                     `json:"version" jsonschema:"schema 版本"`
	IssueID   string                  `json:"issueId" jsonschema:"issue 主键"`
	BaseDir   string                  `json:"baseDir" jsonschema:"工作空间根目录（绝对路径；各仓库在 {baseDir}/{issueId}/repo/{仓库名}）"`
	Status    string                  `json:"status" jsonschema:"顶层状态（含义同 serverStatus，另含 SKIPPED）"`
	Steps     []*WorkspaceStepContent `json:"steps" jsonschema:"各初始化步骤进度（固定顺序：createDirs→sshConfig→cloneRepos）"`
	Manifest  []*WorkspaceRepoRefContent `json:"manifest" jsonschema:"已受理的仓库+基准分支清单（幂等键）"`
	Error     string                  `json:"error,omitempty" jsonschema:"顶层失败原因"`
	CreatedAt string                  `json:"createdAt" jsonschema:"受理时间（RFC3339）"`
	UpdatedAt string                  `json:"updatedAt" jsonschema:"最后更新时间（RFC3339）"`
}

// WorkspaceStepContent 是单个初始化步骤的进度镜像。
type WorkspaceStepContent struct {
	Key     string                     `json:"key" jsonschema:"步骤 key：createDirs/sshConfig/cloneRepos"`
	Title   string                     `json:"title" jsonschema:"步骤中文名"`
	Status  string                     `json:"status" jsonschema:"步骤状态：PENDING/RUNNING/SUCCESS/FAILED/SKIPPED(本期未实现占位)"`
	Repos   []*WorkspaceRepoStateContent `json:"repos,omitempty" jsonschema:"仓库级子状态（仅 cloneRepos 步骤）"`
	Message string                     `json:"message,omitempty" jsonschema:"步骤说明（如 SKIPPED 原因、失败信息）"`
}

// WorkspaceRepoStateContent 是 cloneRepos 步骤内单仓库进度镜像。
type WorkspaceRepoStateContent struct {
	LocalRepositoryID int    `json:"localRepositoryId" jsonschema:"本地仓库 id"`
	Name              string `json:"name" jsonschema:"仓库名（即 repo/ 下子目录名）"`
	RemoteURL         string `json:"remoteUrl" jsonschema:"克隆地址（SSH）"`
	BaseBranch        string `json:"baseBranch,omitempty" jsonschema:"基准分支，留空表示仓库默认分支"`
	TargetBranch      string `json:"targetBranch" jsonschema:"目标分支（固定 agent_{issueId}）"`
	Status            string `json:"status" jsonschema:"仓库状态：PENDING/RUNNING/SUCCESS/FAILED/SKIPPED"`
	Message           string `json:"message,omitempty" jsonschema:"失败原因/说明"`
}

// WorkspaceRepoRefContent 是幂等 manifest 项镜像。
type WorkspaceRepoRefContent struct {
	LocalRepositoryID int    `json:"localRepositoryId" jsonschema:"本地仓库 id"`
	RemoteURL         string `json:"remoteUrl" jsonschema:"克隆地址（SSH）"`
	BaseBranch        string `json:"baseBranch,omitempty" jsonschema:"基准分支，留空表示仓库默认分支"`
}
