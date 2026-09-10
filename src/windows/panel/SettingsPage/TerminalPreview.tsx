import type { TerminalViewTheme } from '@src/windows/panel/DevWorkbenchPage/components/EmbeddedTerminal/terminalTheme';
import { Box } from '@mui/material';
import { Terminal } from '@xterm/xterm';
import { useEffect, useRef } from 'react';
import '@xterm/xterm/css/xterm.css';

// 预览尺寸（orca TerminalSettingsPreview 范式）：36 列钉死防内容折行
// （最长行 32 字符 + 余量），15 行容纳全部样例。
const PREVIEW_COLS = 36;
const PREVIEW_ROWS = 15;

// ANSI 样例 buffer：覆盖 16 色 + 测试输出形态（PASS/✓/✗）+ 代码片段，
// 让主题差异（尤其 ANSI 色板）一眼可辨。行宽 ≤ 32 字符防折行。
const RESET = '\x1B[0m';
const DIM = '\x1B[2m';
const ITALIC = '\x1B[3m';
const RED = '\x1B[31m';
const GREEN = '\x1B[32m';
const YELLOW = '\x1B[33m';
const BLUE = '\x1B[34m';
const MAGENTA = '\x1B[35m';
const CYAN = '\x1B[36m';
const BG_GREEN = '\x1B[42m';
const FG_BLACK = '\x1B[30m';
const BRIGHT = (color: string, text: string) => `\x1B[1${color[3]}m${text}${RESET}`;

const PROMPT = `${BLUE}~/we-term${RESET} ${MAGENTA}main${RESET} ${YELLOW}*${RESET} $ `;

const PREVIEW_LINES = [
  `${PROMPT}pnpm test`,
  ` ${BG_GREEN}${FG_BLACK} PASS ${RESET} terminal.test.ts`,
  ` ${GREEN}✓${RESET} renders sample output ${DIM}(3ms)${RESET}`,
  ` ${RED}✗${RESET} ${BRIGHT(RED, 'bold red error')}${RESET}`,
  ``,
  `${YELLOW}def${RESET} ${CYAN}total${RESET}(xs: list[${CYAN}int${RESET}]) -> ${CYAN}int${RESET}:`,
  `    ${ITALIC}${GREEN}"""Sum the values."""${RESET}`,
  `    ${RESET}return ${BRIGHT(BLUE, 'sum')}${RESET}(xs)`,
  ``,
  `${PROMPT}${DIM}ls -la docs/${RESET}`,
  `${BLUE}drwxr-xr-x${RESET}  terminal_04.md`,
  `${GREEN}-rw-r--r--${RESET}  terminal_05.md`,
  ``,
  `${PROMPT}git log --oneline -1`,
  `${YELLOW}c19275c${RESET} feat: terminal settings`,
];

interface TerminalPreviewProps {
  // 渲染主题（draft 值构建，随选择实时变）
  theme: TerminalViewTheme;
  fontSize: number;
  cursorStyle: 'block' | 'bar' | 'underline';
  cursorBlink: boolean;
  lineHeight: number;
}

// TerminalPreview：设置页内真实小终端（36x15），随 draft 值实时预览主题/字号/
// 光标/行高（orca TerminalSettingsPreview 范式）。单实例 mount 一次 + options
// 运行时赋值（与生产 TerminalView 同机制）；不装 webgl/fit（固定尺寸，DOM
// 渲染器足够）。独立组件文件（fast-refresh 约束同 terminalTheme.ts 注释）。
export default function TerminalPreview({ theme, fontSize, cursorStyle, cursorBlink, lineHeight }: TerminalPreviewProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (container == null) {
      return;
    }
    const terminal = new Terminal({
      allowProposedApi: true,
      cursorBlink,
      cursorStyle,
      fontSize,
      lineHeight,
      scrollback: 0,
      cols: PREVIEW_COLS,
      rows: PREVIEW_ROWS,
      // dimOpacity 非 xterm 字段，剥离（与 TerminalView 主题 effect 同款处理）
      theme: (() => {
        const { dimOpacity: _dim, ...palette } = theme;
        return palette;
      })(),
    });
    terminal.open(container);
    terminal.write(PREVIEW_LINES.join('\r\n'));
    terminalRef.current = terminal;
    return () => {
      terminalRef.current = null;
      terminal.dispose();
      container.replaceChildren();
    };
  // 初值仅 mount 一次；后续变化走下方运行时 effect（单实例复用）。
  }, []);

  // 全量 options 运行时同步（draft 值 → 预览即时变）。
  useEffect(() => {
    const terminal = terminalRef.current;
    if (terminal == null) {
      return;
    }
    const { dimOpacity: _dim, ...palette } = theme;
    terminal.options.theme = palette;
    terminal.options.fontSize = fontSize;
    terminal.options.cursorStyle = cursorStyle;
    terminal.options.cursorBlink = cursorBlink;
    terminal.options.lineHeight = lineHeight;
  }, [theme, fontSize, cursorStyle, cursorBlink, lineHeight]);

  return (
    <Box
      ref={containerRef}
      sx={{ width: 'fit-content', borderRadius: 1, overflow: 'hidden', border: 1, borderColor: 'divider' }}
    />
  );
}
