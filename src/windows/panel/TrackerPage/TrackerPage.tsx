import { AppsOutlined as AppsOutlinedIcon } from '@mui/icons-material';
import { Box, IconButton, Tooltip, Typography } from '@mui/material';
import { useTrackerStore, useWorkspaceProjects } from '@src/state/tracker';
import { useTranslation } from 'react-i18next';
import ProjectIssueListPage from './components/ProjectIssueListPage/ProjectIssueListPage';
import WorkspaceProjectListPage from './components/WorkspaceProjectListPage/WorkspaceProjectListPage';
import WorkspacesPage from './components/WorkspacesPage/WorkspacesPage';

// TrackerPage：控制台「项目事项管理」页面内容组件（工作空间 → 项目 → Issue 三级管理）。
// 三级选择态读写 tracker store（与命令面板共享同一份），故本组件无 props。
// 嵌在 PanelApp 内容区内（panel 顶栏已显示「项目事项管理」页面名），故自身不再重复标题。
// 两态机：未选中工作空间 → 全屏 WorkspacesPage（卡片网格 + CRUD）；
// 选中某工作空间 → 顶部「工作空间切换栏」（名称 + 切换按钮）+ 左项目列表 / 右 projectIssue 三栏。
export default function TrackerPage() {
  const { t } = useTranslation();
  const selected = useTrackerStore(s => s.selectedWorkspace);
  // 仅记选中项目 id（快照）；项目数据（含关联仓库 localRepositoryIds）从下方 query 实时派生，
  // 避免读 store 快照导致 issue 弹窗仓库列表在项目更新关联仓库后陈旧。
  const selectedProjectId = useTrackerStore(s => s.selectedWorkspaceProject?.id ?? null);
  const selectWorkspace = useTrackerStore(s => s.selectWorkspace);
  const selectWorkspaceProject = useTrackerStore(s => s.selectWorkspaceProject);
  // 项目列表 query：与左栏 WorkspaceProjectListPage 共享同一缓存（同 key），命中即零请求。
  const { data: workspaceProjects = [] } = useWorkspaceProjects(selected?.id ?? null);

  // 未选中工作空间：全屏管理工作空间。
  if (!selected) {
    return <WorkspacesPage onSelect={selectWorkspace} />;
  }

  // 按 id 从最新 query 数据派生选中项目：关联仓库等随项目刷新自动更新（修复 issue 弹窗仓库列表陈旧）。
  const selectedWorkspaceProject = selectedProjectId != null
    ? workspaceProjects.find(p => p.id === selectedProjectId) ?? null
    : null;

  // 已选中工作空间：工作空间切换栏 + 三栏工作壳。
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {/* 工作空间切换栏：当前工作空间名 + 切换按钮（回到工作空间网格，清空选中项目） */}
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
          {/* 前缀「当前工作空间：」用次级色 + 细字重，与名称做区分 */}
          <Box component="span" sx={{ color: 'text.secondary', fontWeight: 400 }}>
            {t('tracker:workspace.current')}
          </Box>
          {selected.name}
        </Typography>
        <Tooltip title={t('tracker:workspace.actions.switch')}>
          <IconButton
            size="small"
            onClick={() => {
              selectWorkspaceProject(null);
              selectWorkspace(null);
            }}
            sx={{ ml: 'auto' }}
            aria-label={t('tracker:workspace.actions.switch')}
          >
            <AppsOutlinedIcon />
          </IconButton>
        </Tooltip>
      </Box>

      {/* 主体：左 workspaceProject 列表 + 右 projectIssue 列表 */}
      <Box sx={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* 左栏：workspaceProject 列表（宽 260，选中态读写 store） */}
        <Box
          sx={{
            width: 260,
            flexShrink: 0,
            borderRight: 1,
            borderColor: 'divider',
            overflow: 'hidden',
          }}
        >
          <WorkspaceProjectListPage workspace={selected} />
        </Box>

        {/* 右栏：projectIssue 列表（选中项目后渲染；key 随项目切换重挂载，重置筛选/折叠并重新加载） */}
        <Box sx={{ flex: 1, overflow: 'hidden' }}>
          {selectedWorkspaceProject
            ? (
                <ProjectIssueListPage key={selectedWorkspaceProject.id} workspaceProject={selectedWorkspaceProject} />
              )
            : (
                <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
                  <Typography variant="body2" color="text.secondary">
                    {t('tracker:projectIssue.emptyHint')}
                  </Typography>
                </Box>
              )}
        </Box>
      </Box>
    </Box>
  );
}
