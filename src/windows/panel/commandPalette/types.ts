import type { WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import type { ReactNode } from 'react';

// panel 顶级页面标识（与 PanelApp 的 MenuKey 同源；此处为 SSOT，PanelApp 反向 import 复用，
// 避免 union 字面量双份维护导致不一致）。后端 EVENT_PANEL_NAVIGATE payload 亦为同集合字符串。
export type MenuKey = 'claudeSessions' | 'repositories' | 'serverStatus' | 'tracker' | 'devWorkbench';

// 命令分组：导航 / 动作 / 跳转。决定 Dialog 内 Command.Group 的渲染顺序与分组标题。
export type CommandGroup = 'navigation' | 'action' | 'jump';

// 命令面板二级选择页：跳到工作空间 / 跳到项目。选中"跳转"类命令时切入，展示实体列表供二次过滤选定。
// 借鉴 plane power-k 的 change-page 子页面模式，但裁剪为静态两页（v1 不做工作项属性级多页）。
export type CommandSubPage = 'workspace' | 'project';

// 命令运行期上下文：同时承载"面板自身状态"与"宿主导航/动作能力"。
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
  // 当前选中工作空间 / 项目的名称（派生自 tracker store），供 jump 命令声明式渲染名称注释。
  currentWorkspaceName: string | null;
  currentWorkspaceProjectName: string | null;
  selectWorkspace: (ws: WorkspaceModel) => void;
  selectWorkspaceProject: (workspaceProject: WorkspaceProjectModel) => void;
}

// CommandConfig：一条命令的声明式描述。借鉴 plane TPowerKCommandConfig，
// 裁剪掉 v1 不需要的 keySequence / modifierShortcut / 动态 contextType，
// 保留"命令即数据 + 关键词搜索"核心要素。isVisible（v1 仅 jump.project 用于动态可见）已移除——
// 改为恒定渲染 + isEnabled 软禁用：避免命令列表长度随上下文变化导致 cmdk 选中态失序（down 键跳回 bug），
// 同时让置灰项仍可达、回车无效，契合"先选工作空间再选项目"的引导式交互。
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
  // 运行期软禁用谓词（缺省视为启用）。为 false 时项恒渲染但置灰：仍可键盘/鼠标聚焦，回车与点击无效。
  // 注意：不用 cmdk 原生 disabled（会被踢出键盘导航，反令工作空间变末项、失序 bug 复发）。
  isEnabled?: (ctx: CommandPaletteContextValue) => boolean;
  // 软禁用时展示的提示文案 i18n key（如"需先选择工作空间"），渲染时自动括注。
  disabledHintI18nKey?: string;
  // 动态名称注释（如当前选中工作空间/项目名），返回名称字符串；渲染时自动括注为"（名称）"。
  getSubtitle?: (ctx: CommandPaletteContextValue) => string | null;
  // 选中后执行；经 ctx 调用宿主导航/动作或切换二级页面。
  action: (ctx: CommandPaletteContextValue) => void;
  // 选中后是否关闭面板（导航/动作关闭；跳转切二级页面不关闭）。
  closeOnSelect: boolean;
}
