import type { DraggableProvided, DraggableStateSnapshot } from '@hello-pangea/dnd';
import type { SxProps, Theme } from '@mui/material';
import type { BrowserSessionView, ProjectIssueResponseData } from '@src/services';
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
import { Box, Chip, IconButton, Tooltip, Typography } from '@mui/material';
import { PRIORITY_COLOR } from '@src/components/priorityMeta';
import { formatDate } from '@src/shared/time';
import { isSessionLive } from '@src/state/browserSession';
import { STATE_MAP } from '@src/state/tracker';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate as useRouterNavigate } from 'react-router-dom';
import ChromeIcon from './ChromeIcon';
import { GUTTER_WIDTH, truncateSx } from './shared';

/**
 * 子卡片容器缩进：gutter(28px) + 首行 gap(8px) - 子卡 px(8px) = 28px，使子卡内容起点恰为 gutter+gap，
 * 从而子 issue 优先级色点落在父 issue 优先级色点正下方。
 */
const CHILD_INDENT_PL = '28px';

/**
 * 子任务块统一样式（列表/看板平级共用，模块级常量避免每次渲染重建对象 + MUI sx 重复序列化）：
 * 上下间距与卡片 mb 同值（'12px'）保持固定：mt '0px'（父→子由父卡 mb 提供）、mb 与块内 gap 均 '12px'。
 * ml 平级补偿(border 1px + p 8px = 9px)使子卡左缘对齐父卡内容区；mr '0px' 使子卡右缘与父卡右边框对齐；pl 为子任务层级缩进。
 */
const childrenBlockSx: SxProps<Theme> = {
  display: 'flex',
  flexDirection: 'column',
  mt: '0px',
  mb: '12px',
  ml: '9px',
  mr: '0px',
  pl: CHILD_INDENT_PL,
  gap: '12px',
};
// 看板父卡拖拽时用 CSS 隐藏子任务块（display:none，不卸载 DOM），松手恢复——避免松手瞬间重建子任务子树导致卡顿。
const childrenBlockSxHidden: SxProps<Theme> = { ...childrenBlockSx, display: 'none' };

// 看板拖拽透传（仅看板顶级卡片由外层 Draggable 注入；列表不传）。
export interface IssueCardDnd {
  provided?: DraggableProvided;
  snapshot?: DraggableStateSnapshot;
}

/**
 * 卡片消费场景（组件内部按场景控制样式与展示，不逐一枚举 props）：
 * - tracker（默认）：项目事项管理列表/看板——拖拽标识 + 右侧操作列 + IN_PROGRESS 徽章可跳转工作台。
 * - devWorkbench：开发工作台左树/子任务面板——窄栏裁剪（隐藏拖拽标识）、无操作列，点击/选中由回调驱动（不传即纯展示）。
 */
export type IssueCardScene = 'tracker' | 'devWorkbench';

export interface IssueCardProps {
  issue: ProjectIssueResponseData;
  depth?: number; // 0=顶级（可展开/可新增子），1=子任务（叶节点）
  viewScene?: IssueCardScene;
  // 各父 issue 的子任务统计（done/total）；开发工作台左树按项目派生传入，右栏子任务面板可省略。
  subtaskStats?: SubtaskStats;
  // 子 issue（已按 sortOrder 排序）；展开时内联渲染。子卡片不传（叶节点）。
  childIssues?: ProjectIssueResponseData[];
  expanded?: boolean;
  onToggleExpand?: (id: string) => void;
  /**
   * 点击卡片主体：tracker 场景默认 onEdit 打开编辑抽屉；devWorkbench 场景左树传 onCardClick
   * 选中、子任务面板传 onEdit 打开编辑抽屉（徽章跳转恒 inert，见 canJumpToDev）；都不传则纯展示不可点。
   */
  onCardClick?: (issue: ProjectIssueResponseData) => void;
  onEdit?: (issue: ProjectIssueResponseData) => void;
  onAddChild?: (parent: ProjectIssueResponseData) => void;
  // 选中态高亮边框（depth=0 Paper 卡；depth=1 轻量行不消费）。
  selected?: boolean;
  dnd?: IssueCardDnd;
  // 看板模式标记：顶级卡片由 KanbanColumn 传入、内联子卡片由父级透传。
  kanban?: boolean;
  // 子任务拖拽重排回调（仅列表模式启用）。
  onReorderChild?: (parentId: string, from: number, to: number) => void;
  // 该 issue 关联的浏览器会话投影行（任务派生 profile 命中行；不传即无徽标，左树由 DevTaskTree 派生传入）。
  browserSession?: BrowserSessionView;
}

/**
 * 统一 Issue 卡片（共享组件）：项目事项列表/看板 + 开发工作台左树/子任务面板共用同一组件、同一外观。
 * 场景差异（id/拖拽标识/操作列/徽章跳转等）由 viewScene 在组件内部分支控制，调用方不逐项传样式开关；
 * 列表/看板差异由调用方决定（看板外层包 Draggable 支持拖拽）；点击卡片主体 onCardClick 优先、否则 onEdit，icon/button 用 aria-label。
 */
function IssueCard({
  issue,
  depth = 0,
  viewScene = 'tracker',
  subtaskStats,
  childIssues = [],
  expanded = false,
  onToggleExpand,
  onCardClick,
  onEdit,
  onAddChild,
  selected = false,
  dnd,
  kanban,
  onReorderChild,
  browserSession,
}: IssueCardProps) {
  const { t } = useTranslation();
  const routerNavigate = useRouterNavigate();
  const provided = dnd?.provided;
  const isDragging = dnd?.snapshot?.isDragging ?? false;
  /**
   * 看板模式（单一标记 kanban prop：顶级卡片由 KanbanColumn 传入、内联子卡片由父级透传；列表不传）。
   * 仅影响拖拽行为与拖拽标识颜色（看板顶级可拖、列表子任务可拖），三行布局双模式一致。
   */
  const isKanban = kanban ?? false;
  // devWorkbench 场景展示裁剪。
  const isWorkbench = viewScene === 'devWorkbench';

  const hasChildren = depth === 0 && childIssues.length > 0;
  // 父卡展开其子任务（子任务作为兄弟 DOM 平级渲染，列表/看板共用）。
  const showChildren = hasChildren && expanded;
  /**
   * 看板父卡拖拽时用 CSS 隐藏子任务块（display:none，不卸载 DOM），松手恢复——平级后子任务无法跟随父卡移动，
   * 拖拽中暂隐；用 CSS 隐藏而非卸载，避免松手瞬间重建子任务子树导致卡顿。
   */
  const hideChildrenWhileDragging = isKanban && isDragging;
  const stat = subtaskStats?.get(issue.id);
  // 卡片可点（工作台纯展示场景不传任何点击回调）。
  const clickable = !!onCardClick || !!onEdit;

  // 打开时刻冻结的"现在"，用于逾期判断（new Date(str) 解析为纯函数）。
  const [now] = useState(() => Date.now());
  const state = STATE_MAP.get(issue.stateCode);
  const overdue = !!issue.targetDate
    && new Date(issue.targetDate).getTime() < now
    && issue.stateCode !== 'DONE'
    && issue.stateCode !== 'CANCELLED';

  /**
   * depth=1 子卡片用轻量缩进行（与父卡片视觉区分）；depth=0 用 Paper 卡片（列表/看板/工作台左树一致）。
   * 根为左右布局：[卡片内容(flex:1)] [新增子/编辑 按钮列]，按钮列垂直居中于卡片右侧。
   * hover 反馈所有卡片统一一套：背景加深一档（不按 clickable/场景门控——纯展示卡 hover 也要有反应）。
   */
  const rootSx: SxProps<Theme> = depth === 1
    ? [
        /**
         * depth=1 子卡：统一边框 + 圆角；tracker 场景灰底轻量行（嵌套于父卡下），
         * devWorkbench 场景子任务面板为独立列表，用正常背景色。
         */
        {
          'display': 'flex',
          'flexDirection': 'row',
          'gap': '4px',
          'px': '8px',
          'py': '8px',
          'borderRadius': '3px',
          'bgcolor': 'action.hover',
          'border': '1px solid',
          'borderColor': 'divider',
          'cursor': clickable ? 'pointer' : 'default',
          'position': 'relative',
          '&:hover': { bgcolor: 'action.selected' },
        },
        { '&:hover .child-drag-indicator': { opacity: 1 } },
      ]
    : {
        'display': 'flex',
        'flexDirection': 'row',
        'gap': '4px',
        'p': '8px',
        // 卡片间固定间距 12px（父→父、父→子均由此 mb 提供，子任务块 mt 不再叠加）。
        'mb': '12px',
        'borderRadius': '4px',
        'bgcolor': 'background.paper',
        'border': '1px solid',
        'borderColor': selected ? 'primary.main' : 'divider',
        'boxShadow': isDragging ? 3 : 0,
        'opacity': isDragging ? 0.95 : 1,
        'cursor': clickable ? 'pointer' : 'default',
        // 公共 hover 反馈：背景加深一档 + 抬升阴影（拖拽中 boxShadow 3 优先级更高，不受影响）。
        '&:hover': { bgcolor: 'action.hover', boxShadow: 2 },
      };

  // —— 共享原子 ——
  /**
   * 展开图标列（depth=0 且 tracker 场景才有；位于第三行左侧，无子 issue 时空占位保持左侧对齐）。
   * devWorkbench 场景不展开子任务，不渲染占位——三行内容全部顶格左对齐。
   */
  const gutter = depth === 0 && !isWorkbench && (
    <Box sx={{ width: GUTTER_WIDTH, flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      {hasChildren && (
        <IconButton
          size="small"
          aria-label={expanded ? t('tracker:projectIssue.card.collapse') : t('tracker:projectIssue.card.expand')}
          onClick={(e) => {
            e.stopPropagation();
            onToggleExpand?.(issue.id);
          }}
          sx={{ p: '2px' }}
        >
          {expanded
            ? <KeyboardArrowDownRoundedIcon fontSize="small" />
            : <KeyboardArrowRightRoundedIcon fontSize="small" />}
        </IconButton>
      )}
    </Box>
  );
  // 行内占位（第二行徽章与首行拖拽标识/第三行展开图标的 28px 左列对齐）；devWorkbench 场景不渲染（三行顶格左对齐）。
  const gutterPlaceholder = depth === 0 && !isWorkbench && <Box sx={{ width: GUTTER_WIDTH, flexShrink: 0 }} />;

  /**
   * 首行标题左侧拖拽标识（depth=0 顶级卡才有；28px 列内）。
   * 纯视觉标识：看板正常色提示可拖（整卡即柄），列表禁用色提示不可拖。devWorkbench 场景不渲染。
   */
  const dragIndicatorEl = depth === 0 && !isWorkbench && (
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

  /**
   * 子任务卡（depth=1）左侧缩进空白处的拖拽标识：hover 子卡时显现（列表子任务可拖，正常色提示）。
   * 绝对定位到子卡左侧 28px 缩进区（子块 pl 留白），不改子卡首行/第二行布局。
   * 仅在有 dnd 注入时渲染：看板子卡/工作台子任务面板均无拖拽，不出现无意义的拖拽标识。
   */
  const childDragIndicatorEl = depth === 1 && dnd && (
    <Box
      className="child-drag-indicator"
      aria-hidden
      sx={{
        position: 'absolute',
        left: -GUTTER_WIDTH,
        top: '0px',
        bottom: '0px',
        width: GUTTER_WIDTH,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        color: 'text.secondary',
        opacity: 0,
        pointerEvents: 'none',
        transition: 'opacity 0.15s ease',
      }}
    >
      <DragIndicatorOutlinedIcon fontSize="small" />
    </Box>
  );

  const priorityBadge = (
    <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: '4px', flexShrink: 0 }}>
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>「</Typography>
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>
        {`${t('tracker:projectIssue.detail.priority')}:`}
      </Typography>
      <Box sx={{ width: '8px', height: '8px', borderRadius: '50%', bgcolor: PRIORITY_COLOR[issue.priority], flexShrink: 0 }} />
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, ...truncateSx }}>
        {t(`tracker:projectIssue.priority.${issue.priority}`)}
      </Typography>
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>」</Typography>
    </Box>
  );
  /**
   * F2：进行中（IN_PROGRESS）状态徽章可点击，跳转开发工作台定位该 issue。
   * 仅 tracker 场景开放：devWorkbench 场景卡片本身可点开编辑抽屉（onEdit），徽章若再
   * 激活跳转会把工作台选中切到该卡片 issue（触发其工作空间初始化链路），属误触——恒 inert。
   */
  const canJumpToDev = clickable && !isWorkbench && issue.stateCode === 'IN_PROGRESS';
  const stateBadge = state && (
    <Box
      sx={[
        { display: 'inline-flex', alignItems: 'center', gap: '4px', flexShrink: 0, cursor: canJumpToDev ? 'pointer' : 'inherit' },
        ...(canJumpToDev ? [{ '&:hover': { opacity: 0.7 } }] : []),
      ]}
      onClick={canJumpToDev
        ? (e: MouseEvent) => {
            e.stopPropagation();
            routerNavigate(`/devWorkbench?pid=${issue.projectId}&iid=${issue.id}`);
          }
        : undefined}
    >
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>「</Typography>
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>
        {`${t('tracker:projectIssue.detail.state')}:`}
      </Typography>
      <Box sx={{ width: '8px', height: '8px', borderRadius: '50%', bgcolor: state.color || 'text.disabled', flexShrink: 0 }} />
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, ...truncateSx }}>
        {state.name}
      </Typography>
      <Typography variant="caption" sx={{ color: 'text.secondary', lineHeight: 1, flexShrink: 0 }}>」</Typography>
    </Box>
  );
  const nameEl = <Typography variant="body2" sx={{ flex: 1, minWidth: '0px', ...truncateSx }}>{issue.name}</Typography>;

  const progressEl = stat && stat.total > 0 && (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: '2px', flexShrink: 0, color: 'text.secondary' }}>
      <AssignmentOutlinedIcon sx={{ fontSize: '14px' }} />
      <Typography variant="caption" color="inherit" sx={{ lineHeight: 1 }}>{stat.done}/{stat.total}</Typography>
    </Box>
  );
  const addBtn = depth === 0 && onAddChild && (
    <IconButton
      size="small"
      aria-label={t('tracker:projectIssue.card.addSub')}
      onClick={(e) => {
        e.stopPropagation();
        onAddChild(issue);
      }}
      sx={{ p: '2px' }}
    >
      <AddOutlinedIcon fontSize="small" />
    </IconButton>
  );
  const editBtn = onEdit && (
    <IconButton
      size="small"
      aria-label={t('tracker:projectIssue.card.edit')}
      onClick={(e) => {
        e.stopPropagation();
        onEdit(issue);
      }}
      sx={{ p: '2px' }}
    >
      <EditOutlinedIcon fontSize="small" />
    </IconButton>
  );
  const dateEl = issue.targetDate && (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: '4px', flexShrink: 0, color: overdue ? 'error.main' : 'text.disabled' }}>
      <CalendarMonthOutlinedIcon sx={{ fontSize: '14px' }} />
      <Typography variant="caption" color="inherit" sx={{ lineHeight: 1 }}>{formatDate(issue.targetDate, 'YYYY-MM-DD')}</Typography>
    </Box>
  );
  // 类型名文字徽章（单值，列表/看板一致），按类型色着色，不做 flex 拉伸，与日期/进度统一左对齐。
  const typeBadge = issue.type && (
    <Chip
      label={issue.type.name}
      size="small"
      variant="outlined"
      sx={{ height: '16px', borderColor: issue.type.color || undefined, color: issue.type.color || undefined, flexShrink: 0 }}
    />
  );

  // 浏览器会话徽标（第三行行尾）：有关联会话即亮，存在未释放会话（starting/ready）转圈。
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
   * 卡片主体（首行/第二行/第三行），列表/看板共用同一布局；占根布局左侧（flex:1），
   * 新增子/编辑按钮在根布局右侧独立成列、垂直居中（见 render 处）。
   */
  const cardBodyEl = (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: '4px', flex: 1, minWidth: '0px' }}>
      {/* 三行各自固定高度（24/24/24），内容垂直居中——任何字段组合（有无子任务展开钮/日期/进度/类型）
          卡片高度都完全一致，不再受行内元素实际高度影响 */}
      {/* 首行：拖拽标识 标题（所有场景均不展示 id 尾 8 位，标题占满剩余空间） */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', height: '24px' }}>
        {dragIndicatorEl}
        {nameEl}
      </Box>
      {/* 第二行：占位 优先级 状态（占位与首行拖拽标识列对齐） */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', height: '24px' }}>
        {gutterPlaceholder}
        {priorityBadge}
        {stateBadge}
      </Box>
      {/* 第三行：展开/占位 类型徽章 目标日期 子任务进度 浏览器徽标（行尾），左对齐+行尾徽标。
          渲染条件含 hasChildren/browserSession：展开图标在本行，无类型/日期/进度的父卡片也要渲染以保留展开入口 */}
      {(!!issue.type || !!issue.targetDate || (stat?.total ?? 0) > 0 || hasChildren || !!browserSession) && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', height: '24px' }}>
          {gutter}
          {typeBadge}
          {dateEl}
          {progressEl}
          {browserBadge}
        </Box>
      )}
    </Box>
  );

  /**
   * 点击卡片主体：onCardClick 优先（如开发工作台选中 issue），否则默认 onEdit 打开编辑抽屉。
   * 两者皆无时不挂 onClick（纯展示，如工作台子任务面板）；子任务列表显隐仅由首行左侧展开图标控制。
   */
  const handleCardClick = (e: MouseEvent<HTMLDivElement>) => {
    e.stopPropagation();
    if (onCardClick) {
      onCardClick(issue);
    } else {
      onEdit?.(issue);
    }
  };

  /**
   * 子任务块（平级兄弟 DOM）：看板纯 Box 渲染不可拖、列表 DragDropContext 可拖（同父内排序）。
   * 看板父卡拖拽时用 CSS 隐藏（childrenBlockSxHidden），不卸载 DOM、松手恢复。
   */
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
                <Draggable key={child.id} draggableId={child.id} index={idx}>
                  {(dragProvided, dragSnapshot) => (
                    <IssueCard
                      issue={child}
                      depth={1}
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
        onClick={clickable ? handleCardClick : undefined}
        sx={rootSx}
      >
        {childDragIndicatorEl}
        {cardBodyEl}
        {/* 右侧操作列：新增子/编辑垂直堆叠，相对卡片整体上下居中；devWorkbench 场景不渲染操作列 */}
        {!isWorkbench && (addBtn || editBtn) && (
          <Box sx={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', gap: '2px', flexShrink: 0 }}>
            {addBtn}
            {editBtn}
          </Box>
        )}
      </Box>
      {renderChildrenBlock()}
    </>
  );
}

export default IssueCard;
