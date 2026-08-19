import type { Appearance, Iterm2SplitDirection, Language, TerminalStartupCodeCli } from './appConfig';

export interface LanguageOption {
  value: Language;
  labelKey: string;
}

export const languageOptions: LanguageOption[] = [
  { value: 'system', labelKey: 'settings:app.option.followSystem' },
  { value: 'zh-CN', labelKey: 'settings:app.option.chinese' },
  { value: 'en', labelKey: 'settings:app.option.english' },
];

export interface TerminalStartupCodeCliOption {
  value: TerminalStartupCodeCli;
  labelKey: string;
}

// 嵌入式终端启动自动运行的编程 CLI 工具选项。扩 codex 等新工具时在此加项 +
// appConfig.ts 的 TerminalStartupCodeCli 联合类型加值。
export const terminalStartupCodeCliOptions: TerminalStartupCodeCliOption[] = [
  { value: '', labelKey: 'settings:terminal.option.startupCodeCliNone' },
  { value: 'claude', labelKey: 'settings:terminal.option.startupCodeCliClaude' },
];

export interface AppearanceOption {
  value: Appearance;
  labelKey: string;
}

export const appearanceOptions: AppearanceOption[] = [
  { value: 'system', labelKey: 'settings:app.option.followSystem' },
  { value: 'light', labelKey: 'settings:app.option.light' },
  { value: 'dark', labelKey: 'settings:app.option.dark' },
];

export interface Iterm2SplitDirectionOption {
  value: Iterm2SplitDirection;
  labelKey: string;
}

export const iterm2SplitDirectionOptions: Iterm2SplitDirectionOption[] = [
  { value: 'horizontal', labelKey: 'settings:terminal.option.splitHorizontal' },
  { value: 'vertical', labelKey: 'settings:terminal.option.splitVertical' },
  { value: 'none', labelKey: 'settings:terminal.option.splitNone' },
];
