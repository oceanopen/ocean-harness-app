import type { ProjectIssueResponseData } from '@src/services';
import {
  CleaningServicesOutlined as CleaningServicesOutlinedIcon,
  RefreshOutlined as RefreshOutlinedIcon,
  ViewSidebar as ViewSidebarIcon,
  ViewSidebarOutlined as ViewSidebarOutlinedIcon,
} from '@mui/icons-material';
import { Box, CircularProgress, IconButton, Typography } from '@mui/material';
import { getDevSteps, useDevWorkbenchStore } from '@src/state/devWorkbench';
import { useProjectIssues, useProjectStateViews } from '@src/state/tracker';
import { DEV_IID_PARAM, DEV_PID_PARAM, DEV_STEP_PARAM, numParam } from '@src/windows/panel/routes';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMatch, useSearchParams } from 'react-router-dom';
import DevStepper from './components/DevStepper/DevStepper';
import DevTaskTree from './components/DevTaskTree/DevTaskTree';
import StateBadge from './components/StateBadge';
import StepContent from './components/StepContent/StepContent';

// DevWorkbenchPage：控制台「开发工作台」（执行面：worktree→开发→PR→清理）。
// 左右两栏（无横跨全宽顶栏，页面名由 PanelApp 顶部面包屑显示）：
// 左栏 = 任务树；右栏 = 顶部操作栏（切换步骤条 + #id+名称 + 状态 + 操作入口）+ 内容区（纵向步骤条[可隐藏] + 步骤内容[模块 D]）。
//
// 路由接入（全 query 风格）：issue 选中与步骤查看由 URL 驱动——
//   ?pid=<projectId>&iid=<issueId>   选中 issue（项目→issue，issue 靠 project 加载，故 pid 同在 URL）
//   &step=<stateCode>                查看的步骤（可省略，缺省回落 issue 当前进度）
// 本页单向同步 URL→store（仅活动路由）；隐藏保活时 store 跨顶层切换不丢。
export default function DevWorkbenchPage() {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const urlPid = numParam(searchParams.get(DEV_PID_PARAM));
  const urlIid = numParam(searchParams.get(DEV_IID_PARAM));
  const urlStep = searchParams.get(DEV_STEP_PARAM);
  // 仅当 devWorkbench 是当前活动路由时才同步；隐藏时保留 store 保活。
  const isActive = useMatch('/devWorkbench') != null;

  const selectedIssueId = useDevWorkbenchStore(s => s.selectedIssueId);
  const selectedProjectId = useDevWorkbenchStore(s => s.selectedProjectId);
  const selectIssue = useDevWorkbenchStore(s => s.selectIssue);

  // 加载用 pid：URL 优先（reload/恢复），否则 store（隐藏保活时 URL 无 dev 参数）。
  const loadPid = urlPid ?? selectedProjectId;
  const { data: issues = [], isLoading: issuesLoading } = useProjectIssues(loadPid ?? 0);
  const { views, viewMap } = useProjectStateViews(loadPid ?? 0);

  // 有效选中：URL 优先，否则 store（保活）。
  const effIssueId = urlIid ?? selectedIssueId;
  const hasSelection = effIssueId != null && loadPid != null;
  const issue = issues.find(i => i.id === effIssueId);
  const view = issue ? viewMap.get(issue.stateId) : undefined;
  // 默认查看步骤：URL step 优先；否则回落 issue 当前 state——若该 state 非开发步骤（如 in_progress），
  // 回落第一步，避免右栏空。切 issue 时 URL 不带 step → 自动回落。
  const devSteps = useMemo(() => getDevSteps(views), [views]);
  const fallbackCode = view && devSteps.some(s => s.stateCode === view.stateCode)
    ? view.stateCode
    : devSteps[0]?.stateCode;
  const currentViewingCode = urlStep ?? fallbackCode;

  // 纵向步骤条侧栏开关：选中任务时按 state 设默认（非开发中展开、开发中收起），之后用户可手动切换。
  // 仅在 issue 切换时重置默认（ref 追踪上次设默认的 issue id）；同任务内推进状态不重置，保留用户开合偏好。
  const [stepsPanelOpen, setStepsPanelOpen] = useState(false);
  const stepsPanelDefaultedForRef = useRef<number | null>(null);
  useEffect(() => {
    if (issue && view && issue.id !== stepsPanelDefaultedForRef.current) {
      stepsPanelDefaultedForRef.current = issue.id;
      setStepsPanelOpen(view.stateCode !== 'developing');
    }
  }, [issue, view]);

  // URL → store 单向同步（仅活动路由）：有 iid 回写 issue（含其 projectId）；无 iid 清空；隐藏不动。
  useEffect(() => {
    if (!isActive) {
      return;
    }
    if (urlIid == null) {
      if (selectedIssueId != null) {
        selectIssue(null);
      }
    } else if (selectedIssueId !== urlIid) {
      const target = issues.find((i: ProjectIssueResponseData) => i.id === urlIid);
      if (target) {
        selectIssue(target);
      }
    }
  }, [isActive, urlIid, issues, selectedIssueId, selectIssue]);

  // 点步骤条查看某步：把 step 写入 query（保留 pid/iid）。
  const handleStepClick = useCallback((stepCode: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set(DEV_STEP_PARAM, stepCode);
      return next;
    });
  }, [setSearchParams]);

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
          {stepsPanelOpen && hasSelection && issue && loadPid != null && (
            <Box sx={{ width: 200, flexShrink: 0, borderRight: 1, borderColor: 'divider', overflow: 'auto', p: 2 }}>
              <DevStepper issue={issue} projectId={loadPid} activeStepCode={currentViewingCode} onStepClick={handleStepClick} />
            </Box>
          )}
          <Box sx={{ flex: 1, overflow: 'auto' }}>
            {!hasSelection
              ? (
                  <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
                    <Typography variant="body2" color="text.secondary">选择左侧任务查看开发步骤</Typography>
                  </Box>
                )
              : issue && loadPid != null
                ? (
                    <Box sx={{ p: 2 }}>
                      <StepContent issue={issue} projectId={loadPid} stateCode={currentViewingCode} />
                    </Box>
                  )
                : issuesLoading
                  ? (
                      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
                        <CircularProgress />
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
