package types

/*
 * 浏览器会话域（/api/browserSession）的请求 DTO：响应 shape SSOT 是 browser 包投影（SessionView/BrowserViewSnapshot/Frame）与引擎 CallToolResult 原样透传——镜像到 types 只会双份维护，口径同 acp_session.go。
 */

// BrowserGetInfoRequest 是 POST /api/browserSession/getInfo 的入参（无参，空体 POST）。
type BrowserGetInfoRequest struct{}

/*
 * BrowserEnsureRequest 是 POST /api/browserSession/ensure 的入参（同步等就绪，失败中文报错）；
 * Headless 为 D10 无头覆盖：nil 读 profile 偏好文件，显式传入覆写并回存（面板无头/有头切换 = close + ensure 传新值，登录态在 profile 目录续存）。
 */
type BrowserEnsureRequest struct {
	Profile  string `json:"profile" binding:"required"`
	Headless *bool  `json:"headless,omitempty"`
}

/*
 * BrowserCloseRequest 是 POST /api/browserSession/close 的入参（幂等：未知 profile 与 idle/failed 态再关均为 no-op 成功）。
 */
type BrowserCloseRequest struct {
	Profile string `json:"profile" binding:"required"`
}

/*
 * BrowserNavigateRequest 是 POST /api/browserSession/navigate 的入参（引擎结果原样透传——成功为页面快照文本，引擎错误以 HTTP 层 fail 呈现）。
 */
type BrowserNavigateRequest struct {
	Profile string `json:"profile" binding:"required"`
	URL     string `json:"url" binding:"required"`
}

/*
 * BrowserAnalyzeRequest 是 POST /api/browserSession/analyze 的入参（即时受理「AI 分析此页」入会话实体的 ACP 会话：会话缺失/回合进行中即中文报错，不排队）；
 * TargetID 为页面投影行 CDP targetId（PageView.TargetID），非空先选中该 tab 再快照（支持分析非激活页）。
 */
type BrowserAnalyzeRequest struct {
	Profile        string `json:"profile" binding:"required"`
	ConversationID string `json:"conversationId" binding:"required"`
	TargetID       string `json:"targetId,omitempty"`
}

/*
 * BrowserStreamRequest 是 GET /api/browserSession/stream 的入参（query 绑定；
 * CDP 观察通道实时画面帧流）：TargetID 为页面投影行 CDP targetId（订阅锚随导航稳定）；
 * ThrottleMs 为帧写侧最小间隔（0/缺省不节流，负值按 0 处理）——面板迷你预览档订阅，mailbox keep-latest 语义不变。
 */
type BrowserStreamRequest struct {
	Profile    string `form:"profile" binding:"required"`
	TargetID   string `form:"targetId" binding:"required"`
	ThrottleMs int    `form:"throttleMs"`
}

/*
 * BrowserInputRequest 是 POST /api/browserSession/input 的入参（远程桌面输入分发，kind 判别其余字段取舍；
 * 零值合法的坐标/delta 不设 required，必填校验按 kind 归 service 层映射）。
 */
type BrowserInputRequest struct {
	Profile    string  `json:"profile" binding:"required"`
	Kind       string  `json:"kind" binding:"required"` // mouse|wheel|key|text
	Action     string  `json:"action,omitempty"`        // mouse/key：CDP type（mousePressed 等）
	X          float64 `json:"x,omitempty"`
	Y          float64 `json:"y,omitempty"`
	Button     string  `json:"button,omitempty"`
	ClickCount int     `json:"clickCount,omitempty"`
	Modifiers  int     `json:"modifiers,omitempty"`
	DeltaX     float64 `json:"deltaX,omitempty"`
	DeltaY     float64 `json:"deltaY,omitempty"`
	Key        string  `json:"key,omitempty"`
	Code       string  `json:"code,omitempty"`
	VK         int     `json:"vk,omitempty"` // CDP windowsVirtualKeyCode
	Text       string  `json:"text,omitempty"`
}
