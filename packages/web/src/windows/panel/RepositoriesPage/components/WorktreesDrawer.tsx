import type { LocalRepositoryModel, RepoWorktreeModel } from '@src/services';
import { SiIterm2 } from '@icons-pack/react-simple-icons';
import { AccountTree as AccountTreeIcon, CloseOutlined as CloseOutlinedIcon, FolderOutlined as FolderOutlinedIcon, RefreshOutlined as RefreshOutlinedIcon } from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  Card,
  CardActions,
  CardContent,
  CardHeader,
  Chip,
  CircularProgress,
  Divider,
  IconButton,
  Link,
  Typography,
} from '@mui/material';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import ResizableDrawer from '@src/shared/ResizableDrawer';
import { useLocalRepositoryWorktreeList } from '@src/state/localRepositories';
import { useTranslation } from 'react-i18next';
import { InfoRow, VsCodeIcon } from './RepositoryCard';
import { truncateSx } from './shared';

// worktree 抽屉。
interface WorktreesDrawerProps {
  repo: LocalRepositoryModel;
  onClose: () => void;
  // Finder 打开走页面级回调（失败 toast 归口页面，同 onOpenInTerminal）。
  onOpenFolder: (dir: string) => void;
  // iTerm2 打开走页面级回调（失败 toast 归口页面）；VS Code 失败静默 warn（同仓库卡）。
  onOpenInTerminal: (dir: string, terminal: 'iterm2' | 'terminal') => void;
}

// 从未知错误对象提取 message（Go 经 http.ts 抛 new Error(msg)，Rust IPC 抛裸字符串，两者兼容）。
function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

// VS Code 打开 worktree 目录：失败仅 warn（编辑器未装的常见场景，同仓库卡与 ClaudeSessionCard）。
function openInVsCode(dir: string) {
  unwrap(commands.openInEditor('vscode', dir)).catch((e) => {
    console.warn('[repositories] openInEditor(vscode) failed', e);
  });
}

/*
 * 单个 worktree 卡片，与仓库卡同 anatomy：Header 目录名 → Divider → Content 信息行（目录 Link 点击 Finder 打开、
 * 分支 Chip）→ Divider → 操作栏（space-between：左 VS Code、右 iTerm2，无子目录 Menu、无 IDEA）。
 * 标题取目录路径末段，tooltip 保留完整路径。
 */
function WorktreeCard({ wt, onOpenFolder, onOpenInTerminal }: {
  wt: RepoWorktreeModel;
  onOpenFolder: WorktreesDrawerProps['onOpenFolder'];
  onOpenInTerminal: WorktreesDrawerProps['onOpenInTerminal'];
}) {
  const { t } = useTranslation();
  const name = wt.dir.split(/[\\/]/).filter(Boolean).pop() ?? wt.dir;

  return (
    <Card variant="outlined" sx={{ display: 'flex', flexDirection: 'column' }}>
      <CardHeader
        title={name}
        slotProps={{ title: { fontWeight: 600, noWrap: true, title: wt.dir } }}
      />
      <Divider />
      <CardContent sx={{ flex: 1 }}>
        <InfoRow icon={<FolderOutlinedIcon sx={{ fontSize: '15px' }} />} label={t('repositories:card.dirLabel')}>
          <Link
            component="button"
            type="button"
            onClick={() => onOpenFolder(wt.dir)}
            title={wt.dir}
            underline="hover"
            sx={{
              fontFamily: 'monospace',
              fontSize: '12px',
              textAlign: 'left',
              // inline-block + maxWidth 让热区收窄到文字本身（同仓库卡目录行）。
              display: 'inline-block',
              maxWidth: '100%',
              minWidth: '0px',
              ...truncateSx,
            }}
          >
            {wt.dir}
          </Link>
        </InfoRow>
        <InfoRow icon={<AccountTreeIcon sx={{ fontSize: '15px' }} />} label={t('repositories:card.branchLabel')}>
          {wt.branch
            ? <Chip size="small" variant="outlined" label={wt.branch} />
            : (
                <Typography variant="caption" sx={{ color: 'text.disabled' }}>
                  {t('repositories:worktrees.noBranch')}
                </Typography>
              )}
        </InfoRow>
      </CardContent>
      <Divider />
      <CardActions sx={{ display: 'flex', justifyContent: 'space-between' }}>
        <Button size="small" onClick={() => openInVsCode(wt.dir)} startIcon={<VsCodeIcon />}>
          {t('repositories:card.vscode')}
        </Button>
        <Button
          size="small"
          onClick={() => onOpenInTerminal(wt.dir, 'iterm2')}
          startIcon={<SiIterm2 size={20} color="currentColor" />}
        >
          {t('repositories:card.iTerm2')}
        </Button>
      </CardActions>
    </Card>
  );
}

// worktree 列表抽屉：ResizableDrawer 壳 + 头部（标题 + 刷新/关闭）+ 卡片纵向列表（loading/error/空状态）。
// 数据与卡片数量同源（worktreeList 共享缓存），刷新 = refetch 该接口（仅解析 worktree，轻量）。
function WorktreesDrawer({ repo, onClose, onOpenFolder, onOpenInTerminal }: WorktreesDrawerProps) {
  const { t } = useTranslation();
  const { data, isPending, isError, error, refetch, isFetching } = useLocalRepositoryWorktreeList();
  const worktrees = data?.find(e => e.id === repo.id)?.worktrees ?? [];

  return (
    <ResizableDrawer
      open
      onClose={onClose}
      defaultWidthPct={40}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* 头部 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', p: '16px', borderBottom: '1px solid', borderColor: 'divider' }}>
          <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }} noWrap>
            {t('repositories:worktrees.title', { name: repo.name })}
          </Typography>
          <IconButton size="small" onClick={() => void refetch()} disabled={isFetching} aria-label={t('repositories:worktrees.refresh')}>
            <RefreshOutlinedIcon fontSize="small" />
          </IconButton>
          <IconButton size="small" onClick={onClose} aria-label={t('repositories:add.cancel')}>
            <CloseOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 内容 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: '16px', display: 'flex', flexDirection: 'column', gap: '12px' }}>
          {isPending && (
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', py: '48px' }}>
              <CircularProgress />
            </Box>
          )}
          {isError && (
            <Alert severity="error" action={<Button color="inherit" size="small" onClick={() => void refetch()}>{t('repositories:error.retry')}</Button>}>
              {t('repositories:worktrees.loadFailed', { message: errMsg(error) })}
            </Alert>
          )}
          {!isPending && !isError && worktrees.length === 0 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', py: '48px', gap: '8px' }}>
              <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
                {t('repositories:worktrees.empty')}
              </Typography>
              <Typography variant="body2" color="text.secondary" align="center">
                {t('repositories:worktrees.emptyDesc')}
              </Typography>
            </Box>
          )}
          {worktrees.map(wt => (
            <WorktreeCard key={wt.dir} wt={wt} onOpenFolder={onOpenFolder} onOpenInTerminal={onOpenInTerminal} />
          ))}
        </Box>
      </Box>
    </ResizableDrawer>
  );
}

export default WorktreesDrawer;
