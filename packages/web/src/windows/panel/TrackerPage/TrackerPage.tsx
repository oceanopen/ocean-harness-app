import type { WorkspaceModel } from '@src/services';
import { Box, CircularProgress, Typography } from '@mui/material';
import { useTrackerStore, useWorkspaces } from '@src/state/tracker';
import { strParam, TRACKER_WID_PARAM } from '@src/windows/panel/routes';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useSearchParams } from 'react-router-dom';
import WorkspaceIssueList from './components/WorkspaceIssueList/WorkspaceIssueList';
import WorkspaceList from './components/WorkspaceList/WorkspaceList';
import WorkspacesView from './components/WorkspacesView/WorkspacesView';

// TrackerPage：控制台「任务管理」页面内容组件（工作空间 → 任务 两级管理）。
// 选中态读写 tracker store（与命令面板共享同一份），故本组件无 props。
// 嵌在 PanelApp 内容区内（panel 顶栏已显示「任务管理」页面名），故自身不再重复标题。
//
// 两栏布局：左栏工作空间行式列表（只读 + 叠层开关/刷新工具栏），右栏选中空间的任务视图；
// 空间卡片网格（WorkspacesView 完整视图，空间级 CRUD/类型管理入口）以绝对定位叠层盖在
// 右栏内容上方（仅覆盖右栏、非弹窗样式，左栏列表保持可见）。
//
// 路由接入（全 query 风格）：工作空间选中态由 URL ?wid=<id> 驱动；本页单向同步 URL→store。
// store 全局，页面卸载/重挂载（声明式路由切走即卸载）不丢选中。
export default function TrackerPage() {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const urlWid = strParam(searchParams.get(TRACKER_WID_PARAM));

  const selected = useTrackerStore(s => s.selectedWorkspace);
  const selectWorkspace = useTrackerStore(s => s.selectWorkspace);
  // 工作空间全量（用于按 URL wid 回查实体，reload/恢复时回写 store）；与命令面板/列表共享缓存。
  const { data: workspaces = [], isPending: workspacesPending } = useWorkspaces();

  // 空间选择叠层开合：用户未显式开关时取派生默认值（未选空间首访展开；列表为空强制展开引导创建）。
  // reload 落在 ?wid=<id> 但 store 未回写时也保持展开，避免网格→列表闪烁。
  const [userToggled, setUserToggled] = useState<boolean | null>(null);
  const forceOpen = !workspacesPending && workspaces.length === 0;
  const selectorOpen = userToggled ?? ((selected == null && urlWid == null) || forceOpen);
  const toggleSelector = () => setUserToggled(!selectorOpen);

  // URL → store 单向同步：
  //   有 wid 且 store 不一致 → 按实体回写（reload/前进后退恢复）；
  //   无 wid（bare /tracker）→ 清空选中（右栏回落空态提示）。
  useEffect(() => {
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
  }, [urlWid, selected, workspaces, selectWorkspace]);

  // 不可解析的 wid 兜底：加载完成但列表中无此实体（wid 指向已删除的工作空间 / 加载失败 data=[]），
  // replace 回落 bare /tracker（上方 effect 随之清空 store），避免永久 spinner 无出口。
  useEffect(() => {
    if (urlWid != null && !workspacesPending && !workspaces.some(w => w.id === urlWid)) {
      navigate('/tracker', { replace: true });
    }
  }, [urlWid, workspaces, workspacesPending, navigate]);

  // 选中空间（列表行/叠层卡片点击 / URL 回写同口径）：写 store + 写 URL + 收起叠层。
  const handleSelectWorkspace = (ws: WorkspaceModel) => {
    selectWorkspace(ws);
    navigate(`/tracker?${TRACKER_WID_PARAM}=${ws.id}`);
    setUserToggled(false);
  };

  // reload 落在 ?wid=<id> 但 store 尚未回写时，显示加载态而非列表闪烁一下。
  // （wid 不可解析时由上方兜底 effect replace 回落，不会停在此分支。）
  if (urlWid != null && !selected) {
    return (
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box sx={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
      {/* 左栏：工作空间行式列表（宽 260，选中态经 URL 同步 store） */}
      <Box
        sx={{
          width: '260px',
          flexShrink: 0,
          borderRight: '1px solid',
          borderColor: 'divider',
          overflow: 'hidden',
        }}
      >
        <WorkspaceList
          selectedId={selected?.id ?? null}
          onSelect={handleSelectWorkspace}
          overlayOpen={selectorOpen}
          onToggleOverlay={toggleSelector}
        />
      </Box>

      {/* 右栏：任务视图（key 随空间切换重挂载，重置筛选/视图模式；未选空间时空态提示）。
          空间选择叠层仅覆盖本栏（左栏列表保持可见可点），选中空间或经左栏开关收起。 */}
      <Box sx={{ flex: 1, minWidth: 0, overflow: 'hidden', position: 'relative' }}>
        {selectorOpen && (
          <Box
            sx={{
              position: 'absolute',
              inset: '0px',
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
          ? <WorkspaceIssueList key={selected.id} workspace={selected} />
          : (
              <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', p: '16px' }}>
                <Typography variant="body2" color="text.secondary">
                  {t('tracker:workspace.selectHint')}
                </Typography>
              </Box>
            )}
      </Box>
    </Box>
  );
}
