import { getCurrentWindow } from '@tauri-apps/api/window';
import { useEffect } from 'react';

/**
 * ⌘W/Ctrl+W 关窗兜底：document 级 keydown（不做平台分支），enabled 时 preventDefault 并
 * 调用 `getCurrentWindow().close()`——走 Rust 侧 `CloseRequested → prevent_close + hide` 拦截链。
 * 适用 panel 非 devWorkbench 菜单页（该页有页面级 ⌘W 处理器）；enabled 恒定语义由调用方保证，无陈旧闭包。
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
