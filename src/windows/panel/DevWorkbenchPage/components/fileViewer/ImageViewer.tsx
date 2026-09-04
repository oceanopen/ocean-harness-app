import type { SxProps, Theme } from '@mui/material';
import {
  BrokenImageOutlined as BrokenImageOutlinedIcon,
  FitScreen as FitScreenIcon,
  RestartAlt as RestartAltIcon,
  ZoomIn as ZoomInIcon,
  ZoomOut as ZoomOutIcon,
} from '@mui/icons-material';
import { Box, CircularProgress, IconButton, Tooltip, Typography, useTheme } from '@mui/material';
import { IssueWorkspaceService } from '@src/services';
import { basename } from '@src/shared/repoPath';
import { useEffect, useRef, useState } from 'react';
import {
  TransformComponent,
  TransformWrapper,
  useControls,
  useTransformEffect,
} from 'react-zoom-pan-pinch';

/// 棋盘格透明底（halo 同款四渐变 20px 棋盘；深浅两套配色随 app 主题）。
function checkerboardSx(dark: boolean): SxProps<Theme> {
  const cell = dark ? '#222222' : '#d8d8d8';
  return {
    backgroundColor: dark ? '#1a1a1a' : '#ededed',
    backgroundImage: [
      `linear-gradient(45deg, ${cell} 25%, transparent 25%)`,
      `linear-gradient(-45deg, ${cell} 25%, transparent 25%)`,
      `linear-gradient(45deg, transparent 75%, ${cell} 75%)`,
      `linear-gradient(-45deg, transparent 75%, ${cell} 75%)`,
    ].join(', '),
    backgroundSize: '20px 20px',
    backgroundPosition: '0 0, 0 10px, 10px -10px, -10px 0px',
  };
}

interface ImageViewerToolbarProps {
  natural: { width: number; height: number };
  loaded: boolean;
  /// 适应窗口：调用方以容器实测尺寸计算 contain 比例（先测量后使用）。
  onFit: (controls: ReturnType<typeof useControls>) => void;
}

/// 图片工具栏（TransformWrapper 上下文内）：尺寸/缩放百分比 + 缩放/适应/重置按钮。
/// useTransformEffect 订阅变换态刷新百分比显示。
function ImageViewerToolbar({ natural, loaded, onFit }: ImageViewerToolbarProps) {
  const controls = useControls();
  const [scale, setScale] = useState(1);
  // 变换事件订阅回调（非 effect 同步段——react/set-state-in-effect 误报，事件驱动 setState）
  // eslint-disable-next-line react/set-state-in-effect
  useTransformEffect(({ state }) => setScale(state.scale));

  return (
    <Box
      sx={{
        height: 36,
        flexShrink: 0,
        display: 'flex',
        alignItems: 'center',
        px: 1,
        gap: 0.5,
        borderBottom: 1,
        borderColor: 'divider',
        bgcolor: 'background.paper',
      }}
    >
      {loaded && (
        <Typography variant="caption" color="text.secondary">
          {natural.width}
          {' × '}
          {natural.height}
          {' · '}
          {Math.round(scale * 100)}
          %
        </Typography>
      )}
      <Box sx={{ flex: 1 }} />
      <Tooltip title="缩小">
        <IconButton size="small" aria-label="缩小" onClick={() => void controls.zoomOut()} sx={{ color: 'text.secondary' }}>
          <ZoomOutIcon sx={{ fontSize: 16 }} />
        </IconButton>
      </Tooltip>
      <Tooltip title="放大">
        <IconButton size="small" aria-label="放大" onClick={() => void controls.zoomIn()} sx={{ color: 'text.secondary' }}>
          <ZoomInIcon sx={{ fontSize: 16 }} />
        </IconButton>
      </Tooltip>
      <Tooltip title="适应窗口">
        <IconButton size="small" aria-label="适应窗口" onClick={() => onFit(controls)} sx={{ color: 'text.secondary' }}>
          <FitScreenIcon sx={{ fontSize: 16 }} />
        </IconButton>
      </Tooltip>
      <Tooltip title="实际大小">
        <IconButton size="small" aria-label="实际大小" onClick={() => void controls.resetTransform()} sx={{ color: 'text.secondary' }}>
          <RestartAltIcon sx={{ fontSize: 16 }} />
        </IconButton>
      </Tooltip>
    </Box>
  );
}

interface ImageViewerProps {
  issueId: string;
  baseDir: string;
  path: string;
  /// 缓存刷新令牌（内容重验时变化 → URL 变化强制重新加载；服务端另有 no-store 双保险）。
  version: number | string;
}

/// 图片查看器（观感与交互对齐 halo ImageViewer，实现换 react-zoom-pan-pinch）：fileRaw URL
/// 直连本地文件（零 base64 转码）+ 滚轮缩放/拖拽平移/双击缩放（库内建）+ 适应窗口
/// （fitOnInit 加载即 contain，手动按钮重算）+ 实际大小 + 棋盘格透明底 + 尺寸/百分比显示。
export default function ImageViewer({ issueId, baseDir, path, version }: ImageViewerProps) {
  const theme = useTheme();
  const dark = theme.palette.mode === 'dark';
  const viewportRef = useRef<HTMLDivElement>(null);
  const [url, setUrl] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  const [natural, setNatural] = useState({ width: 0, height: 0 });

  // version 变化（内容重验）即换 URL 重载：渲染期重置加载/失败态（React 官方「props 变化
  // 派生 state」模式，避免 effect 内同步 setState 的二次渲染）。
  const [lastVersion, setLastVersion] = useState(version);
  if (lastVersion !== version) {
    setLastVersion(version);
    setLoaded(false);
    setFailed(false);
  }

  // URL 解析（base 来自 httpServerStatus 缓存，异步一次）；version 变化（重验）换 URL。
  useEffect(() => {
    let alive = true;
    IssueWorkspaceService.fileRawUrl({ issueId, baseDir, path, v: version })
      .then(u => alive && setUrl(u))
      .catch((e) => {
        console.warn('[ImageViewer] resolve fileRaw url failed:', e);
        if (alive) {
          setFailed(true);
        }
      });
    return () => {
      alive = false;
    };
  }, [issueId, baseDir, path, version]);

  // 适应窗口（halo fitScale 同款公式：contain 内缩且不放大，容器实测尺寸先测量后使用）。
  const handleFit = (controls: ReturnType<typeof useControls>) => {
    const c = viewportRef.current;
    if (c == null || natural.width === 0 || natural.height === 0) {
      return;
    }
    const fit = Math.max(0.05, Math.min((c.clientWidth - 48) / natural.width, (c.clientHeight - 48) / natural.height, 1));
    void controls.centerView(fit);
  };

  return (
    <Box sx={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      <TransformWrapper minScale={0.1} maxScale={5} limitToBounds={false} fitOnInit centerOnInit>
        <ImageViewerToolbar natural={natural} loaded={loaded} onFit={handleFit} />
        <Box ref={viewportRef} sx={{ flex: 1, minHeight: 0, overflow: 'hidden', position: 'relative', ...checkerboardSx(dark) }}>
          {failed
            ? (
                <Box sx={{ position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 1 }}>
                  <BrokenImageOutlinedIcon sx={{ fontSize: 40, color: 'text.secondary' }} />
                  <Typography variant="body2" color="text.secondary">图片加载失败（文件可能已移动或删除）</Typography>
                </Box>
              )
            : url != null
              ? (
                  <TransformComponent wrapperStyle={{ width: '100%', height: '100%' }} contentStyle={{ width: '100%', height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                    <img
                      src={url}
                      alt={basename(path)}
                      draggable={false}
                      onLoad={(e) => {
                        setNatural({ width: e.currentTarget.naturalWidth, height: e.currentTarget.naturalHeight });
                        setLoaded(true);
                      }}
                      onError={() => setFailed(true)}
                      style={{ opacity: loaded ? 1 : 0, transition: 'opacity 0.15s' }}
                    />
                  </TransformComponent>
                )
              : null}
          {!loaded && !failed && (
            <Box sx={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <CircularProgress size={20} />
            </Box>
          )}
        </Box>
      </TransformWrapper>
    </Box>
  );
}
