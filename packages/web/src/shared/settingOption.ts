import type { AcpSessionIdleTimeoutMinutes, Appearance, BrowserRefreshMs, Iterm2SplitDirection, Language } from './appConfig';

export interface LanguageOption {
  value: Language;
  labelKey: string;
}

export const languageOptions: LanguageOption[] = [
  { value: 'system', labelKey: 'settings:app.option.followSystem' },
  { value: 'zh-CN', labelKey: 'settings:app.option.chinese' },
  { value: 'en', labelKey: 'settings:app.option.english' },
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

export interface AcpSessionIdleTimeoutOption {
  value: AcpSessionIdleTimeoutMinutes;
  labelKey: string;
}

export const acpSessionIdleTimeoutOptions: AcpSessionIdleTimeoutOption[] = [
  { value: 5, labelKey: 'settings:app.option.idleMinutes' },
  { value: 15, labelKey: 'settings:app.option.idleMinutes' },
  { value: 30, labelKey: 'settings:app.option.idleMinutes' },
  { value: 60, labelKey: 'settings:app.option.idleMinutes' },
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

export interface BrowserRefreshMsOption {
  value: BrowserRefreshMs;
  labelKey: string;
}

// 渲染 label 时以 { seconds: value / 1000 } 插值（500 → 0.5）。
export const browserRefreshMsOptions: BrowserRefreshMsOption[] = [
  { value: 500, labelKey: 'settings:app.option.refreshSeconds' },
  { value: 1000, labelKey: 'settings:app.option.refreshSeconds' },
  { value: 2000, labelKey: 'settings:app.option.refreshSeconds' },
  { value: 3000, labelKey: 'settings:app.option.refreshSeconds' },
  { value: 5000, labelKey: 'settings:app.option.refreshSeconds' },
];
