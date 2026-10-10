package types

// 浏览器会话域（/api/browserSession）的请求 DTO。响应 shape SSOT 是 browser 包投影
//（SessionView/BrowserViewSnapshot/Frame）与引擎 CallToolResult 原样透传——镜像到
// types 只会双份维护，口径同 acp_session.go。

// BrowserGetInfoRequest 是 POST /api/browserSession/getInfo 的入参（无参，空体 POST）。
type BrowserGetInfoRequest struct{}

// BrowserEnsureRequest 是 POST /api/browserSession/ensure 的入参（同步等就绪，失败
// 中文报错）。Headless 为 D10 无头覆盖：nil 读 profile 偏好文件，显式传入覆写并回存
// （面板无头/有头切换 = close + ensure 传新值，登录态在 profile 目录续存）。
type BrowserEnsureRequest struct {
	Profile  string `json:"profile" binding:"required"`
	Headless *bool  `json:"headless,omitempty"`
}

// BrowserCloseRequest 是 POST /api/browserSession/close 的入参（幂等：未知 profile 与
// idle/failed 态再关均为 no-op 成功）。
type BrowserCloseRequest struct {
	Profile string `json:"profile" binding:"required"`
}

// BrowserNavigateRequest 是 POST /api/browserSession/navigate 的入参（引擎结果原样
// 透传——成功为页面快照文本，引擎错误以 HTTP 层 fail 呈现）。
type BrowserNavigateRequest struct {
	Profile string `json:"profile" binding:"required"`
	URL     string `json:"url" binding:"required"`
}

// BrowserScreenshotRequest 是 POST /api/browserSession/screenshot 的入参（截当前页；
// --output-dir 已在 spawn 传入，成功结果含 ImageContent base64 与 TextContent 落盘
// 路径，供面板轮询伪实时）。
type BrowserScreenshotRequest struct {
	Profile string `json:"profile" binding:"required"`
}

// BrowserAnalyzeRequest 是 POST /api/browserSession/analyze 的入参（即时受理「AI 分析
// 此页」入 issue 绑定的 ACP 会话：会话缺失/回合进行中即中文报错，不排队）。PageID 为
// 页面投影行 Index（PageView.Index）：非 nil 先选中该 tab 再快照（支持分析非当前页）。
type BrowserAnalyzeRequest struct {
	Profile string `json:"profile" binding:"required"`
	IssueID string `json:"issueId" binding:"required"`
	PageID  *int   `json:"pageId,omitempty"`
}
