// 工作台布局常量 SSOT（纯常量文件，非组件——常量与组件同文件导出会被
// react-refresh/only-export-components 拦截）。

// 工作台 40px 标题栏对齐带高度（px）：页面标题栏（DevWorkbenchPage 终端列头部）、
// 工具面板 tab 头（ToolPanelArea）与最右工具条（WorkbenchToolRail 竖条宽/顶部方格）
// 三处同高对齐、底边线连通，一律引用此常量，禁止另写 magic number（范式同
// PANEL_TOOLBAR_HEIGHT 二级带）。Tab 内 minHeight/padding 推算见 ToolPanelArea
// tab 头注释（8×2 + label 24 = 40）。
export const WORKBENCH_TITLEBAR_HEIGHT = 40;

// 面板操作栏统一高度（px）：终端 pane 工具栏、右侧工具面板头部共用。依赖方（终端
// exited 覆盖层 top、搜索条浮层 top 偏移）一律引用此常量，禁止另写 magic number。
// 刻意小于标题栏/tab 头的 40px——操作栏是 40px 对齐带之下的二级带，字号/图标也偏小。
export const PANEL_TOOLBAR_HEIGHT = 36;

// 面板区最小宽度（左边界拖拽下限；tab 头 + 列表内容的最小可读宽）。
export const TOOL_AREA_MIN_WIDTH = 360;
// 终端区最小宽度（拖拽上限 = 容器宽 - 此值，保证终端不被挤死；与页面终端区 minWidth 一致）。
export const TERMINAL_MIN_WIDTH = 320;
