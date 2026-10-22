import type { DraggableProvided, DraggableStateSnapshot } from '@hello-pangea/dnd';
import type { SxProps, Theme } from '@mui/material';
import type { BrowserSessionView, WorkspaceIssueResponseData } from '@src/services';
import type { MouseEvent } from 'react';
import { DragIndicatorOutlined as DragIndicatorOutlinedIcon, EditOutlined as EditOutlinedIcon } from '@mui/icons-material';
import { Box, IconButton, Tooltip, Typography } from '@mui/material';
import { formatDate } from '@src/shared/time';
import { isSessionLive } from '@src/state/browserSession';
import { useTranslation } from 'react-i18next';
import ChromeIcon from './ChromeIcon';
import { GUTTER_WIDTH, truncateSx } from './shared';

// 看板拖拽透传（仅看板卡片由外层 Draggable 注入；列表不传）。
export interface IssueCardDnd {
  provided?: DraggableProvided;
  snapshot?: DraggableStateSnapshot;
}

/**
 * 卡片消费场景（组件内部按场景控制样式与展示，不逐一枚举 props）：
 * - tracker（默认）：任务管理列表/看板——拖拽标识 + 右侧操作列。
 * - devWorkbench：开发工作台左树——窄栏裁剪（隐藏拖拽标识）、无操作列，点击/选中由回调驱动（不传即纯展示）。
 */
export type IssueCardScene = 'tracker' | 'devWorkbench';

export interface IssueCardProps {
  issue: WorkspaceIssueResponseData;
  viewScene?: IssueCardScene;
  /**
   * 点击卡片主体：tracker 场景默认 onEdit 打开编辑抽屉；devWorkbench 场景左树传 onCardClick
   * 选中、编辑走标题栏入口；都不传则纯展示不可点。
   */
  onCardClick?: (issue: WorkspaceIssueResponseData) => void;
  onEdit?: (issue: WorkspaceIssueResponseData) => void;
  // 选中态高亮边框（Paper 卡）。
  selected?: boolean;
  dnd?: IssueCardDnd;
  // 看板模式标记：卡片由 KanbanColumn 传入。
  kanban?: boolean;
  // 该 issue 关联的浏览器会话投影行（任务派生 profile 命中行；不传即无徽标，左树由 DevTaskTree 派生传入）。
  browserSession?: BrowserSessionView;
}

/**
 * 统一任务卡片（共享组件）：任务管理列表/看板 + 开发工作台左树共用同一组件、同一外观。
 * 场景差异（拖拽标识/操作列等）由 viewScene 在组件内部分支控制，调用方不逐项传样式开关；
 * 列表/看板差异由调用方决定（看板外层包 Draggable 支持拖拽）；点击卡片主体 onCardClick 优先、否则 onEdit，icon/button 用 aria-label。
 */
function IssueCard({
  issue,
  viewScene = 'tracker',
  onCardClick,
  onEdit,
  selected = false,
  dnd,
  kanban,
  browserSession,
}: IssueCardProps) {
  const { t } = useTranslation();
  const provided = dnd?.provided;
  const isDragging = dnd?.snapshot?.isDragging ?? false;
  // 看板模式（单一标记 kanban prop：卡片由 KanbanColumn 传入；列表不传）。仅影响拖拽行为与拖拽标识颜色。
  const isKanban = kanban ?? false;
  // devWorkbench 场景展示裁剪。
  const isWorkbench = viewScene === 'devWorkbench';
  // 卡片可点（工作台纯展示场景不传任何点击回调）。
  const clickable = !!onCardClick || !!onEdit;

  /**
   * Paper 卡片（列表/看板/工作台左树一致）。根为左右布局：[卡片内容(flex:1)] [编辑按钮列]，
   * 按钮列垂直居中于卡片右侧；mb 提供卡片间固定间距 12px。
   * hover 反馈统一一套：背景加深一档 + 抬升阴影（拖拽中 boxShadow 3 优先级更高，不受影响）。
   */
  const rootSx: SxProps<Theme> = {
    'display': 'flex',
    'flexDirection': 'row',
    'gap': '4px',
    'p': '8px',
    'mb': '12px',
    'borderRadius': '4px',
    'bgcolor': 'background.paper',
    'border': '1px solid',
    'borderColor': selected ? 'primary.main' : 'divider',
    'boxShadow': isDragging ? 3 : 0,
    'opacity': isDragging ? 0.95 : 1,
    'cursor': clickable ? 'pointer' : 'default',
    '&:hover': { bgcolor: 'action.hover', boxShadow: 2 },
  };

  // —— 共享原子 ——
  /**
   * 首行标题左侧拖拽标识（28px 列内）。
   * 纯视觉标识：看板正常色提示可拖（整卡即柄），列表禁用色提示不可拖。devWorkbench 场景不渲染。
   */
  const dragIndicatorEl = !isWorkbench && (
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
  // 行内占位（第二行与首行拖拽标识 28px 左列对齐）；devWorkbench 场景不渲染（顶格左对齐）。
  const gutterPlaceholder = !isWorkbench && <Box sx={{ width: GUTTER_WIDTH, flexShrink: 0 }} />;

  const nameEl = <Typography variant="body2" sx={{ flex: 1, minWidth: '0px', ...truncateSx }}>{issue.name}</Typography>;

  const editBtn = onEdit && (
    <IconButton
      size="small"
      aria-label={t('tracker:workspaceIssue.card.edit')}
      onClick={(e) => {
        e.stopPropagation();
        onEdit(issue);
      }}
      sx={{ p: '2px' }}
    >
      <EditOutlinedIcon fontSize="small" />
    </IconButton>
  );

  // 浏览器会话徽标（第二行行尾）：有关联会话即亮，存在未释放会话（starting/ready）转圈。
  const browserLive = browserSession != null && isSessionLive(browserSession.status);
  const browserBadge = browserSession && (
    <Tooltip title={browserLive ? '浏览器会话运行中' : '浏览器会话已释放'}>
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          ml: 'auto',
          flexShrink: 0,
          color: browserLive ? 'text.secondary' : 'text.disabled',
          ...(browserLive
            ? {
                'animation': 'spin 0.8s linear infinite',
                '@keyframes spin': {
                  from: { transform: 'rotate(0deg)' },
                  to: { transform: 'rotate(360deg)' },
                },
              }
            : {}),
        }}
      >
        <ChromeIcon sx={{ fontSize: '14px' }} />
      </Box>
    </Tooltip>
  );

  /**
   * 卡片主体（固定两行），列表/看板共用同一布局；占根布局左侧（flex:1），
   * 编辑按钮在根布局右侧独立成列、垂直居中（见 render 处）。
   */
  const cardBodyEl = (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: '4px', flex: 1, minWidth: '0px' }}>
      {/* 两行各自固定高度（24/24），内容垂直居中——任何字段组合卡片高度都完全一致（会话启停零跳变） */}
      {/* 首行：拖拽标识 标题 */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', height: '24px' }}>
        {dragIndicatorEl}
        {nameEl}
      </Box>
      {/* 第二行：占位 创建日期（置灰） 会话徽标（行尾） */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', height: '24px' }}>
        {gutterPlaceholder}
        <Typography variant="caption" sx={{ color: 'text.disabled', lineHeight: 1 }}>
          {formatDate(issue.createdAt, 'YYYY-MM-DD')}
        </Typography>
        {browserBadge}
      </Box>
    </Box>
  );

  /**
   * 点击卡片主体：onCardClick 优先（如开发工作台选中 issue），否则默认 onEdit 打开编辑抽屉。
   * 两者皆无时不挂 onClick（纯展示）。
   */
  const handleCardClick = (e: MouseEvent<HTMLDivElement>) => {
    e.stopPropagation();
    if (onCardClick) {
      onCardClick(issue);
    } else {
      onEdit?.(issue);
    }
  };

  return (
    <Box
      {...provided?.draggableProps}
      {...provided?.dragHandleProps}
      ref={provided?.innerRef}
      onClick={clickable ? handleCardClick : undefined}
      sx={rootSx}
    >
      {cardBodyEl}
      {/* 右侧操作列：编辑钮；devWorkbench 场景不渲染操作列 */}
      {!isWorkbench && editBtn && (
        <Box sx={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', gap: '2px', flexShrink: 0 }}>
          {editBtn}
        </Box>
      )}
    </Box>
  );
}

export default IssueCard;
