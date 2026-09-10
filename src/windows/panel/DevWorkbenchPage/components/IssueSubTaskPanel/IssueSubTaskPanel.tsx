import {
  Autorenew as AutorenewIcon,
  ChecklistOutlined as ChecklistOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  IconButton,
  Typography,
} from '@mui/material';
import IssueCard from '@src/components/issueCard/IssueCard';
import { filterIssueSubTasks } from '@src/state/devWorkbench';
import { trackerKeys, useProjectIssues } from '@src/state/tracker';
import { useQueryClient } from '@tanstack/react-query';
import { useMemo } from 'react';
import PanelToolbar from '../PanelToolbar';

interface IssueSubTaskPanelProps {
  projectId: number;
  issueId: string;
}

/**
 * IssueSubTaskPanel：开发工作台工具面板区的「子任务」tab 内容（T3.1，经 toolRegistry 挂载）。
 * 展示当前 issue 的子任务清单（parentId 指向该 issue 的子 issue，sortOrder 升序），
 * 复用共享 IssueCard 子卡样式（depth=1 轻量卡，徽章/日期/标签齐全，不传回调即纯展示——
 * 子任务与终端会话无映射，本期不可点），与项目事项管理/左树视觉交互一致。
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
          参照 ProjectIssueList 刷新先例）。统一样式走 PanelToolbar（与终端 pane 工具栏
          同高同内边距）。 */}
      <PanelToolbar
        left={subTasks.length > 0
          ? (
              <Typography variant="caption" color="text.secondary">
                完成
                {doneCount}/{subTasks.length}
              </Typography>
            )
          : null}
        right={(
          <IconButton
            size="small"
            onClick={refresh}
            disabled={isFetching}
            aria-label="刷新子任务状态"
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
                // 子任务卡片列表：复用共享 IssueCard depth=1（viewScene="devWorkbench" → 正常背景色/隐藏 id，
                // 不传回调 → 无点击/hover，纯展示）；gap 与项目事项管理卡片间固定间距 12px 一致；
                // pt 加大与顶部 tab 栏的留白。
                <Box sx={{ flex: 1, overflow: 'auto', pt: 1.5, pb: 0.5, px: 1.5, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
                  {subTasks.map(task => <IssueCard key={task.id} issue={task} depth={1} viewScene="devWorkbench" />)}
                </Box>
              )}
    </Box>
  );
}
