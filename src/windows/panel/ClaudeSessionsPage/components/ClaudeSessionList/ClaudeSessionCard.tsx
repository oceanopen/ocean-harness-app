import type { ClaudeSessionInfo, TerminalApp } from '@src/shared/bindings';
import type { ReactNode } from 'react';
import { SiIntellijidea, SiIterm2 } from '@icons-pack/react-simple-icons';
import { FolderOutlined as FolderOutlinedIcon, HistoryOutlined as HistoryOutlinedIcon, Terminal as TerminalIcon } from '@mui/icons-material';
import {
  Box,
  Button,
  Card,
  CardActions,
  CardContent,
  CardHeader,
  Divider,
  Typography,
} from '@mui/material';
import vscodeIconSvg from '@src/assets/vscode.svg?raw';
import { commands } from '@src/shared/bindings';
import { CLAUDE_SESSION_STATUS_COLOR, CLAUDE_SESSION_STATUS_I18N_KEY } from '@src/shared/claudeSessionStatus';
import { unwrap } from '@src/shared/commands';
import { formatDate, formatRelativeTime } from '@src/shared/time';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

const hostAppI18nKey: Record<TerminalApp, string> = {
  ITerm2: 'claudeSessions:hostApp.ITerm2',
  Terminal: 'claudeSessions:hostApp.Terminal',
  IntelliJ: 'claudeSessions:hostApp.IntelliJ',
  WeTerm: 'claudeSessions:hostApp.WeTerm',
  Unknown: 'claudeSessions:hostApp.Unknown',
};

// 暂不支持跳转的宿主终端（前端禁用按钮，避免无效 osascript 调用）。
// WeTerm：本 app 嵌入终端，聚焦联动在后续模块接线（当前禁用跳转但不过滤展示）。
const UNSUPPORTED_HOST: TerminalApp[] = ['IntelliJ', 'WeTerm', 'Unknown'];

// VSCode 官方单色品牌图标（src/assets/vscode.svg 通过 ?raw 注入，保留 currentColor 主题色跟随）。
function VsCodeIcon() {
  return (
    <span
      style={{ display: 'inline-flex', width: '1.25rem', height: '1.25rem' }}
      // eslint-disable-next-line react/dom-no-dangerously-set-innerhtml -- 注入项目内静态 SVG 字符串，非外部输入，无 XSS 风险
      dangerouslySetInnerHTML={{ __html: vscodeIconSvg }}
    />
  );
}

const truncateSx = {
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
} as const;

// 卡片信息行：固定 18px 图标列 + 标签列(minWidth:50) + 值列(minWidth:0 支持 ellipsis)。
// 复刻 RepositoryCard 的 InfoRow，保持两页卡片视觉一致（项目无共享卡片组件，按页面自包含惯例本地复制）。
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

interface ClaudeSessionCardProps {
  session: ClaudeSessionInfo;
  onOpenTerminal: (pid: number) => void;
}

function ClaudeSessionCard({ session, onOpenTerminal }: ClaudeSessionCardProps) {
  const { t } = useTranslation();
  const unsupported = UNSUPPORTED_HOST.includes(session.hostApp);
  // Java 项目判断（pom.xml / build.gradle / build.gradle.kts）：
  // 仅用于控制 IDEA——非 Java 项目禁用 IDEA；VSCode 在任何项目下都可用。
  // 命令返回裸 Promise<boolean>（非 typedError），错误时 fallback false（按非 Java 处理）。
  const [isJava, setIsJava] = useState(false);
  useEffect(() => {
    commands.isJavaProject(session.cwd)
      .then(setIsJava)
      .catch(() => setIsJava(false));
  }, [session.cwd]);

  // 编辑器打开：code/idea CLI 命令不存在时后端返回 Err，前端静默 warn（编辑器未装的常见场景，
  // 不值得用 toast 打断；用户从无响应自行判断）。
  const handleOpenInEditor = useCallback((editor: 'vscode' | 'idea') => {
    unwrap(commands.openInEditor(editor, session.cwd)).catch((e) => {
      console.warn(`[claude-sessions] openInEditor(${editor}) failed`, e);
    });
  }, [session.cwd]);

  return (
    <Card variant="outlined" sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <CardHeader
        title={session.projectName || session.cwd}
        slotProps={{ title: { fontWeight: 600, noWrap: true } }}
        sx={{ '& .MuiCardHeader-action': { alignSelf: 'center', mt: 0 } }}
        action={(
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
            <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: CLAUDE_SESSION_STATUS_COLOR[session.status] }} />
            <Typography
              variant="caption"
              sx={{ color: CLAUDE_SESSION_STATUS_COLOR[session.status], fontWeight: 700, fontSize: '0.7rem' }}
            >
              {t(CLAUDE_SESSION_STATUS_I18N_KEY[session.status])}
            </Typography>
          </Box>
        )}
      />
      <Divider />
      <CardContent sx={{ flex: 1 }}>
        <InfoRow icon={<FolderOutlinedIcon sx={{ fontSize: '0.95rem' }} />} label={t('claudeSessions:card.dirLabel')}>
          <Typography
            variant="caption"
            title={session.cwd}
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
            {session.cwd}
          </Typography>
        </InfoRow>
        <InfoRow icon={<HistoryOutlinedIcon sx={{ fontSize: '0.95rem' }} />} label={t('claudeSessions:card.timeLabel')}>
          <Typography variant="caption" sx={{ color: 'text.secondary' }}>
            {formatRelativeTime(session.updatedAt, t)} | {formatDate(session.updatedAt, 'YYYY-MM-DD HH:mm:ss')}
          </Typography>
        </InfoRow>
      </CardContent>
      <Divider />
      <CardActions sx={{ display: 'flex', justifyContent: 'space-between' }}>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button
            size="small"
            onClick={() => handleOpenInEditor('vscode')}
            startIcon={<VsCodeIcon />}
          >
            {t('claudeSessions:editor.vscode')}
          </Button>
          <Button
            size="small"
            disabled={!isJava}
            onClick={() => handleOpenInEditor('idea')}
            startIcon={<SiIntellijidea size="1.15rem" color="currentColor" />}
          >
            {t('claudeSessions:editor.idea')}
          </Button>
        </Box>
        <Button
          size="small"
          disabled={unsupported}
          onClick={() => onOpenTerminal(session.pid)}
          startIcon={session.hostApp === 'ITerm2' ? <SiIterm2 size="1.25rem" color="currentColor" /> : <TerminalIcon style={{ fontSize: '1.5rem' }} />}
        >
          {t(hostAppI18nKey[session.hostApp])}
        </Button>
      </CardActions>
    </Card>
  );
}

export default ClaudeSessionCard;
