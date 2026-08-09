import { AppsOutlined as AppsOutlinedIcon } from '@mui/icons-material';
import { Box, CircularProgress, IconButton, Tooltip, Typography } from '@mui/material';
import { useTrackerStore, useWorkspaceProjects, useWorkspaces } from '@src/state/tracker';
import { numParam, TRACKER_WID_PARAM } from '@src/windows/panel/routes';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useMatch, useNavigate, useSearchParams } from 'react-router-dom';
import ProjectIssueList from './components/ProjectIssueList/ProjectIssueList';
import WorkspaceProjectList from './components/WorkspaceProjectList/WorkspaceProjectList';
import WorkspacesView from './components/WorkspacesView/WorkspacesView';

// TrackerPage：控制台「项目事项管理」页面内容组件（工作空间 → 项目 → Issue 三级管理）。
// 三级选择态读写 tracker store（与命令面板共享同一份），故本组件无 props。
// 嵌在 PanelApp 内容区内（panel 顶栏已显示「项目事项管理」页面名），故自身不再重复标题。
// 两态机：未选中工作空间 → 全屏 WorkspacesView（卡片网格 + CRUD）；
// 选中某工作空间 → 顶部「工作空间切换栏」（名称 + 切换按钮）+ 左项目列表 / 右 projectIssue 三栏。
//
// 路由接入（全 query 风格）：工作空间选中态由 URL ?wid=<id> 驱动；本页单向同步 URL→store（仅活动路由）。
// 项目选中态保留 store（不入 URL）。保活（display:none 不卸载）时 store 跨顶层切换不丢。
export default function TrackerPage() {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const urlWid = numParam(searchParams.get(TRACKER_WID_PARAM));
  // 仅当 tracker 是当前活动路由时才同步；隐藏（切到别的顶层页）时保留 store，避免保活选中被清。
  const isActive = useMatch('/tracker') != null;

  const selected = useTrackerStore(s => s.selectedWorkspace);
  const selectWorkspace = useTrackerStore(s => s.selectWorkspace);
  // 仅记选中项目 id（快照）；项目数据（含关联仓库 localRepositoryIds）从下方 query 实时派生，
  // 避免读 store 快照导致 issue 弹窗仓库列表在项目更新关联仓库后陈旧。
  const selectedProjectId = useTrackerStore(s => s.selectedWorkspaceProject?.id ?? null);
  // 项目列表 query：与左栏 WorkspaceProjectList 共享同一缓存（同 key），命中即零请求。
  const { data: workspaceProjects = [] } = useWorkspaceProjects(selected?.id ?? null);
  // 工作空间全量（用于按 URL wid 回查实体，reload/恢复时回写 store）；与命令面板/WorkspacesView 共享缓存。
  const { data: workspaces = [] } = useWorkspaces();

  // URL → store 单向同步（仅活动路由）：
  //   有 wid 且 store 不一致 → 按实体回写（reload/前进后退恢复）；
  //   无 wid（bare /tracker，仅「切换工作空间」按钮可达）→ 清空选中回网格；
  //   非活动路由（隐藏保活）→ 不动 store。
  useEffect(() => {
    if (!isActive) {
      return;
    }
    if (urlWid == null) {
      if (selected) {
        selectWorkspace(null);
      }
    } else if (selected?.id !== urlWid) {
      const ws = workspaces.find(w => w.id === urlWid);
      if (ws) {
        selectWorkspace(ws);
      }
    }
  }, [isActive, urlWid, selected, workspaces, selectWorkspace]);

  // 未选中工作空间：全屏管理工作空间。
  if (!selected) {
    // reload 落在 ?wid=<id> 但 store 尚未回写时，显示加载态而非网格闪烁一下。
    if (urlWid != null) {
      return (
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
          <CircularProgress />
        </Box>
      );
    }
    return (
      <WorkspacesView
        onSelect={(ws) => {
          selectWorkspace(ws);
          navigate(`/tracker?${TRACKER_WID_PARAM}=${ws.id}`);
        }}
      />
    );
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
              selectWorkspace(null); // 联动清空 workspaceProject；navigate 到 bare /tracker 触发网格
              navigate('/tracker');
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
          <WorkspaceProjectList workspace={selected} />
        </Box>

        {/* 右栏：projectIssue 列表（选中项目后渲染；key 随项目切换重挂载，重置筛选/折叠并重新加载） */}
        <Box sx={{ flex: 1, overflow: 'hidden' }}>
          {selectedWorkspaceProject
            ? (
                <ProjectIssueList key={selectedWorkspaceProject.id} workspaceProject={selectedWorkspaceProject} />
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
