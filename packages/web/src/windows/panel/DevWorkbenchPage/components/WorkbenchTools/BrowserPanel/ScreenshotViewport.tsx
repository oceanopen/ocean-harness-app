import { Box, Typography } from '@mui/material';

/**
 * 视口帧（帧源无关，T3.2 实施决策③）：数据形态只有「一帧图 + 截取时刻」——当前帧源是
 * REST screenshot 轮询，未来换 screencast 帧流（§5 扩展点）时仅替换帧源生产侧，视口
 * 组件零改动。
 */
export interface ScreenshotFrame {
  dataUrl: string;
  /** 截取完成时刻（epoch ms，展示帧龄）。 */
  takenAt: number;
}

interface ScreenshotViewportProps {
  frame: ScreenshotFrame | null;
}

/**
 * ScreenshotViewport：会话卡截图视口——当前帧等比撑满卡片宽度 + 截取时刻标注；无帧时
 * 占位态（等待首帧截图）。
 */
export default function ScreenshotViewport({ frame }: ScreenshotViewportProps) {
  if (frame == null) {
    return (
      <Box
        sx={{
          height: 160,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          border: 1,
          borderColor: 'divider',
          borderRadius: 1,
          bgcolor: 'action.hover',
        }}
      >
        <Typography variant="body2" color="text.secondary">等待首帧截图…</Typography>
      </Box>
    );
  }
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
      <Box
        component="img"
        src={frame.dataUrl}
        alt="页面截图"
        sx={{ display: 'block', width: '100%', border: 1, borderColor: 'divider', borderRadius: 1, bgcolor: '#000' }}
      />
      <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>
        {`截取于 ${new Date(frame.takenAt).toLocaleTimeString('zh-CN', { hour12: false })}`}
      </Typography>
    </Box>
  );
}
