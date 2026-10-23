/*
 * Package mcptool 存放 MCP 工具 handler 实现（单 server 归口：全部工具注册在 mcpservers/mcp_ocean_harness.go，文件按业务域命名——issue/workspace 工具在 mcp_ocean_harness_tools.go、
 * github 工具在 mcp_github_tools.go，后续业务域同理）。handler 嵌入 mcputil.McpTool 获得链式装配（MakeContext → Validate → MakeService，链尾读 .Errors），内部复用既有 service 层（与 HTTP controller 完全共享业务逻辑，
 * 含事务与状态联动）；签名遵循 go-sdk ToolHandlerFor 三返回值形态——成功返回 (nil, 出参, nil)（SDK 自动双挂载 StructuredContent 与 TextContent），失败返回 (nil, 零值, mt.Fail(err))（SDK 自动装配 IsError 中文文案，Fail 只补日志）。
 */
package mcptool

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"ocean-harness/server/internal/dal/types"
	mcpdto "ocean-harness/server/internal/mcpservers/mcp_dto"
	"ocean-harness/server/internal/mcpservers/mcp_util"
	"ocean-harness/server/internal/service"
)

// McpOceanHarnessTool 是 ocean_harness server 的工具集合。
type McpOceanHarnessTool struct {
	mcputil.McpTool
}

// IssueGetInfo 获取 issue 详情（GetInfo 直通，含类型与关联仓库分支）。
func (mt McpOceanHarnessTool) IssueGetInfo(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.IssueIDArgs) (*mcp.CallToolResult, mcpdto.IssueContent, error) {

	issueSvc := service.WorkspaceIssue{}
	if err := mt.MakeContext(ctx).Validate(&input).MakeService(&issueSvc.Service).Errors; err != nil {
		return nil, mcpdto.IssueContent{}, mt.Fail(err)
	}
	data, err := issueSvc.GetInfo(&types.WorkspaceIssueGetInfoRequest{ID: input.IssueID})
	if err != nil {
		return nil, mcpdto.IssueContent{}, mt.Fail(err)
	}
	return nil, newIssueContent(data), nil
}

/*
 * IssueUpdate 部分更新 issue：先 GetInfo 取现值，非空入参覆盖、空值保留，
 * 再走既有全量 Update（stateCode 流转/事务语义与 HTTP 端完全一致）。
 */
func (mt McpOceanHarnessTool) IssueUpdate(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.IssueUpdateArgs) (*mcp.CallToolResult, mcpdto.IssueContent, error) {

	issueSvc := service.WorkspaceIssue{}
	if err := mt.MakeContext(ctx).Validate(&input).MakeService(&issueSvc.Service).Errors; err != nil {
		return nil, mcpdto.IssueContent{}, mt.Fail(err)
	}
	data, err := mt.partialUpdate(&issueSvc, &input)
	if err != nil {
		return nil, mcpdto.IssueContent{}, mt.Fail(err)
	}
	return nil, newIssueContent(data), nil
}

/*
 * WorkspaceStatus 查询 issue 工作空间初始化状态：目录由 service 经
 * issueId 从工作空间解析（后端 SSOT），复用既有 IssueWorkspace.Status（读状态文件派生）。
 */
func (mt McpOceanHarnessTool) WorkspaceStatus(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.WorkspaceStatusArgs) (*mcp.CallToolResult, mcpdto.WorkspaceStatusContent, error) {

	if err := mt.MakeContext(ctx).Validate(&input).Errors; err != nil {
		return nil, mcpdto.WorkspaceStatusContent{}, mt.Fail(err)
	}
	wsSvc := service.IssueWorkspace{}
	mt.MakeService(&wsSvc.Service)
	data, err := wsSvc.Status(&types.IssueWorkspaceStatusRequest{IssueID: input.IssueID})
	if err != nil {
		return nil, mcpdto.WorkspaceStatusContent{}, mt.Fail(err)
	}
	return nil, newWorkspaceStatusContent(data), nil
}

// WorkspaceList 列出全部工作空间（新建在前），透传既有 Workspace.GetList（当前无筛选条件）。
func (mt McpOceanHarnessTool) WorkspaceList(ctx context.Context, _ *mcp.CallToolRequest,
	_ mcpdto.WorkspaceListArgs) (*mcp.CallToolResult, mcpdto.WorkspaceListContent, error) {

	wsSvc := service.Workspace{}
	if err := mt.MakeContext(ctx).MakeService(&wsSvc.Service).Errors; err != nil {
		return nil, mcpdto.WorkspaceListContent{}, mt.Fail(err)
	}
	rows, err := wsSvc.GetList(&types.WorkspaceGetListRequest{})
	if err != nil {
		return nil, mcpdto.WorkspaceListContent{}, mt.Fail(err)
	}
	list := make([]*mcpdto.WorkspaceContent, 0, len(rows))
	for i := range rows {
		list = append(list, &mcpdto.WorkspaceContent{
			ID:          rows[i].ID,
			Name:        rows[i].Name,
			Dir:         rows[i].Dir,
			Description: rows[i].Description,
		})
	}
	return nil, mcpdto.WorkspaceListContent{Workspaces: list}, nil
}

// IssueList 列出某项目下的全部任务（created_at 升序），
// 透传既有 WorkspaceIssue.GetList。
func (mt McpOceanHarnessTool) IssueList(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.IssueListArgs) (*mcp.CallToolResult, mcpdto.IssueListContent, error) {

	issueSvc := service.WorkspaceIssue{}
	if err := mt.MakeContext(ctx).Validate(&input).MakeService(&issueSvc.Service).Errors; err != nil {
		return nil, mcpdto.IssueListContent{}, mt.Fail(err)
	}
	rows, err := issueSvc.GetList(&types.WorkspaceIssueGetListRequest{ProjectID: input.ProjectID})
	if err != nil {
		return nil, mcpdto.IssueListContent{}, mt.Fail(err)
	}
	list := make([]*mcpdto.IssueContent, 0, len(rows))
	for _, it := range rows {
		list = append(list, issueContentPtr(it))
	}
	return nil, mcpdto.IssueListContent{Issues: list}, nil
}

// partialUpdate 是 issue_update 的合并入口（GetInfo → buildIssueUpdateRequest → Update）。
func (mt McpOceanHarnessTool) partialUpdate(issueSvc *service.WorkspaceIssue, args *mcpdto.IssueUpdateArgs) (*types.WorkspaceIssueResponseData, error) {
	cur, err := issueSvc.GetInfo(&types.WorkspaceIssueGetInfoRequest{ID: args.IssueID})
	if err != nil {
		return nil, err
	}
	return issueSvc.Update(buildIssueUpdateRequest(cur, args))
}

/*
 * buildIssueUpdateRequest 把部分更新入参合并到现值，产出 service.Update 的全量请求。
 * 现有 Update 是全量覆盖语义（Name/Description/StartDate/TargetDate 无条件覆写；
 * typeId 与关联仓库分支传空会清空），故须从 GetInfo 结果完整回填，仅覆盖显式给出的非空字段。空串语义即「不改」（不支持经 MCP 置空字段，已写入工具描述）。
 */
func buildIssueUpdateRequest(cur *types.WorkspaceIssueResponseData, args *mcpdto.IssueUpdateArgs) *types.WorkspaceIssueUpdateRequest {
	req := &types.WorkspaceIssueUpdateRequest{
		ID:                   cur.ID,
		Name:                 cur.Name,
		Description:          cur.Description,
		TypeID:               &cur.TypeID, // MCP 不支持改类型，原样回填现值（nil 语义不适用）
		RepositoryBranchList: cur.RepositoryBranchList,
		LaunchSettings:       cur.LaunchSettings, // MCP 不支持改启动设置，原样回填（nil=未覆盖）避免被全量更新清掉
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

func newIssueContent(data *types.WorkspaceIssueResponseData) mcpdto.IssueContent {
	var issueType *mcpdto.IssueTypeContent
	if data.Type != nil {
		issueType = &mcpdto.IssueTypeContent{ID: data.Type.ID, Name: data.Type.Name, Color: data.Type.Color}
	}
	repos := make([]mcpdto.IssueRepoBranchContent, 0, len(data.RepositoryBranchList))
	for _, rb := range data.RepositoryBranchList {
		repos = append(repos, mcpdto.IssueRepoBranchContent{
			LocalRepositoryID: rb.LocalRepositoryID,
			RepositoryBranch:  rb.RepositoryBranch,
		})
	}
	issue := data.ProjectTask // 嵌入 DO（GetInfo/Create/Update 返回值保证非 nil）
	return mcpdto.IssueContent{
		ID:                   issue.ID,
		Name:                 issue.Name,
		Description:          issue.Description,
		Type:                 issueType,
		RepositoryBranchList: repos,
	}
}

func issueContentPtr(data *types.WorkspaceIssueResponseData) *mcpdto.IssueContent {
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
