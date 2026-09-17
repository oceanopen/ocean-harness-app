import type { PaneLayoutNode } from './types';
import { Box } from '@mui/material';
import { ensureLayout, useTerminalPanesStore } from '@src/state/terminalPanes';
import EmbeddedTerminal from '@src/windows/panel/DevWorkbenchPage/components/EmbeddedTerminal/EmbeddedTerminal';
import { useEffect, useRef, useState } from 'react';
import { layoutGeometry } from './layoutGeometry';
import PaneDivider from './PaneDivider';

interface PaneLayoutProps {
  issueId: string;
  node: PaneLayoutNode;
}

// PaneLayout：布局树平铺渲染（docs/terminal_02_split_panes.md §3.4 平铺版）。
//
// 结构恒定原则（分屏零重挂载）：pane 以 key=paneId 平铺为根容器的直接子节点，
// 布局树只产出【几何样式】不产出【组件结构】——分屏/关屏 = 兄弟增删 + 样式
// 更新，存活 pane 的组件实例、xterm 会话、焦点与滚动全保留。此前嵌套 flex
// 递归渲染在分屏时因「根位置元素类型变化（EmbeddedTerminal → Box）」触发
// React 整树卸载重建 → ring 回放进失配几何 + 校正 resize 交错 → 左右分屏
// 顶部空白（宽度变化才有 reflow 失配，上下分屏正常——实测定位）。
//
// 几何由 layoutGeometry 纯函数从（树, 容器尺寸）计算；容器尺寸经根容器
// ResizeObserver 测量——窗口缩放/面板开合挤压/比例调整同路径重算。pane 内
// 终端的 fit/resize 走 TerminalView 自带 ResizeObserver（现有链路零改动）。
//
// 残余伪影定论（勿再追查）：左右分屏/拖分割条后 pane 顶部偶现单行空行，属
// shell 侧固有反应——宽度变化引起折行时 zsh 重绘账目错位多吐换行，iTerm2/
// Terminal.app 外部终端拖窄到折行同样出现（实测对照）；拖拽中连续 cols 变化
// 逐次累积、反向拖拽重排合并回退；上下分屏（纯高度变化）不触发；新 spawn 的
// pane（零 resize）恒干净。应用侧已收敛到每次真实几何变化恰一次 SIGWINCH
// （usePtySession 尺寸台账去重），无进一步优化空间。
export default function PaneLayout({ issueId, node }: PaneLayoutProps) {
  const setRatio = useTerminalPanesStore(s => s.setRatio);

  // 渲染期 hydration（幂等）：store 无记录时从 localStorage 读回（F5 布局还原），
  // 读回值落 store 后本组件随 store 更新重渲染（zustand v5 selector 引用稳定
  // 契约成立，loadLayout 损坏/缺失返回共享 INITIAL_LAYOUT 常量）。
  ensureLayout(issueId);

  const containerRef = useRef<HTMLDivElement>(null);
  // 根容器尺寸（px）：null = 未测得（首帧前），渲染空容器。
  const [size, setSize] = useState<{ width: number; height: number } | null>(null);

  useEffect(() => {
    const container = containerRef.current;
    if (container == null) {
      return;
    }
    const apply = () => {
      const width = container.clientWidth;
      const height = container.clientHeight;
      setSize(prev =>
        prev != null && prev.width === width && prev.height === height ? prev : { width, height });
    };
    // 初测依赖 RO 首次 observe 的固有回调（不做 effect 内同步 setState——
    // react/set-state-in-effect，参照 usePtySession attachKey 的渲染期范式注释）。
    const observer = new ResizeObserver(apply);
    observer.observe(container);
    return () => {
      observer.disconnect();
    };
  }, []);

  const geometry = size == null ? null : layoutGeometry(node, size.width, size.height);

  return (
    <Box ref={containerRef} sx={{ position: 'relative', width: '100%', height: '100%', overflow: 'hidden' }}>
      {geometry != null && (
        <>
          {geometry.panes.map(pane => (
            <Box
              key={pane.paneId}
              sx={{
                position: 'absolute',
                left: pane.x,
                top: pane.y,
                width: pane.width,
                height: pane.height,
                overflow: 'hidden',
              }}
            >
              <EmbeddedTerminal issueId={issueId} paneId={pane.paneId} />
            </Box>
          ))}
          {geometry.dividers.map(divider => (
            <PaneDivider
              key={divider.splitId}
              rect={divider}
              onRatioChange={ratio => setRatio(issueId, divider.splitId, ratio)}
            />
          ))}
        </>
      )}
    </Box>
  );
}
