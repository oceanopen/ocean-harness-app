import { getCurrentWindow } from '@tauri-apps/api/window';
import { useEffect } from 'react';

/**
 * ⌘W/Ctrl+W 关窗兜底：document 级 keydown（`(metaKey || e.ctrlKey)` 不做平台
 * 分支），enabled 时 preventDefault 并调用 `getCurrentWindow().close()`——走
 * Rust 侧 `CloseRequested → prevent_close + hide` 拦截链。
 *
 * 适用 panel 非 devWorkbench 菜单页；devWorkbench 页有页面级 ⌘W 处理器，不挂本 hook。
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
