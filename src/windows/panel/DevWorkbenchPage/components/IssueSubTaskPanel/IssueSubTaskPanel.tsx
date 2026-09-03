import type { ProjectIssueResponseData } from '@src/services';
import type { StateCode } from '@src/state/tracker/stateMeta';
import {
  Autorenew as AutorenewIcon,
  CheckCircle as CheckCircleIcon,
  ChecklistOutlined as ChecklistOutlinedIcon,
  RadioButtonUnchecked as RadioButtonUncheckedIcon,
  RemoveCircleOutlined as RemoveCircleOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  IconButton,
  List,
  ListItem,
  Typography,
} from '@mui/material';
import { filterIssueSubTasks } from '@src/state/devWorkbench';
import { trackerKeys, useProjectIssues } from '@src/state/tracker';
import { useQueryClient } from '@tanstack/react-query';
import { useMemo } from 'react';

/** 子任务状态图标（参照 WorkspaceInitGate StepStatusIcon 风格）：待办（含待办池）灰圈 / 进行中转圈 / 已完成绿勾 / 已取消灰杠。 */
function SubtaskStateIcon({ stateCode }: { stateCode: StateCode }) {
  switch (stateCode) {
    case 'IN_PROGRESS':
      return <CircularProgress size={16} />;
    case 'DONE':
      return <CheckCircleIcon sx={{ fontSize: 18, color: 'success.main' }} />;
    case 'CANCELLED':
      return <RemoveCircleOutlinedIcon sx={{ fontSize: 18, color: 'text.disabled' }} />;
    default:
      return <RadioButtonUncheckedIcon sx={{ fontSize: 18, color: 'text.disabled' }} />;
  }
}

/** 子任务行：序号 + 状态图标 + 标题（noWrap）。本期纯展示不可点（子任务与终端会话无映射）。 */
function SubtaskRow({ index, subtask }: { index: number; subtask: ProjectIssueResponseData }) {
  const cancelled = subtask.stateCode === 'CANCELLED';
  return (
    <ListItem disableGutters sx={{ py: 0.25, px: 1.5 }}>
      <Typography variant="caption" color="text.disabled" sx={{ width: 18, flexShrink: 0, textAlign: 'right', mr: 1 }}>
        {index}
      </Typography>
      <Box sx={{ width: 18, height: 18, flexShrink: 0, mr: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <SubtaskStateIcon stateCode={subtask.stateCode} />
      </Box>
      <Typography
        variant="body2"
        noWrap
        title={subtask.name}
        sx={{ color: cancelled ? 'text.disabled' : 'text.primary', textDecoration: cancelled ? 'line-through' : 'none' }}
      >
        {subtask.name}
      </Typography>
    </ListItem>
  );
}

interface IssueSubTaskPanelProps {
  projectId: number;
  issueId: string;
}

/**
 * IssueSubTaskPanel：开发工作台工具面板区的「子任务」tab 内容（T3.1，经 toolRegistry 挂载）。
 * 展示当前 issue 的子任务清单（parentId 指向该 issue 的子 issue，sortOrder 升序）与状态图标，
 * 供用户在终端跑 agent-dev 时旁观执行进度。本期纯展示（子任务与终端会话无映射，行不可点）。
 * 面板标题由 tab 头承载（「子任务」），本组件头部仅留完成进度 + 刷新按钮。
 *
 * 数据复用 tracker 缓存（与左树/顶栏同 query key 共享，零新增请求）；实时性靠头部刷新按钮
 * 手动 invalidate——agent-dev 经 MCP 写库后后端不推事件，自动刷新待后续单独方案。
 * 挂载方保证选中 issue 有效，projectId/issueId 非空。
 */
export default function IssueSubTaskPanel({ projectId, issueId }: IssueSubTaskPanelProps) {
  const qc = useQueryClient();
  const { data: issues = [], isLoading, error, isFetching } = useProjectIssues(projectId);
  const subTasks = useMemo(() => filterIssueSubTasks(issues, issueId), [issues, issueId]);
  const doneCount = subTasks.filter(t => t.stateCode === 'DONE').length;

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: trackerKeys.projectIssues(projectId) });
  };

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* 头部：完成进度 + 刷新按钮（标题「子任务」由外层 tab 头承载；isFetching 旋转，
          参照 ProjectIssueList 刷新先例） */}
      <Box
        sx={{
          height: 40,
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
          gap: 0.75,
          px: 1.5,
          borderBottom: 1,
          borderColor: 'divider',
        }}
      >
        {subTasks.length > 0
          ? (
              <Typography variant="caption" color="text.secondary">
                完成
                {doneCount}/{subTasks.length}
              </Typography>
            )
          : null}
        <IconButton
          size="small"
          onClick={refresh}
          disabled={isFetching}
          aria-label="刷新子任务状态"
          sx={{ ml: 'auto', color: 'text.secondary' }}
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

      {/* 内容区：错误 / 加载中 / 空态引导 / 列表 */}
      {error
        ? (
            <Box sx={{ p: 1.5 }}>
              <Alert
                severity="error"
                action={<Button color="inherit" size="small" onClick={refresh}>重试</Button>}
              >
                子任务查询失败：{error.message}
              </Alert>
            </Box>
          )
        : isLoading
          ? (
              <Box sx={{ display: 'flex', justifyContent: 'center', p: 2 }}>
                <CircularProgress size={20} />
              </Box>
            )
          : subTasks.length === 0
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
                  <ChecklistOutlinedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />
                  <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>暂无子任务</Typography>
                  <Typography variant="body2" color="text.secondary">
                    在终端运行
                    {' '}
                    <Box component="span" sx={{ fontFamily: 'monospace' }}>/ocean-harness:refine-issue</Box>
                    {' '}
                    拆分子任务
                  </Typography>
                </Box>
              )
            : (
                <List dense disablePadding sx={{ flex: 1, overflow: 'auto', py: 0.5 }}>
                  {subTasks.map((task, i) => <SubtaskRow key={task.id} index={i + 1} subtask={task} />)}
                </List>
              )}
    </Box>
  );
}
