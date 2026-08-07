import type { PointerEvent, ReactNode } from 'react';
import {
  Box,
  Drawer,
} from '@mui/material';
import { useRef, useState } from 'react';

// 拖拽调宽的宽度上下限（视口百分比）：太窄表单挤、太宽遮主面板，30%~85% 取折中。
const MIN_WIDTH_PCT = 30;
const MAX_WIDTH_PCT = 85;

interface ResizableDrawerProps {
  open: boolean;
  onClose?: () => void;
  // 拖拽初始宽度（视口百分比）；每次打开回到此宽度（不持久化）。
  defaultWidthPct: number;
  children: ReactNode;
}

// 可拖拽调宽的右侧抽屉壳：左缘拖拽手柄 + 内容区。头部/内容/底栏由各抽屉以 children 传入。
//
// 宽度百分比 state（初值=defaultWidthPct），拖拽按指针 x 实时更新并 clamp 到 [MIN, MAX]：
// 抽屉从右滑出、手柄在左缘，指针向左拖（clientX 减小）→ 宽度增大。
// 不持久化——抽屉按需挂载/卸载，每次打开自然回到默认宽度（符合「默认宽度作为初始值」）。
function ResizableDrawer({ open, onClose, defaultWidthPct, children }: ResizableDrawerProps) {
  const [widthPct, setWidthPct] = useState(defaultWidthPct);
  const [dragging, setDragging] = useState(false);
  // 拖拽上下文：记录起始指针 x 与起始宽度，pointermove 时据此算增量（避免累计误差）。
  const dragRef = useRef<{ startX: number; startPct: number } | null>(null);

  const widthPx = Math.round((window.innerWidth * widthPct) / 100);

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    dragRef.current = { startX: e.clientX, startPct: widthPct };
    setDragging(true);
  };

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (!dragRef.current) {
      return;
    }
    const dx = dragRef.current.startX - e.clientX;
    const deltaPct = (dx / window.innerWidth) * 100;
    const next = Math.max(MIN_WIDTH_PCT, Math.min(MAX_WIDTH_PCT, dragRef.current.startPct + deltaPct));
    setWidthPct(next);
  };

  const onPointerUp = (e: PointerEvent<HTMLDivElement>) => {
    if (dragRef.current) {
      dragRef.current = null;
      setDragging(false);
    }
    e.currentTarget.releasePointerCapture?.(e.pointerId);
  };

  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
      sx={{ '& .MuiDrawer-paper': { width: { xs: '100%', sm: `${widthPx}px` } } }}
    >
      <Box sx={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
        {/* 左缘拖拽手柄 */}
        <Box
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          sx={{
            'width': 6,
            'flexShrink': 0,
            'cursor': 'col-resize',
            'bgcolor': dragging ? 'primary.main' : 'transparent',
            'transition': dragging ? 'none' : 'background-color 0.15s',
            '&:hover': { bgcolor: 'action.selected' },
          }}
        />
        {/* 内容区：各抽屉的头部/内容/底栏 */}
        <Box sx={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', height: '100%' }}>
          {children}
        </Box>
      </Box>
    </Drawer>
  );
}

export default ResizableDrawer;
