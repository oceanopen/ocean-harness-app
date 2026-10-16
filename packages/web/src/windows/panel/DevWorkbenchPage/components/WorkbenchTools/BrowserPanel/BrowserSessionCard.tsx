import type { BrowserRecentCall, BrowserSessionView } from '@src/services';
import ExpandLessOutlinedIcon from '@mui/icons-material/ExpandLessOutlined';
import ExpandMoreOutlinedIcon from '@mui/icons-material/ExpandMoreOutlined';
import {
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  FormControlLabel,
  IconButton,
  Switch,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import {
  BROWSER_PREVIEW_REFRESH_MS_KEY,
  DEFAULT_BROWSER_PREVIEW_REFRESH_MS,
  parseBrowserRefreshMs,
} from '@src/shared/appConfig';
import { commands } from '@src/shared/bindings';
import { useConfigReady } from '@src/shared/useConfigReady';
import { useConfigValue } from '@src/shared/useConfigValue';
import { useDocumentVisible } from '@src/shared/useDocumentVisible';
import {
  useAnalyzeBrowserSession,
  useBrowserFrameStream,
  useBrowserStageStore,
  useCloseBrowserSession,
  useEnsureBrowserSession,
  useNavigateBrowserSession,
} from '@src/state/browserSession';
import { useState } from 'react';
import { browserCallOutcomeChip, browserSessionStateChip } from './browserSessionStateChip';
import MiniPreview from './MiniPreview';

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

// useConfigValue decode 须稳定引用、useConfigReady keys 须稳定引用（模块级常量）。
const decodePreviewRefreshMs = (raw: string | null) => parseBrowserRefreshMs(raw, DEFAULT_BROWSER_PREVIEW_REFRESH_MS);
const PREVIEW_REFRESH_CONFIG_KEYS = [BROWSER_PREVIEW_REFRESH_MS_KEY];

/**
 * 流水行 key（内容指纹 + 同指纹序号）：环形缓冲整行无业务 id，index key 会随窗口滑动
 * 错位复用（Tooltip 内容串行）；指纹去重序号在滑动下保持行身份。
 */
function recentCallRows(calls: BrowserRecentCall[]): Array<{ call: BrowserRecentCall; key: string }> {
  const seen = new Map<string, number>();
  return calls.map((call) => {
    const base = `${call.tool}|${call.outcome}|${call.issueTag ?? ''}|${call.error ?? ''}`;
    const n = seen.get(base) ?? 0;
    seen.set(base, n + 1);
    return { call, key: `${base}#${n}` };
  });
}

/**
 * 重启恢复目标（无头切换/重开共用）：输入行的 pages 当前页 URL（切换链取 close 响应行
 * ——服务端收尾前捕获的真实页面；重开取 SSE 投影行）；空与 about:blank 无可恢复返回
 * null。多标签仅恢复当前页。
 */
function restoreUrlOf(session: BrowserSessionView): string | null {
  const url = (session.pages ?? []).find(page => page.current)?.url ?? '';
  return url !== '' && url !== 'about:blank' ? url : null;
}

/** 迷你预览固定高度（px，含边框）：低频节流帧的纯展示窗口。 */
const MINI_PREVIEW_HEIGHT = 150;

interface BrowserSessionCardProps {
  session: BrowserSessionView;
  /** 当前选中 issue id（「AI 分析此页」归因）。 */
  issueId: string;
  /** 面板区展开态：帧流闸门之一（收起即停，D4 展示侧对偶）。 */
  panelVisible: boolean;
}

/**
 * BrowserSessionCard：单 profile 浏览器会话卡（紧凑态）——迷你预览（激活期停订的低频节流帧流，
 * 纯展示）、地址栏/页面列表/释放/无头切换与重开（close + ensure 链恢复现场）/「AI 分析此页」受理/
 * 最近操作流水。会话状态全由 SSE sessions 帧驱动，激活与观看页 targetId 走 useBrowserStageStore。
 */
export default function BrowserSessionCard({ session, issueId, panelVisible }: BrowserSessionCardProps) {
  const activatedProfile = useBrowserStageStore(s => s.activatedProfile);
  const activate = useBrowserStageStore(s => s.activate);
  const selectedTargetId = useBrowserStageStore(s => s.watchPage[session.profile] ?? null);
  const toggleWatchPage = useBrowserStageStore(s => s.toggleWatchPage);
  const clearWatchPage = useBrowserStageStore(s => s.clearWatchPage);
  const activated = activatedProfile === session.profile;
  const [expanded, setExpanded] = useState(true);
  const [address, setAddress] = useState('');
  /**
   * 无头切换确认弹窗（null = 关闭）：headless 引擎启动时固化，切换 = close + ensure 传新值；
   * restoreUrl 为弹窗打开时的 pages 快照，仅作文案提示（权威恢复目标 = close 响应行，快照
   * 看不到窗口内手动导航）；编排期弹窗保持打开展示进度与错误。
   */
  const [pendingToggle, setPendingToggle] = useState<{ headless: boolean; restoreUrl: string | null } | null>(null);
  // 恢复编排进行中暂停帧流订阅：编排导航与观察通道同争页面拨号（订阅重建风暴即自愈抖动）。
  const [restoreInFlight, setRestoreInFlight] = useState(false);
  // 释放按钮的独立错误位（closeMutation 与无头切换共用，弹窗关闭时释放失败也要可见）。
  const [releaseError, setReleaseError] = useState<string | null>(null);
  // 聚焦窗口：独立 pending/错误位（OS 层动作，不经 mutations）。
  const [focusing, setFocusing] = useState(false);
  const [focusError, setFocusError] = useState<string | null>(null);

  const ensureMutation = useEnsureBrowserSession();
  const closeMutation = useCloseBrowserSession();
  const navigateMutation = useNavigateBrowserSession();
  const analyzeMutation = useAnalyzeBrowserSession();

  const ready = session.status === 'ready';
  /**
   * 预览订阅闸门：激活期停订（该 profile 的实时订阅归操作台 Stage——单 profile 单订阅）；
   * 窗口隐藏（关托盘/最小化）停订——webview 隐藏不卸载组件树，隐藏期订阅纯空转；
   * 配置就绪同操作台（首帧即终值）。
   */
  const visible = useDocumentVisible();
  const configReady = useConfigReady(PREVIEW_REFRESH_CONFIG_KEYS);
  const refreshMs = useConfigValue(BROWSER_PREVIEW_REFRESH_MS_KEY, decodePreviewRefreshMs, DEFAULT_BROWSER_PREVIEW_REFRESH_MS);
  // 观看目标：点选页优先，未选 = 跟随激活页（投影 SSE 推送驱动重订——新开 tab/手动切换
  // tab 画面实时跟随）；投影未就绪时不订阅（等 SSE 首帧，首帧即终值）。
  const watchTargetId = selectedTargetId ?? (session.pages ?? []).find(page => page.current)?.targetId ?? '';
  const streamEnabled = expanded && panelVisible && ready && !restoreInFlight && !activated && visible && configReady && watchTargetId !== '';
  const stream = useBrowserFrameStream(session.profile, watchTargetId, streamEnabled, refreshMs);

  const recentCalls = session.recentCalls ?? [];
  const chip = browserSessionStateChip(session.status);
  const toggling = closeMutation.isPending || ensureMutation.isPending || navigateMutation.isPending;

  const release = () => {
    setReleaseError(null);
    closeMutation.mutate({ profile: session.profile }, {
      onError: err => setReleaseError(errorMessage(err)),
    });
  };

  const focus = () => {
    setFocusError(null);
    setFocusing(true);
    void commands.focusBrowserWindow(session.profile).then((r) => {
      setFocusing(false);
      if (r.status === 'error') {
        setFocusError(r.error);
      }
    });
  };

  const askToggleHeadless = (next: boolean) => {
    closeMutation.reset();
    ensureMutation.reset();
    navigateMutation.reset();
    setReleaseError(null);
    setPendingToggle({ headless: next, restoreUrl: restoreUrlOf(session) });
  };

  const confirmToggleHeadless = () => {
    if (pendingToggle == null) {
      return;
    }
    const { headless: next } = pendingToggle;
    /**
     * 编排全程暂停帧流订阅；close/ensure 失败保持弹窗展示错误；恢复导航失败不回滚
     * 切换（错误经 ready 区 Alert 兜底展示）。恢复目标取 close 响应行（收尾前服务端
     * 捕获引擎真实当前页——弹窗快照仅作文案，看不到窗口内手动导航）。
     */
    setRestoreInFlight(true);
    const settle = () => setRestoreInFlight(false);
    closeMutation.mutate({ profile: session.profile }, {
      onSuccess: (row) => {
        const restoreUrl = restoreUrlOf(row);
        ensureMutation.mutate({ profile: session.profile, headless: next }, {
          onSuccess: () => {
            clearWatchPage(session.profile); // targetId 随重建失效，恢复跟随激活页
            if (restoreUrl == null) {
              setPendingToggle(null);
              settle();
              return;
            }
            navigateMutation.mutate({ profile: session.profile, url: restoreUrl }, {
              onSettled: () => {
                setPendingToggle(null);
                settle();
              },
            });
          },
          onError: settle,
        });
      },
      onError: settle,
    });
  };

  const reopen = () => {
    // headless 键省略：读 profile 偏好（上次运行值），面板不越权改偏好。
    const restoreUrl = restoreUrlOf(session);
    setRestoreInFlight(true);
    ensureMutation.mutate({ profile: session.profile }, {
      onSuccess: () => {
        clearWatchPage(session.profile); // targetId 随重建失效，恢复跟随激活页
        if (restoreUrl == null) {
          setRestoreInFlight(false);
          return;
        }
        navigateMutation.mutate({ profile: session.profile, url: restoreUrl }, {
          onSettled: () => setRestoreInFlight(false),
        });
      },
      onError: () => setRestoreInFlight(false),
    });
  };

  const submitNavigate = () => {
    const url = address.trim();
    if (url === '') {
      return;
    }
    navigateMutation.mutate({ profile: session.profile, url });
  };

  const analyze = () => {
    // 未点选 = 分析当前激活页（有头模式引擎选中页可能与激活页不一致，显式传 targetId）。
    const targetId = selectedTargetId ?? (session.pages ?? []).find(page => page.current)?.targetId;
    analyzeMutation.mutate({
      profile: session.profile,
      issueId,
      targetId: targetId ?? undefined,
    });
  };

  return (
    <Box sx={{ border: '1px solid', borderColor: 'divider', borderRadius: '4px' }}>
      <Box
        onClick={() => setExpanded(v => !v)}
        sx={{ 'display': 'flex', 'alignItems': 'center', 'gap': '8px', 'px': '12px', 'py': '8px', 'cursor': 'pointer', 'bgcolor': 'action.hover', '&:hover': { bgcolor: 'action.selected' } }}
      >
        <Typography noWrap sx={{ fontSize: '13px', fontWeight: 600, flex: 1, minWidth: '0px' }}>{session.profile}</Typography>
        <Chip size="small" variant="outlined" label={chip.label} color={chip.color} sx={{ fontSize: '12px' }} />
        <Tooltip title={session.headless ? '无头模式：无窗口，面板实时画面即页面' : '有头模式：桌面真实 Chrome 窗口（人工介入通道）'}>
          <Chip size="small" variant="outlined" label={session.headless ? '无头' : '有头'} sx={{ fontSize: '12px', color: 'text.secondary' }} />
        </Tooltip>
        {ready && !session.headless && (
          <Tooltip title="把该 profile 的 Chrome 窗口组前置到桌面最前（多窗口实例场景的一键定位）">
            <Button
              size="small"
              variant="text"
              onClick={(e) => {
                e.stopPropagation();
                focus();
              }}
              disabled={focusing}
              sx={{ ml: 'auto', flexShrink: 0, fontSize: '12px', minWidth: '0px', px: '8px', color: 'text.secondary' }}
            >
              {focusing ? <CircularProgress size={12} color="inherit" /> : '聚焦窗口'}
            </Button>
          </Tooltip>
        )}
        {session.status !== 'idle' && (
          <Tooltip title="立即释放浏览器（引擎与 Chrome 进程即刻收敛，登录态保留在 profile 目录）">
            <Button
              size="small"
              variant="text"
              onClick={(e) => {
                e.stopPropagation();
                release();
              }}
              disabled={closeMutation.isPending}
              sx={{ ml: ready && !session.headless ? '0px' : 'auto', flexShrink: 0, fontSize: '12px', minWidth: '0px', px: '8px', color: 'text.secondary' }}
            >
              {closeMutation.isPending ? <CircularProgress size={12} color="inherit" /> : '释放'}
            </Button>
          </Tooltip>
        )}
        <IconButton
          size="small"
          aria-label={expanded ? '收起会话' : '展开会话'}
          onClick={(e) => {
            e.stopPropagation();
            setExpanded(v => !v);
          }}
          sx={{ 'p': '2px', 'color': 'text.secondary', '&:hover': { bgcolor: 'action.hover' }, ...(session.status !== 'idle' ? null : { ml: 'auto' }) }}
        >
          {expanded ? <ExpandLessOutlinedIcon sx={{ fontSize: '16px' }} /> : <ExpandMoreOutlinedIcon sx={{ fontSize: '16px' }} />}
        </IconButton>
      </Box>

      {expanded && (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: '10px', px: '12px', py: '10px' }}>
          {pendingToggle == null && ensureMutation.error && (
            <Alert severity="error" onClose={() => ensureMutation.reset()} sx={{ fontSize: '12px' }}>
              {`打开失败：${errorMessage(ensureMutation.error)}`}
            </Alert>
          )}
          {releaseError != null && (
            <Alert severity="error" onClose={() => setReleaseError(null)} sx={{ fontSize: '12px' }}>
              {`释放失败：${releaseError}`}
            </Alert>
          )}
          {focusError != null && (
            <Alert severity="error" onClose={() => setFocusError(null)} sx={{ fontSize: '12px' }}>
              {`聚焦失败：${focusError}`}
            </Alert>
          )}
          {session.status === 'failed' && (
            <Alert
              severity="error"
              action={(
                <Button color="inherit" size="small" onClick={reopen} disabled={ensureMutation.isPending}>
                  重开
                </Button>
              )}
            >
              {session.error ?? '浏览器拉起失败'}
            </Alert>
          )}
          {session.status === 'starting' && (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <CircularProgress size={14} sx={{ color: 'text.secondary' }} />
              <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>浏览器正在准备中…</Typography>
            </Box>
          )}
          {session.status === 'idle' && (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>会话已释放（登录态保留）</Typography>
              <Button
                size="small"
                variant="outlined"
                onClick={reopen}
                disabled={ensureMutation.isPending}
                sx={{ ml: 'auto', flexShrink: 0 }}
              >
                {ensureMutation.isPending ? <CircularProgress size={14} color="inherit" /> : '打开会话'}
              </Button>
            </Box>
          )}

          {ready && (
            <>
              {activated
                ? (
                    <Box
                      sx={{
                        height: '150px',
                        flexShrink: 0,
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        border: '1px solid',
                        borderColor: 'primary.main',
                        borderRadius: '4px',
                        bgcolor: 'action.hover',
                      }}
                    >
                      <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>已在工作台操作台打开（实时画面投送中）</Typography>
                    </Box>
                  )
                : (
                    <Tooltip title="点击接管工作台内容区，打开可鼠标/键盘操作的实时画面">
                      <Box
                        onClick={() => activate(session.profile)}
                        sx={{
                          'cursor': 'pointer',
                          // MiniPreview 内层 bordered 盒（占位/画面两态）统一 hover 高亮。
                          '&:hover > div': { borderColor: 'primary.main' },
                        }}
                      >
                        <MiniPreview frameRef={stream.frameRef} hasFrame={stream.hasFrame} height={MINI_PREVIEW_HEIGHT} />
                      </Box>
                    </Tooltip>
                  )}
              {stream.healingMessage != null && (
                <Typography sx={{ fontSize: '12px', color: 'warning.main' }}>{stream.healingMessage}</Typography>
              )}
              {stream.closedReason != null && (
                <Typography sx={{ fontSize: '12px', color: 'warning.main' }}>{`实时画面已停止：${stream.closedReason}`}</Typography>
              )}
              {stream.phase === 'failed' && (
                <Alert
                  severity="error"
                  sx={{ fontSize: '12px' }}
                  action={(
                    <Button color="inherit" size="small" onClick={stream.retry}>
                      重试
                    </Button>
                  )}
                >
                  {`实时画面连接失败：${stream.failedMessage ?? '请稍后重试'}`}
                </Alert>
              )}

              <Box sx={{ display: 'flex', gap: '4px' }}>
                <TextField
                  size="small"
                  fullWidth
                  hiddenLabel
                  placeholder="输入页面地址，回车打开"
                  value={address}
                  onChange={e => setAddress(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      submitNavigate();
                    }
                  }}
                  slotProps={{ input: { sx: { fontSize: '12px' } } }}
                />
                <Button
                  size="small"
                  variant="contained"
                  onClick={submitNavigate}
                  disabled={navigateMutation.isPending || address.trim() === ''}
                  sx={{ flexShrink: 0 }}
                >
                  {navigateMutation.isPending ? <CircularProgress size={14} color="inherit" /> : '打开'}
                </Button>
              </Box>
              {navigateMutation.error && (
                <Alert severity="error" sx={{ fontSize: '12px' }}>
                  {`打开失败：${errorMessage(navigateMutation.error)}`}
                </Alert>
              )}

              <Box>
                <Typography sx={{ fontSize: '12px', color: 'text.secondary', mb: '4px' }}>
                  页面（点选行 = 指定分析目标与观看页，未选 = 跟随激活页）
                </Typography>
                {(session.pages ?? []).length === 0
                  ? <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>暂无页面</Typography>
                  : (session.pages ?? []).map(page => (
                      <Box
                        key={page.targetId ?? page.index}
                        onClick={() => {
                          if (page.targetId != null) {
                            toggleWatchPage(session.profile, page.targetId);
                          }
                        }}
                        sx={{
                          'display': 'flex',
                          'alignItems': 'center',
                          'gap': '8px',
                          'px': '6px',
                          'py': '4px',
                          'borderRadius': '4px',
                          'cursor': page.targetId != null ? 'pointer' : 'default',
                          'bgcolor': selectedTargetId != null && selectedTargetId === page.targetId ? 'action.selected' : 'transparent',
                          '&:hover': { bgcolor: 'action.hover' },
                        }}
                      >
                        {page.current
                          ? <Chip size="small" label="当前" color="primary" variant="outlined" sx={{ fontSize: '12px' }} />
                          : <Typography sx={{ fontSize: '12px', color: 'text.secondary', width: '24px', textAlign: 'center' }}>{page.index}</Typography>}
                        <Tooltip title={page.url}>
                          <Typography sx={{ fontSize: '12px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            {page.title || page.url}
                          </Typography>
                        </Tooltip>
                      </Box>
                    ))}
              </Box>

              <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <FormControlLabel
                  control={<Switch size="small" checked={session.headless} onChange={e => askToggleHeadless(e.target.checked)} />}
                  label="无头模式（无窗口，面板实时画面）"
                  sx={{ '& .MuiFormControlLabel-label': { fontSize: '12px' } }}
                />
                <Button
                  size="small"
                  variant="outlined"
                  onClick={analyze}
                  disabled={analyzeMutation.isPending}
                  sx={{ ml: 'auto', flexShrink: 0 }}
                >
                  {analyzeMutation.isPending ? <CircularProgress size={14} color="inherit" /> : 'AI 分析此页'}
                </Button>
              </Box>
              {analyzeMutation.error && (
                <Alert severity="error" sx={{ fontSize: '12px' }}>
                  {`分析受理失败：${errorMessage(analyzeMutation.error)}`}
                </Alert>
              )}
            </>
          )}

          {recentCalls.length > 0 && (
            <Box>
              <Typography sx={{ fontSize: '12px', color: 'text.secondary', mb: '4px' }}>最近操作</Typography>
              {recentCallRows(recentCalls).map(({ call, key }) => {
                const outcome = browserCallOutcomeChip(call.outcome);
                return (
                  <Box key={key} sx={{ display: 'flex', alignItems: 'center', gap: '8px', px: '4px', py: '2px' }}>
                    <Chip size="small" variant="outlined" label={outcome.label} color={outcome.color} sx={{ fontSize: '12px' }} />
                    <Tooltip title={call.error ?? call.tool}>
                      <Typography sx={{ fontSize: '12px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {call.tool.replace(/^browser_/, '')}{call.issueTag ? ` · ${call.issueTag}` : ''}
                      </Typography>
                    </Tooltip>
                  </Box>
                );
              })}
            </Box>
          )}
        </Box>
      )}

      <Dialog
        open={pendingToggle != null}
        onClose={() => {
          if (!toggling) {
            setPendingToggle(null);
          }
        }}
      >
        <DialogTitle sx={{ fontSize: '16px' }}>{`切换为${pendingToggle?.headless ? '无头' : '有头'}模式`}</DialogTitle>
        <DialogContent>
          <DialogContentText sx={{ fontSize: '13px' }}>
            {`将关闭并重新打开会话「${session.profile}」（约 1–3 秒）。登录态保存在本地 profile 目录，不会丢失。${pendingToggle?.restoreUrl != null ? '完成后将自动恢复上次打开的页面。' : ''}`}
          </DialogContentText>
          {(closeMutation.error || ensureMutation.error) && (
            <Alert severity="error" sx={{ mt: '12px', fontSize: '12px' }}>
              {errorMessage(closeMutation.error ?? ensureMutation.error)}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setPendingToggle(null)} disabled={toggling}>取消</Button>
          <Button onClick={confirmToggleHeadless} variant="contained" disabled={toggling}>
            {toggling ? <CircularProgress size={14} color="inherit" /> : '确认切换'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
