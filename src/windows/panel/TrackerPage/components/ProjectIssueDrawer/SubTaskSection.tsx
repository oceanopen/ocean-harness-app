import type { ProjectIssueResponseData, ProjectStateModel, WorkspaceProjectModel } from '@src/services';
import { AddOutlined as AddOutlinedIcon, DeleteOutlined as DeleteOutlinedIcon } from '@mui/icons-material';
import { Box, Checkbox, CircularProgress, IconButton, TextField, Tooltip, Typography } from '@mui/material';
import { useToast } from '@src/shared/useToast';
import {
  useCreateProjectIssue,
  useDeleteProjectIssue,
  useProjectIssues,
  useUpdateProjectIssue,
} from '@src/state/tracker';
import { PriorityIcon } from '@src/windows/panel/TrackerPage/components/PriorityIcon';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

interface SubTaskSectionProps {
  // 父 issue（须为顶级，parentId===0；调用方负责保证）。
  parentIssue: ProjectIssueResponseData;
  workspaceProject: WorkspaceProjectModel;
  projectStates: ProjectStateModel[];
  // 点击子任务标题：由父抽屉打开嵌套抽屉（避免与本组件循环依赖）。
  onOpenChild: (child: ProjectIssueResponseData) => void;
}

// 父 issue 详情抽屉内的「子任务」section（限一层）：从 useProjectIssues 扁平缓存派生子任务，
// 提供快速输入框（回车即建，继承父 project + state）、勾选完成、删除、点标题打开嵌套抽屉。
// 创建/更新/删除均复用现有 mutation（内部 invalidate 项目 issue 缓存 → 本 section 与列表自动刷新）。
function SubTaskSection({ parentIssue, workspaceProject, projectStates, onOpenChild }: SubTaskSectionProps) {
  const { t } = useTranslation();
  const { show: showToast } = useToast();
  const { data: allIssues = [] } = useProjectIssues(workspaceProject.id);
  const createProjectIssue = useCreateProjectIssue(workspaceProject.id);
  const updateProjectIssue = useUpdateProjectIssue(workspaceProject.id);
  const deleteProjectIssue = useDeleteProjectIssue(workspaceProject.id);

  // 项目首个 completed 组状态（无则 0：勾选完成按钮禁用）。
  const completedStateId = useMemo(
    () => projectStates.find(s => s.stateGroup === 'completed')?.id ?? 0,
    [projectStates],
  );

  const children = useMemo(
    () => allIssues
      .filter(i => i.parentId === parentIssue.id)
      .sort((a, b) => a.sortOrder - b.sortOrder),
    [allIssues, parentIssue.id],
  );
  const doneCount = useMemo(() => children.filter(c => c.completedAt).length, [children]);

  const [input, setInput] = useState('');

  const handleAdd = async () => {
    const trimmed = input.trim();
    if (!trimmed) {
      return;
    }
    try {
      await createProjectIssue.mutateAsync({
        projectId: workspaceProject.id,
        workspaceId: workspaceProject.workspaceId,
        name: trimmed,
        stateId: parentIssue.stateId, // 继承父当前状态
        parentId: parentIssue.id,
      });
      setInput('');
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:projectIssue.toast.createFailed', { message: msg }), 'error');
    }
  };

  // 勾选切换：完成 → 项目首个 completed 组状态；取消完成 → 回到父当前状态。
  const toggleDone = async (child: ProjectIssueResponseData) => {
    const targetStateId = child.completedAt ? parentIssue.stateId : completedStateId;
    if (!targetStateId) {
      return; // 无 completed 组状态，无法标记完成
    }
    try {
      await updateProjectIssue.mutateAsync({
        id: child.id,
        name: child.name,
        description: child.description,
        stateId: targetStateId,
        priority: child.priority,
        startDate: child.startDate,
        targetDate: child.targetDate,
        labelIds: child.labels.map(l => l.id),
      });
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:projectIssue.toast.updateFailed', { message: msg }), 'error');
    }
  };

  const handleDelete = async (id: number) => {
    try {
      await deleteProjectIssue.mutateAsync(id);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(t('tracker:projectIssue.toast.deleteFailed', { message: msg }), 'error');
    }
  };

  const busy = createProjectIssue.isPending;

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
      <Typography variant="caption" color="text.secondary">
        {t('tracker:projectIssue.detail.subtasks.title')}
        {children.length > 0 && ` ${doneCount}/${children.length}`}
      </Typography>

      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
        <TextField
          value={input}
          onChange={e => setInput(e.target.value)}
          placeholder={t('tracker:projectIssue.detail.subtasks.addPlaceholder')}
          size="small"
          fullWidth
          disabled={busy}
          slotProps={{ htmlInput: { maxLength: 255 } }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              void handleAdd();
            }
          }}
        />
        <IconButton
          size="small"
          color="primary"
          disabled={busy || input.trim().length === 0}
          onClick={() => void handleAdd()}
          aria-label={t('tracker:projectIssue.detail.subtasks.title')}
        >
          {busy ? <CircularProgress size={16} /> : <AddOutlinedIcon fontSize="small" />}
        </IconButton>
      </Box>

      {children.length === 0
        ? (
            <Typography variant="caption" color="text.disabled">
              {t('tracker:projectIssue.detail.subtasks.empty')}
            </Typography>
          )
        : (
            children.map((child) => {
              const done = !!child.completedAt;
              return (
                <Box
                  key={child.id}
                  sx={{ display: 'flex', alignItems: 'center', gap: 1, py: 0.25, minHeight: 34 }}
                >
                  <Tooltip title={done ? t('tracker:projectIssue.group.completed') : t('tracker:projectIssue.detail.subtasks.title')}>
                    <Checkbox
                      size="small"
                      checked={done}
                      disabled={!done && completedStateId === 0}
                      onChange={() => void toggleDone(child)}
                    />
                  </Tooltip>
                  <Typography variant="caption" color="text.disabled" sx={{ flexShrink: 0 }}>#{child.id}</Typography>
                  <Typography
                    variant="body2"
                    onClick={() => onOpenChild(child)}
                    title={child.name}
                    sx={{
                      flex: 1,
                      minWidth: 0,
                      cursor: 'pointer',
                      textDecoration: done ? 'line-through' : 'none',
                      color: done ? 'text.disabled' : 'text.primary',
                      ...truncateSx,
                    }}
                  >
                    {child.name}
                  </Typography>
                  <PriorityIcon priority={child.priority} />
                  <Tooltip title={t('tracker:projectIssue.detail.delete')}>
                    <IconButton size="small" onClick={() => void handleDelete(child.id)}>
                      <DeleteOutlinedIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                </Box>
              );
            })
          )}
    </Box>
  );
}

export default SubTaskSection;
