/*
 * browser 工具 handler（T2.2，23+1）：原生 6（status / profile_open / session_close / auth_state / auth_request / tool_list，
 * ToolHandlerFor 三返回值范式，SDK 自动装配）+ 转发 17（统一 forwardTool 管道，手工透传引擎 Content 数组——含 ImageContent，
 * 类型化出参自动装配会丢图）+ 逃生舱 1（tool_call 原样直通）。业务逻辑全部复用 service 层（与 REST 端共享 forwardOK/manager 门面），本文件只做参数搬运与出参整形。
 */
package mcptool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"ocean-harness/server/internal/browser"
	"ocean-harness/server/internal/dal/types"
	mcpdto "ocean-harness/server/internal/mcpservers/mcp_dto"
	"ocean-harness/server/internal/mcpservers/mcp_util"
	"ocean-harness/server/internal/service"
)

// McpBrowserTool 是 ocean_harness server 的 browser_* 工具集合。
type McpBrowserTool struct {
	mcputil.McpTool
}

/*
 * defaultBrowserProfile profile 缺省值（MCP 入参 profile 留空的归一口径；
 * REST 端为 binding:required 无缺省，两侧差异属刻意——MCP 消费者是 AI，省一次必填参数）。
 */
const defaultBrowserProfile = "default"

// resolveProfile MCP 入参 profile 归一：留空用 default。
func resolveProfile(profile string) string {
	if profile == "" {
		return defaultBrowserProfile
	}
	return profile
}

/*
 * engineArgs 引擎转发入参构造：入参 DTO 整体 JSON 往返成 map 后剥掉 profile/issueId
 * 两个服务端自有字段。DTO 引擎参数段的字段名与引擎 schema 一致（逐字段镜像），
 * 零手写映射——引擎拒绝非法值时错误文案的字段名与工具 schema 天然对齐。JSON 往返对纯数据 DTO 不会失败（全为可序列化类型），失败分支兜底空 map，由引擎侧报缺参显式暴露。
 */
func engineArgs(input any) map[string]any {
	raw, err := json.Marshal(input)
	if err != nil {
		return map[string]any{}
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]any{}
	}
	delete(m, "profile")
	delete(m, "issueId")
	return m
}

/*
 * withForwardMeta 归因元数据 TextContent 尾部追加（[profile: x] 或
 * [profile: x,issueId: y]）：浏览器会话全局单源，多消费方（面板/bot/终端）共用 profile，
 * 结果需可辨识来源。引擎自有 Content 不动、顺序不变（CLI 等首文本消费者拿到的仍是引擎正文，归因行不夺位），仅追加。
 */
func withForwardMeta(res *mcp.CallToolResult, profile, issueID string) *mcp.CallToolResult {
	meta := "[profile: " + profile
	if issueID != "" {
		meta += ", issueId: " + issueID
	}
	meta += "]"
	res.Content = append(res.Content, &mcp.TextContent{Text: meta})
	return res
}

/*
 * forwardTool 转发 17 工具的统一管道：链式装配（ctx/vd 校验/日志注入）→ service 中文
 * fail 收敛转发 → 归因元数据追加。成功返回 (res, nil, nil)——go-sdk 对非 nil result
 * 且 nil out 跳过全部出参装配，引擎 Content 数组（含 ImageContent 与落盘路径文本）原样到达客户端。
 */
func forwardTool[A any](mt *McpBrowserTool, ctx context.Context, input *A, profile, issueID, engineTool, action string) (*mcp.CallToolResult, any, error) {
	browserSvc := service.BrowserSession{}
	if err := mt.MakeContext(ctx).Validate(input).MakeService(&browserSvc.Service).Errors; err != nil {
		return nil, nil, mt.Fail(err)
	}
	res, err := browserSvc.ForwardTool(profile, engineTool, engineArgs(input), issueID, action)
	if err != nil {
		return nil, nil, mt.Fail(err)
	}
	return withForwardMeta(res, profile, issueID), nil, nil
}

// —— 原生 6 工具 ——

// Status 浏览器全局投影快照（无副作用读）。
func (mt McpBrowserTool) Status(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserStatusArgs) (*mcp.CallToolResult, mcpdto.BrowserStatusContent, error) {

	browserSvc := service.BrowserSession{}
	if err := mt.MakeContext(ctx).Validate(&input).MakeService(&browserSvc.Service).Errors; err != nil {
		return nil, mcpdto.BrowserStatusContent{}, mt.Fail(err)
	}
	snap := browserSvc.GetInfo()
	out := mcpdto.BrowserStatusContent{Sessions: make([]*mcpdto.BrowserSessionContent, 0, len(snap.Sessions))}
	for i := range snap.Sessions {
		out.Sessions = append(out.Sessions, newSessionContent(&snap.Sessions[i]))
	}
	return nil, out, nil
}

/*
 * ProfileOpen 拉起/复用会话并同步等就绪（headless
 * 留空读 profile 偏好，显式传入覆写回存；切换模式会关掉重开）。
 */
func (mt McpBrowserTool) ProfileOpen(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserProfileOpenArgs) (*mcp.CallToolResult, mcpdto.BrowserSessionContent, error) {

	profile := resolveProfile(input.Profile)
	browserSvc := service.BrowserSession{}
	if err := mt.MakeContext(ctx).Validate(&input).MakeService(&browserSvc.Service).Errors; err != nil {
		return nil, mcpdto.BrowserSessionContent{}, mt.Fail(err)
	}
	view, err := browserSvc.Ensure(&types.BrowserEnsureRequest{Profile: profile, Headless: input.Headless})
	if err != nil {
		return nil, mcpdto.BrowserSessionContent{}, mt.Fail(err)
	}
	return nil, *newSessionContent(&view), nil
}

/*
 * SessionClose 释放引擎进程（幂等，
 * 登录态在 profile 目录续存）。与 browser_close（关当前页 tab）语义不同，工具描述已注明。
 */
func (mt McpBrowserTool) SessionClose(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserSessionCloseArgs) (*mcp.CallToolResult, mcpdto.BrowserSessionCloseContent, error) {

	profile := resolveProfile(input.Profile)
	browserSvc := service.BrowserSession{}
	if err := mt.MakeContext(ctx).Validate(&input).MakeService(&browserSvc.Service).Errors; err != nil {
		return nil, mcpdto.BrowserSessionCloseContent{}, mt.Fail(err)
	}
	if _, err := browserSvc.Close(&types.BrowserCloseRequest{Profile: profile}); err != nil {
		return nil, mcpdto.BrowserSessionCloseContent{}, mt.Fail(err)
	}
	return nil, mcpdto.BrowserSessionCloseContent{
		Message: fmt.Sprintf("profile %s 浏览器会话已关停（未运行亦视为成功）；登录态在 profile 目录续存，下次打开自动恢复", profile),
	}, nil
}

/*
 * AuthState 拉取当前登录态双源（cookies + 当前页
 * localStorage；凭据仅用于本次 browser_auth_request 入参，勿外传勿落盘）。
 */
func (mt McpBrowserTool) AuthState(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserAuthStateArgs) (*mcp.CallToolResult, mcpdto.BrowserAuthStateContent, error) {

	profile := resolveProfile(input.Profile)
	browserSvc := service.BrowserSession{}
	if err := mt.MakeContext(ctx).Validate(&input).MakeService(&browserSvc.Service).Errors; err != nil {
		return nil, mcpdto.BrowserAuthStateContent{}, mt.Fail(err)
	}
	cookies, localStorage, err := browserSvc.AuthState(profile, input.IssueID)
	if err != nil {
		return nil, mcpdto.BrowserAuthStateContent{}, mt.Fail(err)
	}
	return nil, mcpdto.BrowserAuthStateContent{Cookies: cookies, LocalStorage: localStorage}, nil
}

// AuthedRequest 带态 HTTP 请求（引擎实时登录态 cookie 注入后 Go 直发，凭据零落盘）。
func (mt McpBrowserTool) AuthedRequest(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserAuthRequestArgs) (*mcp.CallToolResult, mcpdto.BrowserAuthedResponseContent, error) {

	profile := resolveProfile(input.Profile)
	browserSvc := service.BrowserSession{}
	if err := mt.MakeContext(ctx).Validate(&input).MakeService(&browserSvc.Service).Errors; err != nil {
		return nil, mcpdto.BrowserAuthedResponseContent{}, mt.Fail(err)
	}
	resp, err := browserSvc.AuthedRequest(profile, input.Method, input.URL, input.Headers, input.Body, input.IssueID)
	if err != nil {
		return nil, mcpdto.BrowserAuthedResponseContent{}, mt.Fail(err)
	}
	return nil, mcpdto.BrowserAuthedResponseContent{
		StatusCode: resp.StatusCode,
		Header:     map[string][]string(resp.Header),
		Body:       resp.Body,
		Truncated:  resp.Truncated,
	}, nil
}

// ToolList 查询引擎当前可用工具全集（逃生舱直调的事实源，黑名单工具带 blocked 标注）。
func (mt McpBrowserTool) ToolList(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserToolListArgs) (*mcp.CallToolResult, mcpdto.BrowserToolListContent, error) {

	profile := resolveProfile(input.Profile)
	browserSvc := service.BrowserSession{}
	if err := mt.MakeContext(ctx).Validate(&input).MakeService(&browserSvc.Service).Errors; err != nil {
		return nil, mcpdto.BrowserToolListContent{}, mt.Fail(err)
	}
	tools, err := browserSvc.ListEngineTools(profile, input.IssueID)
	if err != nil {
		return nil, mcpdto.BrowserToolListContent{}, mt.Fail(err)
	}
	out := mcpdto.BrowserToolListContent{Profile: profile, Tools: make([]*mcpdto.BrowserEngineToolContent, 0, len(tools))}
	for _, t := range tools {
		out.Tools = append(out.Tools, &mcpdto.BrowserEngineToolContent{
			Name:        t.Name,
			Title:       t.Title,
			Description: t.Description,
			Blocked:     browser.IsEngineToolBlacklisted(t.Name),
		})
	}
	return nil, out, nil
}

// —— 转发 17 工具（一行一工具：归一 profile → 引擎常量 → 中文动作短语）——

// Navigate 打开地址。
func (mt McpBrowserTool) Navigate(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserNavigateArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolNavigate, "打开页面")
}

// Snapshot 拉取页面快照（操作元素的 ref 事实源）。
func (mt McpBrowserTool) Snapshot(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserSnapshotArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolSnapshot, "拉取页面快照")
}

// Click 点击元素（页面报 modal state 时先处理模态，工具描述已注明）。
func (mt McpBrowserTool) Click(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserClickArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolClick, "点击元素")
}

// Type 输入文本。
func (mt McpBrowserTool) Type(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserTypeArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolType, "输入文本")
}

// FillForm 批量填写表单。
func (mt McpBrowserTool) FillForm(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserFillFormArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolFillForm, "填写表单")
}

// SelectOption 选择下拉项。
func (mt McpBrowserTool) SelectOption(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserSelectOptionArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolSelectOption, "选择下拉项")
}

// WaitFor 等待页面状态（文本出现/消失或定时）。
func (mt McpBrowserTool) WaitFor(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserWaitForArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolWaitFor, "等待页面状态")
}

// Evaluate 页面/元素上执行 JS。
func (mt McpBrowserTool) Evaluate(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserEvaluateArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolEvaluate, "执行页面脚本")
}

// Tabs 管理 tab（list/new/close/select）。
func (mt McpBrowserTool) Tabs(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserTabsArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolTabs, "管理 tab")
}

// Screenshot 截取页面（结果含图片与落盘路径）。
func (mt McpBrowserTool) Screenshot(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserScreenshotArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolTakeScreenshot, "截取页面")
}

// FileUpload 处理文件选择框（paths 留空取消）。
func (mt McpBrowserTool) FileUpload(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserFileUploadArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolFileUpload, "上传文件")
}

// ConsoleMessages 读取控制台消息。
func (mt McpBrowserTool) ConsoleMessages(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserConsoleMessagesArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolConsoleMessages, "读取控制台消息")
}

// NavigateBack 历史后退。
func (mt McpBrowserTool) NavigateBack(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserNavigateBackArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolNavigateBack, "后退")
}

// Close 关闭当前页 tab（非关引擎进程——后者用 browser_session_close）。
func (mt McpBrowserTool) Close(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserCloseArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolClose, "关闭页面")
}

// Find 快照内查找元素（比整页快照省）。
func (mt McpBrowserTool) Find(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserFindArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolFind, "查找页面元素")
}

// PressKey 按键。
func (mt McpBrowserTool) PressKey(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserPressKeyArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolPressKey, "按键")
}

// HandleDialog 处理对话框（dialog modal state 唯一清除工具）。
func (mt McpBrowserTool) HandleDialog(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserHandleDialogArgs) (*mcp.CallToolResult, any, error) {
	return forwardTool(&mt, ctx, &input, resolveProfile(input.Profile), input.IssueID, browser.EngineToolHandleDialog, "处理对话框")
}

// —— 逃生舱 1 ——

/*
 * ToolCall 名单外引擎工具原样直通：黑名单前置拒绝；
 * 引擎结果不转换不包装、isError 原样传导，亦不加归因元数据（直通语义）。args 经 map 型 In 宽松透传（additionalProperties:true，T0 实测）。
 */
func (mt McpBrowserTool) ToolCall(ctx context.Context, _ *mcp.CallToolRequest,
	input mcpdto.BrowserToolCallArgs) (*mcp.CallToolResult, any, error) {

	if err := browser.CheckEngineToolCall(input.Name); err != nil {
		return nil, nil, mt.Fail(err)
	}
	profile := resolveProfile(input.Profile)
	browserSvc := service.BrowserSession{}
	if err := mt.MakeContext(ctx).Validate(&input).MakeService(&browserSvc.Service).Errors; err != nil {
		return nil, nil, mt.Fail(err)
	}
	res, err := browserSvc.ForwardRaw(profile, input.Name, input.Args, input.IssueID)
	if err != nil {
		return nil, nil, mt.Fail(err)
	}
	return res, nil, nil
}

// —— Content 转换 ——

// newSessionContent 会话投影 → 平铺镜像（状态枚举中文释义见 DTO jsonschema tag）。
func newSessionContent(view *browser.SessionView) *mcpdto.BrowserSessionContent {
	out := &mcpdto.BrowserSessionContent{
		Profile:      view.Profile,
		Status:       string(view.Status),
		Headless:     view.Headless,
		Error:        view.Error,
		LastIssueTag: view.LastIssueTag,
	}
	for _, p := range view.Pages {
		out.Pages = append(out.Pages, &mcpdto.BrowserPageContent{Index: p.Index, Title: p.Title, URL: p.URL, Current: p.Current})
	}
	for _, c := range view.RecentCalls {
		out.RecentCalls = append(out.RecentCalls, &mcpdto.BrowserRecentCallContent{
			Tool: c.Tool, IssueTag: c.IssueTag, Outcome: c.Outcome, Error: c.Error,
		})
	}
	return out
}
