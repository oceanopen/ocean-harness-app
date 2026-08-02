import type { SxProps } from '@mui/material';
import type { Priority } from '@src/services';
import { Box, FormControl, InputLabel, MenuItem, Select, Typography } from '@mui/material';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';

// 受控优先级下拉：5 档（色点 + 本地化文案）。仅改本地值，不发请求。
// label 提供时包 FormControl+InputLabel（查询表单复用）；allOption 提供时渲染"全部"项（value='all'）。
const PRIORITY_OPTIONS: Priority[] = ['urgent', 'high', 'medium', 'low', 'none'];

const PRIORITY_COLOR: Record<Priority, string> = {
  urgent: 'error.main',
  high: 'warning.main',
  medium: 'info.main',
  low: 'text.secondary',
  none: 'text.disabled',
};

interface PrioritySelectProps<V extends Priority | 'all' = Priority> {
  value: V;
  onChange: (p: V) => void;
  disabled?: boolean;
  label?: string;
  allOption?: string;
  sx?: SxProps;
}

function PrioritySelect<V extends Priority | 'all' = Priority>({
  value,
  onChange,
  disabled,
  label,
  allOption,
  sx,
}: PrioritySelectProps<V>) {
  const { t } = useTranslation();
  const labelId = useId();
  const renderPriority = (p: Priority) => (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
      <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: PRIORITY_COLOR[p], flexShrink: 0 }} />
      <Typography variant="body2" noWrap>{t(`tracker:projectIssue.priority.${p}`)}</Typography>
    </Box>
  );

  const select = (
    <Select
      size="small"
      fullWidth
      labelId={label ? labelId : undefined}
      label={label}
      value={value}
      disabled={disabled}
      onChange={e => onChange(e.target.value as V)}
      renderValue={v => (
        v === 'all'
          ? <Typography variant="body2" noWrap>{allOption}</Typography>
          : renderPriority(v as Priority)
      )}
    >
      {allOption !== undefined && <MenuItem value="all">{allOption}</MenuItem>}
      {PRIORITY_OPTIONS.map(p => (
        <MenuItem key={p} value={p}>{renderPriority(p)}</MenuItem>
      ))}
    </Select>
  );

  if (!label) {
    return select;
  }
  return (
    <FormControl size="small" sx={sx}>
      <InputLabel id={labelId}>{label}</InputLabel>
      {select}
    </FormControl>
  );
}

export default PrioritySelect;
