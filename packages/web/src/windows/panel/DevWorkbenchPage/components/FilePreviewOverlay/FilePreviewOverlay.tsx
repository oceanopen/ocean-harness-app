import { Box, useTheme } from '@mui/material';
import { useDevWorkbenchStore } from '@src/state/devWorkbench';
import { usePreviewTabs, useWorkspaceFilesStore, workspaceFilesKeys } from '@src/state/workspaceFiles';
import { useQueryClient } from '@tanstack/react-query';
import PreviewContent from './PreviewContent';
import PreviewTabsBar from './PreviewTabsBar';

interface FilePreviewOverlayProps {
  /// 当前选中 issue（浮层 tabs 按 issue 隔离；null = 未选中，恒空浮层）。
  issueId: string | null;
  /// 工作空间根目录（config 订阅值由页面直传，不重复订阅）。
  baseDir: string;
}

/// 工作空间文件预览浮层：铺满终端内容区（挂载于 DevWorkbenchPage 终端内容区 Box，其
/// 补了 position:relative）。浮层铁律（TerminalSearch 先例）：absolute inset 0 不挤压
/// 布局——终端 DOM 尺寸不变，零 SIGWINCH 扰动，会话后端常驻仅视觉遮盖；zIndex 取
/// theme.zIndex.mobileStepper 压过 xterm 内部层（其静态容器不建堆叠上下文，内部层直接
/// 参与外层竞争）；实色背景完整接收 pointerEvents（防点击穿透被 xterm preventDefault 吞掉）。
///
/// 显隐派生自 tabs 非空（无独立开关）；Escape 关激活 tab（关最后一个 = 浮层整体消失）——
/// 工作台沉浸模式（全屏）下让位：Esc 直退全屏（页面级处理器），不关 tab。生命周期语义：
/// 切 issue 浮层自动切到该 issue 的 tab 集（可能为空）；收工具面板/切工具 tab 不影响浮层
/// （树面板只是入口，浮层独立于终端内容区）。
export default function FilePreviewOverlay({ issueId, baseDir }: FilePreviewOverlayProps) {
  const theme = useTheme();
  const queryClient = useQueryClient();
  const setActiveTab = useWorkspaceFilesStore(s => s.setActivePreviewTab);
  const closeTab = useWorkspaceFilesStore(s => s.closePreviewTab);
  const closeAllTabs = useWorkspaceFilesStore(s => s.closeAllPreviewTabs);
  const { tabs, activeTabId } = usePreviewTabs(issueId);

  if (tabs.length === 0) {
    return null;
  }

  // 刷新激活 tab：invalidate 内容 query（与文件树面板刷新同范式）——text 原位重取（SWR
  // 语义，先显旧值后替换），image 经 dataUpdatedAt 版本令牌换 URL 强制重载。
  const refreshActive = () => {
    if (issueId != null && activeTabId != null) {
      void queryClient.invalidateQueries({ queryKey: workspaceFilesKeys.content(issueId, activeTabId) });
    }
  };

  return (
    <Box
      sx={{
        position: 'absolute',
        inset: 0,
        zIndex: theme.zIndex.mobileStepper,
        bgcolor: 'background.paper',
        display: 'flex',
        flexDirection: 'column',
      }}
      onKeyDown={(e) => {
        // 沉浸模式下 Esc 让位页面级全屏退出（getState 现读，不建渲染订阅）
        if (useDevWorkbenchStore.getState().workbenchFullscreen) {
          return;
        }
        if (e.key === 'Escape' && issueId != null && activeTabId != null) {
          closeTab(issueId, activeTabId);
        }
      }}
    >
      <PreviewTabsBar
        tabs={tabs}
        activeTabId={activeTabId}
        onSelect={path => issueId != null && setActiveTab(issueId, path)}
        onClose={path => issueId != null && closeTab(issueId, path)}
        onCloseAll={() => issueId != null && closeAllTabs(issueId)}
        onRefresh={refreshActive}
      />
      {issueId != null && activeTabId != null && (
        <PreviewContent key={activeTabId} issueId={issueId} baseDir={baseDir} path={activeTabId} />
      )}
    </Box>
  );
}
