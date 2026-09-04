import type { TerminalFontSize, TerminalScrollbackRows } from '@src/shared/appConfig';
import type { TerminalThemeId } from './terminalTheme';
import { CreateNewFolderOutlined as CreateNewFolderOutlinedIcon, SettingsOutlined as SettingsOutlinedIcon } from '@mui/icons-material';
import { Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Typography } from '@mui/material';
import {
  decodeWorkspaceBaseDir,
  DEFAULT_TERMINAL_CURSOR_BLINK,
  DEFAULT_TERMINAL_CURSOR_STYLE,
  DEFAULT_TERMINAL_FONT_SIZE,
  DEFAULT_TERMINAL_LINE_HEIGHT,
  DEFAULT_TERMINAL_SCROLLBACK_ROWS,
  DEFAULT_TERMINAL_STARTUP_CODE_CLI,
  DEFAULT_WORKSPACE_BASE_DIR,
  isYes,
  parseTerminalCursorStyle,
  parseTerminalFontSize,
  parseTerminalLineHeight,
  parseTerminalScrollbackRows,
  parseTerminalStartupCodeCli,
  TERMINAL_CURSOR_BLINK_KEY,
  TERMINAL_CURSOR_STYLE_KEY,
  TERMINAL_FONT_SIZE_KEY,
  TERMINAL_LINE_HEIGHT_KEY,
  TERMINAL_SCROLLBACK_ROWS_KEY,
  TERMINAL_STARTUP_CODE_CLI_KEY,
  TERMINAL_THEME_KEY,
  WORKSPACE_BASE_DIR_KEY,
} from '@src/shared/appConfig';
import { commands } from '@src/shared/bindings';
import { openProjectConfigSettings } from '@src/shared/openSettings';
import { useConfigReady } from '@src/shared/useConfigReady';
import { useConfigValue } from '@src/shared/useConfigValue';
import { useToast } from '@src/shared/useToast';
import { useTerminalPanesStore } from '@src/state/terminalPanes';
import { useCallback, useRef, useState } from 'react';
import { buildTerminalTheme, DEFAULT_TERMINAL_THEME_ID, parseTerminalThemeId } from './terminalTheme';
import TerminalView from './TerminalView';
import { useClaudeRunning } from './useClaudeRunning';
import { usePtySession } from './usePtySession';
import { useRefineInjection } from './useRefineInjection';

// 启动自动运行 CLI decode：parse 内含回落（非法/缺失 → none），直接转发。
// 模块级保证引用稳定（useConfigValue 要求）。
function decodeStartupCodeCli(raw: string | null): string {
  return parseTerminalStartupCodeCli(raw);
}

// 终端字号 decode：非法/不在选项集回落 12（terminal_03 §3.3）。模块级保证引用稳定。
function decodeTerminalFontSize(raw: string | null): TerminalFontSize {
  return parseTerminalFontSize(raw);
}

// scrollback 行数 decode：非法/不在选项集回落 1000（terminal_04）。模块级保证引用稳定。
function decodeTerminalScrollbackRows(raw: string | null): TerminalScrollbackRows {
  return parseTerminalScrollbackRows(raw);
}

// 主题 id decode：非法回落 Dracula（terminal_05）。模块级保证引用稳定。
function decodeTerminalThemeId(raw: string | null): TerminalThemeId {
  return parseTerminalThemeId(raw);
}

// 光标样式 decode（terminal_05）。
function decodeTerminalCursorStyle(raw: string | null): 'block' | 'bar' | 'underline' {
  return parseTerminalCursorStyle(raw);
}

// 行高 decode（terminal_05）。
function decodeTerminalLineHeight(raw: string | null): number {
  return parseTerminalLineHeight(raw);
}

// 光标闪烁 decode（terminal_05）：YesNo → boolean，缺失/非法回落 true（默认闪）。
function decodeYesNo(raw: string | null): boolean {
  return raw == null ? true : isYes(raw);
}

// 初始尺寸占位：仅作 usePtySession spawn 的兜底（容器不可见等边缘场景 fit 无
// 实测值时）。正常时序下 attach 直接用 TerminalView fit 实测尺寸。
const INITIAL_COLS = 80;
const INITIAL_ROWS = 24;

// 终端挂载前置配置集：这三个 key 决定字符度量（fontSize/lineHeight → fit 结果
// 与 spawn 尺寸）与编排行为（startup CLI → directCommand / attachKey）。就绪前
// 挂载会走「默认值 → 异步到达纠正」路径——二次 refit 与 attachKey 变化重复编
// 排，每次纠正都是一次打在已绘制提示符上的 SIGWINCH 重绘伪影（% 残迹根因）。
// 模块级常量保证引用稳定（useConfigReady 依赖项要求）。
const TERMINAL_MOUNT_CONFIG_KEYS: readonly string[] = [
  TERMINAL_STARTUP_CODE_CLI_KEY,
  TERMINAL_FONT_SIZE_KEY,
  TERMINAL_LINE_HEIGHT_KEY,
];

interface EmbeddedTerminalProps {
  issueId: string;
  // pane id：'main'（主 pane）| 8 位 uuid（附加 pane）。锚点统一 `issueId::<paneId>`。
  paneId?: string;
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
export default function EmbeddedTerminal({ issueId, paneId = 'main' }: EmbeddedTerminalProps) {
  const isMain = paneId === 'main';
  const { show: showToast, snack: toastSnack } = useToast();
  const baseDir = useConfigValue(WORKSPACE_BASE_DIR_KEY, decodeWorkspaceBaseDir, DEFAULT_WORKSPACE_BASE_DIR);
  const startupCodeCli = useConfigValue(
    TERMINAL_STARTUP_CODE_CLI_KEY,
    decodeStartupCodeCli,
    DEFAULT_TERMINAL_STARTUP_CODE_CLI,
  );
  // 字号（terminal_03 §3.3）：每 pane 各自订阅，设置保存事件驱动全量 pane 生效。
  const fontSize = useConfigValue(
    TERMINAL_FONT_SIZE_KEY,
    decodeTerminalFontSize,
    DEFAULT_TERMINAL_FONT_SIZE,
  );
  // scrollback 行数（terminal_04）：同字号范式，各 pane 订阅热生效。
  const scrollbackRows = useConfigValue(
    TERMINAL_SCROLLBACK_ROWS_KEY,
    decodeTerminalScrollbackRows,
    DEFAULT_TERMINAL_SCROLLBACK_ROWS,
  );
  // 终端 UI 配置组（terminal_05）：主题用户自选（不跟随 app 明暗）+ 光标 + 行高。
  const themeId = useConfigValue(
    TERMINAL_THEME_KEY,
    decodeTerminalThemeId,
    DEFAULT_TERMINAL_THEME_ID,
  );
  const cursorStyle = useConfigValue(
    TERMINAL_CURSOR_STYLE_KEY,
    decodeTerminalCursorStyle,
    DEFAULT_TERMINAL_CURSOR_STYLE,
  );
  const cursorBlink = useConfigValue(
    TERMINAL_CURSOR_BLINK_KEY,
    decodeYesNo,
    DEFAULT_TERMINAL_CURSOR_BLINK === 'Y',
  );
  const lineHeight = useConfigValue(
    TERMINAL_LINE_HEIGHT_KEY,
    decodeTerminalLineHeight,
    DEFAULT_TERMINAL_LINE_HEIGHT,
  );
  // 度量/编排相关配置就绪闸门（见 TERMINAL_MOUNT_CONFIG_KEYS 注释）：就绪后才
  // 挂载 TerminalView 并放行 usePtySession 编排，首帧即真实字号、spawn 即实测
  // 尺寸，一步到位无纠正步骤。
  const configReady = useConfigReady(TERMINAL_MOUNT_CONFIG_KEYS);
  // main 关闭确认弹窗开关（附加 pane 关闭直处理，无确认）。
  const [confirmCloseOpen, setConfirmCloseOpen] = useState(false);
  // main 已关闭态：true = 不渲染 TerminalView，整块换占位视图（无蒙层）。
  // 「重新打开终端」清标志 + session.reopen()（ptyShutdown 已移除会话 →
  // exists=false → 全新 spawn，配置直启时重新 direct spawn）。
  const [mainClosed, setMainClosed] = useState(false);

  const writeDataRef = useRef<((text: string, replay?: boolean) => void) | null>(null);
  // 稳定引用（deps=[]）：直接交给 usePtySession / TerminalView 接线，不走 ref 转发层。
  const handleTerminalData = useCallback((text: string, replay?: boolean) => {
    const write = writeDataRef.current;
    if (write != null) {
      write(text, replay);
    } else {
      console.warn('[EmbeddedTerminal] drop data: terminal not ready, len=', text.length);
    }
  }, []);
  const handleWriteReady = useCallback((write: ((text: string, replay?: boolean) => void) | null) => {
    writeDataRef.current = write;
  }, []);

  const cwd = baseDir ? `${baseDir}/${issueId}` : null;
  // 会话锚点派生：统一 `issueId::<paneId>`（main → `issueId::main`，split → `issueId::<uuid>`）。
  // 后端 store 对 key 透明，仅 pty_shutdown_issue 前缀扫描感知 `::`。
  const sessionId = `${issueId}::${paneId}`;

  // hooks 顶层无条件调用（React 规则）；cwd=null 时 usePtySession 返回哑会话（不发 spawn，
  // status 恒 'connecting'），下方引导分支先于 spinner 渲染，不会闪错态。
  const session = usePtySession({
    sessionId,
    cwd,
    enabled: configReady,
    cols: INITIAL_COLS,
    rows: INITIAL_ROWS,
    // CLI 直启（claude_orca T5.1，唯一自动执行路径）：主 pane + 配置非 none
    // （当前取值集 none/claude）→ PTY 直接 spawn CLI，无 shell 中转；附加 pane
    // 恒 null（裸 shell——用户分屏通常是要手动跑命令）。直启失败（CLI 未安装
    // 等）由 Rust 侧回落普通 shell（warn log），用户可手动启动。
    directCommand: isMain && startupCodeCli !== 'none' ? startupCodeCli : null,
    onData: handleTerminalData,
  });

  const openSettings = useCallback(() => {
    // 语义化深链：错误态引导用户去「项目配置」分区设置工作空间根目录（共享助手）。
    openProjectConfigSettings('EmbeddedTerminal');
  }, []);

  // 一键创建工作目录（mkdir -p 语义），成功后自动重试终端初始化。
  const handleCreateDirectory = useCallback(() => {
    if (!cwd) {
      return;
    }
    void commands.createDirectory(cwd).then((res) => {
      if (res.status === 'ok') {
        session.reopen();
      } else {
        showToast(`创建目录失败：${res.error}`, 'error');
      }
    });
  }, [cwd, session, showToast]);

  // 关闭语义分流（terminal_02 §3.5 + 用户交互反馈）：附加 pane 直处理（ptyShutdown
  // 断会话 + store 树剪枝，pane 从树上消失）；main pane 先二次确认，确认后仅杀会话
  // （树保单 main leaf）+ 进 mainClosed 占位态（不卸载组件，保留重新打开出口）。
  const closePaneTree = useTerminalPanesStore(s => s.closePane);
  // focus 跟随（§3.4）：本 pane 获得键盘焦点 → store activePanes 记活跃位，分割
  // 按钮作用对象跟随。挂载即 focus（TerminalView 现有行为）天然触发首帧激活。
  const setActivePane = useTerminalPanesStore(s => s.setActivePane);
  const handleActive = useCallback(() => {
    setActivePane(issueId, paneId);
  }, [setActivePane, issueId, paneId]);

  // 「启动 claude」（terminal_03 §3.2）：对本 pane 活跃 shell 写入 claude\r。
  // 运行态探测（按钮置灰）走 useClaudeRunning——进程真相（pid 父链匹配），
  // 非输出流启发式。
  const claudeRunning = useClaudeRunning(sessionId, session.status);
  const startClaude = useCallback(() => {
    session.write('claude\r');
  }, [session]);
  // 润色命令注入编排（T3.3）：main pane 专属，消费「AI 润色」按钮写入的一次性意图
  // 标志（工作空间闸门放行、会话 active 后自治编排，见 useRefineInjection 注释）。
  useRefineInjection({
    issueId,
    enabled: isMain,
    sessionStatus: session.status,
    spawnKind: session.spawnKind,
    write: session.write,
    showToast,
  });
  // 「新开终端」按配置直启：配置 none 裸 shell / 配置 claude 起新 claude 会话。
  const reopenPlain = useCallback(() => {
    session.reopen();
  }, [session]);
  const handleClose = useCallback(() => {
    if (isMain) {
      setConfirmCloseOpen(true);
      return;
    }
    session.close();
    closePaneTree(issueId, paneId);
  }, [session, closePaneTree, issueId, paneId, isMain]);

  // 确认关闭 main：杀会话 + 关弹窗 + 进占位态。
  const confirmCloseMain = useCallback(() => {
    setConfirmCloseOpen(false);
    session.close();
    setMainClosed(true);
  }, [session]);

  // 重新打开 main 终端：清占位态 + reopen（attempt 自增 → 重新编排 → exists=false
  // 走全新 spawn，配置直启时重新 direct spawn）。
  const reopenMain = useCallback(() => {
    setMainClosed(false);
    session.reopen();
  }, [session]);

  // 错误态一：工作空间根目录未设置（配置为空串）——哑会话不发 spawn，引导去设置
  if (cwd == null) {
    return (
      <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 1.5, p: 2 }}>
        <Typography variant="body2" color="text.secondary">请先在设置 → 项目配置中设置工作空间根目录</Typography>
        <Button size="small" startIcon={<SettingsOutlinedIcon />} onClick={openSettings}>打开设置</Button>
      </Box>
    );
  }

  // 度量/编排配置未就绪：哑会话（不发 spawn）+ 空占位。一次性等待（本地 IPC，
  // ~ms 级），就绪后 TerminalView 以真实字号首帧 fit、spawn 用实测尺寸——避免
  // 「默认值挂载 → 配置到达 refit」的二次 resize（SIGWINCH 伪影）。
  if (!configReady) {
    return <Box sx={{ height: '100%' }} />;
  }

  // 错误态二：spawn 失败（典型为任务目录不存在）。
  // 三出口：创建目录（mkdir -p 后自动重试）/ 重试（目录已被外部建好）/ 打开设置（根目录配置错了）。
  if (session.status === 'error') {
    return (
      <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 1.5, p: 2 }}>
        <Typography variant="body2" color="text.secondary">{session.errorMessage ?? `任务目录不存在：${cwd}`}</Typography>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button size="small" startIcon={<CreateNewFolderOutlinedIcon />} onClick={handleCreateDirectory}>创建目录</Button>
          <Button size="small" onClick={reopenPlain}>重试</Button>
          <Button size="small" startIcon={<SettingsOutlinedIcon />} onClick={openSettings}>打开设置</Button>
        </Box>
      </Box>
    );
  }

  // main 已关闭占位视图：整块替换 TerminalView（无蒙层），提示工作目录 + 重新打开出口。
  if (mainClosed) {
    return (
      <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 1.5, p: 2 }}>
        <Typography variant="body2" color="text.secondary">当前任务工作目录为：{cwd}</Typography>
        <Button size="small" onClick={reopenMain}>重新打开终端</Button>
      </Box>
    );
  }

  // 完整终端主题（terminal_05）：按用户自选主题 id 构建（暗色目录，不跟随 app
  // 明暗）。id 变化 → 新对象 → TerminalView 主题 effect 运行时赋值（热切换）。
  const terminalTheme = buildTerminalTheme(themeId);

  return (
    <>
      <TerminalView
        theme={terminalTheme}
        fontSize={fontSize}
        scrollbackRows={scrollbackRows}
        cursorStyle={cursorStyle}
        cursorBlink={cursorBlink}
        lineHeight={lineHeight}
        toolbarLabel={isMain ? 'main' : undefined}
        onData={session.write}
        onResize={session.resize}
        exited={session.status === 'exited'}
        onReopen={reopenPlain}
        claudeRunning={claudeRunning}
        onStartClaude={startClaude}
        onClose={handleClose}
        onWriteReady={handleWriteReady}
        onActive={handleActive}
      />
      {/* main 关闭二次确认（附加 pane 无此弹窗） */}
      <Dialog open={confirmCloseOpen} onClose={() => setConfirmCloseOpen(false)} maxWidth="xs" fullWidth>
        <DialogTitle>关闭终端</DialogTitle>
        <DialogContent>
          <Typography variant="body2">
            关闭后终端会话将被终止，可通过「重新打开终端」重新初始化。确定关闭吗？
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button size="small" onClick={() => setConfirmCloseOpen(false)}>取消</Button>
          <Button size="small" variant="contained" color="primary" onClick={confirmCloseMain}>关闭</Button>
        </DialogActions>
      </Dialog>
      {toastSnack}
    </>
  );
}
