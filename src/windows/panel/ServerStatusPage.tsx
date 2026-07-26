import { Autorenew as AutorenewIcon } from '@mui/icons-material';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Chip,
  CircularProgress,
  IconButton,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableRow,
  Typography,
} from '@mui/material';
import { useCallback, useEffect, useState } from 'react';

type LoadStatus = 'loading' | 'ready' | 'error';

// HTTP 服务固定监听地址（与 src-go/cmd/server/main.go 的 listenAddr 一致）。
const SERVER_URL = 'http://127.0.0.1:9000';

// GET /api/sysinfo 返回结构，与 src-go/internal/handler/sysinfo.go 的 SysInfoResponse 对齐。
// 前端直连 fetch（不经 specta），此类型手写维护，改 Go struct 须同步此处。
interface SysInfoData {
  hostname: string;
  goVersion: string;
  os: string;
  arch: string;
  mode: string;
}

// mode 徽章文案（临时测试页面，硬编码）。
const MODE_LABEL: Record<string, string> = {
  dev: 'dev 调试',
  build: 'build 编译',
};

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

// panel 窗口「服务状态」菜单页面：临时测试页，演示前端直连本地 HTTP 服务获取数据。
// dev/build 模式由 Rust 拉起同一服务（mode 字段体现），固定端口 9000，前端直接 fetch。
function ServerStatusPage() {
  const [status, setStatus] = useState<LoadStatus>('loading');
  const [sysInfo, setSysInfo] = useState<SysInfoData | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async (): Promise<void> => {
    setStatus('loading');
    try {
      const resp = await fetch(`${SERVER_URL}/api/sysinfo`);
      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}`);
      }
      setSysInfo((await resp.json()) as SysInfoData);
      setStatus('ready');
    } catch {
      setStatus('error');
    }
  }, []);

  // 初次挂载静默加载。
  useEffect(() => {
    void load();
  }, [load]);

  const handleRefresh = useCallback(async () => {
    setRefreshing(true);
    await load();
    setRefreshing(false);
  }, [load]);

  const modeLabel = sysInfo?.mode ? MODE_LABEL[sysInfo.mode] : undefined;
  const modeColor = sysInfo?.mode === 'build' ? 'success' : 'warning';

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <Box
        sx={{
          p: 2,
          borderBottom: 1,
          borderColor: 'divider',
          display: 'flex',
          alignItems: 'center',
          gap: 1,
        }}
      >
        <Typography variant="body2" sx={{ fontWeight: 600 }}>
          服务状态
        </Typography>
        {modeLabel && (
          <Chip label={modeLabel} color={modeColor} size="small" variant="outlined" />
        )}
        <Box sx={{ flex: 1 }} />
        <IconButton size="small" onClick={handleRefresh} disabled={refreshing} aria-label="refresh">
          <AutorenewIcon sx={spinSx(refreshing)} />
        </IconButton>
      </Box>

      <Box sx={{ flex: 1, minHeight: 0, overflow: 'auto', p: 2 }}>
        {/* 服务地址信息 */}
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          服务地址：
          <Box component="span" sx={{ fontFamily: 'monospace' }}>{SERVER_URL}</Box>
        </Typography>

        {status === 'loading' && (
          <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
            <CircularProgress />
          </Box>
        )}

        {status === 'error' && (
          <Alert
            severity="warning"
            action={(
              <Button
                color="inherit"
                size="small"
                onClick={() => {
                  void load();
                }}
              >
                重试
              </Button>
            )}
          >
            <AlertTitle>获取失败</AlertTitle>
            {`请求本地服务失败，请确认服务已启动（${SERVER_URL}）后重试。`}
          </Alert>
        )}

        {status === 'ready' && sysInfo && (
          <Stack spacing={2}>
            <TableContainer sx={{ border: 1, borderColor: 'divider', borderRadius: 1 }}>
              <Table size="small">
                <TableBody>
                  <TableRow>
                    <TableCell sx={{ fontWeight: 600, width: 140 }}>主机名</TableCell>
                    <TableCell>{sysInfo.hostname}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={{ fontWeight: 600 }}>Go 版本</TableCell>
                    <TableCell>{sysInfo.goVersion}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={{ fontWeight: 600 }}>操作系统</TableCell>
                    <TableCell>{sysInfo.os}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={{ fontWeight: 600 }}>架构</TableCell>
                    <TableCell>{sysInfo.arch}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </TableContainer>
          </Stack>
        )}
      </Box>
    </Box>
  );
}

export default ServerStatusPage;
