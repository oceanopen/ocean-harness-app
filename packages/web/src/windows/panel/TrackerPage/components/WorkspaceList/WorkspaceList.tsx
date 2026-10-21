import type { WorkspaceModel } from '@src/services';
import { AppsOutlined as AppsOutlinedIcon, Autorenew as AutorenewIcon, FolderOutlined as FolderOutlinedIcon } from '@mui/icons-material';
import { Alert, AlertTitle, Box, Button, CircularProgress, IconButton, Tooltip, Typography } from '@mui/material';
import { useWorkspaces } from '@src/state/tracker';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';

const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

interface WorkspaceListProps {
  selectedId: string | null;
  onSelect: (ws: WorkspaceModel) => void;
  // 打开工作空间网格叠层（WorkspacesView，增删改/类型管理入口）。
  onOpenOverlay: () => void;
}

// 工作空间行式列表（嵌于 tracker 左栏，宽 260）：平铺工作空间，点击选中（右栏出任务视图）。
// 空间级 CRUD 不在此处（网格叠层承载），本列表只读 + 刷新/叠层开关工具栏。
function WorkspaceList({ selectedId, onSelect, onOpenOverlay }: WorkspaceListProps) {
  const { t } = useTranslation();
  const { data: workspaces = [], isLoading, isError, isFetching, refetch } = useWorkspaces();

  // 兜底排序（id DESC，新建在前；uuid v7 字典序即时间序）。
  const displayed = useMemo(
    () => [...workspaces].sort((a, b) => (a.id < b.id ? 1 : a.id > b.id ? -1 : 0)),
    [workspaces],
  );

  const ready = !isLoading && !isError;

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 紧凑工具栏：网格叠层开关 + 刷新 */}
      <Box
        sx={{
          p: '12px',
          borderBottom: '1px solid',
          borderColor: 'divider',
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
          justifyContent: 'flex-end',
        }}
      >
        <Tooltip title={t('tracker:workspace.actions.switch')}>
          <IconButton size="small" onClick={onOpenOverlay} aria-label={t('tracker:workspace.actions.switch')}>
            <AppsOutlinedIcon />
          </IconButton>
        </Tooltip>
        <IconButton
          size="small"
          onClick={() => void refetch()}
          disabled={isFetching}
          aria-label={t('tracker:workspace.actions.refresh')}
        >
          <AutorenewIcon
            sx={{
              'animation': isFetching ? 'spin 0.8s linear infinite' : undefined,
              '@keyframes spin': {
                from: { transform: 'rotate(0deg)' },
                to: { transform: 'rotate(360deg)' },
              },
            }}
          />
        </IconButton>
      </Box>

      {/* 内容区 */}
      <Box sx={{ flex: 1, overflow: 'auto' }}>
        {isLoading && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <CircularProgress />
          </Box>
        )}
        {isError && (
          <Box sx={{ p: '12px' }}>
            <Alert
              severity="error"
              action={(
                <Button color="inherit" size="small" onClick={() => refetch()}>
                  {t('tracker:workspace.error.retry')}
                </Button>
              )}
            >
              <AlertTitle>{t('tracker:workspace.error.title')}</AlertTitle>
              {t('tracker:workspace.error.desc')}
            </Alert>
          </Box>
        )}
        {ready && displayed.length === 0 && (
          <Box
            sx={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
              gap: '8px',
              px: '16px',
              py: '24px',
            }}
          >
            <FolderOutlinedIcon sx={{ fontSize: '36px', color: 'text.secondary' }} />
            <Typography variant="body2" sx={{ fontWeight: 600 }}>
              {t('tracker:workspace.empty.title')}
            </Typography>
            <Button
              variant="contained"
              size="small"
              onClick={onOpenOverlay}
              sx={{ mt: '4px' }}
            >
              {t('tracker:workspace.actions.add')}
            </Button>
          </Box>
        )}
        {ready && displayed.length > 0 && (
          <Box sx={{ py: '4px' }}>
            {displayed.map(ws => (
              <WorkspaceRow
                key={ws.id}
                workspace={ws}
                selected={ws.id === selectedId}
                onSelect={onSelect}
              />
            ))}
          </Box>
        )}
      </Box>
    </Box>
  );
}

// 单行：整行可点击选中（onSelect）；左侧 emoji（空则兜底图标）+ 名称/描述。
// 选中行用左侧主色边条 + action.selected 底色标识。
interface WorkspaceRowProps {
  workspace: WorkspaceModel;
  selected: boolean;
  onSelect: (ws: WorkspaceModel) => void;
}

function WorkspaceRow({ workspace, selected, onSelect }: WorkspaceRowProps) {
  const hasDescription = workspace.description.trim().length > 0;
  const emoji = workspace.emoji.trim();

  return (
    <Box
      onClick={() => onSelect(workspace)}
      sx={[
        {
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
          mx: '4px',
          my: '2px',
          px: '8px',
          py: '6px',
          cursor: 'pointer',
          borderRadius: '4px',
          borderLeft: '3px solid',
          borderColor: selected ? 'primary.main' : 'transparent',
          bgcolor: selected ? 'action.selected' : 'transparent',
        },
        // hover 单独成对象：'&:hover' 必须引号，与普通键分离以符合 quote-props consistent。
        { '&:hover': { bgcolor: selected ? 'action.selected' : 'action.hover' } },
      ]}
    >
      <Box sx={{ width: '22px', flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        {emoji
          ? (
              <Typography component="span" sx={{ fontSize: '18px', lineHeight: 1 }}>
                {emoji}
              </Typography>
            )
          : (
              <FolderOutlinedIcon sx={{ fontSize: '18px', color: 'text.disabled' }} />
            )}
      </Box>
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <Typography variant="body2" sx={{ fontWeight: 600, ...truncateSx }} title={workspace.name}>
          {workspace.name}
        </Typography>
        {hasDescription && (
          <Typography variant="caption" component="div" color="text.secondary" sx={truncateSx} title={workspace.description}>
            {workspace.description}
          </Typography>
        )}
      </Box>
    </Box>
  );
}

export default WorkspaceList;
