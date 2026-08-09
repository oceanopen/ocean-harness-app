import type { ProjectIssueResponseData } from '@src/services';
import { Alert, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Paper, Stack, Typography } from '@mui/material';
import { getFirstStateIdOfGroup, useAdvanceDevStep } from '@src/state/devWorkbench';
import { useLocalRepositories } from '@src/state/localRepositories';
import { useProjectStateViews } from '@src/state/tracker';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

// CleanupStep（D4）：待清理。
// 清理确认卡（列将清理的 worktree 路径 + 分支）。
// [清理并完成] 弹确认框→P1 advance-only：仅推进 stateId→completed 组首个（自动归档），不真删 worktree
//   （真两阶段编排 pty_stop_for_worktree + removeWorktree 待模块 G，见 worktree_term.md §9.3）。
// [仅停止,保留 worktree] 弹确认框→推进到 cancelled 组首个（不删 worktree，仅改 stateId）。
// 注：弹窗/提示/报错文案按 i18n 策略硬编码中文（便于排查），仅按钮与短标签走 i18n。
export default function CleanupStep({ issue, projectId }: { issue: ProjectIssueResponseData; projectId: number }) {
  const { t } = useTranslation();
  const { data: repos = [] } = useLocalRepositories();
  const repo = repos.find(r => r.id === issue.localRepositoryId);
  const { views } = useProjectStateViews(projectId);
  const { advance, advancing, snack } = useAdvanceDevStep(projectId);
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);

  const completedId = getFirstStateIdOfGroup('completed', views);
  const onCleanup = () => {
    setCleanupOpen(false);
    if (completedId != null) {
      void advance(issue, completedId);
    }
  };
  const onConfirmCancel = () => {
    setCancelOpen(false);
    const cancelledId = getFirstStateIdOfGroup('cancelled', views);
    if (cancelledId != null) {
      void advance(issue, cancelledId);
    }
  };

  return (
    <Stack spacing={2}>
      <Alert severity="info">P1：实际 worktree 清理待后端落地，点击「清理并完成」仅推进状态到已完成</Alert>
      <Paper variant="outlined" sx={{ p: 2 }}>
        <Typography variant="caption" color="text.secondary">将清理 worktree</Typography>
        <Typography variant="body2" sx={{ wordBreak: 'break-all' }}>
          {repo ? `${repo.localDir}-worktree-${issue.id}` : '无 worktree 记录'}
        </Typography>
        {issue.repositoryBranch && (
          <Typography variant="body2">{t('panel:devWorkbench.branch')}: {issue.repositoryBranch}</Typography>
        )}
      </Paper>
      <Box sx={{ display: 'flex', gap: 1 }}>
        {/* TODO(G): removeWorktree + pty_stop_for_worktree 桩接入后，于 advance 前补两阶段编排（真删 worktree） */}
        <Button variant="contained" disabled={advancing || completedId == null} onClick={() => setCleanupOpen(true)}>
          {t('panel:devWorkbench.cleanupComplete')}
        </Button>
        <Button variant="outlined" disabled={advancing} onClick={() => setCancelOpen(true)}>
          {t('panel:devWorkbench.stopOnly')}
        </Button>
      </Box>
      <Dialog open={cleanupOpen} onClose={advancing ? undefined : () => setCleanupOpen(false)}>
        <DialogTitle>确认完成并归档？</DialogTitle>
        <DialogContent>
          <Typography>将推进到「已完成」并自动归档（P1 不删除 worktree）。</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setCleanupOpen(false)} disabled={advancing}>取消</Button>
          <Button color="primary" variant="contained" onClick={onCleanup} disabled={advancing}>确认完成</Button>
        </DialogActions>
      </Dialog>
      <Dialog open={cancelOpen} onClose={advancing ? undefined : () => setCancelOpen(false)}>
        <DialogTitle>确认取消开发？</DialogTitle>
        <DialogContent>
          <Typography>将该任务移到「已取消」组并从开发工作台移除（worktree 不会被删除）。仍可在事项管理中查看。</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setCancelOpen(false)} disabled={advancing}>继续开发</Button>
          <Button color="error" variant="contained" onClick={onConfirmCancel} disabled={advancing}>确认取消</Button>
        </DialogActions>
      </Dialog>
      {snack}
    </Stack>
  );
}
