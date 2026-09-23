// 工作台 40px 标题栏对齐带高度（px）：页面标题栏（DevWorkbenchPage 终端列头部）、
// 工具面板 tab 头（ToolPanelArea）与最右工具条（WorkbenchToolRail 竖条宽/顶部方格）
// 三处同高对齐、底边线连通，一律引用此常量，禁止另写 magic number（范式同
// PanelToolbar.tsx 的 PANEL_TOOLBAR_HEIGHT 二级带）。Tab 内 minHeight/padding 推算
// 见 ToolPanelArea tab 头注释（8×2 + label 24 = 40）。
export const WORKBENCH_TITLEBAR_HEIGHT = 40;
