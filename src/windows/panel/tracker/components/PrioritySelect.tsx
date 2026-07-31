import type { Priority } from '../IssueListPage';
import { Box, MenuItem, Select, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';

// 受控优先级下拉：5 档（色点 + 本地化文案）。仅改本地值，不发请求。
const PRIORITY_OPTIONS: Priority[] = ['urgent', 'high', 'medium', 'low', 'none'];

const PRIORITY_COLOR: Record<Priority, string> = {
  urgent: 'error.main',
  high: 'warning.main',
  medium: 'info.main',
  low: 'text.secondary',
  none: 'text.disabled',
};

interface PrioritySelectProps {
  value: Priority;
  onChange: (p: Priority) => void;
  disabled?: boolean;
}

function PrioritySelect({ value, onChange, disabled }: PrioritySelectProps) {
  const { t } = useTranslation();
  const renderPriority = (p: Priority) => (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
      <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: PRIORITY_COLOR[p], flexShrink: 0 }} />
      <Typography variant="body2" noWrap>{t(`tracker:issue.priority.${p}`)}</Typography>
    </Box>
  );

  return (
    <Select
      size="small"
      fullWidth
      value={value}
      disabled={disabled}
      onChange={e => onChange(e.target.value as Priority)}
      renderValue={v => renderPriority(v as Priority)}
    >
      {PRIORITY_OPTIONS.map(p => (
        <MenuItem key={p} value={p}>{renderPriority(p)}</MenuItem>
      ))}
    </Select>
  );
}

export default PrioritySelect;
