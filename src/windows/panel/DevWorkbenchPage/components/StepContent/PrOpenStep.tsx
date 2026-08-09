import type { ProjectIssueResponseData } from '@src/services';
import { Alert, Autocomplete, Box, Button, Stack, TextField } from '@mui/material';
import { buildCompareUrl } from '@src/shared/gitRemote';
import { getNextDevStepStateId, useAdvanceDevStep } from '@src/state/devWorkbench';
import { useLocalBranches, useLocalRepositories } from '@src/state/localRepositories';
import { useProjectStateViews } from '@src/state/tracker';
import { open as openUrl } from '@tauri-apps/plugin-shell';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

// PrOpenStep（D3）：生成 PR。
// PR 配置卡：目标分支 base（可改）+ 源分支 head（=issue.repositoryBranch 只读）+ 标题（默认 issue.name）。
// [打开 compare 页] 构造 GitHub/GitLab compare URL（gitRemote.buildCompareUrl）用 plugin-shell 打开；
// 真 PR 创建见 worktree_term.md（本期非目标，先「引导式 compare URL」顶住，§9.2）。
// [合并完成] 推进到下一个开发步骤（cleanup）。
export default function PrOpenStep({ issue, projectId }: { issue: ProjectIssueResponseData; projectId: number }) {
  const { t } = useTranslation();
  const { data: repos = [] } = useLocalRepositories();
  const repo = repos.find(r => r.id === issue.localRepositoryId);
  const { data: branches = [] } = useLocalBranches(issue.localRepositoryId);
  const { views } = useProjectStateViews(projectId);
  const { advance, advancing, snack } = useAdvanceDevStep(projectId);
  const head = issue.repositoryBranch;
  const [base, setBase] = useState(repo?.currentBranch ?? 'main');
  const [title, setTitle] = useState(issue.name);

  const compareUrl = useMemo(
    () => (repo && head ? buildCompareUrl(repo.remoteUrl, base, head) : null),
    [repo, head, base],
  );

  const onOpenCompare = () => {
    if (compareUrl) {
      void openUrl(compareUrl);
    }
  };
  const onComplete = () => {
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
          renderInput={params => <TextField {...params} label={t('panel:devWorkbench.prBase')} />}
          sx={{ flex: 1 }}
        />
        <TextField
          size="small"
          label={t('panel:devWorkbench.prHead')}
          value={head}
          disabled
          sx={{ flex: 1 }}
        />
      </Box>
      <TextField size="small" label={t('panel:devWorkbench.prTitle')} value={title} onChange={e => setTitle(e.target.value)} />
      <Box>
        <Button size="small" variant="outlined" disabled={!compareUrl} onClick={onOpenCompare}>
          {t('panel:devWorkbench.openCompare')}
        </Button>
      </Box>
      <Box>
        <Button variant="contained" disabled={advancing} onClick={onComplete}>
          {t('panel:devWorkbench.prComplete')}
        </Button>
      </Box>
      {snack}
    </Stack>
  );
}
