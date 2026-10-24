import type { ProjectTypeModel } from '@src/services';
import { Box, MenuItem, Select, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';

/**
 * issue 类型单选下拉（选项 = 项目预定义类型，含「未分类」兜底项 value=0）。
 * 仅改本地值，不发请求；选项带色点，未分类/类型已删时显示「未分类」。
 */
interface TypeSelectProps {
  value: number; // typeId，0=未分类
  types: ProjectTypeModel[];
  onChange: (typeId: number) => void;
  disabled?: boolean;
}

function TypeSelect({ value, types, onChange, disabled }: TypeSelectProps) {
  const { t } = useTranslation();
  const renderType = (type: ProjectTypeModel) => (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
      <Box sx={{ width: '10px', height: '10px', borderRadius: '50%', bgcolor: type.color || 'text.disabled', flexShrink: 0 }} />
      <Typography variant="body2" noWrap>{type.name}</Typography>
    </Box>
  );
  const renderValue = (typeId: number) => {
    const cur = types.find(ty => ty.id === typeId);
    return cur
      ? renderType(cur)
      : <Typography variant="body2" noWrap>{t('tracker:workspaceType.uncategorized')}</Typography>;
  };

  return (
    <Select
      size="small"
      fullWidth
      value={value}
      disabled={disabled}
      onChange={e => onChange(e.target.value as number)}
      renderValue={renderValue}
    >
      <MenuItem value={0}>
        <Typography variant="body2" noWrap>{t('tracker:workspaceType.uncategorized')}</Typography>
      </MenuItem>
      {types.map(ty => (
        <MenuItem key={ty.id} value={ty.id}>{renderType(ty)}</MenuItem>
      ))}
    </Select>
  );
}

export default TypeSelect;
