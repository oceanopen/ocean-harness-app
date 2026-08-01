import type { Repository } from '@src/shared/bindings';
import type { MouseEvent, ReactNode } from 'react';
import { SiIntellijidea, SiIterm2 } from '@icons-pack/react-simple-icons';
import {
  AccountTree as AccountTreeIcon,
  Autorenew as AutorenewIcon,
  CloudOutlined as CloudOutlinedIcon,
  DeleteOutlined as DeleteOutlinedIcon,
  EditOutlined as EditOutlinedIcon,
  FolderOutlined as FolderOutlinedIcon,
  HistoryOutlined as HistoryOutlinedIcon,
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
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

// VSCode 官方单色品牌图标（src/assets/vscode.svg 通过 ?raw 注入，保留 currentColor 主题色跟随；同 ClaudeSessionCard）。
function VsCodeIcon() {
  return (
    <span
      style={{ display: 'inline-flex', width: '1.25rem', height: '1.25rem' }}
      // eslint-disable-next-line react/dom-no-dangerously-set-innerhtml -- 注入项目内静态 SVG 字符串，非外部输入，无 XSS 风险
      dangerouslySetInnerHTML={{ __html: vscodeIconSvg }}
    />
  );
}

// 单卡片：Header 仓库名 + 刷新/编辑/删除；Content 仓库目录(点击打开)/仓库地址/当前分支/最近提交；
// Actions 左下 VSCode/IDEA、右下 iTerm2——点击弹出 Menu 选择目标子目录（无子目录则直接打开仓库根目录）。
// 卡片 height:100% + flex column，保证网格内同行卡片高度对齐、操作栏贴底。
//
// 打开目标统一按 dir: string 传递：仓库目录行传仓库根目录，VSCode/IDEA/iTerm2 按菜单所选子目录拼接（无子目录时传根目录）。
interface RepositoryCardProps {
  repo: Repository;
  refreshing: boolean;
  onOpenFolder: (dir: string) => void;
  onOpenInTerminal: (dir: string, terminal: 'iterm2' | 'terminal') => void;
  onRefresh: (repo: Repository) => void;
  onEdit: (repo: Repository) => void;
  onDelete: (repo: Repository) => void;
}

type OpenAction = 'vscode' | 'idea' | 'iterm2';

function InfoRow({ icon, label, children }: { icon: ReactNode; label?: string; children: ReactNode }) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mb: 0.75 }}>
      <Box
        sx={{
          width: 18,
          flexShrink: 0,
          color: 'text.disabled',
          display: 'flex',
          justifyContent: 'center',
        }}
      >
        {icon}
      </Box>
      {label && (
        <Typography variant="caption" sx={{ color: 'text.disabled', flexShrink: 0, minWidth: 50 }}>
          {label}
        </Typography>
      )}
      {/* minWidth:0 让 flex 子项内文本 ellipsis 生效 */}
      <Box sx={{ minWidth: 0, flex: 1, display: 'flex', alignItems: 'flex-start' }}>{children}</Box>
    </Box>
  );
}

function RepositoryCard({ repo, refreshing, onOpenFolder, onOpenInTerminal, onRefresh, onEdit, onDelete }: RepositoryCardProps) {
  const { t } = useTranslation();
  const hasRemote = repo.remoteUrl.length > 0;
  const hasBranch = repo.branch.length > 0;
  const hasCommit = repo.lastCommitAt > 0;
  const hasSubDirs = repo.subDirList.length > 0;

  // 打开目标选择菜单：VSCode/IDEA/iTerm2 点击后若有子目录则弹 Menu 选择，无则直接打开仓库根目录。
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);
  const [menuAction, setMenuAction] = useState<OpenAction | null>(null);

  // Java 项目判断（pom.xml / build.gradle / build.gradle.kts）：
  // 决定 VSCode/IDEA 哪个禁用——Java 项目优先 IDEA，其他优先 VSCode（同 ClaudeSessionCard）。
  // 命令返回裸 Promise<boolean>（非 typedError），错误时 fallback false（按非 Java 处理）。
  const [isJava, setIsJava] = useState(false);
  useEffect(() => {
    commands.isJavaProject(repo.dir)
      .then(setIsJava)
      .catch(() => setIsJava(false));
  }, [repo.dir]);

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
      openTarget(action, repo.dir);
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
    const target = joinRepoDir(repo.dir, subDir);
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
          <Box sx={{ display: 'flex', gap: 0.5 }}>
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
        sx={{ '& .MuiCardHeader-action': { alignSelf: 'center', mt: 0 } }}
      />
      <Divider />
      <CardContent sx={{ flex: 1 }}>
        <InfoRow icon={<FolderOutlinedIcon sx={{ fontSize: '0.95rem' }} />} label={t('repositories:card.dirLabel')}>
          <Link
            component="button"
            type="button"
            onClick={() => onOpenFolder(repo.dir)}
            title={repo.dir}
            underline="hover"
            sx={{
              fontFamily: 'monospace',
              fontSize: '0.75rem',
              textAlign: 'left',
              display: 'block',
              width: '100%',
              minWidth: 0,
              ...truncateSx,
            }}
          >
            {repo.dir}
          </Link>
        </InfoRow>

        <InfoRow icon={<CloudOutlinedIcon sx={{ fontSize: '0.95rem' }} />} label={t('repositories:card.remoteLabel')}>
          <Typography variant="caption" sx={{ ...truncateSx, color: hasRemote ? 'text.primary' : 'text.disabled' }} title={repo.remoteUrl}>
            {hasRemote ? repo.remoteUrl : t('repositories:card.noRemote')}
          </Typography>
        </InfoRow>

        <InfoRow icon={<AccountTreeIcon sx={{ fontSize: '0.95rem' }} />} label={t('repositories:card.branchLabel')}>
          {hasBranch
            ? (
                <Chip size="small" variant="outlined" label={repo.branch} />
              )
            : (
                <Typography variant="caption" sx={{ color: 'text.disabled' }}>
                  {t('repositories:card.noBranch')}
                </Typography>
              )}
        </InfoRow>

        <InfoRow icon={<HistoryOutlinedIcon sx={{ fontSize: '0.95rem' }} />} label={t('repositories:card.commitLabel')}>
          <Typography variant="caption" sx={{ color: hasCommit ? 'text.secondary' : 'text.disabled' }}>
            {hasCommit
              ? `${formatRelativeTime(repo.lastCommitAt, t, 'repositories')} | ${formatDate(repo.lastCommitAt, 'YYYY-MM-DD HH:mm:ss')}`
              : t('repositories:card.noCommit')}
          </Typography>
        </InfoRow>
      </CardContent>
      <Divider />
      <CardActions sx={{ display: 'flex', justifyContent: 'space-between' }}>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button
            size="small"
            disabled={isJava}
            onClick={e => handleActionClick('vscode', e)}
            startIcon={<VsCodeIcon />}
          >
            {t('repositories:card.vscode')}
          </Button>
          <Button
            size="small"
            disabled={!isJava}
            onClick={e => handleActionClick('idea', e)}
            startIcon={<SiIntellijidea size="1.15rem" color="currentColor" />}
          >
            {t('repositories:card.idea')}
          </Button>
        </Box>
        <Button size="small" onClick={e => handleActionClick('iterm2', e)} startIcon={<SiIterm2 size="1.25rem" color="currentColor" />}>
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
            sx={{ fontFamily: 'monospace', fontSize: '0.8rem' }}
          >
            {sub.subDir}
          </MenuItem>
        ))}
      </Menu>
    </Card>
  );
}

export default RepositoryCard;
