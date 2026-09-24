import type { Platform } from '@src/shared/platform';
// 「打开工作区目录」工具目录：id 值域 + 平台过滤 + 打开命令分发的唯一 SSOT（图标
// 组件在 openToolIcons.tsx）。新增工具 = 目录表追加一项（id/label/Icon/platforms），
// 浮层与胶囊图标自动出现；分发函数 openWorkspaceDir 同步补一条 case。
//
// 不做本机安装检测——未安装的工具点击后由打开命令返回 Err、前端 toast 提示
// （RepositoriesPage 同款口径）；平台过滤仅按 OS 静态收窄展示项。
import type { ReactElement } from 'react';
import type { ToolIconProps } from './openToolIcons';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { currentPlatform } from '@src/shared/platform';
import { FinderIcon, ITerm2Icon, TerminalAppIcon, VsCodeIcon, WindowsTerminalIcon } from './openToolIcons';

// ─── id 值域（config workspace_open_tool 的存储值全集，跨平台共用一份）───

export type WorkspaceOpenToolId = 'finder' | 'vscode' | 'iterm2' | 'terminal' | 'windows-terminal';

export const WORKSPACE_OPEN_TOOL_IDS: readonly WorkspaceOpenToolId[] = [
  'finder',
  'vscode',
  'iterm2',
  'terminal',
  'windows-terminal',
];

// 默认值（值域 SSOT 同文件持有，TERMINAL_THEME 先例同款——appConfig 只留 key）。
export const DEFAULT_WORKSPACE_OPEN_TOOL: WorkspaceOpenToolId = 'vscode';

// 配置 decode：全集粗校验（含跨平台值），非法/缺失回退默认 vscode。
// 平台相关的回退（配置值不属于当前平台）由 resolveWorkspaceOpenTool 在渲染期处理。
// 模块级函数保证引用稳定（useConfigValue 依赖项要求）。
export function decodeWorkspaceOpenTool(raw: string | null): WorkspaceOpenToolId {
  return WORKSPACE_OPEN_TOOL_IDS.includes(raw as WorkspaceOpenToolId)
    ? (raw as WorkspaceOpenToolId)
    : DEFAULT_WORKSPACE_OPEN_TOOL;
}

// ─── 工具目录 ───

// 单个打开工具：id 见值域；label 为浮层/无障碍文案；Icon 见 openToolIcons；
// platforms 为展示平台集合（渲染期过滤，非安装检测）。
export interface WorkspaceOpenTool {
  id: WorkspaceOpenToolId;
  label: string;
  Icon: (props: ToolIconProps) => ReactElement;
  platforms: readonly Platform[];
}

// finder 即「系统文件管理器」：Rust open_in_file_manager 跨平台（open/explorer/xdg-open），
// label 按当前平台命名（模块加载期一次求值，运行期平台恒定）。
export const WORKSPACE_OPEN_TOOLS: readonly WorkspaceOpenTool[] = [
  {
    id: 'finder',
    label: currentPlatform === 'macos' ? 'Finder' : currentPlatform === 'windows' ? '文件资源管理器' : '文件管理器',
    Icon: FinderIcon,
    platforms: ['macos', 'windows', 'linux'],
  },
  {
    id: 'vscode',
    label: 'VSCode',
    Icon: VsCodeIcon,
    platforms: ['macos', 'windows', 'linux'],
  },
  {
    id: 'iterm2',
    label: 'iTerm2',
    Icon: ITerm2Icon,
    platforms: ['macos'],
  },
  {
    id: 'terminal',
    label: 'Terminal',
    Icon: TerminalAppIcon,
    platforms: ['macos'],
  },
  {
    id: 'windows-terminal',
    label: 'Windows Terminal',
    Icon: WindowsTerminalIcon,
    platforms: ['windows'],
  },
];

// 当前平台可见的工具列表（浮层渲染数据源）。
export function workspaceOpenToolsFor(platform: Platform): readonly WorkspaceOpenTool[] {
  return WORKSPACE_OPEN_TOOLS.filter(t => t.platforms.includes(platform));
}

// 渲染期解析有效默认工具：配置值不属于当前平台目录（跨平台共用一份存储的换机/
// 改配置场景）时回退默认。decodeWorkspaceOpenTool 已保证值在全集内，此处只做平台收窄。
export function resolveWorkspaceOpenTool(configured: WorkspaceOpenToolId, platform: Platform): WorkspaceOpenToolId {
  return workspaceOpenToolsFor(platform).some(t => t.id === configured)
    ? configured
    : DEFAULT_WORKSPACE_OPEN_TOOL;
}

// 用指定工具打开目录（纯分发，无状态）：finder→文件管理器、vscode→编辑器命令（macOS
// 内部走 open -a 规避 GUI PATH 问题）、其余→终端命令（macOS AppleScript / Windows wt -d）。
// 返回 Promise，失败 throw string（调用方 toast），成功后调用方才落默认值（串行语义）。
export function openWorkspaceDir(toolId: WorkspaceOpenToolId, dir: string): Promise<null> {
  switch (toolId) {
    case 'finder':
      return unwrap(commands.openInFileManager(dir));
    case 'vscode':
      return unwrap(commands.openInEditor('vscode', dir));
    case 'iterm2':
      return unwrap(commands.openInTerminal('iterm2', dir));
    case 'terminal':
      return unwrap(commands.openInTerminal('terminal', dir));
    case 'windows-terminal':
      return unwrap(commands.openInTerminal('windows-terminal', dir));
  }
}
