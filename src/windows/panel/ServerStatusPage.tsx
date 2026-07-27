import type { HttpServerStatus } from '@src/shared/bindings';
import { Autorenew as AutorenewIcon } from '@mui/icons-material';
import {
  Alert,
  Box,
  CircularProgress,
  IconButton,
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

// GET /api/baseInfo/getSysInfo 返回的 data 载荷（与 Go SysInfoResponseData 对齐）。
interface SysInfoResponseData {
  hostname: string;
  goVersion: string;
  os: string;
  arch: string;
  mode: string;
}

// 运行模式徽章文案（mode 取自 Rust status，值为 debug/release/test）。
const MODE_LABEL: Record<string, string> = {
  debug: 'debug 调试',
  release: 'release 正式',
  test: 'test 测试',
};

// 启用服务后 Go 绑定端口需要一点时间，重试拉 sysinfo。
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
// 第一行 Switch 可开关服务（IPC 调 Rust start/stop sidecar）；地址取自 Rust（端口随模式 dev=9000/build=9100）。
function ServerStatusPage() {
  const [status, setStatus] = useState<HttpServerStatus | null>(null);
  const [sysInfo, setSysInfo] = useState<SysInfoResponseData | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [toggling, setToggling] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  // 拉取 sysinfo（带重试：服务刚启动时端口可能尚未就绪）。成功返回 true。
  const fetchSysInfo = useCallback(async (address: string): Promise<boolean> => {
    for (let i = 0; i < FETCH_RETRY_TIMES; i += 1) {
      try {
        const resp = await fetch(`${address}/api/baseInfo/getSysInfo`);
        if (!resp.ok) {
          throw new Error(`HTTP ${resp.status}`);
        }
        const body: ApiResponse<SysInfoResponseData> = await resp.json();
        if (body.code !== 0) {
          throw new Error(body.msg || `code ${body.code}`);
        }
        setSysInfo(body.data);
        return true;
      } catch {
        await sleep(FETCH_RETRY_DELAY_MS);
      }
    }
    return false;
  }, []);

  // 拉取服务状态 + （运行中时）sysinfo。
  const reload = useCallback(async (): Promise<void> => {
    try {
      const s = await commands.httpServerStatus();
      setStatus(s);
      setErrorMsg(null);
      if (s.running) {
        const ok = await fetchSysInfo(s.address);
        if (!ok) {
          setErrorMsg(`请求本地服务失败（${s.address}）`);
        }
      } else {
        setSysInfo(null);
        setErrorMsg(`请先开启本地服务`);
      }
    } catch (e) {
      setErrorMsg(`查询服务状态失败：${String(e)}`);
    }
  }, [fetchSysInfo]);

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

  // Switch 开关：调 Rust set_http_server_enabled，成功后同步状态 + 按需拉 sysinfo。
  const handleToggle = useCallback(
    async (checked: boolean) => {
      setToggling(true);
      setErrorMsg(null);
      try {
        const r = await commands.setHttpServerEnabled(checked);
        if (r.status === 'error') {
          setErrorMsg(`${checked ? '启动' : '停止'}服务失败：${r.error}`);
          return;
        }
        const s = await commands.httpServerStatus();
        setStatus(s);
        if (checked) {
          const ok = await fetchSysInfo(s.address);
          if (!ok) {
            setErrorMsg(`服务已启动，但拉取信息失败（${s.address}）`);
          }
        } else {
          setSysInfo(null);
        }
      } finally {
        setToggling(false);
      }
    },
    [fetchSysInfo],
  );

  const running = status?.running ?? false;
  const modeLabel = status ? MODE_LABEL[status.mode] ?? status.mode : '';
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
            {errorMsg && <Alert severity="warning" onClose={() => setErrorMsg(null)}>{errorMsg}</Alert>}

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
                  {/* 服务地址（来自 Rust，端口随模式） */}
                  <TableRow>
                    <TableCell sx={labelCellSx}>服务地址</TableCell>
                    <TableCell sx={{ fontFamily: 'monospace' }}>{status?.address ?? '-'}</TableCell>
                  </TableRow>
                  {/* 运行模式 */}
                  <TableRow>
                    <TableCell sx={labelCellSx}>运行模式</TableCell>
                    <TableCell>{modeLabel || status?.mode || '-'}</TableCell>
                  </TableRow>
                  {/* 以下为 sysinfo（服务未运行时无数据） */}
                  <TableRow>
                    <TableCell sx={labelCellSx}>主机名</TableCell>
                    <TableCell>{sysInfo?.hostname ?? '-'}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={labelCellSx}>Go 版本</TableCell>
                    <TableCell>{sysInfo?.goVersion ?? '-'}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={labelCellSx}>操作系统</TableCell>
                    <TableCell>{sysInfo?.os ?? '-'}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={labelCellSx}>架构</TableCell>
                    <TableCell>{sysInfo?.arch ?? '-'}</TableCell>
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
    </Box>
  );
}

export default ServerStatusPage;
