import type { ProjectIssueResponseData } from '@src/services';
import type { ToolTab } from '@src/state/workbenchTools';
import type { PointerEvent } from 'react';
import type { WorkbenchToolDef } from './toolRegistry';
import { Close as CloseIcon } from '@mui/icons-material';
import { Box, IconButton, Tab, Tabs, Typography, useTheme } from '@mui/material';
import { useToolTabs, useWorkbenchToolsStore } from '@src/state/workbenchTools';
import { useRef, useState } from 'react';
import { toolDefById } from './toolRegistry';

/// 面板区最小宽度（左边界拖拽下限；tab 头 + 列表内容的最小可读宽）。
export const TOOL_AREA_MIN_WIDTH = 360;
/// 终端区最小宽度（拖拽上限 = 容器宽 - 此值，保证终端不被挤死；与页面终端区 minWidth 一致）。
export const TERMINAL_MIN_WIDTH = 320;

// 拖拽上下文：起始指针坐标 + 起始宽度 + 容器宽（down 时实测，先测量后使用——max 需
// 容器宽参与计算，见 PaneDivider 增量换算先例）。外部事件型数据 → ref。
interface DragContext {
  startClientX: number;
  startWidth: number;
  containerWidth: number;
}

interface ToolPanelAreaProps {
  /// 选中 issue（null = 未选中，面板区收起）。工具渲染上下文 + tabs 归属 key。
  issue: ProjectIssueResponseData | null;
  projectId: number | null;
  /// 面板区展开（页面按 config 折叠态 + 选中态合成传入；收起时外层 width 0）。
  visible: boolean;
  /// 持久化宽度（config 订阅值，拖拽结束经 onWidthCommit 落盘后回填）。
  width: number;
  /// 拖拽结束落盘（up 时一次性回调，写 config——move 高频期只走组件内存态）。
  onWidthCommit: (width: number) => void;
}

/// ToolPanelArea：中栏右侧的工具面板区——tab 头（可滚动 + 每 tab 关闭按钮）+ 当前激活
/// 工具内容（注册表 render 分发）。宽度默认 600（config），左缘 4px 把手拖拽调整：
/// pointer capture，down 实测容器宽 → move 内存态（transition 关）→ up 复位并一次落盘
/// （时序参照 PaneDivider，无防抖）。空 tab 栏保持面板区展开（空态提示；后续版本 tab 栏
/// 加 + 号下拉快捷添加）。非激活 tab 的内容组件不渲染——工具会话必须后端常驻（见
/// toolRegistry 架构红线注释），视口卸载不销毁会话。tabs 读取走域级 useToolTabs
/// （hydration + 响应式订阅成对封装，见 store.ts 注释）。
export default function ToolPanelArea({ issue, projectId, visible, width, onWidthCommit }: ToolPanelAreaProps) {
  const theme = useTheme();
  const closeTab = useWorkbenchToolsStore(s => s.closeTab);
  const setActiveTab = useWorkbenchToolsStore(s => s.setActiveTab);
  const issueId = issue?.id ?? null;
  const { tabs, activeTabId } = useToolTabs(issueId);

  // 遗留 tab 防御：注册表查不到 def（未来下线工具的残留记录）不渲染 tab 头/内容。
  // tab↔def 成对查一次（整个渲染周期唯一查表点）。
  const validEntries = tabs
    .map((tab): { tab: ToolTab; def: WorkbenchToolDef | undefined } => ({ tab, def: toolDefById(tab.toolId) }))
    .filter((e): e is { tab: ToolTab; def: WorkbenchToolDef } => e.def != null);
  // 激活项展示回退：activeTabId 不在有效项中（遗留脏数据/刚关闭清空）时回落首个有效 tab，
  // 保证 tab 头高亮与内容区始终一致、内容区不渲染成无提示空白。
  const displayActiveId = validEntries.some(e => e.tab.id === activeTabId)
    ? activeTabId
    : validEntries[0]?.tab.id ?? null;
  const activeEntry = validEntries.find(e => e.tab.id === displayActiveId) ?? null;

  // 拖拽宽度的两个独立状态：dragging（把手高亮/动画开关，up/cancel 复位）；dragWidth
  // （提交工作宽度，保留至 config 回填避免宽度回跳闪烁——不随 up 复位）。
  const [dragWidth, setDragWidth] = useState<number | null>(null);
  const [dragging, setDragging] = useState(false);
  const dragRef = useRef<DragContext | null>(null);
  const displayWidth = dragWidth ?? width;

  const onHandlePointerDown = (e: PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    // 容器宽 down 时实测一次（把手父级 = 面板区外层，其父级 = 中栏内容行：终端 + 面板区之和）。
    const containerWidth = e.currentTarget.parentElement?.parentElement?.getBoundingClientRect().width ?? 0;
    dragRef.current = { startClientX: e.clientX, startWidth: displayWidth, containerWidth };
    setDragWidth(Math.round(displayWidth));
    setDragging(true);
  };

  const onHandlePointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (drag == null) {
      return;
    }
    // 面板在右、把手在左缘：指针左移（负 delta）宽度增大。
    const next = drag.startWidth - (e.clientX - drag.startClientX);
    const max = Math.max(TOOL_AREA_MIN_WIDTH, drag.containerWidth - TERMINAL_MIN_WIDTH);
    setDragWidth(Math.round(Math.min(Math.max(next, TOOL_AREA_MIN_WIDTH), max)));
  };

  // up 与 pointercancel 同款收尾：复位拖拽态（把手高亮/宽度动画恢复），有变更则一次落盘。
  const onHandlePointerEnd = (e: PointerEvent<HTMLDivElement>) => {
    if (dragRef.current != null) {
      dragRef.current = null;
      setDragging(false);
      if (dragWidth != null && dragWidth !== width) {
        onWidthCommit(dragWidth); // up 收尾：一次性落盘 config（move 期纯内存）
      }
    }
    e.currentTarget.releasePointerCapture?.(e.pointerId);
  };

  return (
    <Box
      sx={{
        position: 'relative',
        width: visible ? displayWidth : 0,
        flexShrink: 0,
        overflow: 'hidden',
        borderLeft: visible ? 1 : 0,
        borderColor: 'divider',
        bgcolor: 'background.paper',
        display: 'flex',
        flexDirection: 'column',
        // 手动开合过渡动画（同左右栏先例）；拖拽中关闭（逐帧宽度变化不是动画场景）。
        transition: dragging
          ? 'none'
          : theme.transitions.create(['width'], {
              duration: theme.transitions.duration.standard,
              easing: theme.transitions.easing.sharp,
            }),
      }}
    >
      {/* 内层固定显示宽（外层 overflow hidden 裁切/动画，范式同左栏） */}
      <Box sx={{ width: displayWidth, height: '100%', display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        {/* tab 头：可滚动；每 tab 带关闭按钮（stopPropagation 防误切 tab） */}
        <Tabs
          value={displayActiveId ?? ''}
          onChange={(_, tabId: string) => {
            if (issueId != null) {
              setActiveTab(issueId, tabId);
            }
          }}
          variant="scrollable"
          scrollButtons={false}
          sx={{ minHeight: 36, flexShrink: 0, borderBottom: 1, borderColor: 'divider' }}
        >
          {validEntries.map(({ tab, def }) => (
            <Tab
              key={tab.id}
              value={tab.id}
              label={(
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.25 }}>
                  {def.title}
                  <IconButton
                    size="small"
                    aria-label={`关闭${def.title}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      if (issueId != null) {
                        closeTab(issueId, tab.id);
                      }
                    }}
                    sx={{ 'ml': 0.25, 'color': 'text.secondary', '&:hover': { bgcolor: 'action.hover' } }}
                  >
                    <CloseIcon sx={{ fontSize: 14 }} />
                  </IconButton>
                </Box>
              )}
              sx={{ minHeight: 36 }}
            />
          ))}
        </Tabs>

        {/* 内容区：激活 tab 的工具视口；空 tab 栏空态提示（面板区保持展开） */}
        {activeEntry == null
          ? (
              <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', p: 2 }}>
                <Typography variant="body2" color="text.secondary">在右侧工具条选择工具</Typography>
              </Box>
            )
          : issue != null && projectId != null
            ? (
                <Box sx={{ flex: 1, minHeight: 0 }}>
                  <Box sx={{ height: '100%' }}>{activeEntry.def.render({ issue, projectId })}</Box>
                </Box>
              )
            : null}
      </Box>

      {/* 左缘拖拽把手：4px 命中区 + hover/拖拽高亮（样式参照 PaneDivider）；仅展开态可拖 */}
      {visible && (
        <Box
          onPointerDown={onHandlePointerDown}
          onPointerMove={onHandlePointerMove}
          onPointerUp={onHandlePointerEnd}
          onPointerCancel={onHandlePointerEnd}
          aria-hidden
          sx={{
            'position': 'absolute',
            'left': 0,
            'top': 0,
            'bottom': 0,
            'width': 4,
            'cursor': 'col-resize',
            'bgcolor': dragging ? 'primary.main' : 'transparent',
            'transition': dragging ? 'none' : 'background-color 0.15s',
            '&:hover': { bgcolor: 'action.selected' },
          }}
        />
      )}
    </Box>
  );
}
