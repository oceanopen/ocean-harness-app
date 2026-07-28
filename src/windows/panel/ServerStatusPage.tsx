import type { HttpServerStatus } from '@src/shared/bindings';
import { Autorenew as AutorenewIcon } from '@mui/icons-material';
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

// 浮层 toast 严重级别（操作失败用 error，状态提示用 warning）。
type ToastSeverity = 'warning' | 'error';

// 启用服务后 Go 绑定端口需要一点时间，重试拉 serverRunInfo。
const FETCH_RETRY_TIMES = 6;
const FETCH_RETRY_DELAY_MS = 400;
// 轮询服务状态，保持 Switch 与后端实际运行态一致。
const STATUS_POLL_INTERVAL_MS = 3000;

// 标签列统一样式（固定宽度 + 轻底色，便于扫读）。
const labelCellSx = { fontWeight: 600, width: 140, bgcolor: 'action.hover', whiteSpace: 'nowrap' } as const;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
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

  // 拉取 serverRunInfo（带重试：服务刚启动时端口可能尚未就绪）。成功返回 true。
  const fetchRunInfo = useCallback(async (address: string): Promise<boolean> => {
    for (let i = 0; i < FETCH_RETRY_TIMES; i += 1) {
      try {
        const resp = await fetch(`${address}/api/baseInfo/getServerRunInfo`);
        if (!resp.ok) {
          throw new Error(`HTTP ${resp.status}`);
        }
        const body: ApiResponse<ServerRunInfo> = await resp.json();
        if (body.code !== 0) {
          throw new Error(body.msg || `code ${body.code}`);
        }
        setRunInfo(body.data);
        return true;
      } catch {
        await sleep(FETCH_RETRY_DELAY_MS);
      }
    }
    return false;
  }, []);

  // 拉取服务状态 + （运行中时）serverRunInfo。
  const reload = useCallback(async (): Promise<void> => {
    try {
      const s = await commands.httpServerStatus();
      setStatus(s);
      if (s.running) {
        const ok = await fetchRunInfo(s.address);
        if (!ok) {
          showToast(`请求本地服务失败（${s.address}）`, 'error');
        }
      } else {
        setRunInfo(null);
        showToast('请先开启本地服务', 'warning');
      }
    } catch (e) {
      showToast(`查询服务状态失败：${String(e)}`, 'error');
    }
  }, [fetchRunInfo, showToast]);

  // 初次挂载加载。
  useEffect(() => {
    void reload();
  }, [reload]);

  // 轮询服务状态（仅同步 running 标志，保持 Switch 准确；轻量）。
  useEffect(() => {
    const id = window.setInterval(() => {
      commands.httpServerStatus().then(setStatus).catch(() => {});
    }, STATUS_POLL_INTERVAL_MS);
    return () => window.clearInterval(id);
  }, []);

  const handleRefresh = useCallback(async () => {
    setRefreshing(true);
    await reload();
    setRefreshing(false);
  }, [reload]);

  // Switch 开关：开启时先清理跨会话残留的 go-server 孤立进程（best-effort，不阻断），再调 Rust 启停服务。
  // 成功后同步状态 + 按需拉 serverRunInfo。
  const handleToggle = useCallback(
    async (checked: boolean) => {
      setToggling(true);
      try {
        if (checked) {
          // 清理占用端口的本应用孤立 go-server，确保新 sidecar 能 bind 成功；失败不阻断启动。
          await commands.cleanupOrphanHttpServer().catch(() => {});
        }
        const r = await commands.setHttpServerEnabled(checked);
        if (r.status === 'error') {
          showToast(`${checked ? '启动' : '停止'}服务失败：${r.error}`, 'error');
          return;
        }
        const s = await commands.httpServerStatus();
        setStatus(s);
        if (checked) {
          const ok = await fetchRunInfo(s.address);
          if (!ok) {
            showToast(`服务已启动，但拉取信息失败（${s.address}）`, 'warning');
          }
        } else {
          setRunInfo(null);
        }
      } finally {
        setToggling(false);
      }
    },
    [fetchRunInfo, showToast],
  );

  const running = status?.running ?? false;
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
                          disabled={toggling}
                          size="small"
                        />
                        <Typography variant="body2" color={running ? 'success.main' : 'text.secondary'}>
                          {running ? '运行中' : '已停止'}
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
                    <TableCell sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>{runInfo?.serverInfo.sqliteDir ?? '-'}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={labelCellSx}>日志目录</TableCell>
                    <TableCell sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>{runInfo?.serverInfo.logDir ?? '-'}</TableCell>
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

            {!running && (
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
