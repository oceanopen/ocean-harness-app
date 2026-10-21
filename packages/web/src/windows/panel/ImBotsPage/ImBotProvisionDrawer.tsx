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
  TextField,
  Typography,
} from '@mui/material';
import {
  useProvisionBegin,
  useProvisionCancel,
  useProvisionPoll,
  useProvisionVerifyCode,
} from '@src/state/imBots';
import QRCode from 'qrcode';
import { useEffect, useRef, useState } from 'react';

// 扫码授权接入抽屉（渠道化）：按渠道申请二维码 → 扫码 → 中间态推进 → 服务端自动建 bot/换绑
// 并连接；轮询走宿主 pollIntervalMs 链式 setTimeout（终态即停），scanned/needs_verification
// 仅微信渠道。二维码渠道差异收口在 resolveQrSrc；rebindBotId 非空为换绑语境。
// 「重新生成二维码」由父组件以 key 重挂载实现（卸载清定时器 + 新 begin 自动取消旧会话）。

const DRAWER_WIDTH = '480px';

/** 二维码总时长（与后端 qrTTL 同源：5 分钟，宿主未下发 TTL 的本地推断值）。 */
const QR_TTL_MS = 5 * 60_000;

/** 配对码形态（与后端校验同形：4-8 位数字）。 */
const VERIFY_CODE_PATTERN = /^\d{4,8}$/;

/** 扫码会话进行中状态集合（关闭时需通知服务端取消；终态不取消）。 */
const IN_FLIGHT_STATES = new Set(['pending', 'scanned', 'needs_verification', 'connecting']);

/** 可重新生成二维码的状态集合：进行中 pending + 失败终态 failed/expired。 */
const REGENERATABLE_STATES = new Set(['pending', 'failed', 'expired']);

/**
 * 二维码来源收口：两侧均为「扫码内容 → 本地渲染 dataURL」，杜绝 webview 外链加载依赖。
 * 微信侧内容为 liteapp 扫码链接（qrcode_img_content 非图片地址），域名白名单对齐后端 isWeixinHost。
 */
async function resolveQrSrc(channel: string, content: string): Promise<string> {
  let host = '';
  try {
    host = new URL(content).hostname;
  } catch {
    host = '';
  }
  const trusted = channel === 'weixin'
    ? host === 'weixin.qq.com' || host.endsWith('.weixin.qq.com') || host === 'wechat.com' || host.endsWith('.wechat.com')
    : host === 'work.weixin.qq.com';
  if (!content.startsWith('https://') || !trusted) {
    throw new Error('二维码内容域名异常');
  }
  return QRCode.toDataURL(content, { width: 240, margin: 1, errorCorrectionLevel: 'M' });
}

/** 渠道扫码引导步骤 + 中间态文案 SSOT。 */
function channelCopy(channel: string): { steps: string[]; scannedHint: string; verifyHint: string; connectingHint: string } {
  if (channel === 'weixin') {
    return {
      steps: [
        '1. 打开微信 App，扫描二维码并确认登录',
        '2. 如微信提示配对码，在下方输入框填入',
        '3. 返回这里等待连接完成',
      ],
      scannedHint: '已扫码，请在手机上确认登录',
      verifyHint: '请在微信中查看配对码并在下方输入',
      connectingHint: '微信已确认，正在连接机器人…',
    };
  }
  return {
    steps: [
      '1. 打开企业微信 App，扫描左侧二维码',
      '2. 在腾讯授权页面确认创建智能机器人',
      '3. 返回这里等待连接完成',
    ],
    scannedHint: '已扫码，请在手机上确认',
    verifyHint: '',
    connectingHint: '企业微信已授权，正在连接机器人…',
  };
}

function ImBotProvisionDrawer(props: {
  open: boolean;
  channelKey: string;
  rebindBotId?: string; // 非空 = 为既有 bot 换绑凭据（空/undefined = 扫码新建）
  onClose: () => void;
  onRegenerate: () => void;
}) {
  const { open, channelKey } = props;
  const rebind = (props.rebindBotId ?? '') !== '';
  const beginMutation = useProvisionBegin();
  const pollMutation = useProvisionPoll();
  const cancelMutation = useProvisionCancel();
  const verifyMutation = useProvisionVerifyCode();
  const copy = channelCopy(channelKey);

  // 回调经 ref 消费：父组件内联箭头每次渲染变化，若入 effect 依赖会使扫码流程随父渲染重启。
  // begin/poll 走 state 层 mutation（connected 时失效列表缓存的副作用收口在 hook 内），
  // mutateAsync 是 MutationObserver 构造期绑定的稳定引用，effect/定时器链可直接捕获。
  const onCloseRef = useRef(props.onClose);
  onCloseRef.current = props.onClose;
  // begin 参数同样经 ref 进 effect（换绑目标由父组件在打开前设定，打开后不再变化）。
  const beginArgsRef = useRef({ channel: channelKey, botId: props.rebindBotId ?? '' });
  beginArgsRef.current = { channel: channelKey, botId: props.rebindBotId ?? '' };

  const [starting, setStarting] = useState(true);
  const [beginError, setBeginError] = useState('');
  const [attempt, setAttempt] = useState<ImBotProvisionView | null>(null);
  const [qrDataUrl, setQrDataUrl] = useState('');
  const [qrError, setQrError] = useState(false);
  const [verifyCode, setVerifyCode] = useState('');
  const [now, setNow] = useState(() => Date.now());

  // resetLocal 清抽屉本地态到「申请中」初值：handleClose 与 connected 收尾共用，防下次打开
  // 短暂残留上一轮的二维码/错误态。
  const resetLocal = () => {
    setStarting(true);
    setAttempt(null);
    setBeginError('');
    setQrDataUrl('');
    setQrError(false);
    setVerifyCode('');
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

    // 轮询步进：中间态（含 scanned / needs_verification）按宿主节奏续轮询；终态停链。
    const tick = async (attemptId: string, fallbackInterval: number) => {
      try {
        const view = await pollMutation.mutateAsync({ channel: beginArgsRef.current.channel, attemptId });
        if (disposed) {
          return;
        }
        setAttempt(view);
        if (view.state === 'connected') {
          stopPoll();
          resetLocal(); // 本地态一并重置（列表缓存已由 poll hook 在 connected 时失效刷新）
          onCloseRef.current(); // bot 已建/换绑并连接，收抽屉回列表
          return;
        }
        if (IN_FLIGHT_STATES.has(view.state)) {
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
        const view = await beginMutation.mutateAsync(beginArgsRef.current);
        if (disposed) {
          return;
        }
        setAttempt(view);
        setStarting(false);
        resolveQrSrc(beginArgsRef.current.channel, view.qrContent ?? '')
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
    if (attempt && IN_FLIGHT_STATES.has(attempt.state)) {
      cancelMutation.mutate({ channel: channelKey, attemptId: attempt.attemptId });
    }
    // 重置到「申请中」初态：下次打开由本组件 effect 直接进入异步流程（无同步 setState）。
    resetLocal();
    onCloseRef.current();
  };

  // 配对码提交：受理后服务端推进状态机，UI 随下一次轮询快照自然收敛（无需本地翻态）。
  const submitVerifyCode = () => {
    if (!attempt || !VERIFY_CODE_PATTERN.test(verifyCode)) {
      return;
    }
    verifyMutation.mutate(
      { channel: channelKey, attemptId: attempt.attemptId, verifyCode },
      { onSuccess: () => setVerifyCode('') },
    );
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
        <Box sx={{ px: '24px', py: '16px', borderBottom: '1px solid', borderColor: 'divider', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <Typography sx={{ fontSize: '16px', fontWeight: 600 }}>
            {rebind ? '重新扫码授权' : '扫码接入机器人'}
          </Typography>
          <IconButton size="small" onClick={handleClose} sx={{ 'ml': 'auto', '& svg': { fontSize: '18px' } }}>
            <CloseOutlinedIcon />
          </IconButton>
        </Box>

        {rebind && (
          <Box sx={{ px: '24px', pt: '16px' }}>
            <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>
              扫码成功后替换该机器人的连接凭据，绑定关系与配置保持不变。
            </Typography>
          </Box>
        )}

        <Box sx={{ flex: 1, overflow: 'auto', p: '24px', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '16px' }}>
          {starting && (
            <>
              <CircularProgress size={28} sx={{ mt: '48px' }} />
              <Typography sx={{ fontSize: '13px', color: 'text.secondary' }}>正在申请授权二维码…</Typography>
            </>
          )}

          {!starting && beginError && (
            <>
              <Typography sx={{ mt: '48px', fontSize: '13px', color: 'error.main' }}>
                {`扫码会话申请失败：${beginError}`}
              </Typography>
              <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>
                可稍后重试，或重新打开扫码接入。
              </Typography>
            </>
          )}

          {!starting && !beginError && attempt?.state === 'pending' && (
            <>
              <Typography sx={{ fontSize: '13px', color: 'text.secondary' }}>
                {`等待${channelKey === 'weixin' ? '微信' : '企业微信'} App 扫码`}
              </Typography>
              <Box sx={{ p: '12px', borderRadius: '8px', border: '1px solid', borderColor: 'divider', bgcolor: 'background.paper' }}>
                {qrError
                  ? <Typography sx={{ width: '240px', height: '240px', display: 'flex', alignItems: 'center', fontSize: '12px', color: 'text.secondary' }}>二维码加载失败，请重新生成</Typography>
                  : (qrDataUrl
                      ? <Box component="img" src={qrDataUrl} alt="扫码授权二维码" sx={{ width: '240px', height: '240px', display: 'block' }} />
                      : <Box sx={{ width: '240px', height: '240px', display: 'flex', alignItems: 'center', justifyContent: 'center' }}><CircularProgress size={22} /></Box>)}
              </Box>
              <Box sx={{ width: '100%', maxWidth: '320px' }}>
                <LinearProgress
                  variant="determinate"
                  value={Math.min(100, Math.round((remainingMs / QR_TTL_MS) * 100))}
                  sx={{ height: '4px', borderRadius: '8px' }}
                />
                <Typography sx={{ mt: '4px', fontSize: '12px', color: 'text.secondary', textAlign: 'center' }}>
                  {`二维码 ${Math.ceil(remainingMs / 1000)}s 后过期`}
                </Typography>
              </Box>
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: '4px', alignSelf: 'flex-start', pl: '8px' }}>
                {copy.steps.map(step => (
                  <Typography key={step} sx={{ fontSize: '12px', color: 'text.secondary' }}>{step}</Typography>
                ))}
              </Box>
            </>
          )}

          {!starting && !beginError && attempt?.state === 'scanned' && (
            <>
              <CircularProgress size={28} sx={{ mt: '48px' }} />
              <Typography sx={{ fontSize: '13px', color: 'text.secondary' }}>{copy.scannedHint}</Typography>
            </>
          )}

          {!starting && !beginError && attempt?.state === 'needs_verification' && (
            <>
              <Typography sx={{ mt: '48px', fontSize: '13px' }}>{copy.verifyHint}</Typography>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', width: '100%', maxWidth: '320px' }}>
                <TextField
                  size="small"
                  fullWidth
                  autoFocus
                  label="配对码"
                  placeholder="4-8 位数字"
                  value={verifyCode}
                  onChange={e => setVerifyCode(e.target.value.replace(/\D/g, '').slice(0, 8))}
                  onKeyDown={e => e.key === 'Enter' && submitVerifyCode()}
                  error={verifyCode !== '' && !VERIFY_CODE_PATTERN.test(verifyCode)}
                  helperText={verifyMutation.error ? verifyMutation.error.message : ' '}
                />
                <Button
                  variant="contained"
                  loading={verifyMutation.isPending}
                  disabled={!VERIFY_CODE_PATTERN.test(verifyCode)}
                  onClick={submitVerifyCode}
                >
                  提交
                </Button>
              </Box>
            </>
          )}

          {!starting && !beginError && attempt?.state === 'connecting' && (
            <>
              <CircularProgress size={28} sx={{ mt: '48px' }} />
              <Typography sx={{ fontSize: '13px', color: 'text.secondary' }}>{copy.connectingHint}</Typography>
            </>
          )}

          {!starting && !beginError && (attempt?.state === 'failed' || attempt?.state === 'expired') && (
            <>
              <Typography sx={{ mt: '48px', fontSize: '13px', color: 'error.main' }}>
                {attempt.state === 'expired' ? '二维码已过期' : '扫码接入失败'}
              </Typography>
              {attempt?.state === 'failed' && attempt.errCode && (
                <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>
                  {`错误码：${attempt.errCode}`}
                </Typography>
              )}
              {attempt.state === 'failed' && (attempt.errCode === 'already-bound' || attempt.errCode === 'account-mismatch') && (
                <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>
                  {attempt.errCode === 'already-bound'
                    ? '该微信账号已被其他机器人绑定；如需改绑，请先删除对应机器人再扫码。'
                    : '扫码账号与该机器人原绑定不一致：换绑仅限同账号重授权，更换账号请删除原机器人后重新扫码。'}
                </Typography>
              )}
            </>
          )}
        </Box>

        {/* 底部操作：进行中 = 重新生成 + 取消；失败态 = 重新生成 + 关闭（重新生成由父组件 key 重挂载驱动） */}
        <Box sx={{ px: '24px', py: '16px', borderTop: '1px solid', borderColor: 'divider', display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
          {REGENERATABLE_STATES.has(attempt?.state ?? '') && (
            <Button
              size="small"
              startIcon={<RefreshOutlinedIcon sx={{ '& svg': { fontSize: '16px' } }} />}
              onClick={props.onRegenerate}
            >
              重新生成二维码
            </Button>
          )}
          <Button onClick={handleClose} color="inherit">{IN_FLIGHT_STATES.has(attempt?.state ?? '') ? '取消' : '关闭'}</Button>
        </Box>
      </Box>
    </Drawer>
  );
}

export default ImBotProvisionDrawer;
