import type { HttpServerStatus } from '@src/shared/bindings';
import { Autorenew as AutorenewIcon, ContentCopy as CopyIcon } from '@mui/icons-material';
import {
  Alert,
  Box,
  CircularProgress,
  IconButton,
  Snackbar,
  Stack,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableRow,
  Typography,
} from '@mui/material';
import { commands } from '@src/shared/bindings';
import { logOnError, unwrap } from '@src/shared/commands';
import { EVENT_HTTP_SERVER_STATE_CHANGED } from '@src/shared/events';
import { listen } from '@tauri-apps/api/event';

import { useCallback, useEffect, useState } from 'react';

// Go 服务统一响应封装（见 src-server/internal/response/response.go）。
interface ApiResponse<T> {
  code: number;
  msg: string;
  data: T;
}

// GET /api/baseInfo/getServerRunInfo 返回的 data 载荷（与 Go ServerRunInfo 对齐）。
// 含 SysInfo（系统信息）与 ServerInfo（服务信息）两块。
interface SysInfo {
  hostname: string;
  goVersion: string;
  os: string;
  arch: string;
}
interface ServerInfo {
  mode: string;
  address: string;
  logDir: string;
  sqliteDir: string;
}
interface ServerRunInfo {
  sysInfo: SysInfo;
  serverInfo: ServerInfo;
}

// 运行模式徽章文案（mode 取自 Go 接口 serverInfo.mode，值为 debug/release/test）。
const MODE_LABEL: Record<string, string> = {
  debug: 'debug 调试',
  release: 'release 正式',
  test: 'test 测试',
};

// 浮层 toast 严重级别（操作失败用 error，状态提示用 warning，成功用 success）。
type ToastSeverity = 'warning' | 'error' | 'success';

// 标签列统一样式（固定宽度 + 轻底色，便于扫读）。
const labelCellSx = { fontWeight: 600, width: 140, bgcolor: 'action.hover', whiteSpace: 'nowrap' } as const;

// 把路径转成 shell 可直接 cd 的形式：双引号包裹。
// 双引号在 bash/zsh（macOS、Linux）与 cmd/PowerShell（Windows）下均可用，跨平台通用、无需关心空格转义差异。
function shellQuote(p: string): string {
  return `"${p}"`;
}

// AutorenewIcon 旋转动画：刷新中持续旋转。
function spinSx(spinning: boolean) {
  return {
    'animation': spinning ? 'spin 0.8s linear infinite' : undefined,
    '@keyframes spin': {
      from: { transform: 'rotate(0deg)' },
      to: { transform: 'rotate(360deg)' },
    },
  };
}

// 把 Rust 清洗后的启动失败详情翻译为「服务启动失败：<友好原因>（<原始错误>）」。
// Rust 已剥离时间戳、提取 error 值（clean_go_error_line），前端只做关键词→中文翻译（文案本地化是前端职责）。
function friendlyStartError(detail: string | null | undefined): string {
  if (!detail) {
    return '服务启动失败，请查看日志目录排查';
  }
  if (/address already in use/i.test(detail)) {
    return `服务启动失败：端口被占用（${detail}）`;
  }
  if (/no such file|sqlite|database/i.test(detail)) {
    return `服务启动失败：数据库初始化异常（${detail}）`;
  }
  return `服务启动失败：${detail}`;
}

// panel 窗口「服务状态」页面：以表格展示本地 HTTP 服务的运行态、地址、模式及系统信息。
// 第一行 Switch 可开关服务（IPC 调 Rust start/stop sidecar）；其余字段取自 Go getServerRunInfo 接口。
// fetch 用的地址取自 Rust（端口随模式 dev=9000/build=9100）。
function ServerStatusPage() {
  const [status, setStatus] = useState<HttpServerStatus | null>(null);
  const [runInfo, setRunInfo] = useState<ServerRunInfo | null>(null);
  // toast：保留最近一次内容，toastOpen 控制显隐（退出动画期间内容不闪烁）。
  const [toast, setToast] = useState<{ text: string; severity: ToastSeverity }>({ text: '', severity: 'warning' });
  const [toastOpen, setToastOpen] = useState(false);
  const [toggling, setToggling] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const showToast = useCallback((text: string, severity: ToastSeverity) => {
    setToast({ text, severity });
    setToastOpen(true);
  }, []);

  // 单一状态同步入口：一律以 Rust 推送的 status 为准（前端不做状态判断）。
  // running → 拉 getServerRunInfo 展示（失败提示"获取服务运行信息失败"）；
  // stopped 且有 lastError → 启动失败，toast 友好翻译后的原因；其余（starting / 主动停止）仅清 runInfo。
  const applyStatus = useCallback(
    async (s: HttpServerStatus) => {
      setStatus(s);
      if (s.runState !== 'running') {
        setRunInfo(null);
        if (s.runState === 'stopped' && s.startLastError) {
          showToast(friendlyStartError(s.startLastError), 'error');
        }
        return;
      }
      // running：Rust 探活保证端口已就绪，直接 fetch 一次（无重试）；失败提示获取服务运行信息失败。
      try {
        const resp = await fetch(`${s.address}/api/baseInfo/getServerRunInfo`);
        if (!resp.ok) {
          throw new Error(`HTTP ${resp.status}`);
        }
        const body: ApiResponse<ServerRunInfo> = await resp.json();
        if (body.code !== 0) {
          throw new Error(body.msg || `code ${body.code}`);
        }
        setRunInfo(body.data);
        showToast('启动成功，获取服务运行信息成功', 'success');
      } catch {
        setRunInfo(null);
        showToast('启动成功，获取服务运行信息失败', 'error');
      }
    },
    [showToast],
  );

  // 初次挂载：拉一次最新状态同步（Rust 为唯一状态源）。
  useEffect(() => {
    void commands.httpServerStatus().then(applyStatus).catch((err: unknown) => {
      console.warn('[http-server] 初始状态拉取失败:', err);
    });
  }, [applyStatus]);

  // 监听 Rust 状态变更事件：payload 即最新 HttpServerStatus，直接 applyStatus，无需二次拉取。
  useEffect(() => {
    const unlistenPromise = listen<HttpServerStatus>(EVENT_HTTP_SERVER_STATE_CHANGED, (e) => {
      console.log('[ServerStatusPage] http-server:state-changed, event.payload:', e.payload);
      void applyStatus(e.payload);
    });
    return () => {
      unlistenPromise
        .then(fn => fn())
        .catch((err: unknown) => console.warn('[http-server:state-changed] unlisten failed:', err));
    };
  }, [applyStatus]);

  // 刷新：重新从 Rust 拉一次最新状态并 applyStatus（成功/失败提示由 applyStatus 内部按需弹出）。
  const handleRefresh = useCallback(async () => {
    setRefreshing(true);
    try {
      const s = await commands.httpServerStatus();
      await applyStatus(s);
    } catch {
      showToast('刷新失败', 'error');
    } finally {
      setRefreshing(false);
    }
  }, [applyStatus, showToast]);

  // Switch 开关：开启前先清理跨会话残留的 go-server 孤儿进程（best-effort），再调 Rust 启停服务。
  // 仅触发动作——服务最终态（starting→running/stopped）由 Rust 探活决定并经事件推送，前端不等待、不判断。
  const handleToggle = useCallback(
    async (checked: boolean) => {
      setToggling(true);
      try {
        if (checked) {
          // 清理占用端口的本应用孤立 go-server，确保新 sidecar 能 bind 成功；best-effort，失败仅打 warn 不阻断。
          await logOnError(commands.cleanupOrphanHttpServer(), 'cleanup-orphan-http-server');
        }
        // setHttpServerEnabled 返回 Result<(), String>，按 commands.ts 约定用 unwrap：失败时 throw 错误字符串。
        await unwrap(commands.setHttpServerEnabled(checked));
      } catch (e) {
        showToast(`${checked ? '启动' : '停止'}服务失败：${e}`, 'error');
      } finally {
        setToggling(false);
      }
    },
    [showToast],
  );

  // 复制目录路径（双引号包裹，便于在任意终端 cd 后直接粘贴）；成功/失败均 toast 反馈。
  const copyDir = useCallback(
    async (path: string) => {
      if (!path) {
        return;
      }
      try {
        await navigator.clipboard.writeText(shellQuote(path));
        showToast('已复制（可直接 cd 后粘贴）', 'success');
      } catch (e) {
        showToast(`复制失败：${String(e)}`, 'error');
      }
    },
    [showToast],
  );

  const runState = status?.runState ?? 'stopped';
  const running = runState === 'running';
  const starting = runState === 'starting';
  // 运行模式取自 Go 接口（serverInfo.mode）。
  const modeLabel = runInfo ? MODE_LABEL[runInfo.serverInfo.mode] ?? runInfo.serverInfo.mode : '';
  const loaded = status !== null;

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <Box sx={{ display: 'flex', justifyContent: 'flex-end', px: 2, pt: 1 }}>
        <IconButton
          onClick={handleRefresh}
          disabled={refreshing || toggling}
          aria-label="refresh"
        >
          <AutorenewIcon sx={spinSx(refreshing)} />
        </IconButton>
      </Box>

      <Box sx={{ flex: 1, minHeight: 0, overflow: 'auto', p: 2 }}>
        {!loaded && (
          <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
            <CircularProgress />
          </Box>
        )}

        {loaded && (
          <Stack spacing={2}>
            <TableContainer sx={{ border: 1, borderColor: 'divider', borderRadius: 1 }}>
              <Table size="medium">
                <TableBody>
                  {/* 第一行：服务状态 + Switch 开关（控制服务启停） */}
                  <TableRow>
                    <TableCell sx={labelCellSx}>服务状态</TableCell>
                    <TableCell>
                      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                        <Switch
                          checked={running}
                          onChange={(_, c) => void handleToggle(c)}
                          disabled={toggling || starting}
                          size="small"
                        />
                        <Typography variant="body2" color={running ? 'success.main' : starting ? 'warning.main' : 'text.secondary'}>
                          {running ? '运行中' : starting ? '启动中...' : '已停止'}
                        </Typography>
                      </Stack>
                    </TableCell>
                  </TableRow>
                  {/* 以下字段均取自 Go getServerRunInfo 接口（服务未运行时无数据，显示 -） */}
                  <TableRow>
                    <TableCell sx={labelCellSx}>服务地址</TableCell>
                    <TableCell sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>{runInfo?.serverInfo.address ?? '-'}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={labelCellSx}>运行模式</TableCell>
                    <TableCell>{modeLabel || '-'}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={labelCellSx}>数据目录</TableCell>
                    <TableCell>
                      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                        <IconButton
                          size="small"
                          onClick={() => void copyDir(runInfo?.serverInfo.sqliteDir ?? '')}
                          disabled={!runInfo?.serverInfo.sqliteDir}
                          aria-label="复制数据目录路径"
                        >
                          <CopyIcon fontSize="small" />
                        </IconButton>
                        <Typography variant="inherit" sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>
                          {runInfo?.serverInfo.sqliteDir ?? '-'}
                        </Typography>
                      </Stack>
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={labelCellSx}>日志目录</TableCell>
                    <TableCell>
                      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                        <IconButton
                          size="small"
                          onClick={() => void copyDir(runInfo?.serverInfo.logDir ?? '')}
                          disabled={!runInfo?.serverInfo.logDir}
                          aria-label="复制日志目录路径"
                        >
                          <CopyIcon fontSize="small" />
                        </IconButton>
                        <Typography variant="inherit" sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>
                          {runInfo?.serverInfo.logDir ?? '-'}
                        </Typography>
                      </Stack>
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={labelCellSx}>主机名</TableCell>
                    <TableCell>{runInfo?.sysInfo.hostname ?? '-'}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={labelCellSx}>Go 版本</TableCell>
                    <TableCell>{runInfo?.sysInfo.goVersion ?? '-'}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={labelCellSx}>操作系统</TableCell>
                    <TableCell>{runInfo?.sysInfo.os ?? '-'}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={labelCellSx}>架构</TableCell>
                    <TableCell>{runInfo?.sysInfo.arch ?? '-'}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </TableContainer>

            {runState === 'stopped' && (
              <Alert severity="info">
                服务未运行，系统信息暂不可用。打开上方「服务状态」开关即可启动本地服务。
              </Alert>
            )}
          </Stack>
        )}
      </Box>

      <Snackbar
        open={toastOpen}
        autoHideDuration={2000}
        onClose={() => setToastOpen(false)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={toast.severity} variant="filled">
          {toast.text}
        </Alert>
      </Snackbar>
    </Box>
  );
}

export default ServerStatusPage;
