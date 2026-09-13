import DvrOutlinedIcon from '@mui/icons-material/DvrOutlined';
import { Box, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';

function EmptyState() {
  const { t } = useTranslation();

  return (
    <Box
      sx={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        height: '100%',
        gap: 1.5,
        px: 3,
        py: 4,
      }}
    >
      <DvrOutlinedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />
      <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
        {t('claudeSessions:empty.title')}
      </Typography>
      <Typography variant="body2" color="text.secondary" align="center">
        {t('claudeSessions:empty.desc')}
      </Typography>
    </Box>
  );
}

export default EmptyState;
