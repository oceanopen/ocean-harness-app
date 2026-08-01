import type { ClaudeSessionInfo, TerminalApp } from '@src/shared/bindings';
import { SiIntellijidea, SiIterm2 } from '@icons-pack/react-simple-icons';
import { Terminal as TerminalIcon } from '@mui/icons-material';
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
  Unknown: 'claudeSessions:hostApp.Unknown',
};

// 暂不支持跳转的宿主终端（前端禁用按钮，避免无效 osascript 调用）。
const UNSUPPORTED_HOST: TerminalApp[] = ['IntelliJ', 'Unknown'];

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

interface ClaudeSessionCardProps {
  session: ClaudeSessionInfo;
  onOpenTerminal: (pid: number) => void;
}

function ClaudeSessionCard({ session, onOpenTerminal }: ClaudeSessionCardProps) {
  const { t } = useTranslation();
  const unsupported = UNSUPPORTED_HOST.includes(session.hostApp);
  // Java 项目判断（pom.xml / build.gradle / build.gradle.kts）：
  // 决定 VSCode/IDEA 哪个禁用——Java 项目优先 IDEA，其他优先 VSCode。
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
    <Card variant="outlined">
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
      <CardContent>
        <Typography
          color="text.secondary"
          sx={{
            fontFamily: 'monospace',
            fontSize: '0.75rem',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            mb: 0.5,
          }}
        >
          {session.cwd}
        </Typography>
        <Typography variant="caption" color="text.secondary" sx={{ opacity: 0.5 }}>
          {formatRelativeTime(session.updatedAt, t)} | {formatDate(session.updatedAt, 'YYYY-MM-DD HH:mm:ss')}
        </Typography>
      </CardContent>
      <Divider />
      <CardActions sx={{ display: 'flex', justifyContent: 'space-between' }}>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button
            size="small"
            disabled={isJava}
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
