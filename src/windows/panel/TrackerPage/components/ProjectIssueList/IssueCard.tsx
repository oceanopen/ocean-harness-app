import type { DraggableProvided, DraggableStateSnapshot } from '@hello-pangea/dnd';
import type { SxProps, Theme } from '@mui/material';
import type { ProjectIssueResponseData } from '@src/services';
import type { ProjectStateView } from '@src/state/tracker';
import type { MouseEvent, ReactNode } from 'react';
import type { SubtaskStats } from './shared';
import { DragDropContext, Draggable, Droppable } from '@hello-pangea/dnd';
import {
  AddOutlined as AddOutlinedIcon,
  AssignmentOutlined as AssignmentOutlinedIcon,
  CalendarMonthOutlined as CalendarMonthOutlinedIcon,
  DragIndicatorOutlined as DragIndicatorOutlinedIcon,
  EditOutlined as EditOutlinedIcon,
  KeyboardArrowDownRounded as KeyboardArrowDownRoundedIcon,
  KeyboardArrowRightRounded as KeyboardArrowRightRoundedIcon,
} from '@mui/icons-material';
import { Box, IconButton, Typography } from '@mui/material';
import { formatDate } from '@src/shared/time';
import { useDevWorkbenchStore } from '@src/state/devWorkbench';
import { useCommandPalette } from '@src/windows/panel/commandPalette/CommandPaletteContext';
import { PRIORITY_COLOR } from '@src/windows/panel/TrackerPage/components/priorityMeta';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { GUTTER_WIDTH, truncateSx } from './shared';

// 子卡片容器缩进：gutter(28) + 首行 gap(8) - 子卡 px(8) = 28，使子卡内容起点恰为 gutter+gap，
// 从而子 issue 优先级色点落在父 issue 优先级色点正下方。
const CHILD_INDENT_PL = 3.5;

// 子任务块统一样式（列表/看板平级共用，模块级常量避免每次渲染重建对象 + MUI sx 重复序列化）：
// mx 平级补偿(border 1px + p:1 8px = 9px)使子卡左右边界与色点对齐父卡内容区；pl 为子任务层级缩进。
const childrenBlockSx: SxProps<Theme> = {
  display: 'flex',
  flexDirection: 'column',
  mt: 0.5,
  mb: 0.75,
  mx: '9px',
  pl: CHILD_INDENT_PL,
  gap: 0.5,
};
// 看板父卡拖拽时用 CSS 隐藏子任务块（display:none，不卸载 DOM），松手恢复——避免松手瞬间重建子任务子树导致卡顿。
const childrenBlockSxHidden: SxProps<Theme> = { ...childrenBlockSx, display: 'none' };

// 看板拖拽透传（仅看板顶级卡片由外层 Draggable 注入；列表不传）。
export interface IssueCardDnd {
  provided?: DraggableProvided;
  snapshot?: DraggableStateSnapshot;
}

export interface IssueCardProps {
  issue: ProjectIssueResponseData;
  depth?: number; // 0=顶级（可展开/可新增子），1=子任务（叶节点）
  stateMap: Map<number, ProjectStateView>;
  subtaskStats: SubtaskStats;
  // 子 issue（已按 sortOrder 排序）；展开时内联渲染。子卡片不传（叶节点）。
  childIssues?: ProjectIssueResponseData[];
  expanded?: boolean;
  onToggleExpand?: (id: number) => void;
  onEdit: (issue: ProjectIssueResponseData) => void;
  onAddChild: (parent: ProjectIssueResponseData) => void;
  dnd?: IssueCardDnd;
  // 看板模式标记：顶级卡片由 KanbanColumn 传入、内联子卡片由父级透传。
  kanban?: boolean;
  // 子任务拖拽重排回调（仅列表模式启用）。
  onReorderChild?: (parentId: number, from: number, to: number) => void;
}

// 统一 Issue 卡片：列表与看板共用同一组件、同一外观（看板式三行 Paper 卡片）。
// 列表/看板唯一差异由调用方决定：看板在外层包 Draggable（经 dnd 透传）支持拖拽，列表不包、纵向排列、外加分组显示/隐藏。
// 三行布局：首行 [展开/占位] #id [优先级] [状态] … [进度][新增][编辑]；看板模式下新增+编辑下移到第二行标题右侧。
// 第二行 [占位] 标题；第三行 [占位] 标签颜色横杠 … 结束日期。
// 点击卡片主体：有子级（父级）→ 切换展开；无子级 → 打开编辑抽屉（onEdit）。
// 所有 icon/button 不挂 Tooltip（避免遮挡鼠标），改用 aria-label。
function IssueCard({
  issue,
  depth = 0,
  stateMap,
  subtaskStats,
  childIssues = [],
  expanded = false,
  onToggleExpand,
  onEdit,
  onAddChild,
  dnd,
  kanban,
  onReorderChild,
}: IssueCardProps) {
  const { t } = useTranslation();
  const { navigate } = useCommandPalette();
  const selectIssue = useDevWorkbenchStore(s => s.selectIssue);
  const provided = dnd?.provided;
  const isDragging = dnd?.snapshot?.isDragging ?? false;
  // 看板模式（单一标记 kanban prop：顶级卡片由 KanbanColumn 传入、内联子卡片由父级透传；列表不传）。
  // 看板下增加/编辑 icon 下移到第二行标题右侧，缓解首行拥挤；列表模式不变。
  const isKanban = kanban ?? false;

  const hasChildren = depth === 0 && childIssues.length > 0;
  // 父卡展开其子任务（子任务作为兄弟 DOM 平级渲染，列表/看板共用）。
  const showChildren = hasChildren && expanded;
  // 看板父卡拖拽时用 CSS 隐藏子任务块（display:none，不卸载 DOM），松手恢复——平级后子任务无法跟随父卡移动，
  // 拖拽中暂隐；用 CSS 隐藏而非卸载，避免松手瞬间重建子任务子树导致卡顿。
  const hideChildrenWhileDragging = isKanban && isDragging;
  // 子任务块实际可见（占空间）：用于父卡底部间距切换（可见时让出 mb 由子块接管；拖拽隐藏时不占空间，按无子任务处理）。
  const childrenVisible = showChildren && !hideChildrenWhileDragging;
  const stat = subtaskStats.get(issue.id);

  // 打开时刻冻结的"现在"，用于逾期判断（new Date(str) 解析为纯函数）。
  const [now] = useState(() => Date.now());
  const state = stateMap.get(issue.stateId);
  const overdue = !!issue.targetDate
    && new Date(issue.targetDate).getTime() < now
    && state?.stateGroupCode !== 'completed'
    && state?.stateGroupCode !== 'cancelled';

  // depth=1 子卡片用轻量缩进行（与父卡片视觉区分）；depth=0 用 Paper 卡片（列表/看板一致）。
  const rootSx: SxProps<Theme> = depth === 1
    ? [
        { display: 'flex', flexDirection: 'column', px: 1, py: 0.5, my: 0.25, borderRadius: 0.75, bgcolor: 'action.hover', cursor: 'pointer', position: 'relative' },
        { '&:hover': { bgcolor: 'action.selected' } },
        { '&:hover .child-drag-indicator': { opacity: 1 } },
        { '&:hover .child-drag-indicator.child-drag-indicator-disabled': { opacity: 0.4 } },
      ]
    : {
        display: 'flex',
        flexDirection: 'column',
        p: 1,
        // 子任务块可见（占空间）时让出底部间距，由兄弟子任务块的 mt/mb 接管父→子、子→下一父间距。
        // 拖拽隐藏（display:none 不占空间）时按无子任务处理，保持父卡与下一卡间距。
        mb: childrenVisible ? 0 : 0.75,
        borderRadius: 1,
        bgcolor: 'background.paper',
        border: 1,
        borderColor: 'divider',
        boxShadow: isDragging ? 3 : 0,
        opacity: isDragging ? 0.95 : 1,
        cursor: 'pointer',
      };

  // —— 共享原子 ——
  // 展开图标列（depth=0 才有；无子 issue 时空占位保持左侧对齐）。
  const gutter = depth === 0 && (
    <Box sx={{ width: GUTTER_WIDTH, flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      {hasChildren && (
        <IconButton
          size="small"
          aria-label={expanded ? t('tracker:projectIssue.card.collapse') : t('tracker:projectIssue.card.expand')}
          onClick={(e) => {
            e.stopPropagation();
            onToggleExpand?.(issue.id);
          }}
          sx={{ p: 0.25 }}
        >
          {expanded
            ? <KeyboardArrowDownRoundedIcon fontSize="small" />
            : <KeyboardArrowRightRoundedIcon fontSize="small" />}
        </IconButton>
      )}
    </Box>
  );
  // 行内占位（让第二、三行内容与首行优先级色点左对齐）。
  const gutterPlaceholder = depth === 0 && <Box sx={{ width: GUTTER_WIDTH, flexShrink: 0 }} />;

  // 第二行标题左侧拖拽标识（depth=0 顶级卡才有；位于展开/折叠 icon 正下方的 28px 列内）。
  // 纯视觉标识：看板正常色提示可拖（整卡即柄），列表禁用色提示不可拖。不改 dragHandleProps 挂载方式。
  const dragIndicatorEl = depth === 0 && (
    <Box
      sx={{
        width: GUTTER_WIDTH,
        flexShrink: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        color: isKanban ? 'text.secondary' : 'text.disabled',
        opacity: isKanban ? 1 : 0.4,
        cursor: isKanban ? 'grab' : 'not-allowed',
      }}
    >
      <DragIndicatorOutlinedIcon fontSize="small" />
    </Box>
  );

  // 子任务卡（depth=1）左侧缩进空白处的拖拽标识：hover 子卡时显现，颜色反映可拖性
  // （列表可拖→正常色，看板不可拖→禁用色）。绝对定位到子卡左侧 28px 缩进区（子块 pl 留白），不改子卡首行/第二行布局。
  const childDragIndicatorEl = depth === 1 && (
    <Box
      className={isKanban ? 'child-drag-indicator child-drag-indicator-disabled' : 'child-drag-indicator'}
      aria-hidden
      sx={{
        position: 'absolute',
        left: -GUTTER_WIDTH,
        top: 0,
        bottom: 0,
        width: GUTTER_WIDTH,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        color: isKanban ? 'text.disabled' : 'text.secondary',
        opacity: 0,
        pointerEvents: 'none',
        transition: 'opacity 0.15s ease',
      }}
    >
      <DragIndicatorOutlinedIcon fontSize="small" />
    </Box>
  );

  const priorityBadge = (
    <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, flexShrink: 0 }}>
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>「</Typography>
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>
        {`${t('tracker:projectIssue.detail.priority')}:`}
      </Typography>
      <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: PRIORITY_COLOR[issue.priority], flexShrink: 0 }} />
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, ...truncateSx }}>
        {t(`tracker:projectIssue.priority.${issue.priority}`)}
      </Typography>
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>」</Typography>
    </Box>
  );
  // F2：started 组非 in_progress 的子状态徽章可点击，跳转开发工作台定位该 issue。
  const canJumpToDev = !!state && state.stateGroupCode === 'started';
  const stateBadge = state && (
    <Box
      sx={[
        { display: 'inline-flex', alignItems: 'center', gap: 0.5, flexShrink: 0, cursor: canJumpToDev ? 'pointer' : 'inherit' },
        ...(canJumpToDev ? [{ '&:hover': { opacity: 0.7 } }] : []),
      ]}
      onClick={canJumpToDev
        ? (e: MouseEvent) => {
            e.stopPropagation();
            selectIssue(issue);
            navigate('devWorkbench');
          }
        : undefined}
    >
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>「</Typography>
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>
        {`${t('tracker:projectIssue.detail.state')}:`}
      </Typography>
      <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: state.color || 'text.disabled', flexShrink: 0 }} />
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, ...truncateSx }}>
        {state.name}
      </Typography>
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>」</Typography>
    </Box>
  );
  const idText = <Typography variant="caption" color="text.disabled" sx={{ flexShrink: 0 }}>#{issue.id}</Typography>;
  const nameEl = <Typography variant="body2" sx={{ flex: 1, minWidth: 0, ...truncateSx }}>{issue.name}</Typography>;

  const progressEl = stat && stat.total > 0 && (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.25, flexShrink: 0, color: 'text.secondary' }}>
      <AssignmentOutlinedIcon sx={{ fontSize: '0.9rem' }} />
      <Typography variant="caption" color="inherit">{stat.done}/{stat.total}</Typography>
    </Box>
  );
  const addBtn = depth === 0 && (
    <IconButton
      size="small"
      aria-label={t('tracker:projectIssue.card.addSub')}
      onClick={(e) => {
        e.stopPropagation();
        onAddChild(issue);
      }}
      sx={{ p: 0.25 }}
    >
      <AddOutlinedIcon fontSize="small" />
    </IconButton>
  );
  const editBtn = (
    <IconButton
      size="small"
      aria-label={t('tracker:projectIssue.card.edit')}
      onClick={(e) => {
        e.stopPropagation();
        onEdit(issue);
      }}
      sx={{ p: 0.25 }}
    >
      <EditOutlinedIcon fontSize="small" />
    </IconButton>
  );
  const rightCluster = (
    <>
      {progressEl}
      {addBtn}
      {editBtn}
    </>
  );

  const dateEl = issue.targetDate && (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexShrink: 0, color: overdue ? 'error.main' : 'text.disabled' }}>
      <CalendarMonthOutlinedIcon sx={{ fontSize: '0.9rem' }} />
      <Typography variant="caption" color="inherit">{formatDate(issue.targetDate, 'YYYY-MM-DD')}</Typography>
    </Box>
  );
  // 标签颜色横杠（列表/看板一致），全部展示可换行。
  const labelBars = issue.labels.length > 0 && (
    <Box sx={{ flex: 1, minWidth: 0, display: 'flex', alignItems: 'center', gap: 0.25, flexWrap: 'wrap' }}>
      {issue.labels.map(l => (
        <Box key={l.id} sx={{ width: 24, height: 4, borderRadius: 1, bgcolor: l.color }} />
      ))}
    </Box>
  );

  // 卡片主体（首行/第二行/第三行），看板/列表双 return 分支共用，避免重复。
  const cardBodyEl = (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
      {/* 首行：展开/占位 #id 优先级 状态 … [列表:进度+新增+编辑] / [看板:仅进度] */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        {gutter}
        {idText}
        {priorityBadge}
        {stateBadge}
        <Box sx={{ flex: 1 }} />
        {isKanban ? progressEl : rightCluster}
      </Box>
      {/* 第二行：拖拽标识 标题 … [看板:新增+编辑] */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        {dragIndicatorEl}
        {nameEl}
        {isKanban && (
          <>
            {addBtn}
            {editBtn}
          </>
        )}
      </Box>
      {/* 第三行：占位 标签颜色横杠 … 结束日期（无标签/日期时不渲染空行） */}
      {(issue.labels.length > 0 || issue.targetDate) && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          {gutterPlaceholder}
          {labelBars ?? <Box sx={{ flex: 1 }} />}
          {dateEl}
        </Box>
      )}
    </Box>
  );

  // 点击卡片主体：有子级（父级）→ 切换展开；无子级 → 打开编辑抽屉（onEdit）。
  const handleCardClick = (e: MouseEvent<HTMLDivElement>) => {
    e.stopPropagation();
    if (hasChildren) {
      onToggleExpand?.(issue.id);
    } else {
      onEdit(issue);
    }
  };

  // 子任务块（平级兄弟 DOM）：看板纯 Box 渲染不可拖、列表 DragDropContext 可拖（同父内排序）。
  // 看板父卡拖拽时用 CSS 隐藏（childrenBlockSxHidden），不卸载 DOM、松手恢复。
  const renderChildrenBlock = (): ReactNode => {
    if (!showChildren) {
      return null;
    }
    const blockSx = hideChildrenWhileDragging ? childrenBlockSxHidden : childrenBlockSx;
    if (isKanban) {
      return (
        <Box sx={blockSx}>
          {childIssues.map(child => (
            <IssueCard
              key={child.id}
              issue={child}
              depth={1}
              stateMap={stateMap}
              subtaskStats={subtaskStats}
              onEdit={onEdit}
              onAddChild={onAddChild}
              kanban={isKanban}
            />
          ))}
        </Box>
      );
    }
    return (
      <DragDropContext
        onDragEnd={(r) => {
          if (r.destination && r.source.index !== r.destination.index) {
            onReorderChild?.(issue.id, r.source.index, r.destination.index);
          }
        }}
      >
        <Droppable droppableId={`children-${issue.id}`}>
          {childProvided => (
            <Box
              ref={childProvided.innerRef}
              {...childProvided.droppableProps}
              sx={blockSx}
            >
              {childIssues.map((child, idx) => (
                <Draggable key={child.id} draggableId={String(child.id)} index={idx}>
                  {(dragProvided, dragSnapshot) => (
                    <IssueCard
                      issue={child}
                      depth={1}
                      stateMap={stateMap}
                      subtaskStats={subtaskStats}
                      onEdit={onEdit}
                      onAddChild={onAddChild}
                      kanban={isKanban}
                      dnd={{ provided: dragProvided, snapshot: dragSnapshot }}
                    />
                  )}
                </Draggable>
              ))}
              {childProvided.placeholder}
            </Box>
          )}
        </Droppable>
      </DragDropContext>
    );
  };

  // 统一平级布局：子任务块作为父卡的兄弟 DOM（不再嵌套在父卡根 Box 内），列表/看板共用一套结构。
  return (
    <>
      <Box
        {...provided?.draggableProps}
        {...provided?.dragHandleProps}
        ref={provided?.innerRef}
        onClick={handleCardClick}
        sx={rootSx}
      >
        {childDragIndicatorEl}
        {cardBodyEl}
      </Box>
      {renderChildrenBlock()}
    </>
  );
}

export default IssueCard;
