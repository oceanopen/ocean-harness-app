import type { Language, ResolvedLanguage } from './appConfig';
import { useEffect } from 'react';
import { I18nextProvider } from 'react-i18next';
import { DEFAULT_LANGUAGE, LANGUAGE_KEY } from './appConfig';
import i18n from './i18n';
import { useConfigValue } from './useConfigValue';
import { useSystemLanguage } from './useSystemLanguage';

interface Props {
  children: React.ReactNode;
}

function isLanguage(v: string | null): v is Language {
  return v === 'system' || v === 'zh-CN' || v === 'en';
}

// 模块级 decode：稳定引用，避免 useConfigValue 每次渲染重复订阅。
function decodeLanguage(v: string | null): Language {
  return isLanguage(v) ? v : DEFAULT_LANGUAGE;
}

function applyLanguage(resolved: ResolvedLanguage) {
  if (i18n.language !== resolved) {
    void i18n.changeLanguage(resolved);
  }
  if (typeof document !== 'undefined') {
    document.documentElement.lang = resolved;
  }
}

export default function AppI18nProvider({ children }: Props) {
  const language = useConfigValue(LANGUAGE_KEY, decodeLanguage, DEFAULT_LANGUAGE);
  const systemLanguage = useSystemLanguage();

  const resolved: ResolvedLanguage = language === 'system' ? systemLanguage : language;

  useEffect(() => {
    applyLanguage(resolved);
  }, [resolved]);

  return <I18nextProvider i18n={i18n}>{children}</I18nextProvider>;
}
