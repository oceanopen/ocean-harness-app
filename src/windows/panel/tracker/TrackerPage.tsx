import type { WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import { AppsOutlined as AppsOutlinedIcon } from '@mui/icons-material';
import { Box, IconButton, Tooltip, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import IssueListPage from './IssueListPage';
import ProjectListPage from './ProjectListPage';
import WorkspacesPage from './WorkspacesPage';

// tracker 三级选择状态由父级 PanelApp 持有：命令面板「跳到工作空间/项目」需回写同一份状态，
// 故 TrackerPage 改为受控——selected/selectedProject 经 props 传入，变更经回调上抛。
interface TrackerPageProps {
  selected: WorkspaceModel | null;
  selectedProject: WorkspaceProjectModel | null;
  onSelectWorkspace: (ws: WorkspaceModel | null) => void;
  onSelectProject: (project: WorkspaceProjectModel | null) => void;
}

// TrackerPage：控制台「工作台」页面内容组件（工作空间 → 项目 → Issue 三级管理）。
// 嵌在 PanelApp 内容区内（panel 顶栏已显示「工作台」页面名），故自身不再重复标题。
// 两态机：未选中工作空间 → 全屏 WorkspacesPage（卡片网格 + CRUD）；
// 选中某工作空间 → 顶部「工作空间切换栏」（名称 + 切换按钮）+ 左项目列表 / 右 issue 三栏。
// 左栏 ProjectListPage 受控选中：行点击上抛 selectedProject，由右栏决定渲染哪个项目的 issue。
// 切换工作空间时一并清空选中项目。
export default function TrackerPage({ selected, selectedProject, onSelectWorkspace, onSelectProject }: TrackerPageProps) {
  const { t } = useTranslation();

  // 未选中工作空间：全屏管理工作空间。
  if (!selected) {
    return <WorkspacesPage onSelect={onSelectWorkspace} />;
  }

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
          {selected.name}
        </Typography>
        <Tooltip title={t('tracker:workspace.actions.switch')}>
          <IconButton
            size="small"
            onClick={() => {
              onSelectProject(null);
              onSelectWorkspace(null);
            }}
            sx={{ ml: 'auto' }}
            aria-label={t('tracker:workspace.actions.switch')}
          >
            <AppsOutlinedIcon />
          </IconButton>
        </Tooltip>
      </Box>

      {/* 主体：左 project 列表 + 右 issue 列表 */}
      <Box sx={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* 左栏：project 列表（宽 260，选中受控上抛） */}
        <Box
          sx={{
            width: 260,
            flexShrink: 0,
            borderRight: 1,
            borderColor: 'divider',
            overflow: 'hidden',
          }}
        >
          <ProjectListPage
            workspace={selected}
            selectedId={selectedProject?.id ?? null}
            onSelect={onSelectProject}
          />
        </Box>

        {/* 右栏：issue 列表（选中项目后渲染；key 随项目切换重挂载，重置筛选/折叠并重新加载） */}
        <Box sx={{ flex: 1, overflow: 'hidden' }}>
          {selectedProject
            ? (
                <IssueListPage key={selectedProject.id} project={selectedProject} />
              )
            : (
                <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: 2 }}>
                  <Typography variant="body2" color="text.secondary">
                    {t('tracker:issue.emptyHint')}
                  </Typography>
                </Box>
              )}
        </Box>
      </Box>
    </Box>
  );
}
