import type { ProjectStateView } from '@src/state/tracker';
import { Box, Typography } from '@mui/material';

// StateBadge：issue 子状态徽章（色点 + state name）。左栏任务行与右栏顶栏共用，保证视觉一致。
// view 为 undefined（stateId 不在 viewMap，如状态被删除）时返回 null。
export default function StateBadge({ view }: { view: ProjectStateView | undefined }) {
  if (!view) {
    return null;
  }
  return (
    <Box sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, flexShrink: 0 }}>
      <Box sx={{ width: 7, height: 7, borderRadius: '50%', bgcolor: view.color || 'text.disabled' }} />
      <Typography variant="caption" sx={{ color: 'text.secondary' }}>{view.name}</Typography>
    </Box>
  );
}
