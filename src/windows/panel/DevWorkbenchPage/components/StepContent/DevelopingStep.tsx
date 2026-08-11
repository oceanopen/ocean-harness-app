import type { ProjectIssueResponseData } from '@src/services';
import {
  CodeOutlined as CodeOutlinedIcon,
  FolderOutlined as FolderOutlinedIcon,
  TerminalOutlined as TerminalOutlinedIcon,
} from '@mui/icons-material';
import { Alert, Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Stack, Typography } from '@mui/material';
import { commands } from '@src/shared/bindings';
import { logOnError } from '@src/shared/commands';
import { getNextDevStepStateId, useAdvanceDevStep } from '@src/state/devWorkbench';
import { useIssueWorktrees } from '@src/state/issueWorktree';
import { useLocalRepositories } from '@src/state/localRepositories';
import { useProjectStateViews } from '@src/state/tracker';
import { useState } from 'react';

// DevelopingStep（D2）：开发中。
// 终端占位（嵌入式终端 P2，见 worktree_term.md）+ 外部打开按钮行（VSCode/iTerm2/访达，复用 bindings 三函数）。
// worktree 路径优先用 createWorktree 创建的 active worktree 路径，无则回退主仓库 localDir。
// [开发完成] 弹确认框→推进 stateId 到下一个开发步骤（pull_request）。
export default function DevelopingStep({ issue, projectId }: { issue: ProjectIssueResponseData; projectId: number }) {
  const { data: repos = [] } = useLocalRepositories();
  const repo = repos.find(r => r.id === issue.localRepositoryId);
  const { data: worktrees = [] } = useIssueWorktrees(issue.id);
  const dir = worktrees[0]?.worktreePath ?? repo?.localDir ?? ''; // 优先真 worktree 路径，回退主仓库
  const { views } = useProjectStateViews(projectId);
  const { advance, advancing, snack } = useAdvanceDevStep(projectId);
  const [dialogConfirmOpen, setDialogConfirmOpen] = useState(false);

  const openIn = (target: 'editor' | 'terminal' | 'finder') => {
    if (!dir) {
      return;
    }
    if (target === 'editor') {
      logOnError(commands.openInEditor('vscode', dir), 'devWorkbench:openInEditor');
    } else if (target === 'terminal') {
      logOnError(commands.openInTerminal('iterm2', dir), 'devWorkbench:openInTerminal');
    } else {
      logOnError(commands.openInFileManager(dir), 'devWorkbench:openInFileManager');
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
      <Alert severity="info">嵌入式终端即将支持，当前可点下方按钮在外部终端/编辑器开发</Alert>
      <Box sx={{
        minHeight: 160,
        border: 1,
        borderColor: 'divider',
        borderRadius: 1,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
      >
        <Typography variant="body2" color="text.secondary">嵌入式终端（P2）</Typography>
      </Box>
      <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
        <Button size="small" startIcon={<CodeOutlinedIcon />} disabled={!dir} onClick={() => openIn('editor')}>
          编辑器打开
        </Button>
        <Button size="small" startIcon={<TerminalOutlinedIcon />} disabled={!dir} onClick={() => openIn('terminal')}>
          终端打开
        </Button>
        <Button size="small" startIcon={<FolderOutlinedIcon />} disabled={!dir} onClick={() => openIn('finder')}>
          访达打开
        </Button>
      </Box>
      <Box>
        <Button variant="contained" disabled={advancing} onClick={() => setDialogConfirmOpen(true)}>
          开发完成
        </Button>
      </Box>
      <Dialog open={dialogConfirmOpen} onClose={advancing ? undefined : () => setDialogConfirmOpen(false)}>
        <DialogTitle>确认开发完成？</DialogTitle>
        <DialogContent>
          <Typography>将推进到「合并请求」步骤。</Typography>
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
