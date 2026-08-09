import type { ProjectIssueResponseData } from '@src/services';
import { Alert, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Paper, Stack, Typography } from '@mui/material';
import { getFirstStateIdOfGroup, useAdvanceDevStep } from '@src/state/devWorkbench';
import { useCleanupAndAdvance, useIssueWorktrees } from '@src/state/issueWorktree';
import { useLocalRepositories } from '@src/state/localRepositories';
import { useProjectStateViews } from '@src/state/tracker';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

// CleanupStep（D4）：待清理。
// 清理确认卡（列将清理的 worktree 路径 + 分支）。
// [清理并完成] 弹确认框→pty_stop_for_worktree + removeWorktree（两阶段，§9.3）→ 推进 stateId→completed 组首个（自动归档）。
//   P1 桩：pty_stop 恒返 0、removeWorktree 软删记录（不真删目录）；真删见 worktree_term.md §9.3。
//   无 active worktree 记录时退化为仅推进状态（advance-only）。
// 取消开发（→cancelled）不在执行面：改由「事项管理」（规划面，完整状态控制）处理。
// 注：弹窗/提示/报错文案按 i18n 策略硬编码中文（便于排查），仅按钮与短标签走 i18n。
export default function CleanupStep({ issue, projectId }: { issue: ProjectIssueResponseData; projectId: number }) {
  const { t } = useTranslation();
  const { data: repos = [] } = useLocalRepositories();
  const repo = repos.find(r => r.id === issue.localRepositoryId);
  const { views } = useProjectStateViews(projectId);
  const { data: worktrees = [] } = useIssueWorktrees(issue.id);
  const activeWorktree = worktrees[0]; // P1 1:1
  const { advance, advancing, snack } = useAdvanceDevStep(projectId);
  const { run: runCleanup, running: cleaning, snack: cleanupSnack } = useCleanupAndAdvance(projectId);
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const busy = advancing || cleaning;

  const completedId = getFirstStateIdOfGroup('completed', views);
  const onCleanup = () => {
    setCleanupOpen(false);
    if (completedId == null) {
      return;
    }
    if (activeWorktree) {
      void runCleanup(activeWorktree.worktreeId, issue, completedId);
    } else {
      void advance(issue, completedId); // 无 worktree 记录，仅推进状态
    }
  };

  return (
    <Stack spacing={2}>
      <Alert severity="info">P1：worktree 实际清理待后端真实现，当前点击「清理并完成」会停止终端桩 + 软删 worktree 记录 + 推进到已完成</Alert>
      <Paper variant="outlined" sx={{ p: 2 }}>
        <Typography variant="caption" color="text.secondary">将清理 worktree</Typography>
        <Typography variant="body2" sx={{ wordBreak: 'break-all' }}>
          {activeWorktree?.worktreePath ?? (repo ? `${repo.localDir}-worktree-${issue.id}` : '无 worktree 记录')}
        </Typography>
        {issue.repositoryBranch && (
          <Typography variant="body2">{t('panel:devWorkbench.branch')}: {issue.repositoryBranch}</Typography>
        )}
      </Paper>
      <Box>
        <Button variant="contained" disabled={busy || completedId == null} onClick={() => setCleanupOpen(true)}>
          {t('panel:devWorkbench.cleanupComplete')}
        </Button>
      </Box>
      <Dialog open={cleanupOpen} onClose={busy ? undefined : () => setCleanupOpen(false)}>
        <DialogTitle>确认完成并归档？</DialogTitle>
        <DialogContent>
          <Typography>将停止终端、清理 worktree 记录并推进到「已完成」（自动归档）。</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setCleanupOpen(false)} disabled={busy}>取消</Button>
          <Button color="primary" variant="contained" onClick={onCleanup} disabled={busy}>确认完成</Button>
        </DialogActions>
      </Dialog>
      {snack}
      {cleanupSnack}
    </Stack>
  );
}
