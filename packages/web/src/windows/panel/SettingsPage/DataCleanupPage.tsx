import type { DataFilePaths } from '@src/shared/bindings';
import ArticleOutlinedIcon from '@mui/icons-material/ArticleOutlined';
import ContentCopyOutlinedIcon from '@mui/icons-material/ContentCopyOutlined';
import SettingsSuggestOutlinedIcon from '@mui/icons-material/SettingsSuggestOutlined';
import StorageOutlinedIcon from '@mui/icons-material/StorageOutlined';
import {
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Stack,
  Typography,
} from '@mui/material';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { useToast } from '@src/shared/useToast';
import { useCallback, useEffect, useState } from 'react';
import { SectionCard, SectionHeader } from './SectionCard';

// 数据清理分区页：应用设置（app.db）、服务数据（server.db）、历史日志三个清理项。
// 应用设置走 SQL 级清空 + 自动重启收敛；服务数据走停 sidecar → 删库 → 重拉 + 整页刷新；
// 日志仅删轮转归档（当前写入文件保留）点击直接执行，重置类均走 Dialog 二次确认。

// 页内文案集中定义（按需求 i18n 仅接入菜单级；文案集中于此，后续迁移时整体搬入
// settings.json 的 dataCleanup 段即可）。
// TODO(i18n): 页内文案迁移到 settings:dataCleanup.* 词条。
const TEXTS = {
  app: {
    title: '应用设置',
    desc: '重置应用设置数据库的全部数据：界面语言、外观、终端配置、轮询周期、GitHub Personal Access Token 等将全部恢复默认值。',
    dialogTitle: '重置应用设置',
    dialogMsg: '重置后，会自动重启应用，是否确认重置？',
  },
  server: {
    title: '服务数据',
    desc: '重置服务数据库的全部数据：工作空间、issue 与子任务、IM 机器人配置与会话绑定、本地仓库登记等将全部清空，回到全新安装状态；服务将自动重启。',
    dialogTitle: '重置服务数据',
    dialogMsg: '将清空服务数据库的全部数据（工作空间、issue、机器人配置、本地仓库登记等），操作不可恢复；服务将自动重启。是否确认重置？',
  },
  logs: {
    title: '日志清理',
    desc: '清理应用与服务运行产生的历史日志文件（轮转归档）；正在写入的当前日志文件保留。',
  },
} as const;

// 路径展示行：label 缺省时仅展示路径（存量卡片形态），有 label 用于日志目录等多行场景。
interface CleanupPathRow {
  label?: string;
  path: string;
}

interface CleanupCardProps {
  icon: React.ReactNode;
  title: string;
  desc: string;
  paths: CleanupPathRow[];
  disabled: boolean;
  onCleanup: () => void;
  onCopyPath: (path: string) => void;
  cleanupLabel?: string;
  busy?: boolean;
}

// 单个清理项卡片：标题栏 + 影响面描述 + 路径行（各行复制按钮）+ 清理按钮。
function CleanupCard({ icon, title, desc, paths: rows, disabled, onCleanup, onCopyPath, cleanupLabel = '重置', busy = false }: CleanupCardProps) {
  return (
    <SectionCard header={<SectionHeader icon={icon} label={title} />}>
      <Box sx={{ px: '16px', py: '16px', display: 'flex', flexDirection: 'column', gap: '12px' }}>
        <Typography variant="body2" color="text.secondary">
          {desc}
        </Typography>
        {rows.map(({ label, path }) => (
          <Stack key={path} direction="row" spacing={1} sx={{ alignItems: 'center' }}>
            <IconButton
              size="small"
              onClick={() => void onCopyPath(path)}
              aria-label={label != null ? `复制${label}` : '复制路径'}
            >
              <ContentCopyOutlinedIcon fontSize="small" />
            </IconButton>
            {label != null && (
              <Typography sx={{ fontSize: '12px', color: 'text.secondary', flexShrink: 0 }}>
                {label}
              </Typography>
            )}
            <Typography sx={{ fontFamily: 'monospace', wordBreak: 'break-all', fontSize: '12px' }}>
              {path}
            </Typography>
          </Stack>
        ))}
        <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
          <Button color="error" variant="outlined" disabled={disabled} onClick={onCleanup}>
            {busy ? <CircularProgress size={16} color="inherit" /> : cleanupLabel}
          </Button>
        </Box>
      </Box>
    </SectionCard>
  );
}

function DataCleanupPage() {
  const { show: showToast, snack } = useToast();
  const [paths, setPaths] = useState<DataFilePaths | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  // 二次确认目标：null = 弹窗关闭；'app' / 'server' 决定确认后执行的清理流。
  const [target, setTarget] = useState<'app' | 'server' | null>(null);
  const [resetting, setResetting] = useState(false);
  // 日志清理与重置流相互独立，单独 loading。
  const [cleaningLogs, setCleaningLogs] = useState(false);

  useEffect(() => {
    unwrap(commands.getDataFilePaths())
      .then(setPaths)
      .catch((e: unknown) => {
        setLoadError(String(e));
      });
  }, []);

  const copyPath = useCallback(async (path: string) => {
    try {
      await navigator.clipboard.writeText(path);
      showToast('已复制路径', 'success');
    } catch (e) {
      showToast(`复制失败：${String(e)}`, 'error');
    }
  }, [showToast]);

  // 应用设置：SQL 级清空后立即触发延迟重启（重启收敛全部内存态，本身即反馈）；
  // 失败留在弹窗内报错并可重试。
  const handleConfirmApp = useCallback(async () => {
    setResetting(true);
    try {
      await unwrap(commands.resetAppConfig());
      await commands.restartApp();
      // 成功后应用即将重启，无需收尾（保持 resetting 阻断残余交互）。
    } catch (e) {
      showToast(`重置应用设置失败：${String(e)}`, 'error');
      setResetting(false);
    }
  }, [showToast]);

  // 服务数据：Rust 停 sidecar → 删 server.db → 重拉；成功后整页刷新清空前端缓存。
  const handleConfirmServer = useCallback(async () => {
    setResetting(true);
    try {
      await unwrap(commands.resetServerData());
      setTarget(null);
      showToast('已重置服务数据', 'success');
      // 留出 toast 展示时间再整页刷新（清空 Query 缓存等全部服务数据视图）。
      window.setTimeout(() => window.location.reload(), 800);
    } catch (e) {
      showToast(`重置服务数据失败：${String(e)}`, 'error');
    } finally {
      setResetting(false);
    }
  }, [showToast]);

  // 日志清理仅删轮转归档、无重启副作用：点击直接执行，toast 反馈删除计数。
  const handleCleanLogs = useCallback(async () => {
    setCleaningLogs(true);
    try {
      const result = await unwrap(commands.cleanLogFiles());
      showToast(`已清理 ${result.appFiles + result.serverFiles} 个日志文件`, 'success');
    } catch (e) {
      showToast(`日志清理失败：${String(e)}`, 'error');
    } finally {
      setCleaningLogs(false);
    }
  }, [showToast]);

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Box sx={{ p: '24px', flex: 1, overflow: 'auto' }}>
        {/* 路径未就绪前不渲染清理卡（编码规则 1：禁止先占位后纠正）；失败给出明确错误。 */}
        {paths === null && loadError === null && (
          <Box sx={{ display: 'flex', justifyContent: 'center', p: '32px' }}>
            <CircularProgress />
          </Box>
        )}
        {loadError !== null && (
          <Typography color="error">{`数据文件路径获取失败：${loadError}`}</Typography>
        )}
        {paths !== null && (
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
            <CleanupCard
              icon={<SettingsSuggestOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />}
              title={TEXTS.app.title}
              desc={TEXTS.app.desc}
              paths={[{ path: paths.appDbPath }]}
              disabled={resetting}
              onCleanup={() => setTarget('app')}
              onCopyPath={copyPath}
            />
            <CleanupCard
              icon={<StorageOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />}
              title={TEXTS.server.title}
              desc={TEXTS.server.desc}
              paths={[{ path: paths.serverDbPath }]}
              disabled={resetting}
              onCleanup={() => setTarget('server')}
              onCopyPath={copyPath}
            />
            <CleanupCard
              icon={<ArticleOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />}
              title={TEXTS.logs.title}
              desc={TEXTS.logs.desc}
              paths={[
                { label: '应用日志目录', path: paths.appLogDir },
                { label: '服务日志目录', path: paths.serverLogDir },
              ]}
              disabled={resetting || cleaningLogs}
              busy={cleaningLogs}
              cleanupLabel="清理"
              onCleanup={() => void handleCleanLogs()}
              onCopyPath={copyPath}
            />
          </Box>
        )}
      </Box>

      <Dialog open={target !== null} onClose={resetting ? undefined : () => setTarget(null)}>
        <DialogTitle>
          {target === 'app' ? TEXTS.app.dialogTitle : TEXTS.server.dialogTitle}
        </DialogTitle>
        <DialogContent>
          <Typography>
            {target === 'app' ? TEXTS.app.dialogMsg : TEXTS.server.dialogMsg}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setTarget(null)} disabled={resetting}>
            取消
          </Button>
          <Button
            color="error"
            variant="contained"
            disabled={resetting}
            onClick={target === 'app' ? handleConfirmApp : handleConfirmServer}
          >
            {resetting ? <CircularProgress size={16} color="inherit" /> : '重置'}
          </Button>
        </DialogActions>
      </Dialog>

      {snack}
    </Box>
  );
}

export default DataCleanupPage;
