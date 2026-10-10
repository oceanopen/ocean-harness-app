package browser

// 引擎工具名单点常量表（T1.3，D5）：转发工具名（ocean 短名）→ 引擎实名。mcp_tool 层
// （T2.2）的 ocean 工具名 = browser_ + 短名，转发时经本表换引擎实名——引擎名字漂移
// 一行改一处（spike 校对入口）。引擎实名已按 @playwright/mcp 0.0.83 README 逐名核实。

// EngineTool* 引擎实名常量：manager 内部消费（pages 投影刷新拉 tab 列表）+ mcp_tool
// 层转发引用。
const (
	EngineToolNavigate        = "browser_navigate"
	EngineToolSnapshot        = "browser_snapshot"
	EngineToolClick           = "browser_click"
	EngineToolType            = "browser_type"
	EngineToolFillForm        = "browser_fill_form"
	EngineToolSelectOption    = "browser_select_option"
	EngineToolWaitFor         = "browser_wait_for"
	EngineToolEvaluate        = "browser_evaluate"
	EngineToolTabs            = "browser_tabs"
	EngineToolTakeScreenshot  = "browser_take_screenshot"
	EngineToolFileUpload      = "browser_file_upload"
	EngineToolConsoleMessages = "browser_console_messages"
	EngineToolNavigateBack    = "browser_navigate_back"
	EngineToolClose           = "browser_close"
	EngineToolFind            = "browser_find"
	EngineToolPressKey        = "browser_press_key"
	EngineToolHandleDialog    = "browser_handle_dialog"
)

// forwardedTools D5 的 17 精选转发全集：ocean 短名 → 引擎实名（11 基础 + console_messages/
// navigate_back/close/find/press_key/handle_dialog 扩员）。
var forwardedTools = map[string]string{
	"navigate":         EngineToolNavigate,
	"snapshot":         EngineToolSnapshot,
	"click":            EngineToolClick,
	"type":             EngineToolType,
	"fill_form":        EngineToolFillForm,
	"select_option":    EngineToolSelectOption,
	"wait_for":         EngineToolWaitFor,
	"evaluate":         EngineToolEvaluate,
	"tabs":             EngineToolTabs,
	"screenshot":       EngineToolTakeScreenshot,
	"file_upload":      EngineToolFileUpload,
	"console_messages": EngineToolConsoleMessages,
	"navigate_back":    EngineToolNavigateBack,
	"close":            EngineToolClose,
	"find":             EngineToolFind,
	"press_key":        EngineToolPressKey,
	"handle_dialog":    EngineToolHandleDialog,
}

// engineToolBlacklist 逃生舱（T2.2 browser_tool_call）拒绝名单单点：引擎 core 常驻的
// RCE 等价工具（引擎进程内执行任意 JS），中文报错说明、拒绝直调。
var engineToolBlacklist = map[string]bool{
	"browser_run_code_unsafe": true,
}

// pagesRefreshExempt 转发成功后不触发 pages 投影刷新的工具：截图不改 tab 状态——豁免
// 使面板截图轮询（1-2s/帧）不会一次轮询触发两次引擎调用；browser_tabs 本身即刷新源。
var pagesRefreshExempt = map[string]bool{
	EngineToolTakeScreenshot: true,
	EngineToolTabs:           true,
}
