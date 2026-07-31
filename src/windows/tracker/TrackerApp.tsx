import type { Workspace } from './WorkspacesPage';
import { AppsOutlined as AppsOutlinedIcon } from '@mui/icons-material';
import { Box, IconButton, Tooltip, Typography } from '@mui/material';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import WorkspacesPage from './WorkspacesPage';

// TrackerApp：工作空间 → 项目 → Issue 三级管理的窗口根。
// 任务9 接入工作空间选择：未选中时全屏展示 WorkspacesPage（卡片网格 + CRUD），
// 选中某工作空间后切换到三栏工作壳（顶栏显示当前工作空间 + 切换按钮，左/右栏占位待任务10/11替换）。
export default function TrackerApp() {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<Workspace | null>(null);

  // 未选中工作空间：全屏管理工作空间。
  if (!selected) {
    return <WorkspacesPage onSelect={setSelected} />;
  }

  // 已选中工作空间：三栏工作壳。
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100vh', overflow: 'hidden' }}>
      {/* 顶栏：当前工作空间名 + 切换按钮（回到工作空间网格） */}
      <Box
        sx={{
          height: 56,
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
          px: 2,
          gap: 1.5,
          borderBottom: 1,
          borderColor: 'divider',
          bgcolor: 'background.paper',
        }}
      >
        <Typography variant="h6" sx={{ fontWeight: 600 }}>
          {t('tracker:title')}
        </Typography>
        <Typography variant="body2" color="text.secondary">/</Typography>
        <Typography variant="subtitle1" sx={{ fontWeight: 600 }} noWrap>
          {selected.name}
        </Typography>
        <Tooltip title={t('tracker:workspace.actions.switch')}>
          <IconButton
            size="small"
            onClick={() => setSelected(null)}
            sx={{ ml: 'auto' }}
            aria-label={t('tracker:workspace.actions.switch')}
          >
            <AppsOutlinedIcon />
          </IconButton>
        </Tooltip>
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
