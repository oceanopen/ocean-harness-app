import {
  CleaningServicesOutlined as CleaningServicesOutlinedIcon,
  RefreshOutlined as RefreshOutlinedIcon,
} from '@mui/icons-material';
import { Box, IconButton, Typography } from '@mui/material';
import { useDevWorkbenchStore } from '@src/state/devWorkbench';
import { useProjectIssues, useProjectStateViews } from '@src/state/tracker';
import { useTranslation } from 'react-i18next';
import DevStepper from './components/DevStepper/DevStepper';
import DevTaskTree from './components/DevTaskTree/DevTaskTree';
import StateBadge from './components/StateBadge';

// DevWorkbenchPage：控制台「开发工作台」（执行面：worktree→开发→PR→清理）。
// 左右两栏（无横跨全宽顶栏，页面名由 PanelApp 顶部面包屑显示）：
// 左栏 = 任务树；右栏 = 顶部操作栏（选中 issue 的 #id + 名称 + 操作入口）+ 内容区（步骤条，模块 C）。
// 顶层按 selectedProjectId 查询 issue（实时派生，避免 store 快照陈旧），顶部与内容区共享同一 issue。
export default function DevWorkbenchPage() {
  const { t } = useTranslation();
  const selectedIssueId = useDevWorkbenchStore(s => s.selectedIssueId);
  const selectedProjectId = useDevWorkbenchStore(s => s.selectedProjectId);
  // 未选时 selectedProjectId 为 null → 用 0 占位（后端返回空，无副作用）；选中后即时命中 tracker 同 key 缓存。
  const { data: issues = [] } = useProjectIssues(selectedProjectId ?? 0);
  const { viewMap } = useProjectStateViews(selectedProjectId ?? 0);
  const issue = issues.find(i => i.id === selectedIssueId);
  const view = issue ? viewMap.get(issue.stateId) : undefined;
  const hasSelection = selectedIssueId != null && selectedProjectId != null;

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
        {/* 顶部操作栏：选中 issue 的 #id + 名称 + 占位操作按钮（待 C/P3 接入；不挂 Tooltip） */}
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
          {hasSelection && issue && (
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }} noWrap>
              <Box component="span" sx={{ color: 'text.secondary', fontWeight: 400 }}>#{issue.id}</Box>
              {' '}
              {issue.name}
            </Typography>
          )}
          <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: 1 }}>
            {hasSelection && issue && <StateBadge view={view} />}
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

        {/* 内容区：步骤条（模块 C）；当前步骤内容区待模块 D */}
        <Box sx={{ flex: 1, overflow: 'auto' }}>
          {!hasSelection
            ? (
                <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
                  <Typography variant="body2" color="text.secondary">{t('panel:devWorkbench.selectHint')}</Typography>
                </Box>
              )
            : issue && selectedProjectId != null
              ? (
                  <Box sx={{ p: 2 }}>
                    <DevStepper issue={issue} projectId={selectedProjectId} />
                    {/* TODO(D): 当前步骤内容区（按 stateCode 切换：wt_init/developing/pr_open/cleanup） */}
                  </Box>
                )
              : (
                  // 陈旧 id（issue 被删/移出）：实时派生自然兜底。
                  <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
                    <Typography variant="body2" color="text.secondary">{t('panel:devWorkbench.staleHint')}</Typography>
                  </Box>
                )}
        </Box>
      </Box>
    </Box>
  );
}
