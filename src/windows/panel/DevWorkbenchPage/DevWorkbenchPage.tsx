import {
  CleaningServicesOutlined as CleaningServicesOutlinedIcon,
  RefreshOutlined as RefreshOutlinedIcon,
  ViewSidebar as ViewSidebarIcon,
  ViewSidebarOutlined as ViewSidebarOutlinedIcon,
} from '@mui/icons-material';
import { Box, IconButton, Typography } from '@mui/material';
import { useDevWorkbenchStore } from '@src/state/devWorkbench';
import { useProjectIssues, useProjectStateViews } from '@src/state/tracker';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import DevStepper from './components/DevStepper/DevStepper';
import DevTaskTree from './components/DevTaskTree/DevTaskTree';
import StateBadge from './components/StateBadge';
import StepContent from './components/StepContent/StepContent';

// DevWorkbenchPage：控制台「开发工作台」（执行面：worktree→开发→PR→清理）。
// 左右两栏（无横跨全宽顶栏，页面名由 PanelApp 顶部面包屑显示）：
// 左栏 = 任务树；右栏 = 顶部操作栏（切换步骤条 + #id+名称 + 状态 + 操作入口）+ 内容区（纵向步骤条[可隐藏] + 步骤内容[模块 D]）。
// 顶层按 selectedProjectId 查询 issue（实时派生，避免 store 快照陈旧）。
export default function DevWorkbenchPage() {
  const { t } = useTranslation();
  const selectedIssueId = useDevWorkbenchStore(s => s.selectedIssueId);
  const selectedProjectId = useDevWorkbenchStore(s => s.selectedProjectId);
  // 纵向步骤条侧栏开关（默认隐藏，避免占用空间）；点顶栏切换 icon 展开。保活时 state 保留。
  const [stepsPanelOpen, setStepsPanelOpen] = useState(false);
  const { data: issues = [] } = useProjectIssues(selectedProjectId ?? 0);
  const { viewMap } = useProjectStateViews(selectedProjectId ?? 0);
  const issue = issues.find(i => i.id === selectedIssueId);
  const view = issue ? viewMap.get(issue.stateId) : undefined;
  const hasSelection = selectedIssueId != null && selectedProjectId != null;
  // 步骤条点击查看的步骤 stateCode（null = 默认 issue 当前步骤）；切换 issue 时重置回当前（adjust during render）。
  const [viewingStateCode, setViewingStateCode] = useState<string | null>(null);
  const [viewingIssueId, setViewingIssueId] = useState<number | null>(selectedIssueId);
  if (selectedIssueId !== viewingIssueId) {
    setViewingIssueId(selectedIssueId);
    setViewingStateCode(null);
  }
  const currentViewingCode = viewingStateCode ?? view?.stateCode;

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
        {/* 顶部操作栏：切换步骤条 + 选中 issue 的 #id + 名称 + 状态徽章 + 占位按钮（不挂 Tooltip） */}
        <Box
          sx={{
            height: 48,
            flexShrink: 0,
            display: 'flex',
            alignItems: 'center',
            px: 1,
            gap: 0.5,
            borderBottom: 1,
            borderColor: 'divider',
            bgcolor: 'background.paper',
          }}
        >
          <IconButton
            size="small"
            onClick={() => setStepsPanelOpen(o => !o)}
            aria-label={t('panel:devWorkbench.toggleSteps')}
            disabled={!hasSelection}
            sx={{ color: 'text.secondary' }}
          >
            {stepsPanelOpen ? <ViewSidebarIcon /> : <ViewSidebarOutlinedIcon />}
          </IconButton>
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

        {/* 内容区：纵向步骤条（可隐藏）+ 步骤内容区 */}
        <Box sx={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
          {stepsPanelOpen && hasSelection && issue && selectedProjectId != null && (
            <Box sx={{ width: 200, flexShrink: 0, borderRight: 1, borderColor: 'divider', overflow: 'auto', p: 2 }}>
              <DevStepper issue={issue} projectId={selectedProjectId} activeStepCode={viewingStateCode} onStepClick={setViewingStateCode} />
            </Box>
          )}
          <Box sx={{ flex: 1, overflow: 'auto' }}>
            {!hasSelection
              ? (
                  <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
                    <Typography variant="body2" color="text.secondary">选择左侧任务查看开发步骤</Typography>
                  </Box>
                )
              : issue && selectedProjectId != null
                ? (
                    <Box sx={{ p: 2 }}>
                      <StepContent issue={issue} projectId={selectedProjectId} stateCode={currentViewingCode} />
                    </Box>
                  )
                : (
                    <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
                      <Typography variant="body2" color="text.secondary">任务不存在或已移出开发流程</Typography>
                    </Box>
                  )}
          </Box>
        </Box>
      </Box>
    </Box>
  );
}
