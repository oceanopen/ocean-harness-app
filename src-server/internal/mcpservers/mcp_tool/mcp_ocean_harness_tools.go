// Package mcptool 存放各 MCP server 的工具 handler 实现（文件按 server 命名，如
// mcp_ocean_harness_tools.go；后续 github server 落 mcp_github_tools.go 同构扩展）。
// handler 嵌入 mcputil.McpTool 获得链式装配（MakeContext → Validate → MakeService，
// 链尾读 .Errors），内部复用既有 service 层（与 HTTP controller 完全共享业务逻辑，
// 含事务与状态联动）；结果包装统一走 mcputil.McpOK / mcputil.McpFail。
package mcptool

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"ocean-harness/src-server/internal/dal/types"
	mcpdto "ocean-harness/src-server/internal/mcpservers/mcp_dto"
	"ocean-harness/src-server/internal/mcpservers/mcp_util"
	"ocean-harness/src-server/internal/service"
)

// McpOceanHarnessTool 是 ocean_harness server 的工具集合。
type McpOceanHarnessTool struct {
	mcputil.McpTool
}

// IssueGetInfo 获取 issue 详情（GetInfo 直通，含标签与关联仓库分支）。
func (mt McpOceanHarnessTool) IssueGetInfo(ctx context.Context, _ *mcp.ServerSession,
	params *mcp.CallToolParamsFor[mcpdto.IssueIDArgs]) (*mcp.CallToolResultFor[mcpdto.IssueContent], error) {

	args := params.Arguments
	issueSvc := service.ProjectIssue{}
	if err := mt.MakeContext(ctx).Validate(&args).MakeService(&issueSvc.Service).Errors; err != nil {
		return mcputil.McpFail[mcpdto.IssueContent](err)
	}
	data, err := issueSvc.GetInfo(&types.ProjectIssueGetInfoRequest{ID: args.IssueID})
	if err != nil {
		return mcputil.McpFail[mcpdto.IssueContent](err)
	}
	return mcputil.McpOK(newIssueContent(data))
}

// IssueUpdate 部分更新 issue：先 GetInfo 取现值，非空入参覆盖、空值保留，再走既有全量
// Update（stateCode 流转/父子联动/事务语义与 HTTP 端完全一致）。
func (mt McpOceanHarnessTool) IssueUpdate(ctx context.Context, _ *mcp.ServerSession,
	params *mcp.CallToolParamsFor[mcpdto.IssueUpdateArgs]) (*mcp.CallToolResultFor[mcpdto.IssueContent], error) {

	args := params.Arguments
	issueSvc := service.ProjectIssue{}
	if err := mt.MakeContext(ctx).Validate(&args).MakeService(&issueSvc.Service).Errors; err != nil {
		return mcputil.McpFail[mcpdto.IssueContent](err)
	}
	data, err := mt.partialUpdate(&issueSvc, &args)
	if err != nil {
		return mcputil.McpFail[mcpdto.IssueContent](err)
	}
	return mcputil.McpOK(newIssueContent(data))
}

// IssueChildList 列出某 issue 的全部一级子任务：GetInfo（父校验）→ GetList（项目扁平
// 列表，单用户本地量级）→ 按 parentId 过滤。刻意不改 service/DTO，MCP 层纯组合。
func (mt McpOceanHarnessTool) IssueChildList(ctx context.Context, _ *mcp.ServerSession,
	params *mcp.CallToolParamsFor[mcpdto.IssueChildListArgs]) (*mcp.CallToolResultFor[mcpdto.IssueChildListContent], error) {

	args := params.Arguments
	issueSvc := service.ProjectIssue{}
	if err := mt.MakeContext(ctx).Validate(&args).MakeService(&issueSvc.Service).Errors; err != nil {
		return mcputil.McpFail[mcpdto.IssueChildListContent](err)
	}
	parent, err := issueSvc.GetInfo(&types.ProjectIssueGetInfoRequest{ID: args.IssueID})
	if err != nil {
		return mcputil.McpFail[mcpdto.IssueChildListContent](err)
	}
	if parent.ParentID != "" {
		return mcputil.McpFail[mcpdto.IssueChildListContent](errors.New("该 issue 已是子任务（仅支持一层子任务）"))
	}
	all, err := issueSvc.GetList(&types.ProjectIssueGetListRequest{ProjectID: parent.ProjectID})
	if err != nil {
		return mcputil.McpFail[mcpdto.IssueChildListContent](err)
	}
	children := make([]*mcpdto.IssueContent, 0, len(all))
	for _, it := range all {
		if it.ParentID == args.IssueID {
			children = append(children, issueContentPtr(it))
		}
	}
	return mcputil.McpOK(mcpdto.IssueChildListContent{Children: children})
}

// IssueChildCreate 为父 issue 创建一级子任务（ProjectID/WorkspaceID 继承父任务）。
// service.Create 会再次校验父存在/同项目/仅一层；此处前置守卫仅为更早给出定向中文提示。
func (mt McpOceanHarnessTool) IssueChildCreate(ctx context.Context, _ *mcp.ServerSession,
	params *mcp.CallToolParamsFor[mcpdto.IssueChildCreateArgs]) (*mcp.CallToolResultFor[mcpdto.IssueContent], error) {

	args := params.Arguments
	if args.Name == "" { // schema required 的兜底（空串漏网时给出业务化提示）
		return mcputil.McpFail[mcpdto.IssueContent](errors.New("name 不能为空"))
	}
	issueSvc := service.ProjectIssue{}
	if err := mt.MakeContext(ctx).Validate(&args).MakeService(&issueSvc.Service).Errors; err != nil {
		return mcputil.McpFail[mcpdto.IssueContent](err)
	}
	parent, err := issueSvc.GetInfo(&types.ProjectIssueGetInfoRequest{ID: args.IssueID})
	if err != nil {
		return mcputil.McpFail[mcpdto.IssueContent](err)
	}
	if parent.ParentID != "" {
		return mcputil.McpFail[mcpdto.IssueContent](errors.New("仅支持一层子任务，不能在子任务下再建子任务"))
	}
	data, err := issueSvc.Create(&types.ProjectIssueCreateRequest{
		ProjectID:   parent.ProjectID,
		WorkspaceID: parent.WorkspaceID,
		Name:        args.Name,
		Description: args.Description,
		StateCode:   args.StateCode, // 空 → service 默认 BACKLOG
		ParentID:    args.IssueID,
	})
	if err != nil {
		return mcputil.McpFail[mcpdto.IssueContent](err)
	}
	return mcputil.McpOK(newIssueContent(data))
}

// IssueChildUpdate 部分更新子任务（与 IssueUpdate 同构），差异仅：目标必须是子任务
// （AI 用错工具时给出定向纠正提示）。
func (mt McpOceanHarnessTool) IssueChildUpdate(ctx context.Context, _ *mcp.ServerSession,
	params *mcp.CallToolParamsFor[mcpdto.IssueUpdateArgs]) (*mcp.CallToolResultFor[mcpdto.IssueContent], error) {

	args := params.Arguments
	issueSvc := service.ProjectIssue{}
	if err := mt.MakeContext(ctx).Validate(&args).MakeService(&issueSvc.Service).Errors; err != nil {
		return mcputil.McpFail[mcpdto.IssueContent](err)
	}
	cur, err := issueSvc.GetInfo(&types.ProjectIssueGetInfoRequest{ID: args.IssueID})
	if err != nil {
		return mcputil.McpFail[mcpdto.IssueContent](err)
	}
	if cur.ParentID == "" {
		return mcputil.McpFail[mcpdto.IssueContent](errors.New("该 issue 不是子任务，请改用 issue_update"))
	}
	data, err := issueSvc.Update(buildIssueUpdateRequest(cur, &args))
	if err != nil {
		return mcputil.McpFail[mcpdto.IssueContent](err)
	}
	return mcputil.McpOK(newIssueContent(data))
}

// WorkspaceStatus 查询 issue 工作空间初始化状态：baseDir 不入参，从 Rust 共享 app.db
// 只读解析（应用设置页唯一真相），复用既有 IssueWorkspace.Status（读状态文件派生，不查库）。
func (mt McpOceanHarnessTool) WorkspaceStatus(ctx context.Context, _ *mcp.ServerSession,
	params *mcp.CallToolParamsFor[mcpdto.WorkspaceStatusArgs]) (*mcp.CallToolResultFor[mcpdto.WorkspaceStatusContent], error) {

	args := params.Arguments
	if err := mt.MakeContext(ctx).Validate(&args).Errors; err != nil {
		return mcputil.McpFail[mcpdto.WorkspaceStatusContent](err)
	}
	baseDir, err := mcputil.ReadWorkspaceBaseDir()
	if err != nil {
		return mcputil.McpFail[mcpdto.WorkspaceStatusContent](err)
	}
	wsSvc := service.IssueWorkspace{}
	mt.MakeService(&wsSvc.Service)
	data, err := wsSvc.Status(&types.IssueWorkspaceStatusRequest{IssueID: args.IssueID, BaseDir: baseDir})
	if err != nil {
		return mcputil.McpFail[mcpdto.WorkspaceStatusContent](err)
	}
	return mcputil.McpOK(newWorkspaceStatusContent(data))
}

// partialUpdate 是 issue_update 的合并入口（GetInfo → buildIssueUpdateRequest → Update）。
func (mt McpOceanHarnessTool) partialUpdate(issueSvc *service.ProjectIssue, args *mcpdto.IssueUpdateArgs) (*types.ProjectIssueResponseData, error) {
	cur, err := issueSvc.GetInfo(&types.ProjectIssueGetInfoRequest{ID: args.IssueID})
	if err != nil {
		return nil, err
	}
	return issueSvc.Update(buildIssueUpdateRequest(cur, args))
}

// buildIssueUpdateRequest 把部分更新入参合并到现值，产出 service.Update 的全量请求。
// 现有 Update 是全量覆盖语义（Name/Description/StartDate/TargetDate 无条件覆写；labels 与
// 关联仓库分支传空会清空关联），故须从 GetInfo 结果完整回填，仅覆盖显式给出的非空字段。
// 空串语义即「不改」（不支持经 MCP 置空字段，已写入工具描述）。
func buildIssueUpdateRequest(cur *types.ProjectIssueResponseData, args *mcpdto.IssueUpdateArgs) *types.ProjectIssueUpdateRequest {
	req := &types.ProjectIssueUpdateRequest{
		ID:                   cur.ID,
		Name:                 cur.Name,
		Description:          cur.Description,
		StateCode:            args.StateCode, // 空 = 不变（service.applyStateTransition 对空 no-op）
		Priority:             cur.Priority,
		IsDraft:              cur.IsDraft,
		StartDate:            cur.StartDate,
		TargetDate:           cur.TargetDate,
		LabelIDs:             make([]int, 0, len(cur.Labels)),
		RepositoryBranchList: cur.RepositoryBranchList,
	}
	for _, l := range cur.Labels {
		req.LabelIDs = append(req.LabelIDs, l.ID)
	}
	if args.Name != "" {
		req.Name = args.Name
	}
	if args.Description != "" {
		req.Description = args.Description
	}
	return req
}

// —— Content 转换（service 出参 → mcp_dto 平铺镜像，时间转 RFC3339 字符串）——

func newIssueContent(data *types.ProjectIssueResponseData) mcpdto.IssueContent {
	labels := make([]mcpdto.IssueLabelContent, 0, len(data.Labels))
	for _, l := range data.Labels {
		labels = append(labels, mcpdto.IssueLabelContent{ID: l.ID, Name: l.Name, Color: l.Color})
	}
	repos := make([]mcpdto.IssueRepoBranchContent, 0, len(data.RepositoryBranchList))
	for _, rb := range data.RepositoryBranchList {
		repos = append(repos, mcpdto.IssueRepoBranchContent{
			LocalRepositoryID: rb.LocalRepositoryID,
			RepositoryBranch:  rb.RepositoryBranch,
		})
	}
	issue := data.ProjectIssue // 嵌入 DO（GetInfo/Create/Update 返回值保证非 nil）
	return mcpdto.IssueContent{
		ID:                   issue.ID,
		Name:                 issue.Name,
		Description:          issue.Description,
		StateCode:            string(issue.StateCode),
		Priority:             string(issue.Priority),
		ParentID:             issue.ParentID,
		IsDraft:              string(issue.IsDraft),
		StartDate:            issue.StartDate,
		TargetDate:           issue.TargetDate,
		CompletedAt:          formatTime(issue.CompletedAt),
		SortOrder:            issue.SortOrder,
		Labels:               labels,
		RepositoryBranchList: repos,
	}
}

func issueContentPtr(data *types.ProjectIssueResponseData) *mcpdto.IssueContent {
	c := newIssueContent(data)
	return &c
}

func newWorkspaceStatusContent(data *types.IssueWorkspaceStatusResponseData) mcpdto.WorkspaceStatusContent {
	content := mcpdto.WorkspaceStatusContent{ServerStatus: string(data.ServerStatus)}
	if data.State == nil {
		return content
	}
	st := data.State
	steps := make([]*mcpdto.WorkspaceStepContent, 0, len(st.Steps))
	for _, s := range st.Steps {
		repos := make([]*mcpdto.WorkspaceRepoStateContent, 0, len(s.Repos))
		for _, r := range s.Repos {
			repos = append(repos, &mcpdto.WorkspaceRepoStateContent{
				LocalRepositoryID: r.LocalRepositoryID,
				Name:              r.Name,
				RemoteURL:         r.RemoteURL,
				BaseBranch:        r.BaseBranch,
				TargetBranch:      r.TargetBranch,
				Status:            string(r.Status),
				Message:           r.Message,
			})
		}
		steps = append(steps, &mcpdto.WorkspaceStepContent{
			Key:     s.Key,
			Title:   s.Title,
			Status:  string(s.Status),
			Repos:   repos,
			Message: s.Message,
		})
	}
	manifest := make([]*mcpdto.WorkspaceRepoRefContent, 0, len(st.Manifest))
	for _, m := range st.Manifest {
		manifest = append(manifest, &mcpdto.WorkspaceRepoRefContent{
			LocalRepositoryID: m.LocalRepositoryID,
			RemoteURL:         m.RemoteURL,
			BaseBranch:        m.BaseBranch,
		})
	}
	content.State = &mcpdto.WorkspaceStateContent{
		Version:   st.Version,
		IssueID:   st.IssueID,
		BaseDir:   st.BaseDir,
		Status:    string(st.Status),
		Steps:     steps,
		Manifest:  manifest,
		Error:     st.Error,
		CreatedAt: st.CreatedAt.Format(time.RFC3339),
		UpdatedAt: st.UpdatedAt.Format(time.RFC3339),
	}
	return content
}

// formatTime 把 *time.Time 转 RFC3339 文本（nil/零值 = 空串 = 未完成/未设置）。
func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
