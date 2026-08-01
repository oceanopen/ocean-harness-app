import type { ProjectStateModel } from '@src/services';
import { Box, MenuItem, Select, Typography } from '@mui/material';

// 受控状态下拉：options 为项目状态列表，每项「色点 + 名称」。
// 仅改本地值（保存按钮统一提交），不发请求。
interface StateSelectProps {
  value: number;
  states: ProjectStateModel[];
  onChange: (stateId: number) => void;
  disabled?: boolean;
}

function StateSelect({ value, states, onChange, disabled }: StateSelectProps) {
  const renderState = (s: ProjectStateModel | undefined) => (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
      <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: s?.color || 'text.disabled', flexShrink: 0 }} />
      <Typography variant="body2" noWrap>{s?.name ?? ''}</Typography>
    </Box>
  );

  return (
    <Select
      size="small"
      fullWidth
      value={value}
      disabled={disabled}
      onChange={e => onChange(Number(e.target.value))}
      renderValue={v => renderState(states.find(s => s.id === v))}
    >
      {states.map(s => (
        <MenuItem key={s.id} value={s.id}>{renderState(s)}</MenuItem>
      ))}
    </Select>
  );
}

export default StateSelect;
