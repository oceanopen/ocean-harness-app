import type { WorkspaceLabelModel } from '@src/service';
import { CheckOutlined as CheckOutlinedIcon, SettingsOutlined as SettingsOutlinedIcon } from '@mui/icons-material';
import { Autocomplete, Box, Button, Chip, TextField, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';

// issue 标签多选：Autocomplete multiple，勾选/取消即触发 onToggle（父级本地切换 labels，统一随保存提交）。
// 底部「管理标签」按钮打开 LabelManagerDialog。受控（value=issueLabels）。
interface LabelSelectProps {
  issueLabels: WorkspaceLabelModel[];
  options: WorkspaceLabelModel[];
  onToggle: (labelId: number) => void;
  onOpenManager: () => void;
  disabled?: boolean;
}

function LabelSelect({ issueLabels, options, onToggle, onOpenManager, disabled }: LabelSelectProps) {
  const { t } = useTranslation();

  return (
    <Box>
      <Autocomplete
        multiple
        options={options}
        value={issueLabels}
        disabled={disabled}
        isOptionEqualToValue={(o, v) => o.id === v.id}
        getOptionLabel={o => o.name}
        disableCloseOnSelect
        limitTags={3}
        onChange={(_e, newValue) => {
          // diff：对增删的 label 各触发一次 onToggle（父级本地切换 labels，统一随保存提交）。
          const prevIds = new Set(issueLabels.map(l => l.id));
          const newIds = new Set(newValue.map(l => l.id));
          newValue.forEach((l) => {
            if (!prevIds.has(l.id)) {
              onToggle(l.id);
            }
          });
          issueLabels.forEach((l) => {
            if (!newIds.has(l.id)) {
              onToggle(l.id);
            }
          });
        }}
        renderValue={(value, getItemProps) =>
          value.map((l, idx) => {
            const { key, ...itemProps } = getItemProps({ index: idx });
            return (
              <Chip
                key={key}
                label={l.name}
                variant="outlined"
                {...itemProps}
                disabled={disabled}
                sx={{ borderColor: l.color || undefined, color: l.color || undefined }}
              />
            );
          })}
        renderOption={(props, option, { selected }) => (
          <li {...props}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, width: '100%' }}>
              <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: option.color || 'text.disabled', flexShrink: 0 }} />
              <Typography variant="body2" sx={{ flex: 1 }}>{option.name}</Typography>
              {selected && <CheckOutlinedIcon fontSize="small" />}
            </Box>
          </li>
        )}
        renderInput={params => (
          <TextField
            {...params}
            label={t('tracker:issue.detail.labels')}
            placeholder={t('tracker:issue.label.namePlaceholder')}
          />
        )}
        noOptionsText={t('tracker:issue.label.empty')}
      />
      <Button
        startIcon={<SettingsOutlinedIcon />}
        onClick={onOpenManager}
        disabled={disabled}
        sx={{ mt: 0.5, textTransform: 'none' }}
      >
        {t('tracker:issue.label.title')}
      </Button>
    </Box>
  );
}

export default LabelSelect;
