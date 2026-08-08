import {
  CleaningServicesOutlined as CleaningServicesOutlinedIcon,
  RefreshOutlined as RefreshOutlinedIcon,
} from '@mui/icons-material';
import { Box, IconButton, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';

// DevWorkbenchPage：控制台「开发工作台」页面内容组件（执行面：worktree→开发→PR→清理）。
// 模块 A 仅骨架：复刻 TrackerPage 的 3-zone shell（顶栏 + 左固定宽 + 右弹性），左右栏占位待模块 B/C 接入。
// 不引入工作空间选择/保活（B 模块接入左任务树时再加）。顶栏刷新/清理中心为占位入口（disabled），待 C/P3 接入。
export default function DevWorkbenchPage() {
  const { t } = useTranslation();

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {/* 顶栏：标题 + 右侧占位入口（刷新/清理中心，待 C/P3 接入；按规范不挂 Tooltip） */}
      <Box
        sx={{
          height: 48,
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
          px: 2,
          gap: 1,
          borderBottom: 1,
          borderColor: 'divider',
          bgcolor: 'background.paper',
        }}
      >
        <Typography variant="subtitle1" sx={{ fontWeight: 600 }} noWrap>
          {t('panel:menu.devWorkbench')}
        </Typography>
        <Box sx={{ ml: 'auto', display: 'flex', gap: 0.5 }}>
          {/* TODO(C): 刷新；占位 disabled + aria-hidden（屏幕阅读器跳过无用占位按钮） */}
          <IconButton size="small" disabled aria-hidden>
            <RefreshOutlinedIcon />
          </IconButton>
          {/* TODO(P3): 清理中心批量入口 */}
          <IconButton size="small" disabled aria-hidden>
            <CleaningServicesOutlinedIcon />
          </IconButton>
        </Box>
      </Box>

      {/* 主体：左任务树占位 + 右步骤条/内容区占位 */}
      <Box sx={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* 左栏：任务树占位（模块 B 接入 workspace→project→dev issue 树） */}
        <Box sx={{ width: 260, flexShrink: 0, borderRight: 1, borderColor: 'divider', overflow: 'hidden' }}>
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
            <Typography variant="body2" color="text.secondary">
              {/* TODO(B): 左任务树 */}
              待实现
            </Typography>
          </Box>
        </Box>

        {/* 右栏：步骤条 + 当前步骤内容区（模块 C/D 接入） */}
        <Box sx={{ flex: 1, overflow: 'hidden' }}>
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
            <Typography variant="body2" color="text.secondary">
              {/* TODO(C/D): 步骤条 + 步骤内容区 */}
              待实现
            </Typography>
          </Box>
        </Box>
      </Box>
    </Box>
  );
}
