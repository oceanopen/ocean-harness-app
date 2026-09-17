import { useEffect, useState } from 'react';

type SystemThemeMode = 'light' | 'dark';

function detectSystemThemeMode(): SystemThemeMode {
  if (typeof window === 'undefined' || !window.matchMedia) {
    return 'dark';
  }
  if (window.matchMedia('(prefers-color-scheme: light)').matches) {
    return 'light';
  }
  if (window.matchMedia('(prefers-color-scheme: dark)').matches) {
    return 'dark';
  }
  return 'dark';
}

export function useSystemThemeMode(): SystemThemeMode {
  const [mode, setMode] = useState<SystemThemeMode>(detectSystemThemeMode);

  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) {
      return;
    }
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const handler = (e: MediaQueryListEvent) => setMode(e.matches ? 'dark' : 'light');
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, []);

  return mode;
}
