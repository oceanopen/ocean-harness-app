import type { CommandPaletteContextValue } from './types';
import { createContext, use } from 'react';

// 命令面板上下文（单 context 持有面板自身状态 + 宿主能力，见 types.CommandPaletteContextValue）。
// 独立成文件：CommandPaletteProvider.tsx 仅默认导出组件，避免与 useCommandPalette 共存触发
// react-refresh/only-export-components（fast refresh 要求文件只导出组件）。
export const CommandPaletteContext = createContext<CommandPaletteContextValue | null>(null);

// 消费命令面板上下文；缺失 Provider 直接报错，避免静默拿到 null 导致运行期崩溃。
export function useCommandPalette(): CommandPaletteContextValue {
  const ctx = use(CommandPaletteContext);
  if (!ctx) {
    throw new Error('useCommandPalette 必须在 CommandPaletteProvider 内使用');
  }
  return ctx;
}
