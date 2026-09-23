// 平台判定：模块级一次判定（webview 的 navigator.platform 在 mac 上为 "MacIntel"），零依赖。
// 全仓唯一平台判定出口（原 CommandPaletteTrigger 内联 isMac 已回流至此）；供 Overlay 标题栏
// 的红绿灯让位带 / 折叠态逻辑、快捷键徽标等 macOS 专属行为门控（其余平台保持原生窗口装饰语义）。
export const isMacOS = typeof navigator !== 'undefined' && /Mac/i.test(navigator.platform);

// macOS Overlay 红绿灯让位带（px）：红绿灯占窗口左上 ~0-70px（随 macOS 版本浮动），取 80px
// 安全边。panel.rs 启用 TitleBarStyle::Overlay 后内容从窗口 (0,0) 铺起，顶部区域凡顶到
// y=0 的横向条带（PanelApp 顶栏/侧边栏头部、沉浸模式工作台标题栏）左侧均须让出该宽度。
export const TRAFFIC_LIGHT_CLEARANCE = 80;
