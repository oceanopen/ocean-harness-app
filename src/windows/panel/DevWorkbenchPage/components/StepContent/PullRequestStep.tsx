import type { ProjectIssueResponseData } from '@src/services';
import { Alert, Autocomplete, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Stack, TextField, Typography } from '@mui/material';
import { buildCompareUrl } from '@src/shared/gitRemote';
import { getNextDevStepStateId, useAdvanceDevStep } from '@src/state/devWorkbench';
import { useLocalBranches, useLocalRepositories } from '@src/state/localRepositories';
import { useProjectStateViews } from '@src/state/tracker';
import { open as openUrl } from '@tauri-apps/plugin-shell';
import { useMemo, useState } from 'react';

// PullRequestStep（D3）：生成 PR。
// PR 配置卡：目标分支 base（可改）+ 源分支 head（=issue.repositoryBranch 只读）+ 标题（默认 issue.name）。
// [打开 compare 页] 构造 GitHub/GitLab compare URL（gitRemote.buildCompareUrl）用 plugin-shell 打开；
// 真 PR 创建见 worktree_term.md（本期非目标，先「引导式 compare URL」顶住，§9.2）。
// [合并完成] 弹确认框→推进到下一个开发步骤（cleanup）。
export default function PullRequestStep({ issue, projectId }: { issue: ProjectIssueResponseData; projectId: number }) {
  const { data: repos = [] } = useLocalRepositories();
  const repo = repos.find(r => r.id === issue.localRepositoryId);
  const { data: branches = [] } = useLocalBranches(issue.localRepositoryId);
  const { views } = useProjectStateViews(projectId);
  const { advance, advancing, snack } = useAdvanceDevStep(projectId);
  const head = issue.repositoryBranch;
  const [base, setBase] = useState(repo?.currentBranch ?? 'main');
  const [title, setTitle] = useState(issue.name);
  const [dialogConfirmOpen, setDialogConfirmOpen] = useState(false);

  const compareUrl = useMemo(
    () => (repo && head ? buildCompareUrl(repo.remoteUrl, base, head) : null),
    [repo, head, base],
  );

  const onOpenCompare = () => {
    if (compareUrl) {
      void openUrl(compareUrl);
    }
  };
  const onConfirm = () => {
    setDialogConfirmOpen(false);
    const next = getNextDevStepStateId(issue.stateId, views);
    if (next != null) {
      void advance(issue, next);
    }
  };

  return (
    <Stack spacing={2}>
      <Alert severity="info">填写分支与标题，打开 compare 页创建 PR</Alert>
      <Box sx={{ display: 'flex', gap: 1 }}>
        <Autocomplete
          size="small"
          freeSolo
          options={branches}
          inputValue={base}
          onInputChange={(_e, v) => setBase(v)}
          renderInput={params => <TextField {...params} label="目标分支" />}
          sx={{ flex: 1 }}
        />
        <TextField
          size="small"
          label="源分支"
          value={head}
          disabled
          sx={{ flex: 1 }}
        />
      </Box>
      <TextField size="small" label="PR 标题" value={title} onChange={e => setTitle(e.target.value)} />
      <Box>
        <Button size="small" variant="outlined" disabled={!compareUrl} onClick={onOpenCompare}>
          打开 compare 页
        </Button>
      </Box>
      <Box>
        <Button variant="contained" disabled={advancing} onClick={() => setDialogConfirmOpen(true)}>
          合并完成
        </Button>
      </Box>
      <Dialog open={dialogConfirmOpen} onClose={advancing ? undefined : () => setDialogConfirmOpen(false)}>
        <DialogTitle>确认合并完成？</DialogTitle>
        <DialogContent>
          <Typography>将推进到「清理」步骤。</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDialogConfirmOpen(false)} disabled={advancing}>取消</Button>
          <Button color="primary" variant="contained" onClick={onConfirm} disabled={advancing}>确认完成</Button>
        </DialogActions>
      </Dialog>
      {snack}
    </Stack>
  );
}
