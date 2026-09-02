import type { PointerEvent } from 'react';
import type { DividerRect } from './layoutGeometry';
import { Box } from '@mui/material';
import { useRef, useState } from 'react';

interface PaneDividerProps {
  // 几何（layoutGeometry 产出）：divider 自身矩形 + 所属 split 矩形/当前 ratio。
  rect: DividerRect;
  // 拖拽中 ratio 变更（first 子占比）。高频调用（pointermove 级），父层直通 store
  // setRatio——树操作按 id 替换且值未变时引用不变，不会多余重渲染。
  onRatioChange: (ratio: number) => void;
}

// 拖拽上下文：起始指针坐标 + 起始比例（几何 props 给出，平铺版不再读 DOM 兄弟
// 实测）。外部事件型数据 → ref（§5.2 前端范式，参照 ResizableDrawer）。
interface DragContext {
  startClientX: number;
  startClientY: number;
  startRatio: number;
}

// PaneDivider：split 分隔条拖拽调比例（平铺几何版，docs/terminal_02_split_panes.md §3.4）。
//
// divider 绝对定位（矩形由 layoutGeometry 按当前 ratio 计算）；拖拽换算改为
// 增量式：指针位移 / 所属 split 沿分割方向总长（splitWidth/splitHeight，含
// divider 占位）→ 比例增量，加起始比例即新值——与几何计算同基准，无 DOM 实测。
// clamp 由 store setRatioNode 统一执行（[MIN, MAX]）。
// pointer capture 模式（setPointerCapture 持续接收 move/up，指针出窗不丢事件）；
// 4px 命中区 + hover 高亮 + 拖拽中高亮。horizontal（左右分）= col-resize。
export default function PaneDivider({ rect, onRatioChange }: PaneDividerProps) {
  const dragRef = useRef<DragContext | null>(null);
  const [dragging, setDragging] = useState(false);

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    dragRef.current = {
      startClientX: e.clientX,
      startClientY: e.clientY,
      startRatio: rect.ratio,
    };
    setDragging(true);
  };

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (drag == null) {
      return;
    }
    const total = rect.direction === 'horizontal' ? rect.splitWidth : rect.splitHeight;
    if (total <= 0) {
      return;
    }
    const delta = rect.direction === 'horizontal'
      ? e.clientX - drag.startClientX
      : e.clientY - drag.startClientY;
    onRatioChange(drag.startRatio + delta / total);
  };

  const onPointerUp = (e: PointerEvent<HTMLDivElement>) => {
    if (dragRef.current != null) {
      dragRef.current = null;
      setDragging(false);
    }
    e.currentTarget.releasePointerCapture?.(e.pointerId);
  };

  return (
    <Box
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      aria-hidden
      sx={{
        'position': 'absolute',
        'left': rect.x,
        'top': rect.y,
        'width': rect.width,
        'height': rect.height,
        'bgcolor': dragging ? 'primary.main' : 'divider',
        'cursor': rect.direction === 'horizontal' ? 'col-resize' : 'row-resize',
        'transition': dragging ? 'none' : 'background-color 0.15s',
        '&:hover': { bgcolor: 'action.selected' },
      }}
    />
  );
}
