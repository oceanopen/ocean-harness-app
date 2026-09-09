import { getCurrentWindow } from '@tauri-apps/api/window';
import { useEffect } from 'react';

/**
 * ⌘W/Ctrl+W 关窗兜底：document 级 keydown（对齐 CommandPaletteProvider ⌘K 先例，
 * `(metaKey || e.ctrlKey)` 不做平台分支），enabled 时 preventDefault 并调用
 * `getCurrentWindow().close()`——走 Rust 侧既有 `CloseRequested → prevent_close + hide`
 * 拦截链，与 macOS 默认菜单时代的 ⌘W 行为一致。
 *
 * 背景：macOS 应用菜单已重建、close_window 不再占用 ⌘W accelerator（Rust
 * shared/app_menu.rs），⌘W 由前端全权接管。本 hook 只承担「保持默认关窗」的兜底
 * （panel 非 devWorkbench 菜单页、settings 窗口）；panel 的 devWorkbench 页有更高
 * 优先级的页面级处理器（文件查看模式关 tab），不挂本 hook。
 *
 * enabled 恒定语义由调用方保证（PanelApp 按 activeMenu 传入布尔表达式，React
 * state 派生值变化时 effect 重挂、监听器刷新，无陈旧闭包）。
 */
export function useCloseWindowShortcut(enabled: boolean): void {
  useEffect(() => {
    if (!enabled) {
      return;
    }
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'w') {
        e.preventDefault();
        void getCurrentWindow().close();
      }
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [enabled]);
}
