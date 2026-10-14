/*
 * browser 工具面 DTO（T2.2，23+1 工具）：原生 6（status / profile_open / session_close / auth_state / auth_request / tool_list）+ 转发 17 + 逃生舱 1（tool_call）。
 * 入参两段来源：转发 17 与逃生舱的引擎参数段是 @playwright/mcp 0.0.83 引擎 zod schema 的逐字段镜像（字段可选性对齐引擎：引擎无 .optional() 的字段不带 omitempty，即自动进 required），profile/issueId 是服务端自有附加段，经 BrowserProfileArgs 匿名嵌入平铺（jsonschema-go 对匿名嵌入字段平铺推导，引擎参数段不受嵌套失真）；原生 6 的入参对齐 service 层既有 REST 请求体语义。
 * 出参 Content 仅原生 6 需要（平铺镜像，见包注释）；转发 17 与逃生舱不走类型化出参（手工透传引擎 Content 数组，无 Out DTO——类型化出参的自动装配会丢 ImageContent）。
 */
package mcpdto

// —— 共用基座 ——

/*
 * BrowserProfileArgs browser_* 工具集共用基座（匿名嵌入平铺）：profile 缺省 default（mcp_tool 层归一，
 * 引擎会话自动 ensure，idle 冷启动 1-3s 属预期）；issueId 仅供服务端操作流水归因（面板 recentCalls 徽标），不参与引擎调用。
 */
type BrowserProfileArgs struct {
	Profile string `json:"profile,omitempty" jsonschema:"浏览器 profile 名，留空用 default；不同 profile 各自独立引擎进程与登录态"`
	IssueID string `json:"issueId,omitempty" jsonschema:"归因 issue 主键（uuid 文本），仅记入浏览器面板操作流水徽标，可留空"`
}

// —— 原生 6 工具入参 ——

// BrowserStatusArgs 是 browser_status 的入参（无参：只读全局会话投影）。
type BrowserStatusArgs struct{}

// BrowserProfileOpenArgs 是 browser_profile_open 的入参（拉起/复用并同步等就绪）。
type BrowserProfileOpenArgs struct {
	BrowserProfileArgs
	Headless *bool `json:"headless,omitempty" jsonschema:"无头模式；留空读 profile 偏好文件，显式传入覆写并回存（模式切换会关掉重开，登录态在 profile 目录续存）"`
}

/*
 * BrowserSessionCloseArgs 是 browser_session_close
 * 的入参（释放引擎进程，幂等；登录态在 profile 目录续存。关停无操作归因语义，不带 issueId）。
 */
type BrowserSessionCloseArgs struct {
	Profile string `json:"profile,omitempty" jsonschema:"浏览器 profile 名，留空用 default"`
}

// BrowserAuthStateArgs 是 browser_auth_state 的入参（当前登录态双源拉取）。
type BrowserAuthStateArgs struct {
	BrowserProfileArgs
}

/*
 * BrowserAuthRequestArgs 是 browser_auth_request
 * 的入参（带态 HTTP：引擎实时登录态 cookie 注入后 Go 直发，凭据零落盘）。
 */
type BrowserAuthRequestArgs struct {
	BrowserProfileArgs
	Method  string            `json:"method" jsonschema:"HTTP 方法（GET/POST/PUT/DELETE/PATCH 等）" vd:"@:$!=''; msg:'method 不能为空'"`
	URL     string            `json:"url" jsonschema:"目标 URL（http/https）" vd:"@:$!=''; msg:'url 不能为空'"`
	Headers map[string]string `json:"headers,omitempty" jsonschema:"附加请求头（显式 User-Agent 优先于浏览器真实 UA 探测值）"`
	Body    string            `json:"body,omitempty" jsonschema:"请求体（POST/PUT 等用；GET 留空）"`
}

// BrowserToolListArgs 是 browser_tool_list 的入参（查引擎当前可用工具全集）。
type BrowserToolListArgs struct {
	BrowserProfileArgs
}

// —— 转发 17 工具入参（引擎参数镜像 + BrowserProfileArgs）——

// BrowserNavigateArgs 是 browser_navigate 的入参。
type BrowserNavigateArgs struct {
	BrowserProfileArgs
	URL string `json:"url" jsonschema:"目标 URL（http/https）" vd:"@:$!=''; msg:'url 不能为空'"`
}

// BrowserSnapshotArgs 是 browser_snapshot 的入参。
type BrowserSnapshotArgs struct {
	BrowserProfileArgs
	Target   string `json:"target,omitempty" jsonschema:"元素快照 ref（或唯一选择器），传入则只截取该元素子树"`
	Filename string `json:"filename,omitempty" jsonschema:"快照保存文件名（相对工作区根），留空随响应返回"`
	Depth    *int   `json:"depth,omitempty" jsonschema:"快照树深度上限"`
	Boxes    *bool  `json:"boxes,omitempty" jsonschema:"true 时快照含各元素包围盒 [box=x,y,width,height]（视口 CSS 像素）"`
}

/*
 * BrowserClickArgs 是 browser_click 的入参（页面点击后若报 modal state，
 * 需先处理模态：alert 类对话框用 browser_handle_dialog、文件选择框用 browser_file_upload）。
 */
type BrowserClickArgs struct {
	BrowserProfileArgs
	Element     string   `json:"element,omitempty" jsonschema:"元素人读描述（供操作权限判定）"`
	Target      string   `json:"target" jsonschema:"元素快照 ref（或唯一选择器），来自最新页面快照——页面变化后重新 snapshot 取新 ref" vd:"@:$!=''; msg:'target 不能为空'"`
	DoubleClick *bool    `json:"doubleClick,omitempty" jsonschema:"true 为双击"`
	Button      string   `json:"button,omitempty" jsonschema:"鼠标按键：left（默认）/right/middle"`
	Modifiers   []string `json:"modifiers,omitempty" jsonschema:"修饰键列表：Alt/Control/ControlOrMeta/Meta/Shift"`
}

// BrowserTypeArgs 是 browser_type 的入参。
type BrowserTypeArgs struct {
	BrowserProfileArgs
	Element string `json:"element,omitempty" jsonschema:"元素人读描述（供操作权限判定）"`
	Target  string `json:"target" jsonschema:"可编辑元素的快照 ref（或唯一选择器）" vd:"@:$!=''; msg:'target 不能为空'"`
	Text    string `json:"text" jsonschema:"要输入的文本（空串即清空输入框）"`
	Submit  *bool  `json:"submit,omitempty" jsonschema:"true 时输入后按 Enter 提交"`
	Slowly  *bool  `json:"slowly,omitempty" jsonschema:"true 时逐字符输入（触发页面按键 handler），默认整段填入"`
}

// BrowserFormField 是 browser_fill_form 的单字段描述。
type BrowserFormField struct {
	Name   string `json:"name" jsonschema:"字段人读名（供操作权限判定）" vd:"@:$!=''; msg:'fields[].name 不能为空'"`
	Target string `json:"target" jsonschema:"字段快照 ref（或唯一选择器）" vd:"@:$!=''; msg:'fields[].target 不能为空'"`
	Type   string `json:"type" jsonschema:"字段类型：textbox/checkbox/radio/combobox/slider" vd:"@:$!=''; msg:'fields[].type 不能为空'"`
	Value  string `json:"value" jsonschema:"填充值；checkbox 填 true/false，combobox 填选项文本，textbox 填文本"`
}

// BrowserFillFormArgs 是 browser_fill_form 的入参。
type BrowserFillFormArgs struct {
	BrowserProfileArgs
	Fields []BrowserFormField `json:"fields" jsonschema:"要填充的字段列表" vd:"@:len($)>0; msg:'fields 不能为空'"`
}

// BrowserSelectOptionArgs 是 browser_select_option 的入参。
type BrowserSelectOptionArgs struct {
	BrowserProfileArgs
	Element string   `json:"element,omitempty" jsonschema:"元素人读描述（供操作权限判定）"`
	Target  string   `json:"target" jsonschema:"下拉框的快照 ref（或唯一选择器）" vd:"@:$!=''; msg:'target 不能为空'"`
	Values  []string `json:"values" jsonschema:"要选中的值（单选传一个，多选传多个）" vd:"@:len($)>0; msg:'values 不能为空'"`
}

// BrowserWaitForArgs 是 browser_wait_for 的入参（time/text/textGone 至少传一）。
type BrowserWaitForArgs struct {
	BrowserProfileArgs
	Time     *float64 `json:"time,omitempty" jsonschema:"等待秒数（至多 30）"`
	Text     string   `json:"text,omitempty" jsonschema:"等待该文本出现"`
	TextGone string   `json:"textGone,omitempty" jsonschema:"等待该文本消失"`
}

// BrowserEvaluateArgs 是 browser_evaluate 的入参（页面/元素上执行 JS）。
type BrowserEvaluateArgs struct {
	BrowserProfileArgs
	Element  string `json:"element,omitempty" jsonschema:"元素人读描述（供操作权限判定；传 target 时使用）"`
	Target   string `json:"target,omitempty" jsonschema:"元素快照 ref（或唯一选择器），传入则在该元素上执行"`
	Function string `json:"function" jsonschema:"JS 函数源码：() => { /* code */ }；传 target 时为 (element) => { /* code */ }" vd:"@:$!=''; msg:'function 不能为空'"`
	Filename string `json:"filename,omitempty" jsonschema:"结果保存文件名（相对工作区根），留空随响应返回"`
}

// BrowserTabsArgs 是 browser_tabs 的入参。
type BrowserTabsArgs struct {
	BrowserProfileArgs
	Action string `json:"action" jsonschema:"操作：list 列表/new 新建/close 关闭/select 选中"`
	Index  *int   `json:"index,omitempty" jsonschema:"tab 序号（close/select 用；close 缺省关当前页，select 必传）"`
	URL    string `json:"url,omitempty" jsonschema:"新 tab 打开的地址（new 用，可留空开空白页）"`
}

/*
 * BrowserScreenshotArgs 是
 * browser_take_screenshot 的入参（截图不可用于继续操作，操作需基于 browser_snapshot 的 ref）。
 */
type BrowserScreenshotArgs struct {
	BrowserProfileArgs
	Element  string `json:"element,omitempty" jsonschema:"元素人读描述（供操作权限判定；传 target 时使用）"`
	Target   string `json:"target,omitempty" jsonschema:"元素快照 ref（或唯一选择器），传入则只截该元素"`
	Type     string `json:"type,omitempty" jsonschema:"图片格式：png/jpeg/webp；留空按 filename 后缀推断，再退 png"`
	Filename string `json:"filename,omitempty" jsonschema:"保存文件名（相对工作区根）；留空存输出目录 page-{时间戳}.{ext}"`
	FullPage *bool  `json:"fullPage,omitempty" jsonschema:"true 截整页可滚动内容（与 target 元素截图互斥）"`
	Scale    string `json:"scale,omitempty" jsonschema:"分辨率：css（默认，CSS 像素）/device（设备像素，高分屏更大）"`
}

/*
 * BrowserFileUploadArgs 是 browser_file_upload
 * 的入参（页面出现文件选择框 modal state 后调用；paths 留空 = 取消选择）。
 */
type BrowserFileUploadArgs struct {
	BrowserProfileArgs
	Paths []string `json:"paths,omitempty" jsonschema:"上传文件的绝对路径列表（可多个）；留空取消文件选择框"`
}

// BrowserConsoleMessagesArgs 是 browser_console_messages 的入参。
type BrowserConsoleMessagesArgs struct {
	BrowserProfileArgs
	Level    string `json:"level,omitempty" jsonschema:"日志级别下限：error/warning/info/debug（含更严重级别），默认 info"`
	All      *bool  `json:"all,omitempty" jsonschema:"true 返回整个会话以来的消息，默认仅本次导航后"`
	Filename string `json:"filename,omitempty" jsonschema:"保存文件名（相对工作区根），留空随响应返回"`
}

// BrowserNavigateBackArgs 是 browser_navigate_back 的入参（无引擎参数）。
type BrowserNavigateBackArgs struct {
	BrowserProfileArgs
}

/*
 * BrowserCloseArgs 是 browser_close 的入参（关当前页 tab，
 * 非关引擎进程——后者用 browser_session_close；浏览器继续使用会自动重开页）。
 */
type BrowserCloseArgs struct {
	BrowserProfileArgs
}

// BrowserFindArgs 是 browser_find 的入参（text/regex 恰传其一）。
type BrowserFindArgs struct {
	BrowserProfileArgs
	Text     string `json:"text,omitempty" jsonschema:"搜索的纯文本（大小写不敏感子串匹配）；与 regex 二选一"`
	Regex    string `json:"regex,omitempty" jsonschema:"正则（默认区分大小写，如 /error/i 加标志）；与 text 二选一"`
	Filename string `json:"filename,omitempty" jsonschema:"结果保存文件名（相对工作区根），留空随响应返回"`
}

/*
 * BrowserPressKeyArgs 是 browser_press_key
 * 的入参（core 常驻；Enter 等提交键显式覆盖输入框的 submit 语义）。
 */
type BrowserPressKeyArgs struct {
	BrowserProfileArgs
	Key string `json:"key" jsonschema:"按键名或字符，如 ArrowLeft/Enter/a" vd:"@:$!=''; msg:'key 不能为空'"`
}

/*
 * BrowserHandleDialogArgs 是
 * browser_handle_dialog 的入参（dialog modal state 的唯一清除工具，accept 必填）。
 */
type BrowserHandleDialogArgs struct {
	BrowserProfileArgs
	Accept     *bool  `json:"accept" jsonschema:"true 接受对话框，false 关闭"`
	PromptText string `json:"promptText,omitempty" jsonschema:"prompt 对话框的输入文本（accept 时）"`
}

// —— 逃生舱 1 工具入参 ——

/*
 * BrowserToolCallArgs 是 browser_tool_call
 * 的入参（名单外引擎工具原样直通：isError 与引擎文本不转换；黑名单 browser_run_code_unsafe 被拒）。
 */
type BrowserToolCallArgs struct {
	BrowserProfileArgs
	Name string         `json:"name" jsonschema:"引擎工具实名（全集见 browser_tool_list；browser_run_code_unsafe 等黑名单工具被拒）" vd:"@:$!=''; msg:'name 不能为空'"`
	Args map[string]any `json:"args,omitempty" jsonschema:"引擎工具入参对象（按该工具的引擎 schema 传，宽松透传）"`
}

// —— 原生 6 工具出参 Content ——

// BrowserPageContent 页面投影行镜像。
type BrowserPageContent struct {
	Index   int    `json:"index" jsonschema:"tab 序号"`
	Title   string `json:"title" jsonschema:"页面标题"`
	URL     string `json:"url" jsonschema:"页面地址"`
	Current bool   `json:"current" jsonschema:"是否当前激活页"`
}

// BrowserRecentCallContent 最近操作流水行镜像。
type BrowserRecentCallContent struct {
	Tool     string `json:"tool" jsonschema:"工具名"`
	IssueTag string `json:"issueTag,omitempty" jsonschema:"归因 issueId；空为面板手动或未归因调用"`
	Outcome  string `json:"outcome" jsonschema:"结果分类：ok 成功/error 失败/timeout 超时/stale_ref 快照 ref 失效/closed 会话已关"`
	Error    string `json:"error,omitempty" jsonschema:"失败原因摘要"`
}

// BrowserSessionContent 单 profile 会话投影镜像（status / profile_open 出参共用）。
type BrowserSessionContent struct {
	Profile      string                      `json:"profile" jsonschema:"profile 名"`
	Status       string                      `json:"status" jsonschema:"状态：idle 已释放/starting 准备中/ready 就绪/failed 失败（见 error 字段）"`
	Headless     bool                        `json:"headless" jsonschema:"是否无头模式"`
	Error        string                      `json:"error,omitempty" jsonschema:"失败原因（failed 态非空）"`
	Pages        []*BrowserPageContent       `json:"pages,omitempty" jsonschema:"当前页面列表"`
	LastIssueTag string                      `json:"lastIssueTag,omitempty" jsonschema:"最近归因的 issueId"`
	RecentCalls  []*BrowserRecentCallContent `json:"recentCalls,omitempty" jsonschema:"最近操作流水（环形，至多 20 条）"`
}

// BrowserStatusContent 是 browser_status 的出参。
type BrowserStatusContent struct {
	Sessions []*BrowserSessionContent `json:"sessions" jsonschema:"各 profile 会话投影（按 profile 字典序）；无会话为空数组"`
}

// BrowserSessionCloseContent 是 browser_session_close 的出参。
type BrowserSessionCloseContent struct {
	Message string `json:"message" jsonschema:"关停结果说明"`
}

/*
 * BrowserAuthStateContent 是 browser_auth_state
 * 的出参（凭据：仅用于本次 browser_auth_request 入参，勿外传勿落盘）。
 */
type BrowserAuthStateContent struct {
	Cookies      string `json:"cookies" jsonschema:"引擎 cookie 全集内联文本（name=value (domain: d, path: p) 逐行）"`
	LocalStorage string `json:"localStorage" jsonschema:"当前页 localStorage 键值内联文本"`
}

// BrowserAuthedResponseContent 是 browser_auth_request 的出参。
type BrowserAuthedResponseContent struct {
	StatusCode int                 `json:"statusCode" jsonschema:"HTTP 状态码"`
	Header     map[string][]string `json:"header,omitempty" jsonschema:"响应头"`
	Body       string              `json:"body" jsonschema:"响应体（超 256KiB 截断，见 truncated）"`
	Truncated  bool                `json:"truncated" jsonschema:"响应体是否被截断"`
}

// BrowserEngineToolContent 引擎单工具描述（browser_tool_list 出参项）。
type BrowserEngineToolContent struct {
	Name        string `json:"name" jsonschema:"引擎工具实名（browser_*）"`
	Title       string `json:"title" jsonschema:"工具短标题（英文，引擎原文）"`
	Description string `json:"description" jsonschema:"工具说明（英文，引擎原文）"`
	Blocked     bool   `json:"blocked" jsonschema:"true = 服务端黑名单拒绝（browser_tool_call 直调被拒，如 browser_run_code_unsafe）"`
}

// BrowserToolListContent 是 browser_tool_list 的出参。
type BrowserToolListContent struct {
	Profile string                      `json:"profile" jsonschema:"profile 名"`
	Tools   []*BrowserEngineToolContent `json:"tools" jsonschema:"引擎当前可用工具全集（含黑名单 blocked 标注）"`
}
