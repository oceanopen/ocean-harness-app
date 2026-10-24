import { getCurrentWindow } from '@tauri-apps/api/window';
import { useEffect } from 'react';

/**
 * ⌘W/Ctrl+W 关窗兜底：document 级 keydown（不做平台分支），enabled 时 preventDefault 并
 * 调用 `getCurrentWindow().close()`——走 Rust 侧 `CloseRequested → prevent_close + hide` 拦截链。
 * panel 全页启用（工作台内容区另有页面级 ⌘W 处理器：关预览 tab 优先、无 tab 时同样关窗）；enabled 恒定语义由调用方保证，无陈旧闭包。
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
