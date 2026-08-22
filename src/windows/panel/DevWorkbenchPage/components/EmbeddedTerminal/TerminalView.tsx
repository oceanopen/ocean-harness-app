import type { IBufferRange, ILink } from '@xterm/xterm';
import type { TerminalViewTheme } from './terminalTheme';
import {
  ChatBubbleOutlined as ChatBubbleOutlineIcon,
  CloseOutlined as CloseOutlinedIcon,
  ContentCopyOutlined as ContentCopyOutlinedIcon,
  ContentPasteOutlined as ContentPasteOutlinedIcon,
  LayersClearOutlined as LayersClearOutlinedIcon,
  SearchOutlined as SearchOutlinedIcon,
  Terminal as TerminalIcon,
} from '@mui/icons-material';
import { Box, Button, IconButton, Typography } from '@mui/material';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { useToast } from '@src/shared/useToast';
import { open as openExternalUrl } from '@tauri-apps/plugin-shell';
import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';
import { WebglAddon } from '@xterm/addon-webgl';
import { Terminal } from '@xterm/xterm';
import { useCallback, useEffect, useRef, useState } from 'react';
import ClaudeIcon from './ClaudeIcon';
import NativeChatView from './NativeChat/NativeChatView';
import TerminalSearch from './TerminalSearch';
import '@xterm/xterm/css/xterm.css';

// —— 链接点击（terminal_03 §3.4）——
// 单行正则匹配（URL + 绝对文件路径），无存在性探测/跨行重组（orca 裁剪对照 §5.4）。
// hover 反馈走 ILink 默认 decorations（underline + pointer cursor）。
// 打开交互：修饰键 + Click——macOS Cmd（Ctrl+Click 是右键语义）、Windows/Linux Ctrl。
// 普通点击静默无动作（防误触——终端里链接混在输出流中，单击常是选区/聚焦意图），
// hover 下划线保留作为「此处可 Cmd/Ctrl+Click」的暗示。

// 打开修饰键判定：metaKey（macOS Cmd）|| ctrlKey（Windows/Linux）。
function hasOpenModifier(event: MouseEvent): boolean {
  return event.metaKey || event.ctrlKey;
}

// URL：http(s) 起到空白止。global 正则复用（lastIndex 状态跨调用维护，exec 循环驱动）。
const URL_PATTERN = /https?:\/\/\S+/g;
// 文件路径：/ 开头的绝对路径（不含空格段）+ 常见源码/文档扩展名，可选 :行号(:列) 后缀
// （编译器/claude 输出的 file.rs:42:7 形态）。不匹配裸目录（无扩展名，误报率高——
// ls 输出里到处是）。
const FILE_PATH_PATTERN = /(\/[\w.@+-]+)*\/[\w.-]+\.(?:rs|ts|tsx|js|jsx|go|py|java|kt|c|h|cpp|hpp|json|toml|yaml|yml|md|txt|log|sh|sql|html|css)(?::\d+(?::\d+)?)?/g;

// 匹配结果 → ILink[]（range 列 0-based index 转 1-based，end 含末字符）。
function buildLinks(
  bufferLineNumber: number,
  lineText: string,
  activate: (text: string) => void,
): ILink[] {
  const links: ILink[] = [];
  for (const pattern of [URL_PATTERN, FILE_PATH_PATTERN]) {
    pattern.lastIndex = 0;
    let match = pattern.exec(lineText);
    while (match != null) {
      const text = match[0];
      // 后缀裁剪：URL 不吞行尾标点（句号/右括号等常见于 git log 输出包裹）
      const trimmedUrl = pattern === URL_PATTERN
        ? text.replace(/[),.;'"\]}]+$/, '')
        : text;
      const finalText = trimmedUrl;
      const start = match.index + (text.length - finalText.length);
      links.push({
        range: {
          start: { x: start + 1, y: bufferLineNumber },
          end: { x: start + finalText.length, y: bufferLineNumber },
        } satisfies IBufferRange,
        text: finalText,
        // decorations 缺省 = underline + pointerCursor 全开（xterm 默认）
        activate: (event: MouseEvent, _text: string) => {
          if (!hasOpenModifier(event)) {
            return;
          }
          activate(finalText);
        },
      });
      match = pattern.exec(lineText);
    }
  }
  return links;
}

// 终端主题定义与构建在 ./terminalTheme.ts（独立文件：组件文件导出非组件会被
// react-refresh/only-export-components 拦截）。
export type { TerminalViewTheme } from './terminalTheme';

interface TerminalViewProps {
  theme: TerminalViewTheme;
  // 终端字号（terminal_font_size 配置）：运行时生效（options.fontSize 赋值 +
  // refit，不重建实例）——首个运行时可变的 option，见组件头注释。
  fontSize: number;
  // 工具栏左侧标识（如 main pane 的 'main' 标签）。不传则不渲染。
  toolbarLabel?: string;
  // 回滚缓冲行数（terminal_scrollback_rows 配置，terminal_04）：运行时纯
  // options.scrollback 赋值（不 refit 不通知 PTY——缓冲容量与尺寸无关）。
  scrollbackRows: number;
  // 光标样式（terminal_05）：block/bar/underline，运行时 options 赋值。
  cursorStyle: 'block' | 'bar' | 'underline';
  // 光标闪烁开关（terminal_05）。
  cursorBlink: boolean;
  // 行高（terminal_05）：运行时赋值后须 refit（度量变化）。
  lineHeight: number;
  // 键盘输入（xterm onData）。要求父层传稳定引用（useCallback），本组件不代理不缓存。
  onData: (data: string) => void;
  // 尺寸变化（addon-fit 实测后的 cols/rows）。同上要求稳定引用。
  onResize: (cols: number, rows: number) => void;
  // 会话已退出：终端交互禁用 + 顶部「会话已结束」条 + 重开按钮
  exited: boolean;
  onReopen: () => void;
  // 重开并启动 claude（terminal_03 §3.2）：reopen 后 spawn 强制带 startup_command，
  // 一次性覆盖 autoRun 配置。恒显示（用户 exit claude 后一键回到 claude）。
  onReopenClaude: () => void;
  // 本终端 claude 运行态（pid 父链匹配探测，useClaudeRunning）：跑着→按钮置灰；
  // 退出→恢复可用。驱动「启动 claude」按钮禁用态。
  claudeRunning: boolean;
  // chat 能力闸门（EmbeddedTerminal 派生）：主 pane + 自动运行非 none + 模式切换开。
  // false 时工具条不渲染 Terminal/Chat 切换 icon。
  chatEnabled: boolean;
  // 会话锚点（`issueId::<paneId>`）：NativeChatView 据此定位 transcript（T2.2）。
  sessionId: string;
  // 启动 claude（工具条按钮）：对活跃 shell 注入 claude\r。
  onStartClaude: () => void;
  // 关闭终端（工具栏）
  onClose: () => void;
  // 输出写入桥：mount 时上抛 write(text, replay?)，unmount 置 null。父层存 ref
  // 直读（勿走 state）。replay=true 表示历史回放（reattach scrollback）——写入
  // 期间抑制 onData（见 replayDepth 注释），防止回放流里的 DA1/CPR 查询被新 xterm
  // 实例解析并应答、应答写回 PTY 成乱码。
  onWriteReady: (write: ((text: string, replay?: boolean) => void) | null) => void;
  // 本 pane 获得键盘焦点时上抛（xterm onFocus；blur 不报——焦点只会转移，新 pane
  // 的 onFocus 自然接管活跃位）。父层据此写 terminalPanes store 的 activePanes
  // （分割/关闭作用对象跟随焦点，terminal_02 §3.4）。要求稳定引用。
  onActive?: () => void;
}

// TerminalView：xterm 封装。生命周期内单 Terminal 实例（theme 变化不重建，仅初值生效——
// panel 窗口主题切换通常伴随整页刷新，可接受）。fontSize 例外：运行时赋值生效
// （terminal_03 §3.3，首个运行时可变 option——字号改动须配 refit，theme 无此要求）。
//
// 事件处理全部函数式：mount effect 按显式顺序一次性建齐（terminal → addon → open →
// 事件接线 → focus → observer → 初始 fit），cleanup 严格逆序。回调直接用 props
// （父层保证稳定引用），不做 ref 转发层。
export default function TerminalView({ theme, fontSize, scrollbackRows, cursorStyle, cursorBlink, lineHeight, toolbarLabel, onData, onResize, exited, onReopen, onReopenClaude, claudeRunning, chatEnabled, sessionId, onStartClaude, onClose, onWriteReady, onActive }: TerminalViewProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  // 实例句柄 ref 桥（effect 闭包 → JSX 回调直读）：terminal / searchAddon / fitAddon。
  // 工具条按钮与搜索条需要实例（clear/selection/paste/findNext），不经 props
  // 下发命令对象——按钮与实例同组件，ref 直读是最近路径。fitAddon 供字号 effect
  // 运行时 refit（options.fontSize 赋值不重算 cols/rows，须手动 fit）。
  const terminalRef = useRef<Terminal | null>(null);
  const searchAddonRef = useRef<SearchAddon | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  // 复制按钮禁用态：有无选区（xterm onSelectionChange，用户交互型 → state）
  const [hasSelection, setHasSelection] = useState(false);
  // Terminal/Chat 视图模式：切换 icon 驱动（chatEnabled 才渲染 icon）。
  const [viewMode, setViewMode] = useState<'terminal' | 'chat'>('terminal');
  // 搜索条开关（terminal_03 §3.1；开关变量前缀约定）
  const [searchOpen, setSearchOpen] = useState(false);
  // 复制/粘贴失败 toast（成功静默）
  const { show: showToast, snack: toastSnack } = useToast();

  // 链接 activate 分流（terminal_03 §3.4）：URL 走 plugin-shell（window.open 被
  // Tauri webview 拦截，MarkdownEditor 先例），路径走 Rust open_path（系统默认
  // 应用）。useToast 的 show 是 useCallback([]) 稳定引用，本回调 deps 只挂它，
  // 供构造 options.linkHandler（OSC 8 链接）与自建 provider（正则匹配）两路共用。
  const activateLink = useCallback((text: string) => {
    if (/^https?:\/\//.test(text)) {
      openExternalUrl(text).catch((e: unknown) => {
        console.warn('[TerminalView] open url failed:', e);
        showToast('打开链接失败', 'error');
      });
    } else {
      unwrap(commands.openPath(text)).catch((e: unknown) => {
        console.warn('[TerminalView] open path failed:', e);
        showToast('打开文件失败', 'error');
      });
    }
  }, [showToast]);

  useEffect(() => {
    const container = containerRef.current;
    if (container == null) {
      return;
    }

    // 1. 建实例（theme 仅初值生效，见组件头注释）。allowProposedApi：搜索装饰
    //    （SearchAddon decorations 分支）依赖 registerDecoration 等 proposed API，
    //    不开则带 decorations 的 findNext/findPrevious 直接抛错、搜索全面失效
    //    （jsdom + 同版本依赖实证：You must set the allowProposedApi option to
    //    true to use proposed API）。
    const terminal = new Terminal({
      allowProposedApi: true,
      convertEol: false,
      cursorBlink,
      cursorStyle,
      fontSize,
      lineHeight,
      scrollback: scrollbackRows,
      fontFamily: 'menlo, monaco, courier-new, monospace',
      // OSC 8 终端超链接（claude/zsh 等现代 CLI 输出的 URL 走此转义序列，与自建
      // 正则 provider 是两条独立路径）兜底接管：不配 linkHandler 则点击走 xterm
      // 默认 confirm() + window.open()——Tauri 下双失败（confirm 映射的 dialog.confirm
      // IPC 未授权 + window.open 被拦截）。activate 转发 activateLink 与正则路径
      // 同分流，含修饰键守卫（Cmd/Ctrl+Click 才打开，见 hasOpenModifier）。
      linkHandler: {
        activate: (event: MouseEvent, text: string) => {
          if (!hasOpenModifier(event)) {
            return;
          }
          activateLink(text);
        },
      },
      theme: {
        background: theme.background,
        foreground: theme.foreground,
        cursor: theme.cursor,
        cursorAccent: theme.cursorAccent,
        selectionBackground: theme.selectionBackground,
        ...(theme.black != null
          ? {
              black: theme.black,
              red: theme.red,
              green: theme.green,
              yellow: theme.yellow,
              blue: theme.blue,
              magenta: theme.magenta,
              cyan: theme.cyan,
              white: theme.white,
              brightBlack: theme.brightBlack,
              brightRed: theme.brightRed,
              brightGreen: theme.brightGreen,
              brightYellow: theme.brightYellow,
              brightBlue: theme.brightBlue,
              brightMagenta: theme.brightMagenta,
              brightCyan: theme.brightCyan,
              brightWhite: theme.brightWhite,
            }
          : {}),
      },
    });

    // 2. fit + webgl + open。webgl 渲染器（高频输出性能）；上下文创建失败
    //    （远程桌面/老 GPU/重挂载竞态）抛错时回退 dom 渲染，不影响功能。
    //    注：曾疑似 webgl 致渲染异常，后证实真凶是 state 桥（已改 ref），webgl 无罪。
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(container);
    try {
      terminal.loadAddon(new WebglAddon());
    } catch (e) {
      console.warn('[TerminalView] webgl unavailable, fallback to dom renderer:', e);
    }
    // 搜索 addon（官方 @xterm/addon-search，terminal_03 任务 1 spike 通过）。
    // 生命周期随 terminal.dispose() 连带（同 fit/webgl 范式，无显式 dispose）。
    const searchAddon = new SearchAddon();
    terminal.loadAddon(searchAddon);

    // 3. 事件接线（props 由父层保证稳定，直接引用）。
    //    onData 经 replayDepth 闸门：回放写入期间 xterm 对回放流里 DA1/CPR 查询
    //    生成的应答被丢弃（不应写回 PTY——历史查询早已过期）；实时流窗口为 0 全放行。
    let replayDepth = 0;
    terminal.onData((data) => {
      if (replayDepth > 0) {
        return;
      }
      onData(data);
    });
    terminal.onResize(({ cols, rows }) => onResize(cols, rows));
    // 选区变化 → 复制按钮禁用态（setSelection 安排在 React 批处理内，频率可控）
    terminal.onSelectionChange(() => {
      setHasSelection(terminal.hasSelection());
    });
    // xterm 6 公开 API 无 focus 事件（旧 onFocusChange 已移除，内部 _onFocus 属
    // 私有）。订阅官方 DOM 结构 .xterm-helper-textarea 的 focus 事件（open 后存在；
    // blur 不报——焦点只会转移，新 pane 的 focus 自然接管活跃位）。
    const activeTextarea = onActive != null
      ? container.querySelector<HTMLTextAreaElement>('.xterm-helper-textarea')
      : null;
    if (onActive != null && activeTextarea != null) {
      activeTextarea.addEventListener('focus', onActive);
    }

    // 4. 输出桥上抛。守卫非字符串输入（null/undefined）：xterm.write 内部直接取
    //    .length 会抛 TypeError 并在 React 19 下卸载整树（白屏），宁可丢包不可崩页。
    //    replay 写入：write 回调在该块解析完毕后触发，配对递减 replayDepth——
    //    解析期间（含触发查询应答的时刻）窗口恒开。
    onWriteReady((text: string, replay?: boolean) => {
      if (typeof text === 'string' && text.length > 0) {
        if (replay) {
          replayDepth += 1;
          terminal.write(text, () => {
            replayDepth -= 1;
          });
        } else {
          terminal.write(text);
        }
      } else if (text != null) {
        console.warn('[TerminalView] discard non-string terminal data:', typeof text, text);
      }
    });

    // 5. 聚焦：xterm 6 open() 后不自动聚焦；macOS WebKit 点击聚焦也可能被外层容器吃掉，
    //    挂载即 focus + 点击兜底，保证键盘直接可用。
    const focusTerminal = () => terminal.focus();
    terminal.focus();
    container.addEventListener('mousedown', focusTerminal);

    // 5.5 链接点击（terminal_03 §3.4）：provideLinks 按行回调（行号 1-based 绝对值，
    // 取行 -1），命中构造 ILink；activate 走组件体 activateLink（URL 走 plugin-shell
    // ——window.open 被 Tauri webview 拦截，MarkdownEditor 先例；路径走 Rust
    // open_path 系统默认应用）。OSC 8 超链接不经此 provider，由构造 options 的
    // linkHandler 同路分流（见上注释）。
    const linkDisposable = terminal.registerLinkProvider({
      provideLinks(bufferLineNumber: number, callback: (links: ILink[] | undefined) => void) {
        const line = terminal.buffer.active.getLine(bufferLineNumber - 1);
        const lineText = line?.translateToString(true) ?? '';
        callback(lineText.length > 0 ? buildLinks(bufferLineNumber, lineText, activateLink) : undefined);
      },
    });

    // 6. 容器尺寸变化 → fit（cols/rows 经 onResize 同步后端）；折叠动画期间宽高为 0
    //    时 fit 会 panic，守卫。
    const observer = new ResizeObserver(() => {
      if (container.clientWidth > 0 && container.clientHeight > 0) {
        fitAddon.fit();
      }
    });
    observer.observe(container);
    if (container.clientWidth > 0 && container.clientHeight > 0) {
      fitAddon.fit();
    }

    // 7. 实例句柄上抛 ref 桥（工具条按钮 / 搜索条 / 字号 effect 经此访问实例）。
    terminalRef.current = terminal;
    searchAddonRef.current = searchAddon;
    fitAddonRef.current = fitAddon;

    // cleanup 严格逆序：observer → 监听 → 桥置空 → dispose → 清残留 DOM。
    // xterm dispose 不保证移除容器内 DOM，StrictMode/HMR 重挂载后旧实例 DOM 层
    // 会叠在新实例上面，显式清空容器（实测教训）。
    return () => {
      observer.disconnect();
      linkDisposable.dispose();
      container.removeEventListener('mousedown', focusTerminal);
      if (onActive != null && activeTextarea != null) {
        activeTextarea.removeEventListener('focus', onActive);
      }
      onWriteReady(null);
      terminalRef.current = null;
      searchAddonRef.current = null;
      fitAddonRef.current = null;
      terminal.dispose();
      container.replaceChildren();
    };
  }, []);

  // 字号运行时生效（terminal_03 §3.3）：options.fontSize 赋值 + refit，不重建实例
  // （webgl 渲染器监听 char size 变化自动重建字形 atlas，官方支持运行时改）。fit 后
  // cols/rows 经 mount effect 接线的 terminal.onResize 自动同步后端 PTY。容器折叠
  // 期间宽高为 0 时 fit 会 panic（同 ResizeObserver 守卫），跳过——展开时 observer
  // 会再 fit，字号赋值不丢失（options 已生效）。
  useEffect(() => {
    const terminal = terminalRef.current;
    const fitAddon = fitAddonRef.current;
    const container = containerRef.current;
    if (terminal == null || fitAddon == null || container == null) {
      return;
    }
    terminal.options.fontSize = fontSize;
    if (container.clientWidth > 0 && container.clientHeight > 0) {
      fitAddon.fit();
    }
  }, [fontSize]);

  // scrollback 运行时生效（terminal_04）：纯 options 赋值——缓冲容量与 cols/rows
  // 无关，不 refit 不通知 PTY（orca 实证同款约束）。调大即刻能多往回翻，调小立即
  // 截尾（xterm 自身裁剪），无闪烁。
  useEffect(() => {
    const terminal = terminalRef.current;
    if (terminal != null) {
      terminal.options.scrollback = scrollbackRows;
    }
  }, [scrollbackRows]);

  // 光标样式/闪烁运行时生效（terminal_05）：纯 options 赋值，即时重绘。
  useEffect(() => {
    const terminal = terminalRef.current;
    if (terminal != null) {
      terminal.options.cursorStyle = cursorStyle;
      terminal.options.cursorBlink = cursorBlink;
    }
  }, [cursorStyle, cursorBlink]);

  // 行高运行时生效（terminal_05）：行高改变字符度量 → 须 refit 重算 cols/rows
  // （同字号范式，含折叠期守卫）。
  useEffect(() => {
    const terminal = terminalRef.current;
    const fitAddon = fitAddonRef.current;
    const container = containerRef.current;
    if (terminal == null || fitAddon == null || container == null) {
      return;
    }
    terminal.options.lineHeight = lineHeight;
    if (container.clientWidth > 0 && container.clientHeight > 0) {
      fitAddon.fit();
    }
  }, [lineHeight]);

  // 主题运行时生效（hello-halo 调研移植）：options.theme 赋值即时重绘，不重建实例
  // ——推翻原「theme 仅初值生效、切换靠刷新」的取舍。dimOpacity 渲染期消费，
  // 不在此赋值。theme 对象每次渲染新建，用 JSON 串比较避免重复赋值。
  const lastThemeJsonRef = useRef<string>('');
  useEffect(() => {
    const terminal = terminalRef.current;
    const themeJson = JSON.stringify(theme);
    if (terminal == null || themeJson === lastThemeJsonRef.current) {
      return;
    }
    const { dimOpacity: _dim, ...xtermTheme } = theme;
    lastThemeJsonRef.current = themeJson;
    terminal.options.theme = xtermTheme;
  }, [theme]);

  // 基础操作组（terminal_03 §3.1）。清屏仅写 \x0c（等价用户按 Ctrl+L，交互程序
  // 自己处理重绘，双管 clear()+\x0c 会两次清屏闪烁——用户确认裁剪）；复制空选
  // 禁用；粘贴走 terminal.paste（bracketed paste 由 xterm 内部处理，回环 onData
  // 与键盘输入同路）；仅失败 toast（成功静默，选区消失即反馈）。
  const handleClear = () => {
    onData('\x0C');
  };
  const handleCopy = () => {
    const terminal = terminalRef.current;
    if (terminal == null) {
      return;
    }
    const selection = terminal.getSelection();
    if (selection === '') {
      return;
    }
    navigator.clipboard
      .writeText(selection)
      .then(() => {
        terminal.clearSelection();
      })
      .catch((e: unknown) => {
        console.warn('[TerminalView] copy to clipboard failed:', e);
        showToast('复制失败', 'error');
      });
  };
  const handlePaste = () => {
    const terminal = terminalRef.current;
    if (terminal == null) {
      return;
    }
    navigator.clipboard
      .readText()
      .then((text) => {
        if (text.length > 0) {
          terminal.paste(text);
        }
      })
      .catch((e: unknown) => {
        console.warn('[TerminalView] read clipboard failed:', e);
        showToast('粘贴失败（无法读取剪贴板）', 'error');
      });
  };
  const toggleSearch = () => {
    setSearchOpen(open => !open);
  };
  // 关闭搜索条：清匹配装饰 + 焦点归还终端（xterm 6 open 后不自动 focus）
  const closeSearch = () => {
    searchAddonRef.current?.clearDecorations();
    setSearchOpen(false);
    terminalRef.current?.focus();
  };

  return (
    <Box sx={{ position: 'relative', height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* 工具栏：左侧 pane 标识（main pane 专属）+ 基础操作组 + 右侧关闭终端
          （aria-label，不挂 Tooltip）。exited 禁用策略：清屏/粘贴对死会话无意义；
          复制/搜索保留——scrollback 检索与历史复制仍有价值。 */}
      <Box sx={{ height: 28, flexShrink: 0, display: 'flex', alignItems: 'center', px: 0.5, gap: 0.5 }}>
        {toolbarLabel != null && (
          <Typography variant="caption" sx={{ fontFamily: 'monospace', color: 'text.secondary', lineHeight: 1 }}>
            {toolbarLabel}
          </Typography>
        )}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.25 }}>
          <IconButton size="small" onClick={handleClear} disabled={exited} aria-label="清屏" sx={{ color: 'text.secondary' }}>
            <LayersClearOutlinedIcon fontSize="small" />
          </IconButton>
          <IconButton size="small" onClick={handleCopy} disabled={!hasSelection} aria-label="复制选区" sx={{ color: 'text.secondary' }}>
            <ContentCopyOutlinedIcon fontSize="small" />
          </IconButton>
          <IconButton size="small" onClick={handlePaste} disabled={exited} aria-label="粘贴" sx={{ color: 'text.secondary' }}>
            <ContentPasteOutlinedIcon fontSize="small" />
          </IconButton>
          <IconButton size="small" onClick={toggleSearch} aria-label="搜索" sx={{ color: 'text.secondary' }}>
            <SearchOutlinedIcon fontSize="small" />
          </IconButton>
          {/* 启动 claude（terminal_03 §3.2）：跑着置灰（探测驱动）、退出恢复、
              exited 禁用（shell 已死注入无意义） */}
          <IconButton
            size="small"
            onClick={onStartClaude}
            disabled={claudeRunning || exited}
            aria-label="启动 claude"
            sx={{ color: 'text.secondary' }}
          >
            <ClaudeIcon fontSize="small" />
          </IconButton>
          {/* Terminal/Chat 视图切换（terminal_chat T2.1）：chatEnabled 才渲染。图标
              反映目标模式：terminal 态显示 chat 气泡（点击进 chat），chat 态显示
              终端（点击回 terminal）。 */}
          {chatEnabled && (
            <IconButton
              size="small"
              onClick={() => setViewMode(viewMode === 'terminal' ? 'chat' : 'terminal')}
              aria-label={viewMode === 'terminal' ? '切换到 Chat 视图' : '切换到 Terminal 视图'}
              sx={{ color: 'text.secondary' }}
            >
              {viewMode === 'terminal' ? <ChatBubbleOutlineIcon fontSize="small" /> : <TerminalIcon fontSize="small" />}
            </IconButton>
          )}
        </Box>
        <Box sx={{ flex: 1 }} />
        <IconButton size="small" onClick={onClose} aria-label="关闭终端" sx={{ color: 'text.secondary' }}>
          <CloseOutlinedIcon fontSize="small" />
        </IconButton>
      </Box>
      {/* 搜索条 overlay：addon 实例就绪才渲染（ref 桥直读，mount 后即有值） */}
      {searchOpen && searchAddonRef.current != null && (
        <TerminalSearch searchAddon={searchAddonRef.current} background={theme.background} onClose={closeSearch} />
      )}
      {exited && (
        <Box
          sx={{
            position: 'absolute',
            top: 28,
            left: 0,
            right: 0,
            zIndex: 1,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            gap: 1,
            py: 0.5,
            bgcolor: 'action.hover',
          }}
        >
          <Typography variant="caption" color="text.secondary">会话已结束</Typography>
          <Button size="small" onClick={onReopen} aria-label="重开终端">重开</Button>
          <Button size="small" onClick={onReopenClaude} aria-label="重开并启动 claude">重开并启动 claude</Button>
        </Box>
      )}
      <Box
        ref={containerRef}
        sx={{
          flex: 1,
          minHeight: 0,
          px: 0.5,
          pb: 0.5,
          opacity: exited ? theme.dimOpacity : 1,
          pointerEvents: exited ? 'none' : 'auto',
        }}
      />
      {/* Chat 视图 overlay（T2.2）：viewMode === 'chat' 时盖住 xterm。zIndex
          ≥1000 压过 xterm 内部 z-5/10（xterm 容器不建堆叠上下文）。xterm 全程存活，
          切回 terminal 仅摘 overlay。 */}
      {viewMode === 'chat' && (
        <Box
          sx={{
            position: 'absolute',
            top: 28,
            left: 0,
            right: 0,
            bottom: 0,
            zIndex: 1000,
            bgcolor: 'background.default',
          }}
        >
          <NativeChatView sessionId={sessionId} onBackToTerminal={() => setViewMode('terminal')} />
        </Box>
      )}
      {toastSnack}
    </Box>
  );
}
