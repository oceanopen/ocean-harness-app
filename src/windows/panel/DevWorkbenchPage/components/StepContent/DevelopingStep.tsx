import type { ProjectIssueResponseData } from '@src/services';
import {
  CodeOutlined as CodeOutlinedIcon,
  FolderOutlined as FolderOutlinedIcon,
  TerminalOutlined as TerminalOutlinedIcon,
} from '@mui/icons-material';
import { Alert, Box, Button, Stack, Typography } from '@mui/material';
import { commands } from '@src/shared/bindings';
import { logOnError } from '@src/shared/commands';
import { getNextDevStepStateId, useAdvanceDevStep } from '@src/state/devWorkbench';
import { useLocalRepositories } from '@src/state/localRepositories';
import { useProjectStateViews } from '@src/state/tracker';
import { useTranslation } from 'react-i18next';

// DevelopingStep（D2）：开发中。
// 终端占位（嵌入式终端 P2，见 worktree_term.md）+ 外部打开按钮行（VSCode/iTerm2/访达，复用 bindings 三函数）。
// worktree 路径暂用 repo.localDir 兜底（真 worktree 路径待模块 G startDev 落地后接入）。
// [开发完成] 推进 stateId 到下一个开发步骤（pr_open）。
export default function DevelopingStep({ issue, projectId }: { issue: ProjectIssueResponseData; projectId: number }) {
  const { t } = useTranslation();
  const { data: repos = [] } = useLocalRepositories();
  const repo = repos.find(r => r.id === issue.localRepositoryId);
  const dir = repo?.localDir ?? ''; // P1 兜底
  const { views } = useProjectStateViews(projectId);
  const { advance, advancing, snack } = useAdvanceDevStep(projectId);

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
  const onComplete = () => {
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
          {t('panel:devWorkbench.openEditor')}
        </Button>
        <Button size="small" startIcon={<TerminalOutlinedIcon />} disabled={!dir} onClick={() => openIn('terminal')}>
          {t('panel:devWorkbench.openTerminal')}
        </Button>
        <Button size="small" startIcon={<FolderOutlinedIcon />} disabled={!dir} onClick={() => openIn('finder')}>
          {t('panel:devWorkbench.openFinder')}
        </Button>
      </Box>
      <Box>
        <Button variant="contained" disabled={advancing} onClick={onComplete}>
          {t('panel:devWorkbench.devComplete')}
        </Button>
      </Box>
      {snack}
    </Stack>
  );
}
