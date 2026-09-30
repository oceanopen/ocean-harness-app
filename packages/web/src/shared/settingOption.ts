import type { Appearance, Iterm2SplitDirection, Language } from './appConfig';

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

export interface Iterm2SplitDirectionOption {
  value: Iterm2SplitDirection;
  labelKey: string;
}

export const iterm2SplitDirectionOptions: Iterm2SplitDirectionOption[] = [
  { value: 'horizontal', labelKey: 'settings:terminal.option.splitHorizontal' },
  { value: 'vertical', labelKey: 'settings:terminal.option.splitVertical' },
  { value: 'none', labelKey: 'settings:terminal.option.splitNone' },
];
