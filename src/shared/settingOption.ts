import type { Appearance, Iterm2SplitDirection, Language } from './app_config';

export interface LanguageOption {
  value: Language;
  labelKey: string;
}

export const languageOptions: LanguageOption[] = [
  { value: 'system', labelKey: 'settings:option.followSystem' },
  { value: 'zh-CN', labelKey: 'settings:option.chinese' },
  { value: 'en', labelKey: 'settings:option.english' },
];

export interface AppearanceOption {
  value: Appearance;
  labelKey: string;
}

export const appearanceOptions: AppearanceOption[] = [
  { value: 'system', labelKey: 'settings:option.followSystem' },
  { value: 'light', labelKey: 'settings:option.light' },
  { value: 'dark', labelKey: 'settings:option.dark' },
];

export interface Iterm2SplitDirectionOption {
  value: Iterm2SplitDirection;
  labelKey: string;
}

export const iterm2SplitDirectionOptions: Iterm2SplitDirectionOption[] = [
  { value: 'horizontal', labelKey: 'settings:option.splitHorizontal' },
  { value: 'vertical', labelKey: 'settings:option.splitVertical' },
  { value: 'none', labelKey: 'settings:option.splitNone' },
];
