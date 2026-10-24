import type { ReactNode } from 'react';
import {
  ChevronRightOutlined as ChevronRightOutlinedIcon,
  ExpandMoreOutlined as ExpandMoreOutlinedIcon,
  MoreHoriz as MoreHorizIcon,
} from '@mui/icons-material';
import { Box, IconButton, Typography } from '@mui/material';

interface TreeRowProps {
  // node=容器节点行（折叠箭头+图标槽+粗体标题）；leaf=叶子行（前置槽位+标题+右侧时间槽）。
  variant: 'node' | 'leaf';
  title: string;
  selected: boolean;
  onClick?: () => void;
  // node：折叠态与图标槽内容（emoji/占位图标由树传入）。
  collapsed?: boolean;
  icon?: ReactNode;
  onToggleCollapsed?: () => void;
  // leaf：左缘缩进 + 前置 8px 槽位内容（运行圆点/类型色点）+ 右侧时间（缺省不渲染时间）。
  inset?: string;
  leadingSlot?: ReactNode;
  timeLabel?: string;
  moreLabel: string;
  onMore: (anchor: HTMLElement) => void;
}

/**
 * 左树行共享渲染（工作空间/项目节点行与会话/任务叶子行同一套视觉规格）：
 * padding/选中高亮/hover/圆角/对齐全在此收敛，缩进经 inset 传入，菜单项由调用方承接。
 */
function TreeRow({
  variant,
  title,
  selected,
  onClick,
  collapsed = false,
  icon,
  onToggleCollapsed,
  inset = '26px',
  leadingSlot,
  timeLabel,
  moreLabel,
  onMore,
}: TreeRowProps) {
  const isNode = variant === 'node';
  return (
    <Box
      onClick={onClick}
      sx={{
        'display': 'flex',
        'alignItems': 'center',
        'gap': isNode
          ? '4px'
          : '6px',
        'mx': '4px',
        'my': '2px',
        'py': '5px',
        'borderRadius': '4px',
        'cursor': onClick != null ? 'pointer' : 'default',
        'bgcolor': selected ? 'action.selected' : 'transparent',
        '&:hover': { bgcolor: selected ? 'action.selected' : 'action.hover' },
        ...(isNode
          ? {
              'px': '4px',
              '& .tree-row-more': { opacity: 0 },
              '&:hover .tree-row-more': { opacity: 1 },
            }
          : {
              'pl': inset,
              'pr': '4px',
              // 右侧槽位：时间与「...」同槽互斥（绝对定位交换，恒右对齐无占位位移；hidden 防隐藏态误点）。
              '& .tree-row-slot': { position: 'relative', width: '64px', height: '26px', flexShrink: 0 },
              '& .tree-row-time': { position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'flex-end' },
              '& .tree-row-more': { position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'flex-end', opacity: 0, visibility: 'hidden' },
              '&:hover .tree-row-more': { opacity: 1, visibility: 'visible' },
              '&:hover .tree-row-time': { opacity: 0 },
            }),
      }}
    >
      {isNode
        ? (
            <>
              <IconButton
                size="small"
                aria-label={collapsed ? '展开' : '折叠'}
                onClick={(e) => {
                  e.stopPropagation();
                  onToggleCollapsed?.();
                }}
                sx={{ 'flexShrink': 0, '& svg': { fontSize: '16px' } }}
              >
                {collapsed ? <ChevronRightOutlinedIcon /> : <ExpandMoreOutlinedIcon />}
              </IconButton>
              <Box sx={{ width: '20px', flexShrink: 0, display: 'flex', justifyContent: 'center' }}>{icon}</Box>
            </>
          )
        : (
            <Box sx={{ width: '8px', flexShrink: 0, display: 'flex', justifyContent: 'center' }}>{leadingSlot}</Box>
          )}
      <Typography
        variant="body2"
        noWrap
        sx={{
          flex: 1,
          minWidth: '0px',
          fontWeight: isNode
            ? 600
            : undefined,
        }}
        title={title}
      >
        {title}
      </Typography>
      {isNode
        ? (
            <IconButton
              className="tree-row-more"
              size="small"
              aria-label={moreLabel}
              onClick={(e) => {
                e.stopPropagation();
                onMore(e.currentTarget);
              }}
              sx={{ 'flexShrink': 0, '& svg': { fontSize: '16px' } }}
            >
              <MoreHorizIcon />
            </IconButton>
          )
        : (
            <Box className="tree-row-slot">
              {timeLabel != null && (
                <Typography variant="caption" color="text.disabled" noWrap className="tree-row-time">
                  {timeLabel}
                </Typography>
              )}
              <IconButton
                className="tree-row-more"
                size="small"
                aria-label={moreLabel}
                onClick={(e) => {
                  e.stopPropagation();
                  onMore(e.currentTarget);
                }}
                sx={{ 'position': 'absolute', '& svg': { fontSize: '16px' } }}
              >
                <MoreHorizIcon />
              </IconButton>
            </Box>
          )}
    </Box>
  );
}

export default TreeRow;
