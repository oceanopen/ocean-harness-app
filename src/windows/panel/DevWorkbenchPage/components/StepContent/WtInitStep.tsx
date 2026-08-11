import type { ProjectIssueResponseData } from '@src/services';
import { Alert, Autocomplete, Button, Card, CardActions, CardContent, CardHeader, Dialog, DialogActions, DialogContent, DialogTitle, Divider, Stack, TextField, Typography } from '@mui/material';
import { getNextDevStepStateId } from '@src/state/devWorkbench';
import { useCreateWorktreeAndAdvance, useIssueWorktrees } from '@src/state/issueWorktree';
import { useLocalBranches, useLocalRepositories } from '@src/state/localRepositories';
import { useProjectStateViews, useWorkspaces } from '@src/state/tracker';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { basenameOfPath, deriveWorktreePath, repoNameFromRemoteUrl } from './worktreePath';

// WtInitStep（D1）：worktree 初始化表单（Card 布局：标题 + 表单项 + footer 按钮）。
// 仓库 + baseBranch（基准分支）+ devBranch（开发分支）+ worktree 路径预览。
// [创建并开始] 弹确认框→调 createWorktree（后端真创建 git worktree + 按 per-workspace worktreeRoot 派生路径）
// → 推进 stateId 到首个开发步骤（developing）。
//
// worktree 路径预览：优先用已有 active worktree 真路径；否则按后端同款公式（worktreePath.ts）派生，
// 须 workspace 已配置 worktreeRoot。workspace 未配置时，路径框替换为提示 + 「去配置」入口，并禁用「创建并开始」。
//
// 切 issue 时由父级 DevWorkbenchPage 的 key={issue.id} 强制重挂载本组件，表单按新 issue 重新初始化
// （有仓库/worktree 信息则展示默认值，无则清空），不受上个 issue 表单残留影响。
export default function WtInitStep({ issue, projectId }: { issue: ProjectIssueResponseData; projectId: number }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: repos = [] } = useLocalRepositories();
  const [repoId, setRepoId] = useState(issue.localRepositoryId);
  const repo = repos.find(r => r.id === repoId);
  const { data: branches = [] } = useLocalBranches(repoId);
  const [baseBranch, setBaseBranch] = useState(repo?.currentBranch ?? '');
  const [devBranch, setDevBranch] = useState(`issue-${issue.id}`);
  const { data: worktrees = [] } = useIssueWorktrees(issue.id);
  const activeWorktree = worktrees[0]; // 1:1
  const { views } = useProjectStateViews(projectId);
  const { run: runCreateWorktree, running: starting, snack: startSnack } = useCreateWorktreeAndAdvance(projectId);
  const [startOpen, setStartOpen] = useState(false);

  // 当前 issue 所属 workspace 的 worktreeRoot（per-workspace 配置，与后端 CreateWorktree 同源）。
  // useWorkspaces 与 DevTaskTree/命令面板共享缓存，命中即零请求。未配置（空串）时给出前置提示。
  const { data: workspaces = [] } = useWorkspaces();
  const worktreeRoot = workspaces.find(w => w.id === issue.workspaceId)?.worktreeRoot.trim() ?? '';
  const worktreeConfigured = worktreeRoot !== '';

  // 预览：优先用已有 active worktree 真路径（createWorktree 创建后回填）；
  // 否则需 workspace 已配置 worktreeRoot，按后端同款公式（worktreePath.ts）派生；再否则空串。
  const worktreePathPreview = useMemo(() => {
    if (activeWorktree?.worktreePath) {
      return activeWorktree.worktreePath;
    }
    if (!worktreeConfigured || !repo) {
      return '';
    }
    const repoName = repoNameFromRemoteUrl(repo.remoteUrl) || basenameOfPath(repo.localDir);
    return deriveWorktreePath(worktreeRoot, repoName, issue.workspaceId, issue.projectId, issue.id);
  }, [activeWorktree, worktreeConfigured, worktreeRoot, repo, issue]);

  // 推进目标：WtInitStep 固定是 wt_init 步骤，"创建并开始"推进到 wt_init 的下一个开发步骤（developing）。
  // 基于 wt_init 的 stateId 算 next（而非 issue.stateId）：issue 可能在 in_progress（wt_init 前驱，被
  // getDevSteps 排除 → getNextDevStepStateId 返回 null），但 WtInitStep 的推进目标与 issue 当前态无关——
  // in_progress 与 wt_init issue 点"创建并开始"都应推进到 developing（创建即开始开发）。
  const wtInitView = views.find(v => v.stateCode === 'wt_init');
  const targetStateId = wtInitView ? getNextDevStepStateId(wtInitView.id, views) : null;

  const onStart = () => {
    setStartOpen(false);
    if (repoId === 0 || !devBranch || !worktreeConfigured || targetStateId == null) {
      return;
    }
    void runCreateWorktree(
      { issueId: issue.id, localRepositoryId: repoId, baseBranch, worktreeBranch: devBranch },
      issue,
      targetStateId,
    );
  };

  return (
    <Stack spacing={2}>
      <Card variant="outlined">
        <CardHeader title="配置 worktree 初始化参数" slotProps={{ title: { variant: 'subtitle1', fontWeight: 600 } }} />
        <Divider />
        <CardContent>
          <Stack spacing={2}>
            <Autocomplete
              size="small"
              options={repos}
              getOptionLabel={r => r.name}
              value={repo ?? null}
              onChange={(_e, v) => {
                setRepoId(v?.id ?? 0);
                setBaseBranch(v?.currentBranch ?? '');
              }}
              isOptionEqualToValue={(a, b) => a.id === b.id}
              renderInput={params => <TextField {...params} label={t('panel:devWorkbench.repo')} />}
            />
            <Autocomplete
              size="small"
              freeSolo
              options={branches}
              inputValue={baseBranch}
              onInputChange={(_e, v) => setBaseBranch(v)}
              renderInput={params => <TextField {...params} label={t('panel:devWorkbench.baseRef')} />}
            />
            <TextField
              size="small"
              label={t('panel:devWorkbench.devBranch')}
              value={devBranch}
              onChange={e => setDevBranch(e.target.value)}
            />
            {worktreeConfigured
              ? (
                  <TextField
                    label={t('panel:devWorkbench.worktreePath')}
                    value={worktreePathPreview || '未选择仓库'}
                    fullWidth
                    slotProps={{ input: { readOnly: true } }}
                    variant="filled"
                  />
                )
              : (
                  <Alert
                    severity="warning"
                    action={(
                      <Button color="warning" size="small" onClick={() => navigate('/tracker')}>
                        去配置
                      </Button>
                    )}
                  >
                    请先配置工作空间 worktree 工作目录
                  </Alert>
                )}
          </Stack>
        </CardContent>
        <Divider />
        <CardActions sx={{ px: 2 }}>
          <Button
            variant="contained"
            disabled={starting || !worktreeConfigured || targetStateId == null || repoId === 0 || !devBranch}
            onClick={() => setStartOpen(true)}
          >
            {t('panel:devWorkbench.createAndStart')}
          </Button>
        </CardActions>
      </Card>
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
