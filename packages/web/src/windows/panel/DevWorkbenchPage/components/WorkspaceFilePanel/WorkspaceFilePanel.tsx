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
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import {
  decodeWorkspaceBaseDir,
  DEFAULT_WORKSPACE_BASE_DIR,
  WORKSPACE_BASE_DIR_KEY,
} from '@src/shared/appConfig';
import { useConfigReady } from '@src/shared/useConfigReady';
import { useConfigValue } from '@src/shared/useConfigValue';
import {
  DEFAULT_EXPANDED_DIR,
  useExpandedDirs,
  useFilePanelMode,
  usePreviewTabs,
  useWorkspaceFilesStore,
  useWorkspaceFileTree,
  useWorkspaceGitChanges,
  workspaceFilesKeys,
} from '@src/state/workspaceFiles';
import { useSettingsNavigate } from '@src/windows/panel/useSettingsNavigate';
import { useQueryClient } from '@tanstack/react-query';
import { useMemo } from 'react';
import PanelToolbar from '../PanelToolbar';
import { buildFileTree } from './buildFileTree';
import { allDirsExpanded, buildGitChangesTree } from './buildGitChangesTree';
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

/// WorkspaceFilePanel：开发工作台工具面板区的「文件」tab 内容（T5.1，经 toolRegistry
/// 挂载）。双模式：全部文件（一次性全目录树）/ Git 变更（未提交变更文件树 + 状态徽标 +
/// +N/-N 行数）。头部模式切换 + 计数 + 刷新按钮（手动刷新口径——无 watcher 无轮询，与
/// 子任务面板 T3.1 同决策）。点击文件行 → openPreviewTab（两模式统一，tab id = 文件路径）
/// → 终端内容区浮层预览，tab 内容形态（文件内容/diff）由浮层按「面板模式 + 变更集」渲染
/// 期派生。模式存域 store（按 issue 会话级，预览浮层同读；切工具 tab 重挂载不丢）。
export default function WorkspaceFilePanel({ issueId }: WorkspaceFilePanelProps) {
  const qc = useQueryClient();
  const mode = useFilePanelMode(issueId);
  const setFilePanelMode = useWorkspaceFilesStore(s => s.setFilePanelMode);
  const baseDir = useConfigValue(WORKSPACE_BASE_DIR_KEY, decodeWorkspaceBaseDir, DEFAULT_WORKSPACE_BASE_DIR);
  const configReady = useConfigReady(FILE_PANEL_CONFIG_KEYS);
  const { data, isLoading, error, isFetching, refetch } = useWorkspaceFileTree(issueId, baseDir);
  // 变更列表仅 git 模式拉取（issueId 传 null 关闸，切回模式命中缓存 + staleTime 0 重验）。
  const gitQuery = useWorkspaceGitChanges(mode === 'git' ? issueId : null, baseDir);

  // 语义化深链：引导用户去「项目配置」分区设置工作空间根目录（统一 hook 入口，EmbeddedTerminal 同源）。
  const openSettings = useSettingsNavigate('projectConfig');

  const toggleDirExpanded = useWorkspaceFilesStore(s => s.toggleDirExpanded);
  const openPreviewTab = useWorkspaceFilesStore(s => s.openPreviewTab);
  const expandedDirsRecorded = useExpandedDirs(issueId);
  const { tabs, activeTabId } = usePreviewTabs(issueId);
  const expandedDirs = expandedDirsRecorded ?? DEFAULT_EXPANDED_DIRS;

  // 全部文件模式：树根 + 计数。
  const treeRoots = useMemo(() => (data != null ? buildFileTree(data.nodes) : []), [data]);
  const fileCount = data?.nodes.filter(n => !n.isDir).length ?? 0;

  // Git 变更模式：变更树（目录默认全展开）+ 行尾标记表 + 计数。
  const changeFiles = gitQuery.data?.files ?? [];
  const gitRoots = useMemo(() => buildGitChangesTree(changeFiles), [changeFiles]);
  const gitExpandedDirs = useMemo(() => allDirsExpanded(gitRoots), [gitRoots]);
  const gitMarks = useMemo(() => new Map(changeFiles.map(f => [f.path, f])), [changeFiles]);

  // 行高亮：tab id = 文件路径（一文件一 tab，无形态前缀），两模式树高亮同源。
  const openPaths = useMemo(() => new Set(tabs), [tabs]);
  const activePath = activeTabId ?? undefined;

  const refresh = () => {
    if (mode === 'git') {
      void qc.invalidateQueries({ queryKey: workspaceFilesKeys.gitChanges(issueId) });
      // 已打开的 diff tab 内容随刷新重取（前缀失效该 issue 的全部 fileDiff key）。
      void qc.invalidateQueries({ queryKey: [...workspaceFilesKeys.root, 'fileDiff', { issueId }] });
    } else {
      void qc.invalidateQueries({ queryKey: workspaceFilesKeys.tree(issueId) });
    }
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

  // 模式差异描述：内容区四分支骨架（错误/加载/空态/树）单份渲染（见 return），模式只换数据与文案。
  const view = mode === 'git'
    ? {
        error: gitQuery.error,
        isLoading: gitQuery.isLoading,
        refetch: gitQuery.refetch,
        errorLabel: '变更列表加载失败',
        isEmpty: gitQuery.data != null && changeFiles.length === 0,
        emptyTitle: '没有未提交变更',
        emptyDesc: '工作区干净——agent 的改动都已 commit',
        tree: (
          <FileTree
            roots={gitRoots}
            expandedDirs={gitExpandedDirs}
            openPaths={openPaths}
            activePath={activePath}
            marks={gitMarks}
            onToggleDir={() => {}} // 目录已全展开，toggle 无操作（展开集是派生全集）
            onOpenFile={path => openPreviewTab(issueId, path)}
          />
        ),
      }
    : {
        error,
        isLoading,
        refetch,
        errorLabel: '文件列表加载失败',
        isEmpty: data != null && treeRoots.length === 0,
        emptyTitle: '工作空间目录为空',
        emptyDesc: '初始化工作空间或等待终端内产出文件后刷新',
        tree: (
          <FileTree
            roots={treeRoots}
            expandedDirs={expandedDirs}
            openPaths={openPaths}
            activePath={activePath}
            onToggleDir={dirPath => toggleDirExpanded(issueId, dirPath)}
            onOpenFile={path => openPreviewTab(issueId, path)}
          />
        ),
      };

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* 头部：左计数；右模式切换（全部文件 / Git 变更）+ 刷新（isFetching 旋转，IssueSubTaskPanel 同款） */}
      <PanelToolbar
        left={(
          <Typography variant="caption" color="text.secondary" noWrap>
            {mode === 'git'
              ? (gitQuery.data != null ? `${changeFiles.length} 个变更` : '')
              : (data != null ? `${fileCount} 个文件${data.truncated ? '（已截断）' : ''}` : '')}
          </Typography>
        )}
        right={(
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
            <ToggleButtonGroup
              size="small"
              exclusive
              value={mode}
              onChange={(_, v) => v != null && setFilePanelMode(issueId, v)}
            >
              <ToggleButton value="all" sx={{ py: 0.25, px: 1 }}>全部文件</ToggleButton>
              <ToggleButton value="git" sx={{ py: 0.25, px: 1 }}>Git 变更</ToggleButton>
            </ToggleButtonGroup>
            <IconButton
              size="small"
              onClick={refresh}
              disabled={mode === 'git' ? gitQuery.isFetching : isFetching}
              aria-label="刷新文件列表"
              sx={{ color: 'text.secondary' }}
            >
              <AutorenewIcon
                fontSize="small"
                sx={{
                  'animation': (mode === 'git' ? gitQuery.isFetching : isFetching) ? 'spin 0.8s linear infinite' : undefined,
                  '@keyframes spin': {
                    from: { transform: 'rotate(0deg)' },
                    to: { transform: 'rotate(360deg)' },
                  },
                }}
              />
            </IconButton>
          </Box>
        )}
      />

      {/* 内容区四分支骨架（错误/加载/空态/树）单份渲染，模式只提供差异描述。 */}
      {view.error != null
        ? (
            <Box sx={{ p: 1.5 }}>
              <Alert
                severity="error"
                action={<Button color="inherit" size="small" onClick={() => void view.refetch()}>重试</Button>}
              >
                {view.errorLabel}：{view.error.message}
              </Alert>
            </Box>
          )
        : view.isLoading
          ? (
              <Box sx={{ display: 'flex', justifyContent: 'center', p: 2 }}>
                <CircularProgress size={20} />
              </Box>
            )
          : view.isEmpty
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
                  <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>{view.emptyTitle}</Typography>
                  <Typography variant="body2" color="text.secondary">{view.emptyDesc}</Typography>
                </Box>
              )
            : view.tree}
    </Box>
  );
}
