import {
  CleaningServicesOutlined as CleaningServicesOutlinedIcon,
  RefreshOutlined as RefreshOutlinedIcon,
} from '@mui/icons-material';
import { Box, IconButton, Typography } from '@mui/material';
import { useDevWorkbenchStore } from '@src/state/devWorkbench';
import { useTranslation } from 'react-i18next';
import DevTaskTree from './components/DevTaskTree/DevTaskTree';

// DevWorkbenchPage：控制台「开发工作台」（执行面：worktree→开发→PR→清理）。
// 左右两栏布局（无横跨全宽的顶栏——页面名由 PanelApp 顶部面包屑显示，页面内不重复）：
// 左栏 = 任务树（跨所有工作空间的 dev issue）；右栏 = 顶部操作栏（选中 issue 摘要 + 操作入口）+ 内容区（步骤条/步骤内容，模块 C/D 接入）。
export default function DevWorkbenchPage() {
  const { t } = useTranslation();
  const selectedIssueId = useDevWorkbenchStore(s => s.selectedIssueId);

  return (
    <Box sx={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
      {/* 左栏：任务树（workspace→project→dev issue 三级，跨所有工作空间） */}
      <Box
        sx={{
          width: 260,
          flexShrink: 0,
          borderRight: 1,
          borderColor: 'divider',
          overflow: 'hidden',
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <DevTaskTree />
      </Box>

      {/* 右栏：顶部操作栏 + 内容区 */}
      <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        {/* 顶部操作栏：选中 issue 摘要 + 占位操作按钮（刷新/清理中心，待 C/P3 接入；按规范不挂 Tooltip） */}
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
          {selectedIssueId != null && (
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }} noWrap>#{selectedIssueId}</Typography>
          )}
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

        {/* 内容区：步骤条 + 当前步骤内容（模块 C/D 待接入） */}
        <Box sx={{ flex: 1, overflow: 'hidden' }}>
          {selectedIssueId != null
            ? (
                <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%', p: 2, gap: 1 }}>
                  {/* TODO(C/D): 步骤条 + 当前步骤内容区 */}
                  <Typography variant="body2" color="text.secondary">步骤条待模块 C/D 实施</Typography>
                </Box>
              )
            : (
                <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
                  <Typography variant="body2" color="text.secondary">{t('panel:devWorkbench.selectHint')}</Typography>
                </Box>
              )}
        </Box>
      </Box>
    </Box>
  );
}
