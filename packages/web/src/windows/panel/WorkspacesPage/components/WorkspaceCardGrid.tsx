import type { WorkspaceModel } from '@src/services';
import {
  DeleteOutlined as DeleteOutlinedIcon,
  EditOutlined as EditOutlinedIcon,
  FolderOutlined as FolderOutlinedIcon,
  WorkspacesOutlined as WorkspacesOutlinedIcon,
} from '@mui/icons-material';
import {
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  IconButton,
  Typography,
} from '@mui/material';
import { formatDate, formatRelativeTime } from '@src/shared/time';
import { useToast } from '@src/shared/useToast';
import { useDeleteWorkspace } from '@src/state/tracker';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import WorkspaceDrawer from './WorkspaceDrawer';

const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

interface WorkspaceCardGridProps {
  // 列表数据（组件内统一排序）；loading = 列表请求在途（spinner 占位）。
  workspaces: WorkspaceModel[];
  loading: boolean;
  // 卡片点击选中上抛（页面写 wid 切入工作台内容区）。
  onSelectWorkspace: (ws: WorkspaceModel) => void;
}

/**
 * 工作空间卡片网格：页面未选中态直出（复刻旧 WorkspacesView 卡片形态）。
 * 整卡点击选中进入；hover 显编辑/删除（抽屉与确认弹窗自持）；空态居中引导新建。
 */
function WorkspaceCardGrid({ workspaces, loading, onSelectWorkspace }: WorkspaceCardGridProps) {
  const { show: showToast, snack } = useToast();
  const deleteWs = useDeleteWorkspace();
  const [editTarget, setEditTarget] = useState<WorkspaceModel | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<WorkspaceModel | null>(null);
  const [deleting, setDeleting] = useState(false);

  // 兜底排序（uuid v7 字典序 DESC，新建在前；与左树同口径）。
  const sorted = useMemo(
    () => [...workspaces].sort((a, b) => (a.id < b.id ? 1 : a.id > b.id ? -1 : 0)),
    [workspaces],
  );

  const handleConfirmDelete = async () => {
    if (deleteTarget == null || deleting) {
      return;
    }
    setDeleting(true);
    try {
      await deleteWs.mutateAsync(deleteTarget.id);
      showToast('工作空间已删除', 'success');
      setDeleteTarget(null);
    } catch (e) {
      showToast(`删除失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Box sx={{ height: '100%', overflow: 'auto' }}>
      {loading && (
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
          <CircularProgress />
        </Box>
      )}
      {!loading && sorted.length === 0 && (
        <Box
          sx={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            height: '100%',
            gap: '12px',
            px: '24px',
            py: '32px',
          }}
        >
          <WorkspacesOutlinedIcon sx={{ fontSize: '48px', color: 'text.secondary' }} />
          <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>暂无工作空间</Typography>
          <Typography variant="body2" color="text.secondary" align="center">点击左上角工具栏「+」新建</Typography>
        </Box>
      )}
      {!loading && sorted.length > 0 && (
        <Box
          sx={{
            p: '16px',
            display: 'grid',
            gap: '16px',
            gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
            alignItems: 'start',
          }}
        >
          {sorted.map(ws => (
            <WorkspaceCard
              key={ws.id}
              ws={ws}
              onSelect={onSelectWorkspace}
              onEdit={setEditTarget}
              onDelete={setDeleteTarget}
            />
          ))}
        </Box>
      )}
      {snack}

      {editTarget != null && (
        <WorkspaceDrawer
          workspace={editTarget}
          onClose={() => setEditTarget(null)}
          onCreated={ws => showToast(`工作空间「${ws.name}」创建成功`, 'success')}
          onUpdated={ws => showToast(`工作空间「${ws.name}」已保存`, 'success')}
        />
      )}

      <Dialog open={deleteTarget != null} onClose={deleting ? undefined : () => setDeleteTarget(null)}>
        <DialogTitle>删除工作空间</DialogTitle>
        <DialogContent>
          <Typography>确认删除工作空间「{deleteTarget?.name ?? ''}」？此操作不可恢复。</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDeleteTarget(null)} disabled={deleting}>取消</Button>
          <Button color="error" variant="contained" onClick={() => void handleConfirmDelete()} disabled={deleting}>删除</Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

// 单卡片：整卡点击选中；hover 显编辑/删除并高亮边框；Header 图标 emoji 优先（空串 = Folder 兜底）。
interface WorkspaceCardProps {
  ws: WorkspaceModel;
  onSelect: (ws: WorkspaceModel) => void;
  onEdit: (ws: WorkspaceModel) => void;
  onDelete: (ws: WorkspaceModel) => void;
}

function WorkspaceCard({ ws, onSelect, onEdit, onDelete }: WorkspaceCardProps) {
  const { t } = useTranslation();
  const hasDescription = ws.description.trim().length > 0;

  return (
    <Card
      variant="outlined"
      onClick={() => onSelect(ws)}
      sx={{
        'height': '100%',
        'display': 'flex',
        'flexDirection': 'column',
        'cursor': 'pointer',
        '& .card-actions': { opacity: 0 },
        '&:hover': { boxShadow: 4, borderColor: 'primary.main' },
        '&:hover .card-actions': { opacity: 1 },
      }}
    >
      <CardHeader
        title={(
          <Box component="span" sx={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
            {ws.emoji.trim()
              ? <Box component="span" sx={{ fontSize: '16px', lineHeight: 1 }}>{ws.emoji}</Box>
              : <FolderOutlinedIcon sx={{ fontSize: '18px', color: 'text.secondary' }} />}
            {ws.name}
          </Box>
        )}
        slotProps={{ title: { fontWeight: 600, noWrap: true } }}
        action={(
          <Box className="card-actions" sx={{ display: 'flex', gap: '4px' }}>
            <IconButton
              size="small"
              aria-label="编辑工作空间"
              onClick={(e) => {
                e.stopPropagation();
                onEdit(ws);
              }}
              sx={{ '& svg': { fontSize: '16px' } }}
            >
              <EditOutlinedIcon />
            </IconButton>
            <IconButton
              size="small"
              aria-label="删除工作空间"
              onClick={(e) => {
                e.stopPropagation();
                onDelete(ws);
              }}
              sx={{ 'color': 'text.secondary', '& svg': { fontSize: '16px' } }}
            >
              <DeleteOutlinedIcon />
            </IconButton>
          </Box>
        )}
        sx={{ '& .MuiCardHeader-action': { alignSelf: 'center', mt: '0px' } }}
      />
      <Divider />
      <CardContent sx={{ flex: 1 }}>
        <Typography variant="body2" color="text.secondary" sx={{ mb: '8px', ...truncateSx }} title={ws.dir}>
          {`目录：${ws.dir}`}
        </Typography>
        <Typography
          variant="body2"
          color={hasDescription ? 'text.secondary' : 'text.disabled'}
          sx={{ mb: '8px', minHeight: '1.5em', ...truncateSx }}
          title={hasDescription ? ws.description : undefined}
        >
          {hasDescription ? ws.description : '暂无描述'}
        </Typography>
        <Typography variant="body2" color="text.disabled" title={formatDate(ws.updatedAt, 'YYYY-MM-DD HH:mm:ss')}>
          {`更新于 ${formatRelativeTime(new Date(ws.updatedAt).getTime(), t, 'tracker')}`}
        </Typography>
      </CardContent>
    </Card>
  );
}

export default WorkspaceCardGrid;
