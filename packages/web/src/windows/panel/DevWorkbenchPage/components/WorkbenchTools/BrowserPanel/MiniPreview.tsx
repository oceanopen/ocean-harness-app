import type { LiveViewportFrame } from '@src/state/browserSession';
import type { RefObject } from 'react';
import { Box, Typography } from '@mui/material';
import { useRef } from 'react';
import { useFramePaint } from './useFramePaint';

interface MiniPreviewProps {
  /** keep-latest 帧箱（卡片经 useBrowserFrameStream 节流档订阅持有）。 */
  frameRef: RefObject<LiveViewportFrame | null>;
  /** 是否已有帧（占位态切换；首帧置 true、重订清帧置 false）。 */
  hasFrame: boolean;
  /** 预览区固定高度（px）。 */
  height: number;
}

/**
 * MiniPreview：会话卡迷你预览（纯展示，无输入监听——操作收敛在操作台 Stage）。帧源为
 * 后端 throttleMs 节流流（约 0.5fps）；绘制 rAF 消费循环同 Stage（useFramePaint 共用）；
 * contain 缩放同 Stage（canvas max 约束 + 留白）。
 */
export default function MiniPreview({ frameRef, hasFrame, height }: MiniPreviewProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  useFramePaint(frameRef, canvasRef);

  const boxSx = {
    height,
    flexShrink: 0,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    border: 1,
    borderColor: 'divider',
    borderRadius: 1,
    overflow: 'hidden',
    lineHeight: 0,
  } as const;

  if (!hasFrame) {
    return (
      <Box sx={{ ...boxSx, bgcolor: 'action.hover' }}>
        <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>等待画面…</Typography>
      </Box>
    );
  }
  return (
    <Box sx={{ ...boxSx, bgcolor: '#000' }}>
      <canvas ref={canvasRef} aria-label="会话画面预览" style={{ display: 'block', maxWidth: '100%', maxHeight: '100%' }} />
    </Box>
  );
}
