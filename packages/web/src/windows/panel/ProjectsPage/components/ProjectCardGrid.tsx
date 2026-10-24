import type { ProjectModel } from '@src/services';
import {
  CheckOutlined as CheckOutlinedIcon,
  DeleteOutlined as DeleteOutlinedIcon,
  EditOutlined as EditOutlinedIcon,
  FolderOutlined as FolderOutlinedIcon,
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
import { useDeleteProject } from '@src/state/projects';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ProjectDrawer from './ProjectDrawer';

const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

interface ProjectCardGridProps {
  // 列表数据（组件内统一排序）；loading = 列表请求在途（spinner 占位）。
  projects: ProjectModel[];
  loading: boolean;
  // 已选项目 id（卡片常驻高亮；选中态 SSOT 在页面 URL pid）。
  selectedProjectId: string | null;
  // 卡片点击选中上抛（页面写 pid 清 tid；任务选择经左树完成）。
  onSelectProject: (project: ProjectModel) => void;
}

/**
 * 项目卡片网格：页面未选中/未选任务态直出（复刻旧工作空间卡片形态）。
 * 整卡点击选中项目（左树随之展开高亮）；hover 显编辑/删除（抽屉与确认弹窗自持）。
 */
function ProjectCardGrid({ projects, loading, selectedProjectId, onSelectProject }: ProjectCardGridProps) {
  const { show: showToast, snack } = useToast();
  const deleteProject = useDeleteProject();
  const [editTarget, setEditTarget] = useState<ProjectModel | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ProjectModel | null>(null);
  const [deleting, setDeleting] = useState(false);

  // 兜底排序（uuid v7 字典序 DESC，新建在前；与左树同口径）。
  const sorted = useMemo(
    () => [...projects].sort((a, b) => (a.id < b.id ? 1 : a.id > b.id ? -1 : 0)),
    [projects],
  );

  const handleConfirmDelete = async () => {
    if (deleteTarget == null || deleting) {
      return;
    }
    setDeleting(true);
    try {
      await deleteProject.mutateAsync({ id: deleteTarget.id });
      showToast('项目已删除', 'success');
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
          <FolderOutlinedIcon sx={{ fontSize: '48px', color: 'text.secondary' }} />
          <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>暂无项目</Typography>
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
          {sorted.map(project => (
            <ProjectCard
              key={project.id}
              project={project}
              selected={project.id === selectedProjectId}
              onSelect={onSelectProject}
              onEdit={setEditTarget}
              onDelete={setDeleteTarget}
            />
          ))}
        </Box>
      )}
      {snack}

      {editTarget != null && (
        <ProjectDrawer
          project={editTarget}
          onClose={() => setEditTarget(null)}
          onUpdated={project => showToast(`项目「${project.name}」已保存`, 'success')}
        />
      )}

      <Dialog open={deleteTarget != null} onClose={deleting ? undefined : () => setDeleteTarget(null)}>
        <DialogTitle>删除项目</DialogTitle>
        <DialogContent>
          <Typography>确认删除项目「{deleteTarget?.name ?? ''}」？其下任务/会话/类型与仓库关联将一并移除，此操作不可恢复。</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDeleteTarget(null)} disabled={deleting}>取消</Button>
          <Button color="error" variant="contained" onClick={() => void handleConfirmDelete()} disabled={deleting}>删除</Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

// 单卡片：整卡点击选中项目；已选卡片边框常亮 + 对勾；hover 显编辑/删除并加深阴影。
interface ProjectCardProps {
  project: ProjectModel;
  selected: boolean;
  onSelect: (project: ProjectModel) => void;
  onEdit: (project: ProjectModel) => void;
  onDelete: (project: ProjectModel) => void;
}

function ProjectCard({ project, selected, onSelect, onEdit, onDelete }: ProjectCardProps) {
  const { t } = useTranslation();
  const hasDescription = project.description.trim().length > 0;

  return (
    <Card
      variant="outlined"
      onClick={() => onSelect(project)}
      sx={{
        'height': '100%',
        'display': 'flex',
        'flexDirection': 'column',
        'cursor': 'pointer',
        'borderColor': selected ? 'primary.main' : undefined,
        '& .card-actions': { opacity: 0 },
        '&:hover': { boxShadow: 4, borderColor: 'primary.main' },
        '&:hover .card-actions': { opacity: 1 },
      }}
    >
      <CardHeader
        title={(
          <Box component="span" sx={{ display: 'inline-flex', alignItems: 'center', gap: '4px' }}>
            {project.emoji.trim()
              ? <Box component="span" sx={{ fontSize: '16px', lineHeight: 1 }}>{project.emoji}</Box>
              : <FolderOutlinedIcon sx={{ fontSize: '18px', color: 'text.secondary' }} />}
            {project.name}
            {selected && (
              <CheckOutlinedIcon color="primary" sx={{ fontSize: '18px' }} aria-label="当前项目" />
            )}
          </Box>
        )}
        slotProps={{ title: { fontWeight: 600, noWrap: true } }}
        action={(
          <Box className="card-actions" sx={{ display: 'flex', gap: '4px' }}>
            <IconButton
              size="small"
              aria-label="编辑项目"
              onClick={(e) => {
                e.stopPropagation();
                onEdit(project);
              }}
              sx={{ '& svg': { fontSize: '16px' } }}
            >
              <EditOutlinedIcon />
            </IconButton>
            <IconButton
              size="small"
              aria-label="删除项目"
              onClick={(e) => {
                e.stopPropagation();
                onDelete(project);
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
        <Typography variant="body2" color="text.secondary" sx={{ mb: '8px', ...truncateSx }} title={project.dir}>
          {`目录：${project.dir}`}
        </Typography>
        <Typography
          variant="body2"
          color={hasDescription ? 'text.secondary' : 'text.disabled'}
          sx={{ mb: '8px', minHeight: '1.5em', ...truncateSx }}
          title={hasDescription ? project.description : undefined}
        >
          {hasDescription ? project.description : '暂无描述'}
        </Typography>
        <Typography variant="body2" color="text.disabled" title={formatDate(project.updatedAt, 'YYYY-MM-DD HH:mm:ss')}>
          {`更新于 ${formatRelativeTime(new Date(project.updatedAt).getTime(), t, 'tracker')}`}
        </Typography>
      </CardContent>
    </Card>
  );
}

export default ProjectCardGrid;
