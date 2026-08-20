// 终端主题（terminal_05：用户自选暗色主题目录，不跟随 app 明暗）。
// 色值来源 orca terminal-themes/popular-dark-core.ts（权威调色板照抄）。
// 初版（terminal_03 收尾）的 Tango 明暗双主题（hello-halo 移植）已退役为
// 本目录的参考来源；dimOpacity 渲染期消费（exited 置灰），不进 xterm options。
// 独立文件：TerminalView.tsx 是 fast-refresh 组件文件，导出非组件会被
// react-refresh/only-export-components 拦截。

export interface TerminalViewTheme {
  background: string;
  foreground: string;
  cursor: string;
  cursorAccent: string;
  selectionBackground: string;
  selectionForeground: string;
  black: string;
  red: string;
  green: string;
  yellow: string;
  blue: string;
  magenta: string;
  cyan: string;
  white: string;
  brightBlack: string;
  brightRed: string;
  brightGreen: string;
  brightYellow: string;
  brightBlue: string;
  brightMagenta: string;
  brightCyan: string;
  brightWhite: string;
  // 会话已结束时终端置灰（视觉上与活动会话区分）
  dimOpacity: number;
}

// 主题目录（全部暗色高频）。默认 we-dark = Dracula × halo 暗色融合（2026-08-20
// 用户定稿）：底色取 halo 深黑档与 Dracula 的插值（#1a1a1f，比 Dracula 更沉）、
// 前景亮化（#f5f5f0）、ANSI 彩色全保 Dracula（鲜活来源）、black 压暗一档保层次、
// 选区紫基调（Dracula 识别度）偏 halo 蓝。其余五套 orca popular-dark-core 照抄。
export const TERMINAL_THEME_IDS = [
  'we-dark',
  'dracula',
  'one-dark',
  'tokyo-night',
  'gruvbox-dark',
  'catppuccin-mocha',
  'nord',
] as const;
export type TerminalThemeId = (typeof TERMINAL_THEME_IDS)[number];
export const DEFAULT_TERMINAL_THEME_ID: TerminalThemeId = 'we-dark';

// xterm options 主题部分（不含 dimOpacity）。
type TerminalPalette = Omit<TerminalViewTheme, 'dimOpacity'>;

const TERMINAL_THEMES: Record<TerminalThemeId, TerminalPalette> = {
  'we-dark': {
    background: '#1a1a1f',
    foreground: '#f5f5f0',
    cursor: '#f8f8f2',
    cursorAccent: '#1a1a1f',
    selectionBackground: '#3b4963',
    selectionForeground: '#f5f5f0',
    black: '#18181c',
    red: '#ff5555',
    green: '#50fa7b',
    yellow: '#f1fa8c',
    blue: '#bd93f9',
    magenta: '#ff79c6',
    cyan: '#8be9fd',
    white: '#f8f8f2',
    brightBlack: '#6272a4',
    brightRed: '#ff6e6e',
    brightGreen: '#69ff94',
    brightYellow: '#ffffa5',
    brightBlue: '#d6acff',
    brightMagenta: '#ff92df',
    brightCyan: '#a4ffff',
    brightWhite: '#ffffff',
  },
  'dracula': {
    background: '#282a36',
    foreground: '#f8f8f2',
    cursor: '#f8f8f2',
    cursorAccent: '#282a36',
    selectionBackground: '#44475a',
    selectionForeground: '#f8f8f2',
    black: '#21222c',
    red: '#ff5555',
    green: '#50fa7b',
    yellow: '#f1fa8c',
    blue: '#bd93f9',
    magenta: '#ff79c6',
    cyan: '#8be9fd',
    white: '#f8f8f2',
    brightBlack: '#6272a4',
    brightRed: '#ff6e6e',
    brightGreen: '#69ff94',
    brightYellow: '#ffffa5',
    brightBlue: '#d6acff',
    brightMagenta: '#ff92df',
    brightCyan: '#a4ffff',
    brightWhite: '#ffffff',
  },
  'one-dark': {
    background: '#282c34',
    foreground: '#abb2bf',
    cursor: '#528bff',
    cursorAccent: '#282c34',
    selectionBackground: '#3e4451',
    selectionForeground: '#abb2bf',
    black: '#282c34',
    red: '#e06c75',
    green: '#98c379',
    yellow: '#e5c07b',
    blue: '#61afef',
    magenta: '#c678dd',
    cyan: '#56b6c2',
    white: '#abb2bf',
    brightBlack: '#5c6370',
    brightRed: '#e06c75',
    brightGreen: '#98c379',
    brightYellow: '#e5c07b',
    brightBlue: '#61afef',
    brightMagenta: '#c678dd',
    brightCyan: '#56b6c2',
    brightWhite: '#ffffff',
  },
  'tokyo-night': {
    background: '#1a1b26',
    foreground: '#c0caf5',
    cursor: '#c0caf5',
    cursorAccent: '#1a1b26',
    selectionBackground: '#33467c',
    selectionForeground: '#c0caf5',
    black: '#15161e',
    red: '#f7768e',
    green: '#9ece6a',
    yellow: '#e0af68',
    blue: '#7aa2f7',
    magenta: '#bb9af7',
    cyan: '#7dcfff',
    white: '#a9b1d6',
    brightBlack: '#414868',
    brightRed: '#f7768e',
    brightGreen: '#9ece6a',
    brightYellow: '#e0af68',
    brightBlue: '#7aa2f7',
    brightMagenta: '#bb9af7',
    brightCyan: '#7dcfff',
    brightWhite: '#c0caf5',
  },
  'gruvbox-dark': {
    background: '#282828',
    foreground: '#ebdbb2',
    cursor: '#ebdbb2',
    cursorAccent: '#282828',
    selectionBackground: '#504945',
    selectionForeground: '#ebdbb2',
    black: '#282828',
    red: '#cc241d',
    green: '#98971a',
    yellow: '#d79921',
    blue: '#458588',
    magenta: '#b16286',
    cyan: '#689d6a',
    white: '#a89984',
    brightBlack: '#928374',
    brightRed: '#fb4934',
    brightGreen: '#b8bb26',
    brightYellow: '#fabd2f',
    brightBlue: '#83a598',
    brightMagenta: '#d3869b',
    brightCyan: '#8ec07c',
    brightWhite: '#ebdbb2',
  },
  'catppuccin-mocha': {
    background: '#1e1e2e',
    foreground: '#cdd6f4',
    cursor: '#f5e0dc',
    cursorAccent: '#1e1e2e',
    selectionBackground: '#585b70',
    selectionForeground: '#cdd6f4',
    black: '#45475a',
    red: '#f38ba8',
    green: '#a6e3a1',
    yellow: '#f9e2af',
    blue: '#89b4fa',
    magenta: '#f5c2e7',
    cyan: '#94e2d5',
    white: '#bac2de',
    brightBlack: '#585b70',
    brightRed: '#f38ba8',
    brightGreen: '#a6e3a1',
    brightYellow: '#f9e2af',
    brightBlue: '#89b4fa',
    brightMagenta: '#f5c2e7',
    brightCyan: '#94e2d5',
    brightWhite: '#a6adc8',
  },
  'nord': {
    background: '#2e3440',
    foreground: '#d8dee9',
    cursor: '#d8dee9',
    cursorAccent: '#2e3440',
    selectionBackground: '#434c5e',
    selectionForeground: '#d8dee9',
    black: '#3b4252',
    red: '#bf616a',
    green: '#a3be8c',
    yellow: '#ebcb8b',
    blue: '#81a1c1',
    magenta: '#b48ead',
    cyan: '#88c0d0',
    white: '#e5e9f0',
    brightBlack: '#4c566a',
    brightRed: '#bf616a',
    brightGreen: '#a3be8c',
    brightYellow: '#ebcb8b',
    brightBlue: '#81a1c1',
    brightMagenta: '#b48ead',
    brightCyan: '#8fbcbb',
    brightWhite: '#eceff4',
  },
};

// 非法/缺失主题 id 回落默认 Dracula。
export function parseTerminalThemeId(value: string | null): TerminalThemeId {
  return (TERMINAL_THEME_IDS as readonly string[]).includes(value ?? '')
    ? (value as TerminalThemeId)
    : DEFAULT_TERMINAL_THEME_ID;
}

// 按主题 id 取完整渲染主题（dimOpacity 拼装，EmbeddedTerminal 渲染期调用）。
export function buildTerminalTheme(themeId: TerminalThemeId): TerminalViewTheme {
  return { ...TERMINAL_THEMES[themeId], dimOpacity: 0.5 };
}
