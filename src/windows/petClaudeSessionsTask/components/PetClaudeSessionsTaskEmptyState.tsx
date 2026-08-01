import { Box, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';

// 面板空状态：紧凑居中，文案对应 petClaudeSessionsTask:task.empty.*。
function PetClaudeSessionsTaskEmptyState() {
  const { t } = useTranslation();

  return (
    <Box
      sx={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        flex: 1,
        gap: 0.5,
        px: 2,
        py: 3,
      }}
    >
      <Typography variant="body2" sx={{ fontWeight: 600 }}>
        {t('petClaudeSessionsTask:task.empty.title')}
      </Typography>
      <Typography variant="caption" color="text.secondary" align="center">
        {t('petClaudeSessionsTask:task.empty.desc')}
      </Typography>
    </Box>
  );
}

export default PetClaudeSessionsTaskEmptyState;
