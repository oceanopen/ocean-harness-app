import type { ProjectIssueResponseData } from '@src/services';
import { Autocomplete, Box, Button, Stack, TextField, Typography } from '@mui/material';
import { useLocalBranches, useLocalRepositories } from '@src/state/localRepositories';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

// WtInitStep（D1）：worktree 初始化表单。
// 仓库 + baseRef（基准分支）+ devBranch（开发分支）+ worktree 路径预览（按 worktree_term.md §5.3 派生占位）。
// [创建并开始] 当前 disabled——后端 startDev 桩待模块 G；表单交互可用（填值），点击暂不触发后端。
export default function WtInitStep({ issue }: { issue: ProjectIssueResponseData }) {
  const { t } = useTranslation();
  const { data: repos = [] } = useLocalRepositories();
  const [repoId, setRepoId] = useState(issue.localRepositoryId);
  const repo = repos.find(r => r.id === repoId);
  const { data: branches = [] } = useLocalBranches(repoId);
  const [baseRef, setBaseRef] = useState(repo?.currentBranch ?? '');
  const [devBranch, setDevBranch] = useState(`issue-${issue.id}`);
  // P1 桩：路径按 <localDir>-worktree-<issueId> 派生（真派生规则见 worktree_term.md §5.3，待 G 落地）。
  const worktreePathPreview = useMemo(
    () => (repo ? `${repo.localDir}-worktree-${issue.id}` : ''),
    [repo, issue.id],
  );

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
        {/* TODO(G): startDev 桩接入后启用，成功后推进 stateId 到 developing */}
        <Button variant="contained" disabled>{t('panel:devWorkbench.createAndStart')}</Button>
      </Box>
    </Stack>
  );
}
