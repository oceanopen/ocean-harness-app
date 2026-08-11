import type { ProjectIssueResponseData } from '@src/services';
import { Alert, Autocomplete, Button, Card, CardActions, CardContent, CardHeader, Dialog, DialogActions, DialogContent, DialogTitle, Divider, Stack, TextField, Typography } from '@mui/material';
import { getNextDevStepStateId } from '@src/state/devWorkbench';
import { useCreateWorktreeAndAdvance, useIssueWorktrees, useUpdateWorktree } from '@src/state/issueWorktree';
import { useLocalBranches, useLocalRepositories } from '@src/state/localRepositories';
import { useProjectStateViews, useWorkspaces } from '@src/state/tracker';
import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { basenameOfPath, deriveWorktreePath, repoNameFromRemoteUrl } from './worktreePath';

// WorktreeInitStep（D1）：worktree 初始化表单（Card 布局：标题 + 表单项 + footer 按钮）。
// 仓库 + baseBranch（基准分支）+ devBranch（开发分支）+ worktree 路径预览。
// 双模式：加载时按 useIssueWorktrees 判断是否已有 active worktree——
//   无 → 创建模式：[创建并开始] 弹确认框→调 createWorktree（后端真创建 git worktree）→ 推进 stateId 到 developing。
//   有 → 更新模式：按钮「更新」+ 展示记录ID 只读表单，[更新] 弹确认框→调 updateWorktree（分支变了后端删旧重建，不推进 stateId）。
//
// worktree 路径预览：优先用已有 active worktree 真路径；否则按后端同款公式（worktreePath.ts）派生，
// 须 workspace 已配置 worktreeRoot。workspace 未配置时，路径框替换为提示 + 「去配置」入口，并禁用主按钮。
//
// 切 issue 时由父级 DevWorkbenchPage 的 key={issue.id} 强制重挂载本组件，表单按新 issue 重新初始化
// （有仓库/worktree 信息则展示默认值，无则清空），不受上个 issue 表单残留影响。
// 注：步骤操作内容一律硬编码中文（不走 i18n）；仅菜单/路由等必要地方支持 i18n。
export default function WorktreeInitStep({ issue, projectId }: { issue: ProjectIssueResponseData; projectId: number }) {
  const navigate = useNavigate();
  const { data: repos = [] } = useLocalRepositories();
  const { data: worktrees = [] } = useIssueWorktrees(issue.id);
  const activeWorktree = worktrees[0]; // 1:1
  const isUpdate = !!activeWorktree; // 已有 active worktree → 更新模式
  const [repoId, setRepoId] = useState(activeWorktree?.localRepositoryId ?? issue.localRepositoryId);
  const repo = repos.find(r => r.id === repoId);
  const { data: branches = [] } = useLocalBranches(repoId);
  // 基准分支默认值：更新模式预填 worktree 记录值；创建模式优先仓库默认分支，缺失回退当前分支。
  const [baseBranch, setBaseBranch] = useState(activeWorktree?.baseBranch || repo?.defaultBranch || repo?.currentBranch || '');
  const [devBranch, setDevBranch] = useState(activeWorktree?.worktreeBranch || `issue-${issue.id}`);
  const { views } = useProjectStateViews(projectId);
  const { run: runCreateWorktree, running: creating, snack: createSnack } = useCreateWorktreeAndAdvance(projectId);
  const { run: runUpdateWorktree, running: updating, snack: updateSnack } = useUpdateWorktree();
  const [dialogConfirmOpen, setDialogConfirmOpen] = useState(false);
  // activeWorktree 异步加载（首次访问无缓存）后在渲染期同步预填表单（React 推荐的「渲染期调整 state」，
  // 替代 effect setState 以避免额外渲染）。切 issue 由父级 key 重挂载保证 useState 重置。
  const [syncedWtId, setSyncedWtId] = useState<number | null>(activeWorktree?.id ?? null);
  if (activeWorktree && activeWorktree.id !== syncedWtId) {
    setSyncedWtId(activeWorktree.id);
    setRepoId(activeWorktree.localRepositoryId);
    setBaseBranch(activeWorktree.baseBranch);
    setDevBranch(activeWorktree.worktreeBranch);
  }

  // 当前 issue 所属 workspace 的 worktreeRoot（per-workspace 配置，与后端 CreateWorktree 同源）。
  // useWorkspaces 与 DevTaskTree/命令面板共享缓存，命中即零请求。未配置（空串）时给出前置提示。
  const { data: workspaces = [] } = useWorkspaces();
  const worktreeRoot = workspaces.find(w => w.id === issue.workspaceId)?.worktreeRoot.trim() ?? '';
  const worktreeConfigured = worktreeRoot !== '';

  // 预览：优先用已有 active worktree 真路径（createWorktree/updateWorktree 后回填）；
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

  // 推进目标（仅创建模式用）：WorktreeInitStep 固定是 wt_init 步骤，"创建并开始"推进到 developing。
  const worktreeInitView = views.find(v => v.stateCode === 'wt_init');
  const targetStateId = worktreeInitView ? getNextDevStepStateId(worktreeInitView.id, views) : null;

  const onConfirm = () => {
    setDialogConfirmOpen(false);
    if (repoId === 0 || !devBranch || !worktreeConfigured) {
      return;
    }
    if (isUpdate) {
      // 更新模式：调 updateWorktree（分支变了后端删旧重建，不推进 stateId）。
      void runUpdateWorktree({ id: activeWorktree.id, localRepositoryId: repoId, baseBranch, worktreeBranch: devBranch }, issue.id);
      return;
    }
    // 创建模式：createWorktree + 推进 stateId 到 developing。
    if (targetStateId == null) {
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
        <CardHeader title="Worktree 配置" slotProps={{ title: { variant: 'subtitle1', fontWeight: 600 } }} />
        <Divider />
        <CardContent>
          <Stack spacing={2}>
            {isUpdate && (
              <TextField
                label="记录 ID"
                value={activeWorktree.id}
                fullWidth
                slotProps={{ input: { readOnly: true } }}
                variant="filled"
              />
            )}
            <Autocomplete
              size="small"
              options={repos}
              getOptionLabel={r => r.name}
              value={repo ?? null}
              onChange={(_e, v) => {
                setRepoId(v?.id ?? 0);
                // 切仓库后重新派生基准分支默认值（新仓库默认分支 → 当前分支）。
                setBaseBranch(v?.defaultBranch || v?.currentBranch || '');
              }}
              isOptionEqualToValue={(a, b) => a.id === b.id}
              renderInput={params => <TextField {...params} label="仓库" />}
            />
            <Autocomplete
              size="small"
              freeSolo
              options={branches}
              inputValue={baseBranch}
              onInputChange={(_e, v) => setBaseBranch(v)}
              renderInput={params => <TextField {...params} label="基准分支" />}
            />
            <TextField
              size="small"
              label="开发分支"
              value={devBranch}
              onChange={e => setDevBranch(e.target.value)}
            />
            {worktreeConfigured
              ? (
                  <TextField
                    label="worktree 路径"
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
            disabled={
              (isUpdate ? updating : creating)
              || !worktreeConfigured
              || (!isUpdate && targetStateId == null)
              || repoId === 0
              || !devBranch
            }
            onClick={() => setDialogConfirmOpen(true)}
          >
            {isUpdate ? '更新 Worktree' : '创建 Worktree'}
          </Button>
        </CardActions>
      </Card>
      <Dialog open={dialogConfirmOpen} onClose={(isUpdate ? updating : creating) ? undefined : () => setDialogConfirmOpen(false)}>
        <DialogTitle>{isUpdate ? '确认更新 Worktree？' : '确认创建 Worktree？'}</DialogTitle>
        <DialogContent>
          <Typography>
            {isUpdate
              ? `将基于分支「${devBranch}」更新 Worktree（分支变化时删旧重建）。`
              : `将基于分支「${devBranch}」创建 Worktree 并推进到「开发中」步骤。`}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDialogConfirmOpen(false)} disabled={isUpdate ? updating : creating}>取消</Button>
          <Button color="primary" variant="contained" onClick={onConfirm} disabled={isUpdate ? updating : creating}>
            {isUpdate ? '确认更新' : '确认创建'}
          </Button>
        </DialogActions>
      </Dialog>
      {isUpdate ? updateSnack : createSnack}
    </Stack>
  );
}
