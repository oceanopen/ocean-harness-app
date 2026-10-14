import type { BrowserSessionView } from '@src/services';
import { Alert, Box, Button, CircularProgress, Typography, useTheme } from '@mui/material';
import { useDocumentVisible } from '@src/shared/useDocumentVisible';
import {
  useBrowserFrameStream,
  useBrowserSessionEvents,
  useBrowserSessionView,
  useBrowserStageStore,
} from '@src/state/browserSession';
import { useDevWorkbenchStore } from '@src/state/devWorkbench';
import { useEffect } from 'react';
import LiveViewport from './LiveViewport';

/**
 * BrowserStageOverlay：浏览器操作台浮层——激活 profile 接管工作台终端内容区
 * （FilePreviewOverlay 同款接管语义，absolute inset 0 不挤压布局：终端 DOM 尺寸不变、
 * 零 SIGWINCH 扰动，会话后端常驻仅视觉遮盖；实色背景完整接收 pointerEvents 防点击
 * 穿透被 xterm 吞掉）。激活态经 useBrowserStageStore 共享（BrowserPanel 迷你预览点击
 * 激活）；Esc 关浮层（焦点在画面画布时让位——Esc 先服务于远端页面，如关闭页面内
 * 对话框；沉浸全屏下让位页面级退出）。显隐派生自 activatedProfile；激活期自持投影
 * SSE 订阅（浏览器工具 tab 关闭后浮层仍随帧刷新状态/页面列表）。
 */
export default function BrowserStageOverlay() {
  const activatedProfile = useBrowserStageStore(s => s.activatedProfile);
  if (activatedProfile == null) {
    return null;
  }
  return <StageContent key={activatedProfile} profile={activatedProfile} />;
}

function StageContent({ profile }: { profile: string }) {
  const theme = useTheme();
  const deactivate = useBrowserStageStore(s => s.deactivate);
  const selectedPageId = useBrowserStageStore(s => s.watchPage[profile] ?? null);
  const { data: snapshot } = useBrowserSessionView();
  // 激活期自持投影订阅：本浮层独立于浏览器工具 tab 的挂载而存在。
  useBrowserSessionEvents();

  const session: BrowserSessionView | null = snapshot?.sessions.find(item => item.profile === profile) ?? null;
  const ready = session?.status === 'ready';
  // 观看页码：卡内点选优先，未选 = 当前页；pages 未就绪以 0 起步，首帧即真实页。
  const pageId = selectedPageId ?? (session?.pages ?? []).find(page => page.current)?.index ?? 0;
  // 帧流闸门：ready && 窗口可见（面板关托盘/最小化时 webview 隐藏但组件树不卸载，
  // 隐藏即断流，恢复显示重订回放首帧）；throttleMs 300 = 写侧节流档（~3fps，迷你
  // 预览 2000 同机制），绘制侧 rAF 合帧见 useFramePaint。
  const visible = useDocumentVisible();
  const stream = useBrowserFrameStream(profile, pageId, ready && visible, 300);

  // Esc 关浮层：焦点在画面画布（远端页面键盘交互）时让位；沉浸全屏下让位页面级退出
  // （getState 现读，不建渲染订阅，范式同 FilePreviewOverlay）。
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') {
        return;
      }
      if (useDevWorkbenchStore.getState().workbenchFullscreen) {
        return;
      }
      const target = e.target;
      // 焦点在画面画布/其隐藏键盘宿主上 = 远端页面键盘交互中（Esc 先服务远端，如关闭
      // 页面内对话框），不关浮层。
      if (target instanceof HTMLElement && (target.tagName === 'CANVAS' || target.dataset.browserStageInput != null)) {
        return;
      }
      deactivate();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [deactivate]);

  return (
    <Box
      sx={{
        position: 'absolute',
        inset: '0px',
        zIndex: theme.zIndex.mobileStepper,
        bgcolor: 'background.paper',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: '8px', px: '12px', height: '40px', flexShrink: 0, borderBottom: '1px solid', borderColor: 'divider' }}>
        <Typography sx={{ fontSize: '13px', fontWeight: 600 }}>{`操作台：${profile}`}</Typography>
        <Typography sx={{ fontSize: '12px', color: 'text.secondary', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {stream.target != null
            ? `正在投送：${stream.target.title || stream.target.url}`
            : (ready ? '正在连接实时画面…' : '')}
        </Typography>
        <Typography
          onClick={deactivate}
          sx={{ 'ml': 'auto', 'flexShrink': 0, 'fontSize': '12px', 'color': 'text.secondary', 'cursor': 'pointer', '&:hover': { color: 'text.primary' } }}
        >
          取消激活（Esc）
        </Typography>
      </Box>

      <Box sx={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', gap: '8px', p: '8px' }}>
        {!ready && (
          <Box sx={{ flex: 1, minHeight: '0px', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: '8px', border: '1px solid', borderColor: 'divider', borderRadius: '4px' }}>
            {session == null && (
              <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>会话投影加载中…</Typography>
            )}
            {session?.status === 'starting' && (
              <>
                <CircularProgress size={16} sx={{ color: 'text.secondary' }} />
                <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>浏览器正在准备中…</Typography>
              </>
            )}
            {session?.status === 'idle' && (
              <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>会话已释放，在右侧会话卡「打开会话」后恢复画面</Typography>
            )}
            {session?.status === 'failed' && (
              <Typography sx={{ fontSize: '12px', color: 'error.main' }}>{session.error ?? '浏览器拉起失败'}</Typography>
            )}
          </Box>
        )}
        {ready && session != null && (
          <>
            <LiveViewport profile={profile} frameRef={stream.frameRef} hasFrame={stream.hasFrame} interactive={stream.phase === 'live'} />
            {stream.healingMessage != null && (
              <Alert severity="warning" sx={{ fontSize: '12px', flexShrink: 0 }}>
                {stream.healingMessage}
              </Alert>
            )}
            {stream.closedReason != null && (
              <Alert severity="warning" sx={{ fontSize: '12px', flexShrink: 0 }}>
                {`实时画面已停止：${stream.closedReason}（选择其他页面可重新订阅）`}
              </Alert>
            )}
            {stream.phase === 'failed' && (
              <Alert
                severity="error"
                sx={{ fontSize: '12px', flexShrink: 0 }}
                action={(
                  <Button color="inherit" size="small" onClick={stream.retry}>
                    重试
                  </Button>
                )}
              >
                {`实时画面连接失败：${stream.failedMessage ?? '请稍后重试'}`}
              </Alert>
            )}
          </>
        )}
      </Box>
    </Box>
  );
}
