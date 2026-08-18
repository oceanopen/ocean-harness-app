import { SettingsOutlined as SettingsOutlinedIcon } from '@mui/icons-material';
import { Box, Button, Typography, useTheme } from '@mui/material';
import { DEFAULT_WORKSPACE_BASE_DIR, WORKSPACE_BASE_DIR_KEY } from '@src/shared/appConfig';
import { commands } from '@src/shared/bindings';
import { useConfigValue } from '@src/shared/useConfigValue';
import { useCallback, useRef } from 'react';
import TerminalView from './TerminalView';
import { usePtySession } from './usePtySession';

// 工作空间根目录 decode：缺失回落空串（= 未设置）。模块级保证引用稳定（useConfigValue 要求）。
function decodeWorkspaceBaseDir(raw: string | null): string {
  return raw ?? DEFAULT_WORKSPACE_BASE_DIR;
}

// 初始尺寸占位：真实尺寸由 TerminalView fit 后经 onResize 校正
const INITIAL_COLS = 80;
const INITIAL_ROWS = 24;

interface EmbeddedTerminalProps {
  issueId: string;
}

// EmbeddedTerminal：开发工作台右侧嵌入式终端容器（docs/embedded_terminal.md §3.8）。
// 职责：读 workspace_base_dir 派生 cwd（`${base}/${issueId}`）、两个错误态
// （根目录未设置 / 任务目录不存在——目录创建属 skills 集成，本期仅提示）、
// 组装 TerminalView + usePtySession。父层以 issueId 为 key 挂载本组件，
// 切换 issue 即重挂载（unmount 仅断订阅，后端会话/ring 常驻，回切 reattach 重载）。
//
// 数据通路（函数式，两条单向线，无响应式层）：
//   输出：Channel → usePtySession(onData) → writeDataRef → terminal.write
//   输入：xterm onData → session.write → ptyWrite → shell
// writeDataRef 是全链唯一 ref 桥：Channel 数据可在任何时刻到达（含 StrictMode
// 双挂载窗口），ref 直读直写不经渲染周期——state 版桥在 React 19 StrictMode 下
// 实测出现「fn→null→fn 连续 setState 后闭包仍读到 null」，输出全丢。
export default function EmbeddedTerminal({ issueId }: EmbeddedTerminalProps) {
  const theme = useTheme();
  const baseDir = useConfigValue(WORKSPACE_BASE_DIR_KEY, decodeWorkspaceBaseDir, DEFAULT_WORKSPACE_BASE_DIR);

  const writeDataRef = useRef<((text: string) => void) | null>(null);
  // 稳定引用（deps=[]）：直接交给 usePtySession / TerminalView 接线，不走 ref 转发层。
  const handleTerminalData = useCallback((text: string) => {
    const write = writeDataRef.current;
    if (write != null) {
      write(text);
    } else {
      console.warn('[EmbeddedTerminal] drop data: terminal not ready, len=', text.length);
    }
  }, []);
  const handleWriteReady = useCallback((write: ((text: string) => void) | null) => {
    writeDataRef.current = write;
  }, []);

  const cwd = baseDir ? `${baseDir}/${issueId}` : null;

  // hooks 顶层无条件调用（React 规则）；cwd=null 时 usePtySession 返回哑会话（不发 spawn，
  // status 恒 'connecting'），下方引导分支先于 spinner 渲染，不会闪错态。
  const session = usePtySession({
    issueId,
    cwd,
    cols: INITIAL_COLS,
    rows: INITIAL_ROWS,
    onData: handleTerminalData,
  });

  const openSettings = useCallback(() => {
    // 语义化深链：错误态引导用户去「项目配置」分区设置工作空间根目录。
    void commands.showSettingsWindow('projectConfig').then((res) => {
      if (res.status === 'error') {
        console.warn('[EmbeddedTerminal] open settings failed:', res.error);
      }
    });
  }, []);

  // 错误态一：工作空间根目录未设置（配置为空串）——哑会话不发 spawn，引导去设置
  if (cwd == null) {
    return (
      <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 1.5, p: 2 }}>
        <Typography variant="body2" color="text.secondary">请先在设置 → 项目配置中设置工作空间根目录</Typography>
        <Button size="small" startIcon={<SettingsOutlinedIcon />} onClick={openSettings}>打开设置</Button>
      </Box>
    );
  }

  // 错误态二：spawn 失败（典型为任务目录不存在——本模块不创建目录，skills 集成职责）。
  // 双出口：重试（目录已被外部建好的场景）/ 打开设置（根目录配置错了的场景）。
  if (session.status === 'error') {
    return (
      <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 1.5, p: 2 }}>
        <Typography variant="body2" color="text.secondary">{session.errorMessage ?? `任务目录不存在：${cwd}`}</Typography>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button size="small" onClick={session.reopen}>重试</Button>
          <Button size="small" startIcon={<SettingsOutlinedIcon />} onClick={openSettings}>打开设置</Button>
        </Box>
      </Box>
    );
  }

  const terminalTheme = {
    background: theme.palette.mode === 'dark' ? '#1e1e1e' : '#ffffff',
    foreground: theme.palette.mode === 'dark' ? '#d4d4d4' : '#333333',
    cursor: theme.palette.text.primary,
    dimOpacity: 0.5,
  };

  return (
    <TerminalView
      theme={terminalTheme}
      onData={session.write}
      onResize={session.resize}
      exited={session.status === 'exited'}
      onReopen={session.reopen}
      onClose={session.close}
      onWriteReady={handleWriteReady}
    />
  );
}
