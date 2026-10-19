import type { LocalRepositoryModel } from '@src/services';
import type { MouseEvent, ReactNode } from 'react';
import { SiIntellijidea, SiIterm2 } from '@icons-pack/react-simple-icons';
import {
  AccountTree as AccountTreeIcon,
  Autorenew as AutorenewIcon,
  CallSplitOutlined as CallSplitOutlinedIcon,
  CloudOutlined as CloudOutlinedIcon,
  DeleteOutlined as DeleteOutlinedIcon,
  EditOutlined as EditOutlinedIcon,
  FolderOutlined as FolderOutlinedIcon,
  HistoryOutlined as HistoryOutlinedIcon,
  StarBorderOutlined as StarBorderOutlinedIcon,
} from '@mui/icons-material';
import {
  Box,
  Button,
  Card,
  CardActions,
  CardContent,
  CardHeader,
  Chip,
  Divider,
  IconButton,
  Link,
  Menu,
  MenuItem,
  Typography,
} from '@mui/material';
import vscodeIconSvg from '@src/assets/vscode.svg?raw';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { joinRepoDir } from '@src/shared/repoPath';
import { formatDate, formatRelativeTime } from '@src/shared/time';
import { useLocalRepositoryWorktreeList } from '@src/state/localRepositories';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

// VSCode 官方单色品牌图标（src/assets/vscode.svg 通过 ?raw 注入，保留 currentColor 主题色跟随；同 ClaudeSessionCard）。
// 导出供 WorktreesDrawer 操作栏复用。
export function VsCodeIcon() {
  return (
    <span
      style={{ display: 'inline-flex', width: '1.25rem', height: '1.25rem' }}
      // eslint-disable-next-line react/dom-no-dangerously-set-innerhtml -- 注入项目内静态 SVG 字符串，非外部输入，无 XSS 风险
      dangerouslySetInnerHTML={{ __html: vscodeIconSvg }}
    />
  );
}

// 单卡片：Header 仓库名 + 刷新/编辑/删除；Content 仓库目录(点击打开)/仓库地址/当前分支/最近提交/worktree 数量(点击开抽屉)；
// Actions 左下 VSCode/IDEA、右下 iTerm2——点击弹出 Menu 选择目标子目录（无子目录则直接打开仓库根目录）。
// 卡片 height:100% + flex column，保证网格内同行卡片高度对齐、操作栏贴底。
//
// 目录/worktree 数量的 Link 热区仅限文字本身（inline-block + maxWidth 收窄），避免整行误触跳转。
//
// 数据来自 Go 服务 LocalRepositoryModel（localDir/currentBranch）。OS 原生动作（打开 IDE/终端/Finder、
// Java 判断）仍走 Rust commands，传入 repo.localDir 作为目标目录。
interface RepositoryCardProps {
  repo: LocalRepositoryModel;
  refreshing: boolean;
  onOpenFolder: (dir: string) => void;
  onOpenInTerminal: (dir: string, terminal: 'iterm2' | 'terminal') => void;
  onOpenWorktrees: (repo: LocalRepositoryModel) => void;
  onRefresh: (repo: LocalRepositoryModel) => void;
  onEdit: (repo: LocalRepositoryModel) => void;
  onDelete: (repo: LocalRepositoryModel) => void;
}

type OpenAction = 'vscode' | 'idea' | 'iterm2';

function InfoRow({ icon, label, children }: { icon: ReactNode; label?: string; children: ReactNode }) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: '6px', mb: '6px' }}>
      <Box
        sx={{
          width: '18px',
          flexShrink: 0,
          color: 'text.disabled',
          display: 'flex',
          justifyContent: 'center',
        }}
      >
        {icon}
      </Box>
      {label && (
        <Typography variant="caption" sx={{ color: 'text.disabled', flexShrink: 0, minWidth: '50px' }}>
          {label}
        </Typography>
      )}
      {/* minWidth:0 让 flex 子项内文本 ellipsis 生效 */}
      <Box sx={{ minWidth: 0, flex: 1, display: 'flex', alignItems: 'flex-start' }}>{children}</Box>
    </Box>
  );
}

function RepositoryCard({ repo, refreshing, onOpenFolder, onOpenInTerminal, onOpenWorktrees, onRefresh, onEdit, onDelete }: RepositoryCardProps) {
  const { t } = useTranslation();
  const hasRemote = repo.remoteUrl.length > 0;
  const hasBranch = repo.currentBranch.length > 0;
  const hasDefaultBranch = repo.defaultBranch.length > 0;
  const hasCommit = repo.lastCommitAt > 0;
  const hasSubDirs = repo.subDirList.length > 0;
  // 额外 worktree 数量：与抽屉同源（共享 worktreeList 缓存，多卡片同 query 只发一次请求）。
  const worktreeCount = useLocalRepositoryWorktreeList().data?.find(e => e.id === repo.id)?.worktrees.length ?? 0;

  // 打开目标选择菜单：VSCode/IDEA/iTerm2 点击后若有子目录则弹 Menu 选择，无则直接打开仓库根目录。
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);
  const [menuAction, setMenuAction] = useState<OpenAction | null>(null);

  // Java 项目判断（pom.xml / build.gradle / build.gradle.kts）：
  // 仅用于控制 IDEA——非 Java 项目禁用 IDEA；VSCode 在任何项目下都可用（同 ClaudeSessionCard）。
  // 命令返回裸 Promise<boolean>（非 typedError），错误时 fallback false（按非 Java 处理）。
  const [isJava, setIsJava] = useState(false);
  useEffect(() => {
    commands.isJavaProject(repo.localDir)
      .then(setIsJava)
      .catch(() => setIsJava(false));
  }, [repo.localDir]);

  // 执行打开：vscode/idea 走 openInEditor（失败静默 warn，编辑器未装的常见场景；同 ClaudeSessionCard）；
  // iterm2 走 onOpenInTerminal（失败 toast）。
  const openTarget = useCallback((action: OpenAction, dir: string) => {
    if (action === 'iterm2') {
      onOpenInTerminal(dir, 'iterm2');
    } else {
      unwrap(commands.openInEditor(action, dir)).catch((e) => {
        console.warn(`[repositories] openInEditor(${action}) failed`, e);
      });
    }
  }, [onOpenInTerminal]);

  // 操作按钮点击：无子目录直接打开仓库根目录；有子目录弹 Menu 选择。
  const handleActionClick = (action: OpenAction, event: MouseEvent<HTMLElement>) => {
    if (!hasSubDirs) {
      openTarget(action, repo.localDir);
      return;
    }
    setMenuAction(action);
    setMenuAnchor(event.currentTarget);
  };

  const handleMenuClose = () => {
    setMenuAnchor(null);
    setMenuAction(null);
  };

  // 菜单选中某子目录：拼接仓库目录 + 子目录后打开。
  const handleMenuItemClick = (subDir: string) => {
    const target = joinRepoDir(repo.localDir, subDir);
    const action = menuAction;
    handleMenuClose();
    if (action) {
      openTarget(action, target);
    }
  };

  return (
    <Card variant="outlined" sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <CardHeader
        title={repo.name}
        slotProps={{ title: { fontWeight: 600, noWrap: true } }}
        action={(
          <Box sx={{ display: 'flex', gap: '4px' }}>
            <IconButton
              size="small"
              onClick={() => onRefresh(repo)}
              disabled={refreshing}
              aria-label={t('repositories:card.refresh')}
            >
              <AutorenewIcon
                sx={{
                  'animation': refreshing ? 'spin 0.8s linear infinite' : undefined,
                  '@keyframes spin': {
                    from: { transform: 'rotate(0deg)' },
                    to: { transform: 'rotate(360deg)' },
                  },
                }}
              />
            </IconButton>
            <IconButton size="small" onClick={() => onEdit(repo)} aria-label={t('repositories:card.edit')}>
              <EditOutlinedIcon />
            </IconButton>
            <IconButton size="small" onClick={() => onDelete(repo)} aria-label={t('repositories:card.delete')}>
              <DeleteOutlinedIcon />
            </IconButton>
          </Box>
        )}
        sx={{ '& .MuiCardHeader-action': { alignSelf: 'center', mt: '0px' } }}
      />
      <Divider />
      <CardContent sx={{ flex: 1 }}>
        <InfoRow icon={<FolderOutlinedIcon sx={{ fontSize: '15px' }} />} label={t('repositories:card.dirLabel')}>
          <Link
            component="button"
            type="button"
            onClick={() => onOpenFolder(repo.localDir)}
            title={repo.localDir}
            underline="hover"
            sx={{
              fontFamily: 'monospace',
              fontSize: '12px',
              textAlign: 'left',
              // inline-block + maxWidth 让热区收窄到文字本身（block + 100% 会让整行都可点击误触），
              // maxWidth 同时保留 ellipsis 截断能力。
              display: 'inline-block',
              maxWidth: '100%',
              minWidth: '0px',
              ...truncateSx,
            }}
          >
            {repo.localDir}
          </Link>
        </InfoRow>

        <InfoRow icon={<CloudOutlinedIcon sx={{ fontSize: '15px' }} />} label={t('repositories:card.remoteLabel')}>
          <Typography variant="caption" sx={{ ...truncateSx, color: hasRemote ? 'text.primary' : 'text.disabled' }} title={repo.remoteUrl}>
            {hasRemote ? repo.remoteUrl : t('repositories:card.noRemote')}
          </Typography>
        </InfoRow>

        <InfoRow icon={<StarBorderOutlinedIcon sx={{ fontSize: '15px' }} />} label={t('repositories:card.defaultBranchLabel')}>
          {hasDefaultBranch
            ? (
                <Chip size="small" variant="outlined" label={repo.defaultBranch} />
              )
            : (
                <Typography variant="caption" sx={{ color: 'text.disabled' }}>
                  {t('repositories:card.noDefaultBranch')}
                </Typography>
              )}
        </InfoRow>

        <InfoRow icon={<AccountTreeIcon sx={{ fontSize: '15px' }} />} label={t('repositories:card.branchLabel')}>
          {hasBranch
            ? (
                <Chip size="small" variant="outlined" label={repo.currentBranch} />
              )
            : (
                <Typography variant="caption" sx={{ color: 'text.disabled' }}>
                  {t('repositories:card.noBranch')}
                </Typography>
              )}
        </InfoRow>

        <InfoRow icon={<HistoryOutlinedIcon sx={{ fontSize: '15px' }} />} label={t('repositories:card.commitLabel')}>
          <Typography variant="caption" sx={{ color: hasCommit ? 'text.secondary' : 'text.disabled' }}>
            {hasCommit
              ? `${formatRelativeTime(repo.lastCommitAt, t, 'repositories')} | ${formatDate(repo.lastCommitAt, 'YYYY-MM-DD HH:mm:ss')}`
              : t('repositories:card.noCommit')}
          </Typography>
        </InfoRow>

        {/* worktree 数量：>0 为可点 link 开抽屉，未就绪/为 0 显示 '--' 不可点。 */}
        <InfoRow icon={<CallSplitOutlinedIcon sx={{ fontSize: '15px' }} />} label={t('repositories:card.worktreeLabel')}>
          {worktreeCount > 0
            ? (
                <Link
                  component="button"
                  type="button"
                  onClick={() => onOpenWorktrees(repo)}
                  underline="hover"
                  sx={{ fontSize: '12px', display: 'inline-block' }}
                >
                  {worktreeCount} 个
                </Link>
              )
            : (
                <Typography variant="caption" sx={{ color: 'text.disabled' }}>
                  --
                </Typography>
              )}
        </InfoRow>
      </CardContent>
      <Divider />
      <CardActions sx={{ display: 'flex', justifyContent: 'space-between' }}>
        <Box sx={{ display: 'flex', gap: '8px' }}>
          <Button
            size="small"
            onClick={e => handleActionClick('vscode', e)}
            startIcon={<VsCodeIcon />}
          >
            {t('repositories:card.vscode')}
          </Button>
          <Button
            size="small"
            disabled={!isJava}
            onClick={e => handleActionClick('idea', e)}
            startIcon={<SiIntellijidea size={18} color="currentColor" />}
          >
            {t('repositories:card.idea')}
          </Button>
        </Box>
        <Button size="small" onClick={e => handleActionClick('iterm2', e)} startIcon={<SiIterm2 size={20} color="currentColor" />}>
          {t('repositories:card.iTerm2')}
        </Button>
      </CardActions>

      {/* 子目录选择菜单：仅当 subDirList 非空时，操作按钮点击触发。 */}
      <Menu anchorEl={menuAnchor} open={Boolean(menuAnchor)} onClose={handleMenuClose}>
        {repo.subDirList.map(sub => (
          <MenuItem
            key={sub.subDir}
            onClick={() => handleMenuItemClick(sub.subDir)}
            title={sub.subDirDescription || sub.subDir}
            sx={{ fontFamily: 'monospace', fontSize: '13px' }}
          >
            {sub.subDir}
          </MenuItem>
        ))}
      </Menu>
    </Card>
  );
}

export default RepositoryCard;
