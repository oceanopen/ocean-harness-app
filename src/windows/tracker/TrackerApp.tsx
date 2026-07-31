import { Box, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';

// TrackerApp：工作空间 → 项目 → Issue 三级管理的窗口根。
// 任务8 仅搭三栏布局壳（顶栏 workspace 选择器位 + 左栏 project 列表 + 右栏 issue 列表），
// 各栏占位；业务（接口调用、CRUD、导航 state）在任务 9-12 逐步接入。
export default function TrackerApp() {
  const { t } = useTranslation();

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100vh', overflow: 'hidden' }}>
      {/* 顶栏：Tracker 标题 + workspace 选择器位（任务9 实现） */}
      <Box
        sx={{
          height: 56,
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
          px: 2,
          gap: 2,
          borderBottom: 1,
          borderColor: 'divider',
          bgcolor: 'background.paper',
        }}
      >
        <Typography variant="h6" sx={{ fontWeight: 600 }}>
          {t('tracker:title')}
        </Typography>
        <Typography variant="body2" color="text.secondary">
          {t('tracker:workspace.selectPlaceholder')}
        </Typography>
      </Box>

      {/* 主体：左 project 列表 + 右 issue 列表 */}
      <Box sx={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* 左栏：project 列表（任务10 实现） */}
        <Box
          sx={{
            width: 260,
            flexShrink: 0,
            borderRight: 1,
            borderColor: 'divider',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            p: 2,
          }}
        >
          <Typography variant="body2" color="text.secondary">
            {t('tracker:project.comingSoon')}
          </Typography>
        </Box>

        {/* 右栏：issue 列表（任务11 实现） */}
        <Box
          sx={{
            flex: 1,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            p: 2,
          }}
        >
          <Typography variant="body2" color="text.secondary">
            {t('tracker:issue.comingSoon')}
          </Typography>
        </Box>
      </Box>
    </Box>
  );
}
