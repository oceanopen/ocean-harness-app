import type { SxProps } from '@mui/material';
import type { StateGroupMeta } from '@src/services';
import type { ProjectStateView } from '@src/state/tracker';
import { Box, FormControl, InputLabel, ListSubheader, MenuItem, Select, Typography } from '@mui/material';
import { useId } from 'react';

// 受控状态下拉：options 为项目状态列表，每项「色点 + 名称（默认状态显示中文）」。
// 仅改本地值（保存按钮统一提交），不发请求。
// label 提供时包 FormControl+InputLabel（查询表单复用）；allOption 提供时渲染"全部"项（value='all'，置顶于所有分组之上）。
// stateGroups 提供时按 stateGroup 分组渲染（ListSubheader 组头「色点+组名」+ 组内 MenuItem，空组跳过）：
//   组头不可选故整体淡化（opacity）、上下留白更宽；组内 state 项相对组头左缩进以体现层级。
//   未提供时退回扁平列表（向后兼容）。组顺序与组内状态顺序均按 sortOrder 升序。
interface ProjectStateSelectProps<V extends number | 'all' = number> {
  value: V;
  projectStates: ProjectStateView[];
  onChange: (stateId: V) => void;
  disabled?: boolean;
  label?: string;
  allOption?: string;
  stateGroups?: StateGroupMeta[];
  sx?: SxProps;
}

function ProjectStateSelect<V extends number | 'all' = number>({
  value,
  projectStates,
  onChange,
  disabled,
  label,
  allOption,
  stateGroups,
  sx,
}: ProjectStateSelectProps<V>) {
  const labelId = useId();
  const renderState = (s: ProjectStateView | undefined) => (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
      <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: s?.color || 'text.disabled', flexShrink: 0 }} />
      <Typography variant="body2" noWrap>{s ? s.name : ''}</Typography>
    </Box>
  );

  // 分组头「色点 + 组名」：ListSubheader 不可选（无 value），仅作分隔标题；该组无状态时返回 null 跳过，避免空分组。
  // 组头不可选故整体淡化（opacity）、上下留白更宽（py）；组内 state 项左缩进（pl）以与组头形成层级。
  const renderGroup = (g: StateGroupMeta) => {
    const states = projectStates
      .filter(s => s.stateGroupCode === g.code)
      .sort((a, b) => a.sortOrder - b.sortOrder);
    if (states.length === 0) {
      return [];
    }
    // 返回扁平数组（ListSubheader + MenuItems）而非 Fragment：MUI Select 用 React.Children.toArray 处理
    // 直接 children，不展开 Fragment → cloneElement 注入的 onClick 会挂到 Fragment 上失效（选项点不动）。
    // optionNodes 用 flatMap 展平嵌套数组，使 ListSubheader/MenuItem 成为 Select 直接 children。
    return [
      <ListSubheader key={`grp-${g.code}`} sx={{ py: 1.5 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, opacity: 0.4 }}>
          <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: g.color || 'text.disabled', flexShrink: 0 }} />
          <Typography component="span" variant="caption" sx={{ fontWeight: 600 }}>{g.name}</Typography>
        </Box>
      </ListSubheader>,
      ...states.map(s => (
        <MenuItem key={s.id} value={s.id} sx={{ pl: 3 }}>{renderState(s)}</MenuItem>
      )),
    ];
  };

  // 分组态：stateGroups 按 sortOrder 升序遍历渲染；非分组态（无 stateGroups）退回扁平 MenuItem 列表。
  const optionNodes = stateGroups && stateGroups.length > 0
    ? [...stateGroups].sort((a, b) => a.sortOrder - b.sortOrder).flatMap(renderGroup)
    : projectStates.map(s => (
        <MenuItem key={s.id} value={s.id}>{renderState(s)}</MenuItem>
      ));

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
      {optionNodes}
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
