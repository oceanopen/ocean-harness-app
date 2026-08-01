import type { WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import type { ReactNode } from 'react';

// panel 顶级页面标识（与 PanelApp 的 MenuKey 同源；此处为 SSOT，PanelApp 反向 import 复用，
// 避免 union 字面量双份维护导致不一致）。后端 EVENT_PANEL_NAVIGATE payload 亦为同集合字符串。
export type MenuKey = 'claudeSessions' | 'repositories' | 'serverStatus' | 'tracker';

// 命令分组：导航 / 动作 / 跳转。决定 Dialog 内 Command.Group 的渲染顺序与分组标题。
export type CommandGroup = 'navigation' | 'action' | 'jump';

// 命令面板二级选择页：跳到工作空间 / 跳到项目。选中"跳转"类命令时切入，展示实体列表供二次过滤选定。
// 借鉴 plane power-k 的 change-page 子页面模式，但裁剪为静态两页（v1 不做工作项属性级多页）。
export type CommandSubPage = 'workspace' | 'project';

// 命令运行期上下文：同时承载"面板自身状态"与"宿主导航/动作能力"。
// 命令的 isVisible / action 接收本类型，因此命令即数据、与 UI 解耦（借鉴 plane TPowerKContext 思路，
// 合并为单 context，避免 plane 那套 MobX store + 多 context 的复杂度）。
export interface CommandPaletteContextValue {
  // 面板自身状态
  isOpen: boolean;
  open: () => void;
  close: () => void;
  toggle: () => void;
  subPage: CommandSubPage | null;
  setSubPage: (page: CommandSubPage | null) => void;

  // 宿主：导航与动作
  activeMenu: MenuKey;
  navigate: (menu: MenuKey) => void;
  openSettings: () => void;
  toggleSidebar: () => void;

  // 宿主：tracker 跳转（二级页面选中实体后回写）
  currentWorkspaceId: number | null;
  selectWorkspace: (ws: WorkspaceModel) => void;
  selectWorkspaceProject: (workspaceProject: WorkspaceProjectModel) => void;
}

// CommandConfig：一条命令的声明式描述。借鉴 plane TPowerKCommandConfig，
// 裁剪掉 v1 不需要的 keySequence / modifierShortcut / isEnabled / 动态 contextType，
// 保留"命令即数据 + 动态可见 + 关键词搜索"三要素。action 经 ctx 调用宿主能力，命令本身不持有 UI。
export interface CommandConfig {
  id: string;
  group: CommandGroup;
  // i18n 标题 key（如 'panel:commandPalette.nav.tracker'），同时作为 cmdk 过滤 value。
  titleI18nKey: string;
  icon: ReactNode;
  // cmdk 额外搜索关键词（别名/英文等），与标题一同参与模糊匹配。
  keywords?: string[];
  // 展示用快捷键提示（v1 仅展示，不实现按键分发）。
  shortcut?: string;
  // 运行期动态可见（如"跳到项目"仅在某工作空间已选中时出现）。
  isVisible?: (ctx: CommandPaletteContextValue) => boolean;
  // 选中后执行；经 ctx 调用宿主导航/动作或切换二级页面。
  action: (ctx: CommandPaletteContextValue) => void;
  // 选中后是否关闭面板（导航/动作关闭；跳转切二级页面不关闭）。
  closeOnSelect: boolean;
}
