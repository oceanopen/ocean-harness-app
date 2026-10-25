import type { ProjectModel, ProjectTaskResponseData } from '@src/services';
import {
  AddOutlined as AddOutlinedIcon,
  AppsOutlined as AppsOutlinedIcon,
  FolderOutlined as FolderOutlinedIcon,
} from '@mui/icons-material';
import {
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Menu,
  MenuItem,
  Typography,
} from '@mui/material';
import TreeRow from '@src/components/conversationTree/TreeRow';
import { useToast } from '@src/shared/useToast';
import { useProjects } from '@src/state/projects';
import { useDeleteProjectTask, useProjectTasks } from '@src/state/tracker';
import { useMemo, useState } from 'react';
import ProjectDrawer from './ProjectDrawer';
import ProjectTaskDrawer from './ProjectTaskDrawer';

interface ProjectTreeProps {
  // 选中任务所属项目 id（容器高亮）；选中任务 id（任务行高亮）。选中态 SSOT 在页面 URL。
  selectedProjectId: string | null;
  selectedTaskId: string | null;
  // 「列表」开关（SSOT 在页面：驱动内容区项目卡片网格，树上只做高亮与上抛）。
  listOpen: boolean;
  onToggleList: () => void;
  onSelectTask: (projectId: string, taskId: string) => void;
  onSelectProject: (projectId: string) => void;
}

// 抽屉态（条件挂载，每次打开新挂载）：项目新建/编辑 + 任务新建/编辑。
type DrawerState
  = { kind: 'project-create' }
    | { kind: 'project-edit'; project: ProjectModel }
    | { kind: 'task-create'; project: ProjectModel }
    | { kind: 'task-edit'; project: ProjectModel; task: ProjectTaskResponseData };

/**
 * 项目任务页左树（与工作空间页左树同构）：顶部「新建/列表」工具栏（列表开关上抛页面
 * 驱动内容区卡片网格）+ 两级平铺树（项目节点 → 任务行，任务即叶子——内容区恒由任务
 * 主会话编排承载）。项目节点「...」= 编辑项目/新建任务；任务节点「...」= 编辑/删除任务。
 */
function ProjectTree({ selectedProjectId, selectedTaskId, listOpen, onToggleList, onSelectTask, onSelectProject }: ProjectTreeProps) {
  const { data: projects = [], isLoading } = useProjects();
  const { show: showToast, snack } = useToast();
  // 折叠集合（默认全展开）：仅记录用户显式收起的项目节点。
  const [collapsedProjectIds, setCollapsedProjectIds] = useState<Set<string>>(() => new Set());
  const [drawer, setDrawer] = useState<DrawerState | null>(null);

  // 兜底排序（uuid v7 字典序 DESC，新建在前；与工作空间页同口径）。
  const sorted = useMemo(
    () => [...projects].sort((a, b) => (a.id < b.id ? 1 : a.id > b.id ? -1 : 0)),
    [projects],
  );

  const toggleProjectCollapsed = (id: string) => {
    setCollapsedProjectIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  // 选中项目变化即展开该节点（卡片选中联动；props 变化调整 state 的 render 期模式，
  // 选中后手动收起不被覆盖）。默认全展开，集合仅存显式收起项。
  const [lastSelectedProjectId, setLastSelectedProjectId] = useState(selectedProjectId);
  if (lastSelectedProjectId !== selectedProjectId) {
    setLastSelectedProjectId(selectedProjectId);
    if (selectedProjectId != null) {
      setCollapsedProjectIds((prev) => {
        if (!prev.has(selectedProjectId)) {
          return prev;
        }
        const next = new Set(prev);
        next.delete(selectedProjectId);
        return next;
      });
    }
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      {/* 工具栏：计数 + 新建项目 + 列表浮层开关 */}
      <Box
        sx={{
          height: '40px',
          flexShrink: 0,
          px: '8px',
          borderBottom: '1px solid',
          borderColor: 'divider',
          display: 'flex',
          alignItems: 'center',
          gap: '4px',
        }}
      >
        <Typography variant="caption" color="text.secondary" sx={{ flex: 1 }}>
          {`共 ${projects.length} 个项目`}
        </Typography>
        <IconButton size="small" onClick={() => setDrawer({ kind: 'project-create' })} aria-label="新建项目">
          <AddOutlinedIcon sx={{ fontSize: '18px' }} />
        </IconButton>
        <IconButton
          size="small"
          color={listOpen ? 'primary' : 'default'}
          onClick={onToggleList}
          aria-label={listOpen ? '收起列表' : '项目列表'}
        >
          <AppsOutlinedIcon sx={{ fontSize: '18px' }} />
        </IconButton>
      </Box>

      {/* 树体 */}
      <Box sx={{ flex: 1, minHeight: '0px' }}>
        <Box sx={{ height: '100%', overflow: 'auto', py: '4px' }}>
          {isLoading && projects.length === 0 && (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: '24px' }}>
              <CircularProgress size={20} />
            </Box>
          )}
          {!isLoading && projects.length === 0 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '8px', px: '16px', py: '32px' }}>
              <FolderOutlinedIcon sx={{ fontSize: '36px', color: 'text.secondary' }} />
              <Typography variant="body2" color="text.secondary" align="center">暂无项目</Typography>
              <Button variant="contained" size="small" startIcon={<AddOutlinedIcon />} onClick={() => setDrawer({ kind: 'project-create' })}>
                新建项目
              </Button>
            </Box>
          )}
          {sorted.map(project => (
            <ProjectNode
              key={project.id}
              project={project}
              selected={project.id === selectedProjectId && selectedTaskId == null}
              selectedTaskId={selectedTaskId}
              collapsed={collapsedProjectIds.has(project.id)}
              onSelectTask={onSelectTask}
              onSelectProject={onSelectProject}
              onToggleCollapsed={() => toggleProjectCollapsed(project.id)}
              onEdit={() => setDrawer({ kind: 'project-edit', project })}
              onCreateTask={() => setDrawer({ kind: 'task-create', project })}
              onEditTask={task => setDrawer({ kind: 'task-edit', project, task })}
            />
          ))}
        </Box>
      </Box>

      {snack}

      {drawer?.kind === 'project-create' && (
        <ProjectDrawer
          onClose={() => setDrawer(null)}
          onCreated={project => showToast(`项目「${project.name}」创建成功`, 'success')}
        />
      )}
      {drawer?.kind === 'project-edit' && (
        <ProjectDrawer
          project={drawer.project}
          onClose={() => setDrawer(null)}
          onUpdated={project => showToast(`项目「${project.name}」已保存`, 'success')}
        />
      )}
      {drawer?.kind === 'task-create' && (
        <ProjectTaskDrawer
          mode="create"
          project={drawer.project}
          onClose={() => setDrawer(null)}
          onCreated={(task) => {
            showToast(`任务「${task.name}」创建成功`, 'success');
            onSelectTask(drawer.project.id, task.id);
          }}
        />
      )}
      {drawer?.kind === 'task-edit' && (
        <ProjectTaskDrawer
          mode="edit"
          project={drawer.project}
          task={drawer.task}
          onClose={() => setDrawer(null)}
          onUpdated={() => showToast('任务已保存', 'success')}
          onDeleted={() => showToast('任务已删除', 'success')}
        />
      )}
    </Box>
  );
}

// 项目节点：标题行（折叠箭头 + 图标 + 名称 + hover「...」）+ 展开时的任务行列表。
interface ProjectNodeProps {
  project: ProjectModel;
  selected: boolean;
  selectedTaskId: string | null;
  collapsed: boolean;
  onSelectTask: (projectId: string, taskId: string) => void;
  onSelectProject: (projectId: string) => void;
  onToggleCollapsed: () => void;
  onEdit: () => void;
  onCreateTask: () => void;
  onEditTask: (task: ProjectTaskResponseData) => void;
}

function ProjectNode({
  project,
  selected,
  selectedTaskId,
  collapsed,
  onSelectTask,
  onSelectProject,
  onToggleCollapsed,
  onEdit,
  onCreateTask,
  onEditTask,
}: ProjectNodeProps) {
  const { data: taskList = [] } = useProjectTasks(project.id);
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);
  const emoji = project.emoji.trim();

  // 任务排序：created_at 升序（O9 契约；客户端重排，不动共享缓存）。
  const tasks = useMemo(
    () => [...taskList].sort((a, b) => (a.createdAt < b.createdAt ? -1 : a.createdAt > b.createdAt ? 1 : 0)),
    [taskList],
  );

  return (
    <Box>
      <TreeRow
        variant="node"
        title={project.name}
        selected={selected}
        onClick={() => onSelectProject(project.id)}
        collapsed={collapsed}
        icon={emoji
          ? <Typography component="span" sx={{ fontSize: '14px', lineHeight: 1 }}>{emoji}</Typography>
          : <FolderOutlinedIcon sx={{ fontSize: '16px', color: 'text.disabled' }} />}
        onToggleCollapsed={onToggleCollapsed}
        moreLabel="项目操作"
        onMore={setMenuAnchor}
      />
      {/* 项目节点「...」菜单：编辑项目 / 新建任务 */}
      <Menu anchorEl={menuAnchor} open={menuAnchor != null} onClose={() => setMenuAnchor(null)}>
        <MenuItem
          onClick={() => {
            setMenuAnchor(null);
            onEdit();
          }}
        >
          编辑项目…
        </MenuItem>
        <MenuItem
          onClick={() => {
            setMenuAnchor(null);
            onCreateTask();
          }}
        >
          新建任务
        </MenuItem>
      </Menu>
      {!collapsed && tasks.map(task => (
        <TaskNode
          key={task.id}
          project={project}
          task={task}
          selected={task.id === selectedTaskId}
          onSelect={() => onSelectTask(project.id, task.id)}
          onEdit={() => onEditTask(task)}
        />
      ))}
    </Box>
  );
}

// 任务节点（叶子，任务即最后一级）：任务行（类型色点 + 名称 + hover「...」）；删除自持（mutation 随节点）。
interface TaskNodeProps {
  project: ProjectModel;
  task: ProjectTaskResponseData;
  selected: boolean;
  onSelect: () => void;
  onEdit: () => void;
}

function TaskNode({ project, task, selected, onSelect, onEdit }: TaskNodeProps) {
  const { show: showToast, snack } = useToast();
  const deleteTask = useDeleteProjectTask(project.id);
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const handleDelete = async () => {
    if (deleting) {
      return;
    }
    setDeleting(true);
    try {
      await deleteTask.mutateAsync(task.id);
      showToast('任务已删除', 'success');
      setDeleteOpen(false);
    } catch (e) {
      showToast(`删除失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Box>
      <TreeRow
        variant="leaf"
        title={task.name}
        selected={selected}
        onClick={onSelect}
        leadingSlot={<Box sx={{ width: '8px', height: '8px', borderRadius: '50%', bgcolor: task.type?.color || 'text.disabled', flexShrink: 0 }} />}
        moreLabel="任务操作"
        onMore={setMenuAnchor}
      />
      {/* 任务节点「...」菜单：编辑任务 / 删除任务 */}
      <Menu anchorEl={menuAnchor} open={menuAnchor != null} onClose={() => setMenuAnchor(null)}>
        <MenuItem
          onClick={() => {
            setMenuAnchor(null);
            onEdit();
          }}
        >
          编辑任务…
        </MenuItem>
        <MenuItem
          onClick={() => {
            setMenuAnchor(null);
            setDeleteOpen(true);
          }}
        >
          删除…
        </MenuItem>
      </Menu>
      {snack}

      <Dialog open={deleteOpen} onClose={deleting ? undefined : () => setDeleteOpen(false)}>
        <DialogTitle>删除任务</DialogTitle>
        <DialogContent>
          <Typography>确认删除任务「{task.name}」？其工作空间目录与会话记录将一并移除，此操作不可恢复。</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDeleteOpen(false)} disabled={deleting}>取消</Button>
          <Button color="error" variant="contained" onClick={() => void handleDelete()} disabled={deleting}>删除</Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

export default ProjectTree;
