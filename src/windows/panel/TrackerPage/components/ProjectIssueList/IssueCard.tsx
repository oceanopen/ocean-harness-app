import type { DraggableProvided, DraggableStateSnapshot } from '@hello-pangea/dnd';
import type { SxProps, Theme } from '@mui/material';
import type { ProjectIssueResponseData } from '@src/services';
import type { ProjectStateView } from '@src/state/tracker';
import type { MouseEvent } from 'react';
import type { SubtaskStats } from './shared';
import {
  AddOutlined as AddOutlinedIcon,
  AssignmentOutlined as AssignmentOutlinedIcon,
  CalendarMonthOutlined as CalendarMonthOutlinedIcon,
  EditOutlined as EditOutlinedIcon,
  KeyboardArrowDownRounded as KeyboardArrowDownRoundedIcon,
  KeyboardArrowRightRounded as KeyboardArrowRightRoundedIcon,
} from '@mui/icons-material';
import { Box, IconButton, Typography } from '@mui/material';
import { formatDate } from '@src/shared/time';
import { PRIORITY_COLOR } from '@src/windows/panel/TrackerPage/components/priorityMeta';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { GUTTER_WIDTH, truncateSx } from './shared';

// 子卡片容器缩进：gutter(28) + 首行 gap(8) - 子卡 px(8) = 28，使子卡内容起点恰为 gutter+gap，
// 从而子 issue 优先级色点落在父 issue 优先级色点正下方。
const CHILD_INDENT_PL = 3.5;

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
}

// 统一 Issue 卡片：列表与看板共用同一组件、同一外观（看板式三行 Paper 卡片）。
// 列表/看板唯一差异由调用方决定：看板在外层包 Draggable（经 dnd 透传）支持拖拽，列表不包、纵向排列、外加分组显示/隐藏。
// 三行布局：首行 [展开/占位][优先级色点+描述] #id … [进度][新增][编辑]（无标题）；
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
}: IssueCardProps) {
  const { t } = useTranslation();
  const provided = dnd?.provided;
  const isDragging = dnd?.snapshot?.isDragging ?? false;

  const hasChildren = depth === 0 && childIssues.length > 0;
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
        { display: 'flex', flexDirection: 'column', px: 1, py: 0.5, my: 0.25, borderRadius: 0.75, bgcolor: 'action.hover', cursor: 'pointer' },
        { '&:hover': { bgcolor: 'action.selected' } },
      ]
    : {
        display: 'flex',
        flexDirection: 'column',
        p: 1,
        mb: 0.75,
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

  const priorityBadge = (
    <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, flexShrink: 0 }}>
      <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: PRIORITY_COLOR[issue.priority], flexShrink: 0 }} />
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, ...truncateSx }}>
        {t(`tracker:projectIssue.priority.${issue.priority}`)}
      </Typography>
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

  return (
    <Box
      {...(depth === 0 ? provided?.draggableProps : undefined)}
      {...(depth === 0 ? provided?.dragHandleProps : undefined)}
      ref={depth === 0 ? provided?.innerRef : undefined}
      onClick={(e: MouseEvent<HTMLDivElement>) => {
        e.stopPropagation();
        if (hasChildren) {
          onToggleExpand?.(issue.id);
        } else {
          onEdit(issue);
        }
      }}
      sx={rootSx}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
        {/* 首行：展开/占位 优先级 #id …（无标题） 进度 新增 编辑 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          {gutter}
          {priorityBadge}
          {idText}
          <Box sx={{ flex: 1 }} />
          {rightCluster}
        </Box>
        {/* 第二行：占位 标题 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          {gutterPlaceholder}
          {nameEl}
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

      {/* 内联子卡片（仅顶级 + 展开 + 有子 issue）；缩进使子卡优先级色点对齐父级 */}
      {depth === 0 && expanded && childIssues.length > 0 && (
        <Box sx={{ display: 'flex', flexDirection: 'column', mt: 0.5, pl: CHILD_INDENT_PL }}>
          {childIssues.map(child => (
            <IssueCard
              key={child.id}
              issue={child}
              depth={1}
              stateMap={stateMap}
              subtaskStats={subtaskStats}
              onEdit={onEdit}
              onAddChild={onAddChild}
            />
          ))}
        </Box>
      )}
    </Box>
  );
}

export default IssueCard;
