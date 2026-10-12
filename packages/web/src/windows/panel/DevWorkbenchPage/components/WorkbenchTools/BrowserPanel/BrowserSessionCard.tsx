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
  useAnalyzeBrowserSession,
  useBrowserFrameStream,
  useCloseBrowserSession,
  useEnsureBrowserSession,
  useNavigateBrowserSession,
} from '@src/state/browserSession';
import { useState } from 'react';
import { browserCallOutcomeChip, browserSessionStateChip } from './browserSessionStateChip';
import LiveViewport from './LiveViewport';

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

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

interface BrowserSessionCardProps {
  session: BrowserSessionView;
  /** 当前选中 issue id（「AI 分析此页」归因）。 */
  issueId: string;
  /** 面板区展开态：轮询闸门之一（收起即停，D4 展示侧对偶）。 */
  panelVisible: boolean;
}

/**
 * BrowserSessionCard：单 profile 浏览器会话卡（展开态默认）——实时视口（ready 且面板
 * 可见且无恢复编排才订阅帧流，CDP 观察通道远程桌面语义）、地址栏打开、页面列表
 * （点选 = 分析目标与观看页码）、无头切换/重开（close + ensure 链，成功后按 close 时
 * 捕获的真实当前页恢复现场；确认弹窗保持展示错误）、「AI 分析此页」受理、「最近操作」
 * 流水。会话状态迁移全由 SSE sessions 帧驱动，卡片不维护会话状态副本。
 */
export default function BrowserSessionCard({ session, issueId, panelVisible }: BrowserSessionCardProps) {
  const [expanded, setExpanded] = useState(true);
  const [selectedPageId, setSelectedPageId] = useState<number | null>(null);
  const [address, setAddress] = useState('');
  // 无头切换确认弹窗（null = 关闭）：headless 在引擎启动时固化、运行中不可改，切换 =
  // close + ensure 传新值；restoreUrl 为打开弹窗时的 pages 快照，仅作确认文案提示
  // （权威恢复目标 = close 响应行——服务端收尾前捕获引擎真实当前页，弹窗快照看不到
  // 浏览器窗口内的手动导航）。编排链路期间弹窗保持打开以展示进度与错误。
  const [pendingToggle, setPendingToggle] = useState<{ headless: boolean; restoreUrl: string | null } | null>(null);
  // 恢复编排进行中暂停帧流订阅：编排导航与观察通道同争页面拨号（订阅重建风暴即自愈
  // 抖动）。
  const [restoreInFlight, setRestoreInFlight] = useState(false);

  const ensureMutation = useEnsureBrowserSession();
  const closeMutation = useCloseBrowserSession();
  const navigateMutation = useNavigateBrowserSession();
  const analyzeMutation = useAnalyzeBrowserSession();

  const ready = session.status === 'ready';
  const streamEnabled = expanded && panelVisible && ready && !restoreInFlight;
  // 观看页码：点选页优先，未选 = 当前页（分析目标与观看页码同一语义）；pages 未就绪
  // 以 0 起步，首帧即真实页。
  const pageId = selectedPageId ?? (session.pages ?? []).find(page => page.current)?.index ?? 0;
  const stream = useBrowserFrameStream(session.profile, pageId, streamEnabled);

  const recentCalls = session.recentCalls ?? [];
  const chip = browserSessionStateChip(session.status);
  const toggling = closeMutation.isPending || ensureMutation.isPending || navigateMutation.isPending;

  const askToggleHeadless = (next: boolean) => {
    closeMutation.reset();
    ensureMutation.reset();
    navigateMutation.reset();
    setPendingToggle({ headless: next, restoreUrl: restoreUrlOf(session) });
  };

  const confirmToggleHeadless = () => {
    if (pendingToggle == null) {
      return;
    }
    const { headless: next } = pendingToggle;
    // 编排全程暂停截图轮询；close/ensure 失败保持弹窗展示错误；恢复导航失败不回滚
    // 切换（错误经 ready 区 Alert 兜底展示）。恢复目标取 close 响应行（收尾前服务端
    // 捕获引擎真实当前页——弹窗快照仅作文案，看不到窗口内手动导航）。
    setRestoreInFlight(true);
    const settle = () => setRestoreInFlight(false);
    closeMutation.mutate({ profile: session.profile }, {
      onSuccess: (row) => {
        const restoreUrl = restoreUrlOf(row);
        ensureMutation.mutate({ profile: session.profile, headless: next }, {
          onSuccess: () => {
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
    analyzeMutation.mutate({
      profile: session.profile,
      issueId,
      pageId: selectedPageId ?? undefined,
    });
  };

  return (
    <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 1 }}>
      <Box
        onClick={() => setExpanded(v => !v)}
        sx={{ 'display': 'flex', 'alignItems': 'center', 'gap': 1, 'px': 1.5, 'py': 1, 'cursor': 'pointer', 'bgcolor': 'action.hover', '&:hover': { bgcolor: 'action.selected' } }}
      >
        <Typography sx={{ fontSize: 13, fontWeight: 600 }}>{session.profile}</Typography>
        <Chip size="small" variant="outlined" label={chip.label} color={chip.color} sx={{ fontSize: 12 }} />
        <Tooltip title={session.headless ? '无头模式：无窗口，面板实时画面即页面' : '有头模式：桌面真实 Chrome 窗口（人工介入通道）'}>
          <Chip size="small" variant="outlined" label={session.headless ? '无头' : '有头'} sx={{ fontSize: 12, color: 'text.secondary' }} />
        </Tooltip>
        {session.lastIssueTag && (
          <Tooltip title="最近操作的关联任务">
            <Chip size="small" variant="outlined" label={session.lastIssueTag} sx={{ fontSize: 12, color: 'text.secondary' }} />
          </Tooltip>
        )}
        <IconButton
          size="small"
          aria-label={expanded ? '收起会话' : '展开会话'}
          onClick={(e) => {
            e.stopPropagation();
            setExpanded(v => !v);
          }}
          sx={{ 'ml': 'auto', 'p': 0.25, 'color': 'text.secondary', '&:hover': { bgcolor: 'action.hover' } }}
        >
          {expanded ? <ExpandLessOutlinedIcon sx={{ fontSize: 16 }} /> : <ExpandMoreOutlinedIcon sx={{ fontSize: 16 }} />}
        </IconButton>
      </Box>

      {expanded && (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.25, px: 1.5, py: 1.25 }}>
          {pendingToggle == null && ensureMutation.error && (
            <Alert severity="error" onClose={() => ensureMutation.reset()} sx={{ fontSize: 12 }}>
              {`打开失败：${errorMessage(ensureMutation.error)}`}
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
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <CircularProgress size={14} sx={{ color: 'text.secondary' }} />
              <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>浏览器正在准备中…</Typography>
            </Box>
          )}
          {session.status === 'idle' && (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>会话已释放（登录态保留）</Typography>
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
              <LiveViewport profile={session.profile} frameRef={stream.frameRef} tick={stream.tick} interactive={stream.phase === 'live'} />
              {stream.target && (
                <Typography sx={{ fontSize: 12, color: 'text.secondary', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {`正在投送：${stream.target.title || stream.target.url}`}
                </Typography>
              )}
              {stream.healingMessage != null && (
                <Alert severity="warning" sx={{ fontSize: 12 }}>
                  {stream.healingMessage}
                </Alert>
              )}
              {stream.closedReason != null && (
                <Alert severity="warning" sx={{ fontSize: 12 }}>
                  {`实时画面已停止：${stream.closedReason}（选择其他页面可重新订阅）`}
                </Alert>
              )}

              <Box sx={{ display: 'flex', gap: 0.5 }}>
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
                  slotProps={{ input: { sx: { fontSize: 12 } } }}
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
                <Alert severity="error" sx={{ fontSize: 12 }}>
                  {`打开失败：${errorMessage(navigateMutation.error)}`}
                </Alert>
              )}

              <Box>
                <Typography sx={{ fontSize: 12, color: 'text.secondary', mb: 0.5 }}>
                  页面（点选行 = 指定分析目标，未选 = 当前页）
                </Typography>
                {(session.pages ?? []).length === 0
                  ? <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>暂无页面</Typography>
                  : (session.pages ?? []).map(page => (
                      <Box
                        key={page.index}
                        onClick={() => setSelectedPageId(prev => (prev === page.index ? null : page.index))}
                        sx={{
                          'display': 'flex',
                          'alignItems': 'center',
                          'gap': 1,
                          'px': 0.75,
                          'py': 0.5,
                          'borderRadius': 1,
                          'cursor': 'pointer',
                          'bgcolor': selectedPageId === page.index ? 'action.selected' : 'transparent',
                          '&:hover': { bgcolor: 'action.hover' },
                        }}
                      >
                        {page.current
                          ? <Chip size="small" label="当前" color="primary" variant="outlined" sx={{ fontSize: 12 }} />
                          : <Typography sx={{ fontSize: 12, color: 'text.secondary', width: 24, textAlign: 'center' }}>{page.index}</Typography>}
                        <Tooltip title={page.url}>
                          <Typography sx={{ fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            {page.title || page.url}
                          </Typography>
                        </Tooltip>
                      </Box>
                    ))}
              </Box>

              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <FormControlLabel
                  control={<Switch size="small" checked={session.headless} onChange={e => askToggleHeadless(e.target.checked)} />}
                  label="无头模式（无窗口，面板实时画面）"
                  sx={{ '& .MuiFormControlLabel-label': { fontSize: 12 } }}
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
                <Alert severity="error" sx={{ fontSize: 12 }}>
                  {`分析受理失败：${errorMessage(analyzeMutation.error)}`}
                </Alert>
              )}
            </>
          )}

          {recentCalls.length > 0 && (
            <Box>
              <Typography sx={{ fontSize: 12, color: 'text.secondary', mb: 0.5 }}>最近操作</Typography>
              {recentCallRows(recentCalls).map(({ call, key }) => {
                const outcome = browserCallOutcomeChip(call.outcome);
                return (
                  <Box key={key} sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 0.5, py: 0.25 }}>
                    <Chip size="small" variant="outlined" label={outcome.label} color={outcome.color} sx={{ fontSize: 12 }} />
                    <Tooltip title={call.error ?? call.tool}>
                      <Typography sx={{ fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
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
        <DialogTitle sx={{ fontSize: 16 }}>{`切换为${pendingToggle?.headless ? '无头' : '有头'}模式`}</DialogTitle>
        <DialogContent>
          <DialogContentText sx={{ fontSize: 13 }}>
            {`将关闭并重新打开会话「${session.profile}」（约 1–3 秒）。登录态保存在本地 profile 目录，不会丢失。${pendingToggle?.restoreUrl != null ? '完成后将自动恢复上次打开的页面。' : ''}`}
          </DialogContentText>
          {(closeMutation.error || ensureMutation.error) && (
            <Alert severity="error" sx={{ mt: 1.5, fontSize: 12 }}>
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
