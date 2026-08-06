import type { SxProps } from '@mui/material';
import type { ProjectStateView } from '@src/state/tracker';
import { Box, FormControl, InputLabel, MenuItem, Select, Typography } from '@mui/material';
import { useId } from 'react';

// 受控状态下拉：options 为项目状态列表，每项「色点 + 名称（默认状态显示中文）」。
// 仅改本地值（保存按钮统一提交），不发请求。
// label 提供时包 FormControl+InputLabel（查询表单复用）；allOption 提供时渲染"全部"项（value='all'）。
interface ProjectStateSelectProps<V extends number | 'all' = number> {
  value: V;
  projectStates: ProjectStateView[];
  onChange: (stateId: V) => void;
  disabled?: boolean;
  label?: string;
  allOption?: string;
  sx?: SxProps;
}

function ProjectStateSelect<V extends number | 'all' = number>({
  value,
  projectStates,
  onChange,
  disabled,
  label,
  allOption,
  sx,
}: ProjectStateSelectProps<V>) {
  const labelId = useId();
  const renderState = (s: ProjectStateView | undefined) => (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
      <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: s?.color || 'text.disabled', flexShrink: 0 }} />
      <Typography variant="body2" noWrap>{s ? s.name : ''}</Typography>
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
      onChange={(e) => {
        const v = e.target.value;
        onChange((v === 'all' ? 'all' : Number(v)) as V);
      }}
      renderValue={v => (
        v === 'all'
          ? <Typography variant="body2" noWrap>{allOption}</Typography>
          : renderState(projectStates.find(s => s.id === (v as number)))
      )}
    >
      {allOption !== undefined && <MenuItem value="all">{allOption}</MenuItem>}
      {projectStates.map(s => (
        <MenuItem key={s.id} value={s.id}>{renderState(s)}</MenuItem>
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
