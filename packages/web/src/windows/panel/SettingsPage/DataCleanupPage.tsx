import type { DataFilePaths } from '@src/shared/bindings';
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

// 数据清理分区页：应用设置（app.db）与服务数据（server.db）两个清理项，各自展示
// 数据库文件实际路径并提供「重置」（均为不可逆操作，Dialog 二次确认）。
// 应用设置走 SQL 级清空 + 自动重启应用收敛；服务数据走 Rust 停 sidecar → 删库文件 →
// 重拉（迁移自动重建），成功后整页刷新清空前端缓存。

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
} as const;

// 模块段卡片标题栏/容器：与 TerminalConfigPage 本地组件同构（设置页各分区自持样式，
// 出现第三个消费方时再考虑提升到公共组件）。
function SectionHeader({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        px: 2,
        py: 1.5,
        bgcolor: 'action.hover',
        borderBottom: 1,
        borderColor: 'divider',
      }}
    >
      {icon}
      <Typography variant="body2" sx={{ fontWeight: 600 }}>
        {label}
      </Typography>
    </Box>
  );
}

function SectionCard({ header, children }: { header: React.ReactNode; children: React.ReactNode }) {
  return (
    <Box sx={{ borderRadius: 2, border: 1, borderColor: 'divider', overflow: 'hidden' }}>
      {header}
      {children}
    </Box>
  );
}

interface CleanupCardProps {
  icon: React.ReactNode;
  title: string;
  desc: string;
  path: string;
  disabled: boolean;
  onCleanup: () => void;
  onCopyPath: (path: string) => void;
}

// 单个清理项卡片：标题栏 + 影响面描述 + 数据库文件路径（复制按钮）+ 清理按钮。
function CleanupCard({ icon, title, desc, path, disabled, onCleanup, onCopyPath }: CleanupCardProps) {
  return (
    <SectionCard header={<SectionHeader icon={icon} label={title} />}>
      <Box sx={{ px: 2, py: 2, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
        <Typography variant="body2" color="text.secondary">
          {desc}
        </Typography>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          <IconButton
            size="small"
            onClick={() => void onCopyPath(path)}
            aria-label="复制数据库文件路径"
          >
            <ContentCopyOutlinedIcon fontSize="small" />
          </IconButton>
          <Typography sx={{ fontFamily: 'monospace', wordBreak: 'break-all', fontSize: 12 }}>
            {path}
          </Typography>
        </Stack>
        <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
          <Button color="error" variant="outlined" disabled={disabled} onClick={onCleanup}>
            重置
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

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Box sx={{ p: 3, flex: 1, overflow: 'auto' }}>
        {/* 路径未就绪前不渲染清理卡（编码规则 1：禁止先占位后纠正）；失败给出明确错误。 */}
        {paths === null && loadError === null && (
          <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
            <CircularProgress />
          </Box>
        )}
        {loadError !== null && (
          <Typography color="error">{`数据文件路径获取失败：${loadError}`}</Typography>
        )}
        {paths !== null && (
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            <CleanupCard
              icon={<SettingsSuggestOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />}
              title={TEXTS.app.title}
              desc={TEXTS.app.desc}
              path={paths.appDbPath}
              disabled={resetting}
              onCleanup={() => setTarget('app')}
              onCopyPath={copyPath}
            />
            <CleanupCard
              icon={<StorageOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />}
              title={TEXTS.server.title}
              desc={TEXTS.server.desc}
              path={paths.serverDbPath}
              disabled={resetting}
              onCleanup={() => setTarget('server')}
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
