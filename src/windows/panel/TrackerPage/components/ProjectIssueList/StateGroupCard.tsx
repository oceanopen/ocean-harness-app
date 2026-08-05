import type { ReactNode } from 'react';
import { AddOutlined as AddOutlinedIcon } from '@mui/icons-material';
import { Box, Chip, IconButton, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { GUTTER_WIDTH, truncateSx } from './shared';

interface StateGroupCardProps {
  // 色点颜色（复用 state.color；列表分组取该组首个状态色，看板取列状态色）。缺省用 text.disabled。
  color?: string;
  name: string;
  count: number;
  onAdd: () => void;
  // 首行最左侧的展开/收起 icon（仅列表分组头传入）：置于 GUTTER_WIDTH 宽的 gutter 内，
  // 与下方 IssueCard 的展开列等宽对齐，从而展开 icon、状态色点与 issue 卡片逐列对齐。
  leading?: ReactNode;
}

// 状态组卡片首行（列表分组头 / 看板列头 共用）：[leading?][色点][名称…][计数][新增 icon]。
// leading 提供时（列表）渲染在 GUTTER_WIDTH gutter 内；行 gap 与 IssueCard 一致(1)以保证色点对齐。
// 仅渲染首行内容，外壳（列表 Paper 卡片 + Collapse；看板 列 Paper + Droppable + 滚动）由调用方包裹。
// 新增 icon 不挂 Tooltip，用 aria-label。
function StateGroupCard({ color, name, count, onAdd, leading }: StateGroupCardProps) {
  const { t } = useTranslation();
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
      {leading && (
        <Box sx={{ width: GUTTER_WIDTH, flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          {leading}
        </Box>
      )}
      <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: color || 'text.disabled', flexShrink: 0 }} />
      <Typography variant="subtitle2" sx={{ flex: 1, minWidth: 0, fontWeight: 600, ...truncateSx }}>{name}</Typography>
      <Chip label={count} size="small" sx={{ height: 18, fontSize: '0.7rem' }} />
      <IconButton
        size="small"
        aria-label={t('tracker:projectIssue.actions.add')}
        onClick={(e) => {
          e.stopPropagation();
          onAdd();
        }}
      >
        <AddOutlinedIcon fontSize="small" />
      </IconButton>
    </Box>
  );
}

export default StateGroupCard;
