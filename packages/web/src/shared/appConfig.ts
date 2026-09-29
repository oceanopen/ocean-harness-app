import type { YesNo } from './bindings';
import { commands } from './bindings';
import { unwrap } from './commands';

// —— Rust 单源常量 re-export（SSOT：app/src/shared/app_config.rs 等，经
// .constant() 生成于 bindings.ts）。值类型为字面量类型（as const），消费方类型安全。 ——
export {
  DEFAULT_ITERM2_SPLIT_DIRECTION,
  DEFAULT_POLL_INTERVAL_SECS,
  DEFAULT_TERMINAL_POST_OPEN_COMMAND,
  GITHUB_PAT_KEY,
  ITERM2_SPLIT_DIRECTION_KEY,
  LANGUAGE_KEY,
  MAX_POLL_INTERVAL_SECS,
  MIN_POLL_INTERVAL_SECS,
  POLL_INTERVAL_SECS_KEY,
  TERMINAL_POST_OPEN_COMMAND_KEY,
} from './bindings';

// GitHub Personal Access Token 默认值：空串 = 未配置（敏感值，设置页不回显明文）。
export const DEFAULT_GITHUB_PAT = '';

// 本文件是前端配置项消费的统一出口。两类来源：
// 1. 后端读取的 key：SSOT 在 app/src/shared/app_config.rs，经 .constant()
//    导出到 bindings.ts，此处仅 re-export。
// 2. 纯前端 key：本文件定义即 SSOT，后端不读取。

// Y/N 布尔风格配置值（satisfies 关联后端 YesNo 类型，值漂移编译报错）。
export const YES_NO = {
  YES: 'Y',
  NO: 'N',
} as const satisfies Record<string, YesNo>;

export function isYes(value: string | null): boolean {
  return value === YES_NO.YES;
}

export function toYesNo(value: boolean): YesNo {
  return value ? YES_NO.YES : YES_NO.NO;
}

export function parseYesNo(value: string | null, fallback: YesNo): YesNo {
  return value === YES_NO.YES || value === YES_NO.NO ? value : fallback;
}

export type Appearance = 'system' | 'light' | 'dark';

export const APPEARANCE_KEY = 'appearance';
export const DEFAULT_APPEARANCE: Appearance = 'system';

export type Language = 'system' | 'zh-CN' | 'en';

export type ResolvedLanguage = Exclude<Language, 'system'>;

export const DEFAULT_LANGUAGE: Language = 'system';

// iTerm2 分屏方向。horizontal = 上下分屏，vertical = 左右分屏，none = 不分屏。
// 类型与默认值均为字面量联合，默认值从 Rust 单源 re-export（as const 兼容）。
export type Iterm2SplitDirection = 'horizontal' | 'vertical' | 'none';

// 嵌入式终端启动时自动运行的编程 CLI（PTY 直接 spawn，无 shell 中转）。
// 'none' = 开普通 shell（默认）；'claude' = 自动启动 claude。
export type TerminalStartupCodeCli = 'none' | 'claude';

export const TERMINAL_STARTUP_CODE_CLI_KEY = 'terminal_startup_code_cli';
export const DEFAULT_TERMINAL_STARTUP_CODE_CLI: TerminalStartupCodeCli = 'none';

export function parseTerminalStartupCodeCli(value: string | null): TerminalStartupCodeCli {
  // '' 是旧存储值，归一为 'none'（MUI Select 无法匹配空字符串为选中值）
  return value === 'claude' ? value : 'none';
}

// 嵌入式终端字号。离散选项：脏值一律回落默认 12。
export const TERMINAL_FONT_SIZE_KEY = 'terminal_font_size';
export const TERMINAL_FONT_SIZE_OPTIONS = [10, 11, 12, 13, 14, 15, 16] as const;
export type TerminalFontSize = (typeof TERMINAL_FONT_SIZE_OPTIONS)[number];
export const DEFAULT_TERMINAL_FONT_SIZE: TerminalFontSize = 12;

export function parseTerminalFontSize(value: string | null): TerminalFontSize {
  const parsed = value != null ? Number.parseInt(value, 10) : Number.NaN;
  return (TERMINAL_FONT_SIZE_OPTIONS as readonly number[]).includes(parsed)
    ? (parsed as TerminalFontSize)
    : DEFAULT_TERMINAL_FONT_SIZE;
}

// 嵌入式终端回滚缓冲行数。离散选项：脏值回落默认 1000。
export const TERMINAL_SCROLLBACK_ROWS_KEY = 'terminal_scrollback_rows';
export const TERMINAL_SCROLLBACK_ROWS_OPTIONS = [1000, 2000, 3000, 5000] as const;
export type TerminalScrollbackRows = (typeof TERMINAL_SCROLLBACK_ROWS_OPTIONS)[number];
export const DEFAULT_TERMINAL_SCROLLBACK_ROWS: TerminalScrollbackRows = 1000;

export function parseTerminalScrollbackRows(value: string | null): TerminalScrollbackRows {
  const parsed = value != null ? Number.parseInt(value, 10) : Number.NaN;
  return (TERMINAL_SCROLLBACK_ROWS_OPTIONS as readonly number[]).includes(parsed)
    ? (parsed as TerminalScrollbackRows)
    : DEFAULT_TERMINAL_SCROLLBACK_ROWS;
}

// 终端主题 id。合法值/parse 在 terminalTheme.ts（值域 SSOT 同文件）。
export const TERMINAL_THEME_KEY = 'terminal_theme';

// 终端光标样式，默认 block。
export type TerminalCursorStyle = 'block' | 'bar' | 'underline';
export const TERMINAL_CURSOR_STYLE_KEY = 'terminal_cursor_style';
export const DEFAULT_TERMINAL_CURSOR_STYLE: TerminalCursorStyle = 'block';
export const TERMINAL_CURSOR_STYLE_OPTIONS = ['block', 'bar', 'underline'] as const;

export function parseTerminalCursorStyle(value: string | null): TerminalCursorStyle {
  return (TERMINAL_CURSOR_STYLE_OPTIONS as readonly string[]).includes(value ?? '')
    ? (value as TerminalCursorStyle)
    : DEFAULT_TERMINAL_CURSOR_STYLE;
}

// 终端光标闪烁开关：YesNo，默认 YES。
export const TERMINAL_CURSOR_BLINK_KEY = 'terminal_cursor_blink';
export const DEFAULT_TERMINAL_CURSOR_BLINK = YES_NO.YES;

// 终端行高：离散选项，默认 1。
export const TERMINAL_LINE_HEIGHT_KEY = 'terminal_line_height';
export const TERMINAL_LINE_HEIGHT_OPTIONS = [1, 1.1, 1.2, 1.3, 1.4, 1.5] as const;
export type TerminalLineHeight = (typeof TERMINAL_LINE_HEIGHT_OPTIONS)[number];
export const DEFAULT_TERMINAL_LINE_HEIGHT: TerminalLineHeight = 1;

export function parseTerminalLineHeight(value: string | null): TerminalLineHeight {
  const parsed = value != null ? Number.parseFloat(value) : Number.NaN;
  return (TERMINAL_LINE_HEIGHT_OPTIONS as readonly number[]).includes(parsed)
    ? (parsed as TerminalLineHeight)
    : DEFAULT_TERMINAL_LINE_HEIGHT;
}

// HTTP 本地服务端口已彻底固化（Rust 编译期常量：dev=9000/release=9100），无前端配置项；
// 前端消费服务地址一律走 http_server_status 实时获取，不持有端口字面量。

// panel 窗口侧边栏折叠状态：YesNo，缺失视为 NO（默认展开）。
export const PANEL_SIDEBAR_COLLAPSED_KEY = 'panel_sidebar_collapsed';
export const DEFAULT_PANEL_SIDEBAR_COLLAPSED = YES_NO.NO;

// 开发工作台左栏 issue 任务树折叠状态：YesNo，缺失视为 NO（默认展开）。
export const PANEL_DEV_TREE_COLLAPSED_KEY = 'panel_dev_tree_collapsed';
export const DEFAULT_PANEL_DEV_TREE_COLLAPSED = YES_NO.NO;

// 开发工作台右侧工具面板区折叠状态：YesNo，缺失视为 YES（默认收起）。
export const PANEL_DEV_TOOL_AREA_COLLAPSED_KEY = 'panel_dev_tool_area_collapsed';
export const DEFAULT_PANEL_DEV_TOOL_AREA_COLLAPSED = YES_NO.YES;

// 开发工作台工具面板区宽度（px）。值为数字字符串，脏值由消费方 decode 回落默认。
export const PANEL_DEV_TOOL_AREA_WIDTH_KEY = 'panel_dev_tool_area_width';
export const DEFAULT_PANEL_DEV_TOOL_AREA_WIDTH = 600;

// 工作区目录默认打开工具（开发工作台标题栏胶囊按钮）。
// 值域/默认值/decode 在 OpenWorkspaceDir/openTools.tsx。
export const WORKSPACE_OPEN_TOOL_KEY = 'workspace_open_tool';

// commands.xxx() 返回 tauri-specta 的 typedError 包装。unwrap 展开为 throw 风格，
// 保持 getAppConfig/setAppConfig 的对外 API 不变（错误时 throw）。
export async function getAppConfig(key: string): Promise<string | null> {
  return unwrap(commands.getAppConfig(key));
}

export async function setAppConfig(key: string, value: string): Promise<void> {
  await unwrap(commands.setAppConfig(key, value));
}
