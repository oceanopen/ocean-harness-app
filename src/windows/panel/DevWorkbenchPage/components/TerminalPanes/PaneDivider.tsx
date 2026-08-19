import type { PointerEvent } from 'react';
import type { SplitDirection } from './types';
import { Box } from '@mui/material';
import { useRef, useState } from 'react';

interface PaneDividerProps {
  direction: SplitDirection;
  // 拖拽中 ratio 变更（first 子占比）。高频调用（pointermove 级），父层直通 store
  // setRatio——树操作按 id 替换且值未变时引用不变，不会多余重渲染。
  onRatioChange: (ratio: number) => void;
}

// 拖拽上下文：起始指针坐标 + 实渲染起始 ratio + 容器总长，按增量比例换算（避免
// 累计误差）。外部事件型数据 → ref（§5.2 前端范式，参照 ResizableDrawer）。
interface DragContext {
  startClientX: number;
  startClientY: number;
  startRatio: number;
  // 拖拽方向总长（px）：horizontal 取容器宽、vertical 取容器高。
  totalSize: number;
}

// PaneDivider：split 分隔条拖拽调比例（docs/terminal_02_split_panes.md §3.4）。
//
// pointer capture 模式（setPointerCapture 持续接收 move/up，指针出窗不丢事件）；
// ratio = 起始比例 + 指针位移/容器总长，clamp 由 setRatioNode 统一执行；
// horizontal（左右分）= col-resize 光标横向拖，vertical（上下分）= row-resize 纵向拖。
// 4px 命中区 + hover 高亮 + 拖拽中高亮。
export default function PaneDivider({ direction, onRatioChange }: PaneDividerProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const dragRef = useRef<DragContext | null>(null);
  const [dragging, setDragging] = useState(false);

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    const container = containerRef.current?.parentElement;
    if (container == null) {
      return;
    }
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    // 从 DOM 实测容器尺寸换算比例（flex-grow 实际渲染结果可能与 ratio 有亚像素差，
    // 以实渲染为准重设起点，拖拽首帧不跳变）。起始 ratio 由 flexGrow 内联值读回。
    const first = container.firstElementChild as HTMLElement | null;
    const second = container.lastElementChild as HTMLElement | null;
    const size = direction === 'horizontal' ? container.clientWidth : container.clientHeight;
    let startRatio = 0.5;
    if (first && second && size > 0) {
      const firstSize = direction === 'horizontal' ? first.clientWidth : first.clientHeight;
      const secondSize = direction === 'horizontal' ? second.clientWidth : second.clientHeight;
      const span = firstSize + secondSize;
      if (span > 0) {
        startRatio = firstSize / span;
      }
    }
    dragRef.current = {
      startClientX: e.clientX,
      startClientY: e.clientY,
      startRatio,
      totalSize: size,
    };
    setDragging(true);
  };

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (drag == null || drag.totalSize <= 0) {
      return;
    }
    const delta = direction === 'horizontal'
      ? e.clientX - drag.startClientX
      : e.clientY - drag.startClientY;
    onRatioChange(drag.startRatio + delta / drag.totalSize);
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
      ref={containerRef}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      aria-hidden
      sx={{
        'flexShrink': 0,
        'width': direction === 'horizontal' ? 4 : '100%',
        'height': direction === 'horizontal' ? '100%' : 4,
        'bgcolor': dragging ? 'primary.main' : 'divider',
        'cursor': direction === 'horizontal' ? 'col-resize' : 'row-resize',
        'transition': dragging ? 'none' : 'background-color 0.15s',
        '&:hover': { bgcolor: 'action.selected' },
      }}
    />
  );
}
