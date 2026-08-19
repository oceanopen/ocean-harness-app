import { CloseOutlined as CloseOutlinedIcon } from '@mui/icons-material';
import { Box, Button, IconButton, Typography } from '@mui/material';
import { FitAddon } from '@xterm/addon-fit';
import { WebglAddon } from '@xterm/addon-webgl';
import { Terminal } from '@xterm/xterm';
import { useEffect, useRef } from 'react';
import '@xterm/xterm/css/xterm.css';

export interface TerminalViewTheme {
  background: string;
  foreground: string;
  cursor: string;
  // 会话已结束时终端置灰（视觉上与活动会话区分）
  dimOpacity: number;
}

interface TerminalViewProps {
  theme: TerminalViewTheme;
  // 工具栏左侧标识（如 main pane 的 'main' 标签）。不传则不渲染。
  toolbarLabel?: string;
  // 键盘输入（xterm onData）。要求父层传稳定引用（useCallback），本组件不代理不缓存。
  onData: (data: string) => void;
  // 尺寸变化（addon-fit 实测后的 cols/rows）。同上要求稳定引用。
  onResize: (cols: number, rows: number) => void;
  // 会话已退出：终端交互禁用 + 顶部「会话已结束」条 + 重开按钮
  exited: boolean;
  onReopen: () => void;
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
// panel 窗口主题切换通常伴随整页刷新，可接受）。
//
// 事件处理全部函数式：mount effect 按显式顺序一次性建齐（terminal → addon → open →
// 事件接线 → focus → observer → 初始 fit），cleanup 严格逆序。回调直接用 props
// （父层保证稳定引用），不做 ref 转发层。
export default function TerminalView({ theme, toolbarLabel, onData, onResize, exited, onReopen, onClose, onWriteReady, onActive }: TerminalViewProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (container == null) {
      return;
    }

    // 1. 建实例（theme 仅初值生效，见组件头注释）
    const terminal = new Terminal({
      convertEol: false,
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'menlo, monaco, courier-new, monospace',
      theme: {
        background: theme.background,
        foreground: theme.foreground,
        cursor: theme.cursor,
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

    // cleanup 严格逆序：observer → 监听 → 桥置空 → dispose → 清残留 DOM。
    // xterm dispose 不保证移除容器内 DOM，StrictMode/HMR 重挂载后旧实例 DOM 层
    // 会叠在新实例上面，显式清空容器（实测教训）。
    return () => {
      observer.disconnect();
      container.removeEventListener('mousedown', focusTerminal);
      if (onActive != null && activeTextarea != null) {
        activeTextarea.removeEventListener('focus', onActive);
      }
      onWriteReady(null);
      terminal.dispose();
      container.replaceChildren();
    };
  }, []);

  return (
    <Box sx={{ position: 'relative', height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* 工具栏：左侧 pane 标识（main pane 专属）+ 右侧关闭终端（aria-label，不挂 Tooltip） */}
      <Box sx={{ height: 28, flexShrink: 0, display: 'flex', alignItems: 'center', px: 0.5, gap: 0.5 }}>
        {toolbarLabel != null && (
          <Typography variant="caption" sx={{ fontFamily: 'monospace', color: 'text.secondary', lineHeight: 1 }}>
            {toolbarLabel}
          </Typography>
        )}
        <Box sx={{ flex: 1 }} />
        <IconButton size="small" onClick={onClose} aria-label="关闭终端" sx={{ color: 'text.secondary' }}>
          <CloseOutlinedIcon fontSize="small" />
        </IconButton>
      </Box>
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
    </Box>
  );
}
