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
        // 全局字号基准 12（系数 12/14）：仅作用于未覆写变体与组件内部 pxToRem 消费（图标等）。
        // 业务字号规则（用户定稿）：文字最小 12px、一律整数 px、禁止小数渲染——主题 fontSize:12
        // 的 12/14 系数会让所有变体实际渲染成小数（caption ≈10.29 / subtitle1 ≈13.71），因此
        // 下方将全部变体显式覆写为整数 px 字面值（覆写值不再过 pxToRem，即为最终渲染值）。
        // 应用字号阶梯（整数 px）：12（caption/body2/button）→ 13（subtitle2）→ 14（body1/subtitle1）
        // → 18（h6）→ 20/24/28/32/38（h5-h1 桌面紧凑阶梯，当前未用）；行高保持 MUI 无单位比值默认
        // （随字号等比缩放，非 fontSize 不受整数规则约束）。
        typography: {
          fontSize: 12,
          h1: { fontSize: 38 },
          h2: { fontSize: 32 },
          h3: { fontSize: 28 },
          h4: { fontSize: 24 },
          h5: { fontSize: 20 },
          h6: { fontSize: 18 },
          subtitle1: { fontSize: 14 },
          subtitle2: { fontSize: 13 },
          body1: { fontSize: 14 },
          body2: { fontSize: 12 },
          button: { fontSize: 12 },
          caption: { fontSize: 12 },
          overline: { fontSize: 12 },
        },
        components: {
          // MuiChip label 默认 pxToRem(13)（12/14 系数下 ≈11.1px）低于 12px 下限——显式抬到 12。
          MuiChip: {
            styleOverrides: { root: { fontSize: 12 } },
          },
          // MuiToggleButton sizeSmall 默认 pxToRem(13)（≈11.1px）同上——root 与 sizeSmall 一并覆写
          // （sizeSmall 变体类后于 root 生成，仅覆 root 会被 small 覆盖回去）。
          MuiToggleButton: {
            styleOverrides: { root: { fontSize: 12 }, sizeSmall: { fontSize: 12 } },
          },
          // MuiTooltip 默认 pxToRem(11)（≈9.4px）是全应用最小文字——抬到 12px 下限（字号走
          // tooltip 槽位，Tooltip 无 root 槽）；placement 默认 top 弹在鼠标上方不遮操作区，
          // 贴边场景显式覆盖：折叠侧边栏图标 placement="right"（PanelApp）、工作台工具条 placement="left"（WorkbenchToolRail）。
          MuiTooltip: {
            styleOverrides: { tooltip: { fontSize: 12 } },
            defaultProps: { placement: 'top' },
          },
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
