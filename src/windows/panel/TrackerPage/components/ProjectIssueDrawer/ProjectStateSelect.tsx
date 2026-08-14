import type { SxProps } from '@mui/material';
import type { StateCode } from '@src/state/tracker';
import { Box, FormControl, InputLabel, MenuItem, Select, Typography } from '@mui/material';
import { STATE_CATALOG } from '@src/state/tracker';
import { useId } from 'react';

// 受控状态下拉：options 为固定 5 状态（STATE_CATALOG，顺序即数组序），每项「色点 + 名称」。
// 仅改本地值（保存按钮统一提交），不发请求。
// label 提供时包 FormControl+InputLabel（查询表单复用）；allOption 提供时渲染"全部"项（value='all'，置顶）。
interface ProjectStateSelectProps<V extends StateCode | 'all' = StateCode> {
  value: V;
  onChange: (stateCode: V) => void;
  disabled?: boolean;
  label?: string;
  allOption?: string;
  sx?: SxProps;
}

function ProjectStateSelect<V extends StateCode | 'all' = StateCode>({
  value,
  onChange,
  disabled,
  label,
  allOption,
  sx,
}: ProjectStateSelectProps<V>) {
  const labelId = useId();
  const metaOf = (code: StateCode) => STATE_CATALOG.find(s => s.code === code);
  const renderState = (code: StateCode) => {
    const meta = metaOf(code);
    return (
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
        <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: meta?.color || 'text.disabled', flexShrink: 0 }} />
        <Typography variant="body2" noWrap>{meta?.name ?? code}</Typography>
      </Box>
    );
  };

  const select = (
    <Select
      size="small"
      fullWidth
      labelId={label ? labelId : undefined}
      label={label}
      value={value}
      disabled={disabled}
      onChange={(e) => {
        const v = e.target.value;
        onChange((v === 'all' ? 'all' : v as StateCode) as V);
      }}
      renderValue={v => (
        v === 'all'
          ? <Typography variant="body2" noWrap>{allOption}</Typography>
          : renderState(v as StateCode)
      )}
    >
      {allOption !== undefined && <MenuItem value="all">{allOption}</MenuItem>}
      {STATE_CATALOG.map(s => (
        <MenuItem key={s.code} value={s.code}>{renderState(s.code)}</MenuItem>
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

export default ProjectStateSelect;
