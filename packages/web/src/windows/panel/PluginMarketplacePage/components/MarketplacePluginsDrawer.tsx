import type { MarketplacePluginModel, PluginMarketplaceModel } from '@src/services';
import { CloseOutlined as CloseOutlinedIcon } from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  IconButton,
  Typography,
} from '@mui/material';
import ResizableDrawer from '@src/shared/ResizableDrawer';
import {
  useDisablePlugin,
  useEnablePlugin,
  useInstallPlugin,
  useUninstallPlugin,
  useUpdatePluginOp,
} from '@src/state/pluginMarketplace';
import { clamp2Sx, truncateSx } from '../styles';

// 插件列表抽屉：展示某市场下的全部插件（清单投影 × claude 安装状态），提供安装/卸载/
// 启用/禁用/更新操作（user scope，与命令行行为一致）。marketplace 对象来自页面最新
// 投影（写操作后整体替换），行内状态随数据刷新自动更新，无需本地维护。
interface MarketplacePluginsDrawerProps {
  marketplace: PluginMarketplaceModel;
  showToast: (text: string, severity: 'success' | 'error') => void;
  onClose: () => void;
}

// 从未知错误对象提取 message（Go 经 http.ts 抛 new Error(msg)）。
function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

// 插件操作类型（与后端 /api/plugin/<action> 一一对应）。
type PluginAction = 'install' | 'uninstall' | 'enable' | 'disable' | 'update';

const ACTION_LABEL: Record<PluginAction, string> = {
  install: '安装',
  uninstall: '卸载',
  enable: '启用',
  disable: '禁用',
  update: '更新',
};

// 操作进行中标识的唯一拼装点（pending 判定与按钮 spinner 匹配共用，防两处拼接漂移）。
function opKey(action: PluginAction, pluginName: string): string {
  return `${action}:${pluginName}`;
}

// 组件统计 → 展示文案（值为 0 的项省略；外部引用源无本地目录可统计）。
function componentsText(p: MarketplacePluginModel): string | null {
  if (p.sourceExternal) {
    return '外部引用源（组件统计不可用）';
  }
  const parts: string[] = [];
  if (p.components.commands > 0) {
    parts.push(`命令 ${p.components.commands}`);
  }
  if (p.components.skills > 0) {
    parts.push(`技能 ${p.components.skills}`);
  }
  if (p.components.agents > 0) {
    parts.push(`智能体 ${p.components.agents}`);
  }
  if (p.components.hooks > 0) {
    parts.push('Hooks');
  }
  if (p.components.mcpServers > 0) {
    parts.push('MCP');
  }
  return parts.length > 0 ? parts.join(' · ') : null;
}

// 行内状态徽标：未安装（默认灰）/ 已启用（success）/ 已禁用（warning）。
function StatusChip({ plugin }: { plugin: MarketplacePluginModel }) {
  if (!plugin.installed) {
    return <Chip size="small" variant="outlined" label="未安装" />;
  }
  return plugin.enabled
    ? <Chip size="small" color="success" variant="outlined" label="已启用" />
    : <Chip size="small" color="warning" variant="outlined" label="已禁用" />;
}

// 按插件状态生成操作集：未安装→安装；已禁用→启用/卸载；已启用→更新/禁用/卸载；
// 非 user scope 安装→空（本应用操作恒 user scope，点击必然失败，见 PluginRow 说明）。
function actionsOf(p: MarketplacePluginModel): { action: PluginAction; variant: 'contained' | 'outlined' | 'text'; color: 'primary' | 'error' }[] {
  if (!p.installed) {
    return [{ action: 'install', variant: 'contained', color: 'primary' }];
  }
  if (p.scope !== 'user') {
    return [];
  }
  return p.enabled
    ? [
        { action: 'update', variant: 'outlined', color: 'primary' },
        { action: 'disable', variant: 'outlined', color: 'primary' },
        { action: 'uninstall', variant: 'text', color: 'error' },
      ]
    : [
        { action: 'enable', variant: 'contained', color: 'primary' },
        { action: 'uninstall', variant: 'text', color: 'error' },
      ];
}

function MarketplacePluginsDrawer({ marketplace, showToast, onClose }: MarketplacePluginsDrawerProps) {
  const installMu = useInstallPlugin();
  const uninstallMu = useUninstallPlugin();
  const enableMu = useEnablePlugin();
  const disableMu = useDisablePlugin();
  const updateMu = useUpdatePluginOp();

  // claude 侧安装状态文件为共享写入目标（服务端已有包级 mutex 串行），前端配合全局禁用：
  // 任一操作进行中禁用全部操作按钮，避免用户误以为可并行发起。
  const mutations = { install: installMu, uninstall: uninstallMu, enable: enableMu, disable: disableMu, update: updateMu } as const;
  const entries = Object.entries(mutations) as [PluginAction, typeof installMu][];
  const anyPending = entries.some(([, mu]) => mu.isPending);
  const pendingEntry = entries.find(([, mu]) => mu.isPending) ?? null;
  const pendingKey = pendingEntry ? opKey(pendingEntry[0], pendingEntry[1].variables?.name ?? '') : null;

  const handleOperate = async (action: PluginAction, plugin: MarketplacePluginModel) => {
    try {
      await mutations[action].mutateAsync({ name: plugin.name, marketplace: marketplace.name });
      showToast(`${ACTION_LABEL[action]}成功：${plugin.name}`, 'success');
    } catch (e) {
      showToast(`${ACTION_LABEL[action]}失败：${errMsg(e)}`, 'error');
    }
  };

  return (
    <ResizableDrawer open onClose={onClose} defaultWidthPct={44}>
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* 头部：市场名 + 来源 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography variant="subtitle1" sx={{ fontWeight: 600, ...truncateSx }}>
              {marketplace.name}
            </Typography>
            <Typography variant="caption" sx={{ color: 'text.disabled', fontFamily: 'monospace', ...truncateSx }}>
              {marketplace.sourceDetail || marketplace.installLocation}
            </Typography>
          </Box>
          <IconButton size="small" onClick={onClose} aria-label="关闭">
            <CloseOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 内容：扫描异常 / 支持性提示 / 插件行列表 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: 2, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
          {marketplace.scanError && <Alert severity="warning">{marketplace.scanError}</Alert>}
          {marketplace.supportedClis.length === 0 && !marketplace.scanError && (
            <Alert severity="info">
              未探测到受支持的开发工具清单目录（当前识别 .claude-plugin/marketplace.json）
            </Alert>
          )}
          {marketplace.plugins.length === 0 && !marketplace.scanError && (
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', py: 6 }}>
              <Typography variant="body2" color="text.secondary">
                该市场暂无插件
              </Typography>
            </Box>
          )}
          {marketplace.plugins.map((p) => {
            const comps = componentsText(p);
            const actions = actionsOf(p);
            return (
              <Box
                key={p.name}
                sx={{
                  border: 1,
                  borderColor: 'divider',
                  borderRadius: 1,
                  p: 1.5,
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 0.75,
                }}
              >
                {/* 行头：插件名 + 版本 + 状态 */}
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
                  <Typography variant="body2" sx={{ fontWeight: 600, minWidth: 0, ...truncateSx }} title={p.name}>
                    {p.name}
                  </Typography>
                  {(p.installed ? p.installedVersion : p.version) && (
                    <Typography variant="caption" sx={{ color: 'text.disabled', flexShrink: 0 }}>
                      {`v${p.installed ? p.installedVersion : p.version}`}
                    </Typography>
                  )}
                  <Box sx={{ flex: 1 }} />
                  <StatusChip plugin={p} />
                </Box>

                {/* 描述（可空） */}
                {p.description && (
                  <Typography variant="caption" sx={{ color: 'text.secondary', ...clamp2Sx }}>
                    {p.description}
                  </Typography>
                )}

                {/* 组件统计 */}
                {comps && (
                  <Typography variant="caption" sx={{ color: 'text.disabled' }}>
                    {comps}
                  </Typography>
                )}

                {/* 操作区：非 user scope 安装的插件不给操作按钮（本应用操作恒 user scope，
                    点击必然失败），改为提示走命令行管理；其余按状态渲染按钮组，
                    任一操作进行中全局禁用（配合服务端串行锁，防误并发）。 */}
                {actions.length > 0
                  ? (
                      <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                        {actions.map(({ action, variant, color }) => (
                          <Button
                            key={action}
                            size="small"
                            variant={variant}
                            color={color}
                            disabled={anyPending}
                            onClick={() => void handleOperate(action, p)}
                            startIcon={pendingKey === opKey(action, p.name) ? <CircularProgress size={12} color="inherit" /> : undefined}
                          >
                            {ACTION_LABEL[action]}
                          </Button>
                        ))}
                      </Box>
                    )
                  : p.installed && p.scope !== 'user'
                    ? (
                        <Typography variant="caption" sx={{ color: 'text.disabled', alignSelf: 'flex-end' }}>
                          {`以 ${p.scope} scope 安装，请在命令行管理`}
                        </Typography>
                      )
                    : null}
              </Box>
            );
          })}
        </Box>
      </Box>
    </ResizableDrawer>
  );
}

export default MarketplacePluginsDrawer;
