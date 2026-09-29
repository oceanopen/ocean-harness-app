import type { ImBotProvisionView } from '@src/services';
import CloseOutlinedIcon from '@mui/icons-material/CloseOutlined';
import RefreshOutlinedIcon from '@mui/icons-material/RefreshOutlined';
import {
  Box,
  Button,
  CircularProgress,
  Drawer,
  IconButton,
  LinearProgress,
  Typography,
} from '@mui/material';
import { useProvisionBegin, useProvisionCancel, useProvisionPoll } from '@src/state/imBots';
import QRCode from 'qrcode';
import { useEffect, useRef, useState } from 'react';

// 扫码授权接入抽屉（体验对齐 dsh-im QrPanel）：打开即申请二维码 → 用户企业微信 App 扫码 →
// 腾讯授权页确认创建 → 本机轮询取凭据（服务端自动建 bot 并连接）。凭据全程不经过前端。
// 轮询节奏 = 宿主下发的 pollIntervalMs（协议节奏，链式 setTimeout，终态即停）。
// 「重新生成二维码」由父组件以 key 重挂载实现（卸载清理定时器 + 新 begin 自动取消旧会话）。

const DRAWER_WIDTH = 480;

/** 二维码总时长（与后端 qrTTL 同源：5 分钟，腾讯未下发 TTL 的本地推断值）。 */
const QR_TTL_MS = 5 * 60_000;

/** 组件挂载期一次性二维码渲染：qrContent → dataURL（content 域名已由后端强校验，前端再兜底）。 */
async function renderQr(content: string): Promise<string> {
  if (!content.startsWith('https://work.weixin.qq.com/')) {
    throw new Error('授权链接域名异常');
  }
  return QRCode.toDataURL(content, { width: 240, margin: 1, errorCorrectionLevel: 'M' });
}

function ImBotProvisionDrawer(props: { open: boolean; onClose: () => void; onRegenerate: () => void }) {
  const { open } = props;
  const beginMutation = useProvisionBegin();
  const pollMutation = useProvisionPoll();
  const cancelMutation = useProvisionCancel();

  // 回调经 ref 消费：父组件内联箭头每次渲染变化，若入 effect 依赖会使扫码流程随父渲染重启。
  // begin/poll 走 state 层 mutation（connected 时失效列表缓存的副作用收口在 hook 内），
  // mutateAsync 是 MutationObserver 构造期绑定的稳定引用，effect/定时器链可直接捕获。
  const onCloseRef = useRef(props.onClose);
  onCloseRef.current = props.onClose;

  const [starting, setStarting] = useState(true);
  const [beginError, setBeginError] = useState('');
  const [attempt, setAttempt] = useState<ImBotProvisionView | null>(null);
  const [qrDataUrl, setQrDataUrl] = useState('');
  const [qrError, setQrError] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  // resetLocal 清抽屉本地态到「申请中」初值：handleClose 与 connected 收尾共用，防下次打开
  // 短暂残留上一轮的二维码/错误态。
  const resetLocal = () => {
    setStarting(true);
    setAttempt(null);
    setBeginError('');
    setQrDataUrl('');
    setQrError(false);
  };

  // 主流程：open 触发一次（begin → 起轮询链）；卸载/关闭清理全部定时器；
  // 关闭时若仍在进行中，异步通知服务端取消（fire-and-forget）。
  useEffect(() => {
    if (!open) {
      return undefined;
    }
    let disposed = false;
    let pollTimer: number | null = null;

    const stopPoll = () => {
      if (pollTimer != null) {
        window.clearTimeout(pollTimer);
        pollTimer = null;
      }
    };

    const tick = async (attemptId: string, fallbackInterval: number) => {
      try {
        const view = await pollMutation.mutateAsync({ attemptId });
        if (disposed) {
          return;
        }
        setAttempt(view);
        if (view.state === 'connected') {
          stopPoll();
          resetLocal(); // 本地态一并重置（列表缓存已由 poll hook 在 connected 时失效刷新）
          onCloseRef.current(); // 新 bot 已建并连接，收抽屉回列表
          return;
        }
        if (view.state === 'pending' || view.state === 'connecting') {
          pollTimer = window.setTimeout(() => void tick(attemptId, fallbackInterval), view.pollIntervalMs || fallbackInterval);
        }
        // failed / expired / cancelled：终态停轮询，UI 停在错误分支
      } catch {
        // 网络抖动按节奏重试（会话仍在服务端）
        if (!disposed) {
          pollTimer = window.setTimeout(() => void tick(attemptId, fallbackInterval), fallbackInterval);
        }
      }
    };

    // 状态重置在 handleClose（事件处理器）完成：本 effect 仅跑异步流程，无同步 setState。
    void (async () => {
      try {
        const view = await beginMutation.mutateAsync();
        if (disposed) {
          return;
        }
        setAttempt(view);
        setStarting(false);
        renderQr(view.qrContent ?? '')
          .then((url) => {
            if (!disposed) {
              setQrDataUrl(url);
            }
          })
          .catch(() => {
            if (!disposed) {
              setQrError(true);
            }
          });
        pollTimer = window.setTimeout(() => void tick(view.attemptId, view.pollIntervalMs), view.pollIntervalMs);
      } catch (e) {
        if (!disposed) {
          setStarting(false);
          setBeginError((e as Error).message);
        }
      }
    })();

    return () => {
      disposed = true;
      stopPoll();
    };
  }, [open]);

  // 倒计时 ticker（1s；仅 pending 且有 QR 语境时驱动进度条）。
  useEffect(() => {
    if (!open || attempt?.state !== 'pending') {
      return undefined;
    }
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, [open, attempt?.state]);

  const handleClose = () => {
    // 进行中关闭：服务端会话一并取消（防止僵尸 attempt 占住单会话槽）。
    if (attempt && (attempt.state === 'pending' || attempt.state === 'connecting')) {
      cancelMutation.mutate({ attemptId: attempt.attemptId });
    }
    // 重置到「申请中」初态：下次打开由本组件 effect 直接进入异步流程（无同步 setState）。
    resetLocal();
    onCloseRef.current();
  };

  const remainingMs = attempt && attempt.expiresAt > 0 ? Math.max(0, attempt.expiresAt - now) : 0;

  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={handleClose}
      sx={{ '& .MuiDrawer-paper': { width: DRAWER_WIDTH, maxWidth: '90vw' } }}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        <Box sx={{ px: 3, py: 2, borderBottom: 1, borderColor: 'divider', display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography sx={{ fontSize: 16, fontWeight: 600 }}>扫码接入机器人</Typography>
          <IconButton size="small" onClick={handleClose} sx={{ 'ml': 'auto', '& svg': { fontSize: 18 } }}>
            <CloseOutlinedIcon />
          </IconButton>
        </Box>

        <Box sx={{ flex: 1, overflow: 'auto', p: 3, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 2 }}>
          {starting && (
            <>
              <CircularProgress size={28} sx={{ mt: 6 }} />
              <Typography sx={{ fontSize: 13, color: 'text.secondary' }}>正在申请授权二维码…</Typography>
            </>
          )}

          {!starting && beginError && (
            <>
              <Typography sx={{ mt: 6, fontSize: 13, color: 'error.main' }}>
                {`扫码会话申请失败：${beginError}`}
              </Typography>
              <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>
                可改用「手动接入」，或稍后重试。
              </Typography>
            </>
          )}

          {!starting && !beginError && attempt?.state === 'pending' && (
            <>
              <Typography sx={{ fontSize: 13, color: 'text.secondary' }}>
                等待企业微信 App 扫码
              </Typography>
              <Box sx={{ p: 1.5, borderRadius: 2, border: 1, borderColor: 'divider', bgcolor: 'background.paper' }}>
                {qrError
                  ? <Typography sx={{ width: 240, height: 240, display: 'flex', alignItems: 'center', fontSize: 12, color: 'text.secondary' }}>二维码渲染失败，请重新生成</Typography>
                  : (qrDataUrl
                      ? <Box component="img" src={qrDataUrl} alt="企业微信扫码授权二维码" sx={{ width: 240, height: 240, display: 'block' }} />
                      : <Box sx={{ width: 240, height: 240, display: 'flex', alignItems: 'center', justifyContent: 'center' }}><CircularProgress size={22} /></Box>)}
              </Box>
              <Box sx={{ width: '100%', maxWidth: 320 }}>
                <LinearProgress
                  variant="determinate"
                  value={Math.min(100, Math.round((remainingMs / QR_TTL_MS) * 100))}
                  sx={{ height: 4, borderRadius: 2 }}
                />
                <Typography sx={{ mt: 0.5, fontSize: 12, color: 'text.secondary', textAlign: 'center' }}>
                  {`二维码 ${Math.ceil(remainingMs / 1000)}s 后过期`}
                </Typography>
              </Box>
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, alignSelf: 'flex-start', pl: 1 }}>
                <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>1. 打开企业微信 App，扫描左侧二维码</Typography>
                <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>2. 在腾讯授权页面确认创建智能机器人</Typography>
                <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>3. 返回这里等待连接完成</Typography>
              </Box>
            </>
          )}

          {!starting && !beginError && attempt?.state === 'connecting' && (
            <>
              <CircularProgress size={28} sx={{ mt: 6 }} />
              <Typography sx={{ fontSize: 13, color: 'text.secondary' }}>
                企业微信已授权，正在连接机器人…
              </Typography>
            </>
          )}

          {!starting && !beginError && (attempt?.state === 'failed' || attempt?.state === 'expired') && (
            <>
              <Typography sx={{ mt: 6, fontSize: 13, color: 'error.main' }}>
                {attempt.state === 'expired' ? '二维码已过期' : '扫码接入失败'}
              </Typography>
              {attempt.errCode && (
                <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>
                  {`错误码：${attempt.errCode}`}
                </Typography>
              )}
            </>
          )}
        </Box>

        {/* 底部操作：进行中 = 重新生成 + 取消；失败态 = 重新生成 + 关闭（重新生成由父组件 key 重挂载驱动） */}
        <Box sx={{ px: 3, py: 2, borderTop: 1, borderColor: 'divider', display: 'flex', justifyContent: 'flex-end', gap: 1 }}>
          {attempt?.state === 'pending' && (
            <Button
              size="small"
              startIcon={<RefreshOutlinedIcon sx={{ '& svg': { fontSize: 16 } }} />}
              onClick={props.onRegenerate}
            >
              重新生成二维码
            </Button>
          )}
          <Button onClick={handleClose} color="inherit">取消</Button>
        </Box>
      </Box>
    </Drawer>
  );
}

export default ImBotProvisionDrawer;
