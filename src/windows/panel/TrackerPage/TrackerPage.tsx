import {
  AddOutlined as AddOutlinedIcon,
  AppsOutlined as AppsOutlinedIcon,
} from '@mui/icons-material';
import { Box, CircularProgress, IconButton, Typography } from '@mui/material';
import { useTrackerStore, useWorkspaceProjects, useWorkspaces } from '@src/state/tracker';
import { numParam, TRACKER_WID_PARAM } from '@src/windows/panel/routes';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMatch, useNavigate, useSearchParams } from 'react-router-dom';
import ProjectIssueList from './components/ProjectIssueList/ProjectIssueList';
import WorkspaceProjectList from './components/WorkspaceProjectList/WorkspaceProjectList';
import WorkspaceDrawer from './components/WorkspacesView/WorkspaceDrawer';
import WorkspacesView from './components/WorkspacesView/WorkspacesView';

// TrackerPage：控制台「项目事项管理」页面内容组件（工作空间 → 项目 → Issue 三级管理）。
// 三级选择态读写 tracker store（与命令面板共享同一份），故本组件无 props。
// 嵌在 PanelApp 内容区内（panel 顶栏已显示「项目事项管理」页面名），故自身不再重复标题。
//
// 标题栏恒驻（选中/未选两态）：左侧「当前工作空间：<名称>」（未选显示「请先选择工作空间」置灰），
// 右侧 icon 组 [新建空间 | 展开空间列表]。空间卡片网格（WorkspacesView 完整视图）以页面内
// 绝对定位叠层盖在主体内容上方（铺满标题栏以下区域，非弹窗样式）；未选时默认展开，选中后可再开。
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

  // 空间选择浮层开合：未选空间时默认展开（首访引导）；选中后收起，点列表 icon 可再开。
  // reload 落在 ?wid=<id> 但 store 未回写时也保持展开，避免网格→浮层闪烁。
  const [selectorOpen, setSelectorOpen] = useState(selected == null && urlWid == null);
  // 新建空间抽屉（标题栏 add icon 快捷入口）。
  const [drawerCreateOpen, setDrawerCreateOpen] = useState(false);

  // URL → store 单向同步（仅活动路由）：
  //   有 wid 且 store 不一致 → 按实体回写（reload/前进后退恢复）；
  //   无 wid（bare /tracker，仅浮层「切换」语义可达）→ 清空选中回网格；
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

  // 选中空间（浮层卡片点击 / URL 回写同口径）：写 store + 写 URL + 收起浮层。
  const handleSelectWorkspace = (ws: Parameters<typeof selectWorkspace>[0]) => {
    if (ws) {
      selectWorkspace(ws);
      navigate(`/tracker?${TRACKER_WID_PARAM}=${ws.id}`);
    } else {
      selectWorkspace(null);
      navigate('/tracker');
    }
    setSelectorOpen(false);
  };

  // reload 落在 ?wid=<id> 但 store 尚未回写时，显示加载态而非网格闪烁一下。
  if (urlWid != null && !selected) {
    return (
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
        <CircularProgress />
      </Box>
    );
  }

  // 按 id 从最新 query 数据派生选中项目：关联仓库等随项目刷新自动更新（修复 issue 弹窗仓库列表陈旧）。
  const selectedWorkspaceProject = selectedProjectId != null
    ? workspaceProjects.find(p => p.id === selectedProjectId) ?? null
    : null;

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {/* 工作空间标题栏（恒驻）：当前工作空间名（未选置灰提示）+ 新建/列表 icon 组 */}
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
          {selected
            ? (
              // 前缀「当前工作空间：」用次级色 + 细字重，与名称做区分
                <>
                  <Box component="span" sx={{ color: 'text.secondary', fontWeight: 400 }}>
                    {t('tracker:workspace.current')}
                  </Box>
                  {selected.name}
                </>
              )
            : (
              // 未选：整句置灰（与「当前工作空间：」前缀同色）
                <Box component="span" sx={{ color: 'text.secondary', fontWeight: 400 }}>
                  {t('tracker:workspace.selectHint')}
                </Box>
              )}
        </Typography>
        <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: 0.5 }}>
          {/* 快捷新建空间：直接打开新建抽屉（浮层内的新建按钮已移除，入口上收至此） */}
          <IconButton
            size="small"
            onClick={() => setDrawerCreateOpen(true)}
            aria-label={t('tracker:workspace.actions.addShort')}
            sx={{ color: 'text.secondary' }}
          >
            <AddOutlinedIcon />
          </IconButton>
          {/* 展开空间列表浮层（标题栏下方，不遮盖标题栏） */}
          <IconButton
            size="small"
            onClick={() => setSelectorOpen(o => !o)}
            aria-label={t('tracker:workspace.actions.switch')}
            sx={{ color: 'text.secondary' }}
          >
            <AppsOutlinedIcon />
          </IconButton>
        </Box>
      </Box>

      {/* 主体区域（标题栏以下整块）：已选 → 左项目列表 / 右 projectIssue 三栏；未选 → 空态引导。
          空间选择叠层（WorkspacesView 完整视图）叠加在本区域内上方（非弹窗样式——铺满整个主体区、
          无边框/圆角/阴影）。 */}
      <Box sx={{ flex: 1, display: 'flex', overflow: 'hidden', position: 'relative' }}>
        {/* 空间选择叠层：铺满主体区（标题栏以下），选中空间或再点列表 icon 收起 */}
        {selectorOpen && (
          <Box
            sx={{
              position: 'absolute',
              inset: 0,
              zIndex: theme => theme.zIndex.appBar - 1,
              bgcolor: 'background.default',
              display: 'flex',
              flexDirection: 'column',
            }}
          >
            <WorkspacesView onSelect={ws => handleSelectWorkspace(ws)} />
          </Box>
        )}
        {selected
          ? (
              <>
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
              </>
            )
          : (
              <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', p: 2 }}>
                <Typography variant="body2" color="text.secondary">
                  {t('tracker:workspace.selectHint')}
                </Typography>
              </Box>
            )}
      </Box>

      {/* 新建空间抽屉（标题栏 add icon 快捷入口；创建成功后 invalidate 自动刷新浮层网格并关闭抽屉） */}
      {drawerCreateOpen && (
        <WorkspaceDrawer
          onClose={() => setDrawerCreateOpen(false)}
          onCreated={() => setDrawerCreateOpen(false)}
        />
      )}
    </Box>
  );
}
