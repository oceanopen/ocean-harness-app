import type { ProjectIssueResponseData } from '@src/services';
import { Autocomplete, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Stack, TextField, Typography } from '@mui/material';
import { getNextDevStepStateId } from '@src/state/devWorkbench';
import { useCreateWorktreeAndAdvance, useIssueWorktrees } from '@src/state/issueWorktree';
import { useLocalBranches, useLocalRepositories } from '@src/state/localRepositories';
import { useProjectStateViews } from '@src/state/tracker';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

// WtInitStep（D1）：worktree 初始化表单。
// 仓库 + baseRef（基准分支）+ devBranch（开发分支）+ worktree 路径预览。
// [创建并开始] 弹确认框→调 createWorktree（P1 桩：后端派生假路径写记录）→ 推进 stateId 到首个开发步骤（developing）。
// worktree 路径：优先用已有 active worktree 记录（createWorktree 创建后回填），否则派生占位预览。
export default function WtInitStep({ issue, projectId }: { issue: ProjectIssueResponseData; projectId: number }) {
  const { t } = useTranslation();
  const { data: repos = [] } = useLocalRepositories();
  const [repoId, setRepoId] = useState(issue.localRepositoryId);
  const repo = repos.find(r => r.id === repoId);
  const { data: branches = [] } = useLocalBranches(repoId);
  const [baseRef, setBaseRef] = useState(repo?.currentBranch ?? '');
  const [devBranch, setDevBranch] = useState(`issue-${issue.id}`);
  const { data: worktrees = [] } = useIssueWorktrees(issue.id);
  const activeWorktree = worktrees[0]; // P1 1:1
  const { views } = useProjectStateViews(projectId);
  const { run: runCreateWorktree, running: starting, snack: startSnack } = useCreateWorktreeAndAdvance(projectId);
  const [startOpen, setStartOpen] = useState(false);

  // 预览：优先用已有 active worktree 路径（createWorktree 创建后回填），否则派生占位。
  const worktreePathPreview = useMemo(
    () => activeWorktree?.worktreePath ?? (repo ? `${repo.localDir}-worktree-${issue.id}` : ''),
    [activeWorktree, repo, issue.id],
  );
  // 推进目标：wt_init → 下一个开发步骤（developing）。
  const targetStateId = getNextDevStepStateId(issue.stateId, views);

  const onStart = () => {
    setStartOpen(false);
    if (repoId === 0 || !devBranch || targetStateId == null) {
      return;
    }
    void runCreateWorktree(
      { issueId: issue.id, localRepositoryId: repoId, baseRef, branch: devBranch },
      issue,
      targetStateId,
    );
  };

  return (
    <Stack spacing={2}>
      <Typography variant="body2" color="text.secondary">配置 worktree 初始化参数</Typography>
      <Autocomplete
        size="small"
        options={repos}
        getOptionLabel={r => r.name}
        value={repo ?? null}
        onChange={(_e, v) => {
          setRepoId(v?.id ?? 0);
          setBaseRef(v?.currentBranch ?? '');
        }}
        isOptionEqualToValue={(a, b) => a.id === b.id}
        renderInput={params => <TextField {...params} label={t('panel:devWorkbench.repo')} />}
      />
      <Autocomplete
        size="small"
        freeSolo
        options={branches}
        inputValue={baseRef}
        onInputChange={(_e, v) => setBaseRef(v)}
        renderInput={params => <TextField {...params} label={t('panel:devWorkbench.baseRef')} />}
      />
      <TextField
        size="small"
        label={t('panel:devWorkbench.devBranch')}
        value={devBranch}
        onChange={e => setDevBranch(e.target.value)}
      />
      <Box sx={{ p: 1, bgcolor: 'action.hover', borderRadius: 1 }}>
        <Typography variant="caption" color="text.secondary">{t('panel:devWorkbench.worktreePath')}</Typography>
        <Typography variant="body2" sx={{ wordBreak: 'break-all' }}>
          {worktreePathPreview || '未选择仓库'}
        </Typography>
      </Box>
      <Box>
        <Button
          variant="contained"
          disabled={starting || targetStateId == null || repoId === 0 || !devBranch}
          onClick={() => setStartOpen(true)}
        >
          {t('panel:devWorkbench.createAndStart')}
        </Button>
      </Box>
      <Dialog open={startOpen} onClose={starting ? undefined : () => setStartOpen(false)}>
        <DialogTitle>确认创建 worktree 并开始？</DialogTitle>
        <DialogContent>
          <Typography>将基于分支「{devBranch}」创建 worktree 并推进到「开发中」步骤。</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setStartOpen(false)} disabled={starting}>取消</Button>
          <Button color="primary" variant="contained" onClick={onStart} disabled={starting}>确认创建</Button>
        </DialogActions>
      </Dialog>
      {startSnack}
    </Stack>
  );
}
