import {
  Autorenew as AutorenewIcon,
  FolderOutlined as FolderOutlinedIcon,
  SettingsOutlined as SettingsOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  IconButton,
  Typography,
} from '@mui/material';
import {
  decodeWorkspaceBaseDir,
  DEFAULT_WORKSPACE_BASE_DIR,
  WORKSPACE_BASE_DIR_KEY,
} from '@src/shared/appConfig';
import { openProjectConfigSettings } from '@src/shared/openSettings';
import { useConfigReady } from '@src/shared/useConfigReady';
import { useConfigValue } from '@src/shared/useConfigValue';
import {
  DEFAULT_EXPANDED_DIR,
  useExpandedDirs,
  usePreviewTabs,
  useWorkspaceFilesStore,
  useWorkspaceFileTree,
  workspaceFilesKeys,
} from '@src/state/workspaceFiles';
import { useQueryClient } from '@tanstack/react-query';
import { useMemo } from 'react';
import PanelToolbar from '../PanelToolbar';
import { buildFileTree } from './buildFileTree';
import FileTree from './FileTree';

// baseDir 分支渲染（未设置引导 vs 文件树）的挂载前置闸门：useConfigValue 初值为同步
// 默认值（空串 = 未设置），不闸门会先闪「请先设置」分支再被真实值纠正——每次纠正都是
// 一次错误分支闪现（编码规则 1）。模块级常量保证引用稳定（useConfigReady 要求）。
const FILE_PANEL_CONFIG_KEYS: readonly string[] = [WORKSPACE_BASE_DIR_KEY];

/// 默认展开目录集（repo/——展开即直见各仓库名）。仅作「该 issue 从未 toggle 过」时的
/// 渲染期回落，不随持久化流转（与 store.toggleDirExpanded 的基底一致）。
const DEFAULT_EXPANDED_DIRS: ReadonlySet<string> = new Set([DEFAULT_EXPANDED_DIR]);

interface WorkspaceFilePanelProps {
  issueId: string;
}

/// WorkspaceFilePanel：开发工作台工具面板区的「文件」tab 内容（T5.1 本期，经 toolRegistry
/// 挂载）。展示当前 issue 工作空间 {workspace_base_dir}/{issueId}/ 的一次性全目录树，
/// 头部文件计数 + 刷新按钮（手动刷新口径——无 watcher 无轮询，与子任务面板 T3.1 同决策）。
/// 点击文件行 → openPreviewTab → 终端内容区浮层预览（浮层组件独立挂载，与本面板无耦合）。
/// 面板标题由 tab 头承载（「文件」），本组件头部仅计数/截断提示 + 刷新。
export default function WorkspaceFilePanel({ issueId }: WorkspaceFilePanelProps) {
  const qc = useQueryClient();
  const baseDir = useConfigValue(WORKSPACE_BASE_DIR_KEY, decodeWorkspaceBaseDir, DEFAULT_WORKSPACE_BASE_DIR);
  const configReady = useConfigReady(FILE_PANEL_CONFIG_KEYS);
  const { data, isLoading, error, isFetching, refetch } = useWorkspaceFileTree(issueId, baseDir);

  // 语义化深链：引导用户去「项目配置」分区设置工作空间根目录（共享助手，EmbeddedTerminal 同源）。
  const openSettings = () => openProjectConfigSettings('WorkspaceFilePanel');

  const toggleDirExpanded = useWorkspaceFilesStore(s => s.toggleDirExpanded);
  const openPreviewTab = useWorkspaceFilesStore(s => s.openPreviewTab);
  const expandedDirsRecorded = useExpandedDirs(issueId);
  const { tabs } = usePreviewTabs(issueId);
  const expandedDirs = expandedDirsRecorded ?? DEFAULT_EXPANDED_DIRS;
  const openPaths = useMemo(() => new Set(tabs.map(t => t.path)), [tabs]);
  const treeRoots = useMemo(() => (data != null ? buildFileTree(data.nodes) : []), [data]);
  const fileCount = data?.nodes.filter(n => !n.isDir).length ?? 0;

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: workspaceFilesKeys.tree(issueId) });
  };

  // baseDir 配置未就绪：空占位（闸门理由见 FILE_PANEL_CONFIG_KEYS 注释）。
  if (!configReady) {
    return <Box sx={{ height: '100%' }} />;
  }

  // 错误态一：工作空间根目录未设置（配置为空串）——引导去设置（EmbeddedTerminal 同款）。
  if (baseDir === '') {
    return (
      <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 1.5, p: 2 }}>
        <Typography variant="body2" color="text.secondary">请先在设置 → 项目配置中设置工作空间根目录</Typography>
        <Button size="small" startIcon={<SettingsOutlinedIcon />} onClick={openSettings}>打开设置</Button>
      </Box>
    );
  }

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* 头部：文件计数（+截断提示）+ 刷新（isFetching 旋转，IssueSubTaskPanel 同款） */}
      <PanelToolbar
        left={(
          <Typography variant="caption" color="text.secondary" noWrap>
            {data != null ? `${fileCount} 个文件${data.truncated ? '（已截断）' : ''}` : ''}
          </Typography>
        )}
        right={(
          <IconButton
            size="small"
            onClick={refresh}
            disabled={isFetching}
            aria-label="刷新文件列表"
            sx={{ color: 'text.secondary' }}
          >
            <AutorenewIcon
              fontSize="small"
              sx={{
                'animation': isFetching ? 'spin 0.8s linear infinite' : undefined,
                '@keyframes spin': {
                  from: { transform: 'rotate(0deg)' },
                  to: { transform: 'rotate(360deg)' },
                },
              }}
            />
          </IconButton>
        )}
      />

      {/* 内容区：错误 / 加载中 / 空目录 / 文件树 */}
      {error
        ? (
            <Box sx={{ p: 1.5 }}>
              <Alert
                severity="error"
                action={<Button color="inherit" size="small" onClick={() => void refetch()}>重试</Button>}
              >
                文件列表加载失败：{error.message}
              </Alert>
            </Box>
          )
        : isLoading
          ? (
              <Box sx={{ display: 'flex', justifyContent: 'center', p: 2 }}>
                <CircularProgress size={20} />
              </Box>
            )
          : treeRoots.length === 0
            ? (
                <Box
                  sx={{
                    flex: 1,
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    justifyContent: 'center',
                    gap: 1,
                    p: 2,
                    textAlign: 'center',
                  }}
                >
                  <FolderOutlinedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />
                  <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>工作空间目录为空</Typography>
                  <Typography variant="body2" color="text.secondary">
                    初始化工作空间或等待终端内产出文件后刷新
                  </Typography>
                </Box>
              )
            : (
                <FileTree
                  roots={treeRoots}
                  expandedDirs={expandedDirs}
                  openPaths={openPaths}
                  onToggleDir={dirPath => toggleDirExpanded(issueId, dirPath)}
                  onOpenFile={path => openPreviewTab(issueId, path)}
                />
              )}
    </Box>
  );
}
