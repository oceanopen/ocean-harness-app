import type { Appearance } from './appConfig';
import { createTheme, CssBaseline, ThemeProvider } from '@mui/material';
import { useMemo } from 'react';
import { APPEARANCE_KEY, DEFAULT_APPEARANCE } from './appConfig';
import { useConfigValue } from './useConfigValue';
import { useSystemThemeMode } from './useSystemTheme';

interface Props {
  children: React.ReactNode;
}

function isAppearance(v: string | null): v is Appearance {
  return v === 'system' || v === 'light' || v === 'dark';
}

// 模块级 decode：稳定引用，避免 useConfigValue 每次渲染重复订阅。
function decodeAppearance(v: string | null): Appearance {
  return isAppearance(v) ? v : DEFAULT_APPEARANCE;
}

export default function AppThemeProvider({ children }: Props) {
  const appearance = useConfigValue(APPEARANCE_KEY, decodeAppearance, DEFAULT_APPEARANCE);
  const systemMode = useSystemThemeMode();

  const resolvedMode = appearance === 'system' ? systemMode : appearance;

  const theme = useMemo(
    () =>
      createTheme({
        palette: { mode: resolvedMode },
        // 全局默认字号 12（MUI 默认 14 偏大）：表单提示文案/输入值随之缩小。
        typography: { fontSize: 12 },
        components: {
          // MuiButton 默认 text-transform: uppercase 会把 "iTerm2" 渲染成 "ITEM2"，
          // 全局关掉，让按钮文案保持原样大小写。
          MuiButton: {
            styleOverrides: { root: { textTransform: 'none' } },
          },
          // MuiTab 同款问题（根样式默认 uppercase）：文件预览 tab 的文件名被整体大写，
          // 与磁盘实际文件名（大小写敏感）不一致——全局关掉，tab 文案保持原样。
          MuiTab: {
            styleOverrides: { root: { textTransform: 'none' } },
          },
          // 全局关闭 webview（macOS WKWebView）对输入框的自动大写/自动纠正/拼写检查，
          // 避免输入英文时首字母被自动大写。作用于所有 InputBase 派生组件（TextField/Select 输入框等）。
          MuiInputBase: {
            defaultProps: {
              slotProps: {
                input: { spellCheck: false, autoCapitalize: 'off', autoCorrect: 'off' },
              },
            },
          },
        },
      }),
    [resolvedMode],
  );

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      {children}
    </ThemeProvider>
  );
}
