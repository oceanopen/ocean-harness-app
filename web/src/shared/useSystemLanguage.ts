import type { ResolvedLanguage } from './appConfig';
import { useEffect, useState } from 'react';

function detectSystemLanguage(): ResolvedLanguage {
  if (typeof navigator === 'undefined') {
    return 'en';
  }
  const lang = navigator.language || 'en';
  return lang.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en';
}

export function useSystemLanguage(): ResolvedLanguage {
  const [language, setLanguage] = useState<ResolvedLanguage>(detectSystemLanguage);

  useEffect(() => {
    if (typeof window === 'undefined' || !Object.hasOwn(navigator, 'languages')) {
      return;
    }
    const handler = () => setLanguage(detectSystemLanguage());
    window.addEventListener('languagechange', handler);
    return () => window.removeEventListener('languagechange', handler);
  }, []);

  return language;
}
