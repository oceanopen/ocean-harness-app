import type { ProjectIssueResponseData } from '@src/services';
import { Box, Button, Paper, Stack, Typography } from '@mui/material';
import { getFirstStateIdOfGroup, useAdvanceDevStep } from '@src/state/devWorkbench';
import { useLocalRepositories } from '@src/state/localRepositories';
import { useProjectStateViews } from '@src/state/tracker';
import { useTranslation } from 'react-i18next';

// CleanupStep（D4）：待清理。
// 清理确认卡（列将删除的 worktree 路径 + 分支）。
// [清理并完成] 当前 disabled——后端 removeWorktree + pty_stop_for_worktree 两阶段编排待模块 G（worktree_term.md §9.3）。
// [仅停止,保留 worktree] 推进到 cancelled 组首个（不删 worktree，仅改 stateId）。
export default function CleanupStep({ issue, projectId }: { issue: ProjectIssueResponseData; projectId: number }) {
  const { t } = useTranslation();
  const { data: repos = [] } = useLocalRepositories();
  const repo = repos.find(r => r.id === issue.localRepositoryId);
  const { views } = useProjectStateViews(projectId);
  const { advance, advancing } = useAdvanceDevStep(projectId);

  const onStopOnly = () => {
    const cancelledId = getFirstStateIdOfGroup('cancelled', views);
    if (cancelledId != null) {
      void advance(issue, cancelledId);
    }
  };

  return (
    <Stack spacing={2}>
      <Paper variant="outlined" sx={{ p: 2 }}>
        <Typography variant="caption" color="text.secondary">{t('panel:devWorkbench.cleanupWillRemove')}</Typography>
        <Typography variant="body2" sx={{ wordBreak: 'break-all' }}>
          {repo ? `${repo.localDir}-worktree-${issue.id}` : t('panel:devWorkbench.noWorktree')}
        </Typography>
        {issue.repositoryBranch && (
          <Typography variant="body2">{t('panel:devWorkbench.branch')}: {issue.repositoryBranch}</Typography>
        )}
      </Paper>
      <Box sx={{ display: 'flex', gap: 1 }}>
        {/* TODO(G): removeWorktree + pty_stop_for_worktree 桩接入后启用，成功后推进到 completed 组首个（自动归档） */}
        <Button variant="contained" disabled>{t('panel:devWorkbench.cleanupComplete')}</Button>
        <Button variant="outlined" disabled={advancing} onClick={onStopOnly}>
          {t('panel:devWorkbench.stopOnly')}
        </Button>
      </Box>
    </Stack>
  );
}
