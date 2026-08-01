import type { WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import type { ReactNode } from 'react';
import type { CommandPaletteContextValue, CommandSubPage, MenuKey } from './types';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { CommandPaletteContext } from './CommandPaletteContext';
import CommandPaletteDialog from './CommandPaletteDialog';

interface CommandPaletteProviderProps {
  // 宿主注入：当前页面 + 导航/动作回调 + tracker 跳转回写。
  // 由 PanelApp 构造（持有 activeMenu 与上提后的 tracker 选择状态）。
  activeMenu: MenuKey;
  navigate: (menu: MenuKey) => void;
  openSettings: () => void;
  toggleSidebar: () => void;
  currentWorkspaceId: number | null;
  selectWorkspace: (ws: WorkspaceModel) => void;
  selectWorkspaceProject: (workspaceProject: WorkspaceProjectModel) => void;
  children: ReactNode;
}

// 命令面板 Provider：托管 isOpen / subPage 状态 + 全局 ⌘K 监听，并在树内挂载 Dialog。
// ⌘K/Ctrl+K 永远唤起（输入框内也拦），对齐 plane ShortcutHandler 的核心约定。
function CommandPaletteProvider(props: CommandPaletteProviderProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [subPage, setSubPage] = useState<CommandSubPage | null>(null);

  const open = useCallback(() => setIsOpen(true), []);
  // 关闭一并清空二级页，避免下次打开残留子页面。
  const close = useCallback(() => {
    setIsOpen(false);
    setSubPage(null);
  }, []);
  const toggle = useCallback(() => setIsOpen(v => !v), []);

  // 全局 ⌘K / Ctrl+K 切换：document 级监听，无视当前焦点（含 input/contenteditable）。
  // 借鉴 plane ShortcutHandler.handleKeyDown 的特判分支——⌘K 是面板自身的总开关，优先于"输入框不拦截"规则。
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setIsOpen(v => !v);
      }
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, []);

  // 兜底：面板关闭时复位二级页。用渲染期「据 isOpen 调整 state」模式（React 推荐）替代 effect 内
  // 同步 setState——覆盖 close() 之外的关闭路径（⌘K toggle），与 close() 内联复位双保险。
  if (!isOpen && subPage !== null) {
    setSubPage(null);
  }

  const value = useMemo<CommandPaletteContextValue>(() => ({
    isOpen,
    open,
    close,
    toggle,
    subPage,
    setSubPage,
    activeMenu: props.activeMenu,
    navigate: props.navigate,
    openSettings: props.openSettings,
    toggleSidebar: props.toggleSidebar,
    currentWorkspaceId: props.currentWorkspaceId,
    selectWorkspace: props.selectWorkspace,
    selectWorkspaceProject: props.selectWorkspaceProject,
  }), [
    isOpen,
    open,
    close,
    toggle,
    subPage,
    setSubPage,
    props.activeMenu,
    props.navigate,
    props.openSettings,
    props.toggleSidebar,
    props.currentWorkspaceId,
    props.selectWorkspace,
    props.selectWorkspaceProject,
  ]);

  return (
    <CommandPaletteContext value={value}>
      {props.children}
      <CommandPaletteDialog />
    </CommandPaletteContext>
  );
}

export default CommandPaletteProvider;
