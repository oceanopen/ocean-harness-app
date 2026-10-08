import type { PluginMarketplaceModel } from '@src/services';
import {
  AddOutlined as AddOutlinedIcon,
  Autorenew as AutorenewIcon,
  StorefrontOutlined as StorefrontOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Snackbar,
  TextField,
  Typography,
} from '@mui/material';
import {
  usePluginMarketplaces,
  useRemovePluginMarketplace,
  useUpdatePluginMarketplace,
} from '@src/state/pluginMarketplace';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import AddMarketplaceDrawer from './components/AddMarketplaceDrawer';
import MarketplaceCard from './components/MarketplaceCard';
import MarketplacePluginsDrawer from './components/MarketplacePluginsDrawer';

type LoadStatus = 'loading' | 'ready' | 'error';

// 浮层 toast 严重级别（成功用 success，失败用 error）。
type ToastSeverity = 'success' | 'error';

// 从未知错误对象提取 message（Go 经 http.ts 抛 new Error(msg)，Rust IPC 抛裸字符串，两者兼容）。
function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

// panel 窗口「插件市场」菜单页面：三态机 + 顶栏(搜索+操作) + 响应式卡片网格 + toast。
// 数据为 Go 服务实时投影（claude CLI × 市场清单扫描，无本地表），页面切走即卸载。
// 自动刷新：挂载由 query 自动拉取；PanelApp 监听 panel:shown 事件，当前页为插件市场时
// 经 windowShownTrigger 触发 refetch。页面内文案为硬编码中文（i18n 仅覆盖菜单/页面名，
// 国际化扩展口子见 panel.json menu.pluginMarketplace）。
function PluginMarketplacePage({ windowShownTrigger }: { windowShownTrigger: number }) {
  const { data, isPending, isError, error, refetch, isFetching } = usePluginMarketplaces();
  const removeMu = useRemovePluginMarketplace();
  const updateMu = useUpdatePluginMarketplace();

  const marketplaces = data?.marketplaces ?? [];
  const status: LoadStatus = isPending ? 'loading' : isError ? 'error' : 'ready';

  // toast：保留最近一次内容，toastOpen 控制显隐（退出动画期间内容不闪烁）。
  const [toast, setToast] = useState<{ text: string; severity: ToastSeverity }>({ text: '', severity: 'success' });
  const [toastOpen, setToastOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [addDrawerOpen, setAddDrawerOpen] = useState(false);
  // 抽屉/移除目标记录市场名（而非对象快照）：写操作后列表数据整体替换，按名派生可让
  // 抽屉内容随最新投影自动更新（安装/启停后行内状态即时变化）。
  const [drawerName, setDrawerName] = useState<string | null>(null);
  const [removeTargetName, setRemoveTargetName] = useState<string | null>(null);

  const showToast = useCallback((text: string, severity: ToastSeverity) => {
    setToast({ text, severity });
    setToastOpen(true);
  }, []);

  // 页面 shown 触发 refetch：挂载首跳过（query 已自动拉取），仅响应后续 panel:shown。
  const shownInitRef = useRef(false);
  useEffect(() => {
    if (!shownInitRef.current) {
      shownInitRef.current = true;
      return;
    }
    void refetch();
  }, [windowShownTrigger, refetch]);

  // 客户端模糊过滤：市场名 / 描述 / 来源明细。
  const displayed = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) {
      return marketplaces;
    }
    return marketplaces.filter(mk =>
      mk.name.toLowerCase().includes(q)
      || mk.description.toLowerCase().includes(q)
      || mk.sourceDetail.toLowerCase().includes(q));
  }, [marketplaces, search]);

  const drawerMarketplace = drawerName != null
    ? marketplaces.find(mk => mk.name === drawerName) ?? null
    : null;
  const removeTarget = removeTargetName != null
    ? marketplaces.find(mk => mk.name === removeTargetName) ?? null
    : null;

  const handleRefresh = useCallback(() => {
    void refetch().then(({ isError: failed }) => {
      showToast(failed ? '刷新失败：插件市场列表拉取出错' : '已刷新', failed ? 'error' : 'success');
    });
  }, [refetch, showToast]);

  const handleUpdateMarketplace = useCallback(async (mk: PluginMarketplaceModel) => {
    try {
      await updateMu.mutateAsync({ name: mk.name });
      showToast(`已更新市场：${mk.name}`, 'success');
    } catch (e) {
      showToast(`更新失败：${errMsg(e)}`, 'error');
    }
  }, [showToast, updateMu]);

  // 确认移除市场（claude 语义：连带卸载该市场已安装的全部插件）。
  const handleConfirmRemove = useCallback(async () => {
    if (!removeTarget) {
      return;
    }
    try {
      await removeMu.mutateAsync({ name: removeTarget.name });
      showToast(`已移除市场：${removeTarget.name}`, 'success');
      setRemoveTargetName(null);
    } catch (e) {
      showToast(`移除失败：${errMsg(e)}`, 'error');
    }
  }, [removeTarget, showToast, removeMu]);

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* 顶栏：统计 + 搜索 + 操作栏 */}
      <Box
        sx={{
          p: 2,
          borderBottom: 1,
          borderColor: 'divider',
          display: 'flex',
          alignItems: 'center',
          gap: 1.5,
          flexWrap: 'wrap',
        }}
      >
        <Typography variant="body2" sx={{ fontWeight: 600, flexShrink: 0 }}>
          {`共 ${marketplaces.length} 个插件市场`}
        </Typography>
        <TextField
          size="small"
          placeholder="搜索市场名称 / 描述 / 来源"
          value={search}
          onChange={e => setSearch(e.target.value)}
          sx={{ flexGrow: 1, minWidth: 160 }}
        />
        <Box sx={{ display: 'flex', gap: 1, flexShrink: 0 }}>
          <Button
            variant="contained"
            size="small"
            startIcon={<AddOutlinedIcon />}
            onClick={() => setAddDrawerOpen(true)}
          >
            添加插件市场
          </Button>
          {/* 刷新与「更新/移除市场」互斥：写操作（更新市场最长 120s）进行中禁点刷新，
              防止并发 getList 与写操作的 claude 进程交错、以及迟到的投影响应倒序覆盖缓存
              （服务端另有包级 mutex 串行本域请求，此处是 UI 层配合）。 */}
          <IconButton
            size="small"
            onClick={handleRefresh}
            disabled={isFetching || updateMu.isPending || removeMu.isPending}
            aria-label="刷新插件市场列表"
          >
            <AutorenewIcon
              sx={{
                'animation': isFetching ? 'spin 0.8s linear infinite' : undefined,
                '@keyframes spin': {
                  from: { transform: 'rotate(0deg)' },
                  to: { transform: 'rotate(360deg)' },
                },
              }}
            />
          </IconButton>
        </Box>
      </Box>

      {/* 内容区 */}
      <Box sx={{ flex: 1, overflow: 'auto' }}>
        {status === 'loading' && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <CircularProgress />
          </Box>
        )}
        {status === 'error' && (
          <Box sx={{ p: 2 }}>
            <Alert
              severity="error"
              action={(
                <Button color="inherit" size="small" onClick={() => void refetch()}>
                  重试
                </Button>
              )}
            >
              <AlertTitle>插件市场列表加载失败</AlertTitle>
              请确认 go-server 与 Claude Code CLI 可用后重试
              {/* 后端真实原因（http.ts 自 code!==0 的 msg 抛出，如「vendored claude 二进制缺失: …」）：
                  透出直达用户，可选中复制，避免纯硬编码文案掩盖排障线索。 */}
              {error != null && (
                <Typography
                  sx={{
                    mt: 1,
                    fontSize: 12,
                    color: 'text.secondary',
                    userSelect: 'text',
                    wordBreak: 'break-all',
                  }}
                >
                  {errMsg(error)}
                </Typography>
              )}
            </Alert>
          </Box>
        )}
        {status === 'ready' && marketplaces.length === 0 && (
          <Box
            sx={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              height: '100%',
              gap: 1.5,
              px: 3,
              py: 4,
            }}
          >
            <StorefrontOutlinedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />
            <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
              暂无插件市场
            </Typography>
            <Typography variant="body2" color="text.secondary" align="center">
              添加本地插件仓库路径、GitHub 仓库（owner/repo）或 git 地址，
              将通过 Claude Code CLI 注册并保持与命令行一致
            </Typography>
            <Button variant="contained" startIcon={<AddOutlinedIcon />} onClick={() => setAddDrawerOpen(true)} sx={{ mt: 1 }}>
              添加插件市场
            </Button>
          </Box>
        )}
        {status === 'ready' && marketplaces.length > 0 && displayed.length === 0 && (
          <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <Typography variant="body2" color="text.secondary">
              无匹配的插件市场
            </Typography>
          </Box>
        )}
        {status === 'ready' && displayed.length > 0 && (
          <Box
            sx={{
              p: 2,
              display: 'grid',
              gap: 2,
              gridTemplateColumns: {
                xs: '1fr',
                sm: 'repeat(1, 1fr)',
                md: 'repeat(2, 1fr)',
                lg: 'repeat(2, 1fr)',
              },
              alignItems: 'start',
            }}
          >
            {displayed.map(mk => (
              <MarketplaceCard
                key={mk.name}
                mk={mk}
                updating={updateMu.isPending && updateMu.variables?.name === mk.name}
                onOpen={mk => setDrawerName(mk.name)}
                onUpdate={handleUpdateMarketplace}
                onRemove={mk => setRemoveTargetName(mk.name)}
              />
            ))}
          </Box>
        )}
      </Box>

      <Snackbar
        open={toastOpen}
        autoHideDuration={2000}
        onClose={() => setToastOpen(false)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity={toast.severity} variant="filled">
          {toast.text}
        </Alert>
      </Snackbar>

      {addDrawerOpen && (
        <AddMarketplaceDrawer
          onClose={() => setAddDrawerOpen(false)}
        />
      )}

      {drawerMarketplace && (
        <MarketplacePluginsDrawer
          marketplace={drawerMarketplace}
          showToast={showToast}
          onClose={() => setDrawerName(null)}
        />
      )}

      <Dialog open={removeTarget !== null} onClose={removeMu.isPending ? undefined : () => setRemoveTargetName(null)}>
        <DialogTitle>移除插件市场</DialogTitle>
        <DialogContent>
          <Typography>
            {`确定移除市场「${removeTarget?.name ?? ''}」吗？`}
          </Typography>
          <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', mt: 1 }}>
            移除将同时卸载该市场已安装的全部插件（与 claude plugin marketplace remove 行为一致）。
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setRemoveTargetName(null)} disabled={removeMu.isPending}>
            取消
          </Button>
          <Button color="error" variant="contained" onClick={handleConfirmRemove} disabled={removeMu.isPending}>
            移除
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

export default PluginMarketplacePage;
