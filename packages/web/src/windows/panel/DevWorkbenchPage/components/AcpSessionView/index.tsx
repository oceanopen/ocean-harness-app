import type { AcpConversationEntry, AcpViewSnapshot } from '@src/services';
import type { AcpEventsStream } from '@src/state/acpSession';
import { Alert, Box, Button, CircularProgress, Typography } from '@mui/material';
import { isAcpSessionSubscribable, useAcpSessionEvents, useAcpSessionView, useEnsureAcpSession } from '@src/state/acpSession';
import { useEffect, useRef } from 'react';
import MessageList from './MessageList';
import PendingCard from './PendingCard';
import PromptComposer from './PromptComposer';

/**
 * AcpSessionView：issue 主窗口 ACP 模式的会话视图（TerminalLaunchFlow acp 分支渲染，与终端树完全同位）。
 * 编排时序（编码规则 1）：挂载即幂等受理 ensure（starting/ready join 返现状，failed/terminated 重建，受理显式携带 pickedLaunchMode='acp'，后端门禁以此放行且不落库）→ SSE 订阅建连首帧全量快照 → 视图按快照 status 分支渲染。
 * 受理同步失败先于快照分支渲染失败态 + 重新启动，绝不落死输入框；订阅经 isAcpSessionSubscribable 闸门（D3），订阅失败置 failed 终态横幅手动重试；挂起审批/表单在 MessageList 与输入区之间平铺 PendingCard 应答。
 */
export default function AcpSessionView({ conversationId }: { conversationId: string }) {
  const ensure = useEnsureAcpSession();
  const ensuredRef = useRef(false);

  /**
   * 挂载即受理（幂等）：ref 防重入（effect 依赖的 mutation 对象随渲染更新，ref 在前
   * 挡住重复受理；StrictMode 双跑下 ensure 后端幂等，ref 仅省一次无效请求）。
   * conversationId 空串 = 上游闸门未就绪的防御传参（正常由 T5.3 编排下传非空 id），不受理。
   */
  useEffect(() => {
    if (ensuredRef.current || conversationId === '') {
      return;
    }
    ensuredRef.current = true;
    ensure.mutate({ conversationId, pickedLaunchMode: 'acp' });
  }, [ensure, conversationId]);

  const { data: snapshot, isPending, error, refetch } = useAcpSessionView(conversationId);

  /**
   * SSE 订阅（D3 闸门）：快照可订阅状态（starting/ready/failed）才建连——idle（从未受理）/
   * terminated（已终结）不订阅，治首开订阅先于 ensure 的 404 竞态；terminated 随「重新启动」
   * 受理回包迁回 starting，闸门重开自动重订。首帧快照入缓存，增量帧归约更新；闸门关闭即断，重开重建连。
   */
  const events = useAcpSessionEvents(conversationId, isAcpSessionSubscribable(snapshot?.status));

  // 未接通空态：上游会话 id 未就绪的防御降级面（正常链路已由 T5.3 编排接通）。
  if (conversationId === '') {
    return (
      <FullCenter>
        <Typography variant="body2" color="text.secondary">会话未接通（工作目录会话编排接入后可用）</Typography>
      </FullCenter>
    );
  }

  // 受理同步失败优先呈现（配置校验拒绝等）：先于快照分支，杜绝 idle 死输入框。
  if (ensure.isError) {
    return (
      <FullCenter>
        <Typography variant="body2" color="error">会话创建失败：{ensure.error.message}</Typography>
        <Button
          variant="outlined"
          size="small"
          onClick={() => ensure.mutate({ conversationId, pickedLaunchMode: 'acp' })}
          loading={ensure.isPending}
          sx={{ mt: '12px' }}
        >
          重新启动
        </Button>
      </FullCenter>
    );
  }
  if (isPending) {
    return <FullCenter><CircularProgress /></FullCenter>;
  }
  if (error != null || snapshot == null) {
    return (
      <FullCenter>
        <Typography variant="body2" color="error">会话状态读取失败{error != null ? `：${error.message}` : ''}</Typography>
        <Button variant="outlined" size="small" onClick={() => void refetch()} sx={{ mt: '12px' }}>
          重试
        </Button>
      </FullCenter>
    );
  }
  return <StatusBranch conversationId={conversationId} snapshot={snapshot} events={events} onRestart={() => ensure.mutate({ conversationId, pickedLaunchMode: 'acp' })} ensurePending={ensure.isPending} />;
}

/** 按快照 status 分支：starting 启动中 / failed+terminated 原因与重启 / idle+ready 会话视图。 */function StatusBranch({ conversationId, snapshot, events, onRestart, ensurePending }: {
  conversationId: string;
  snapshot: AcpViewSnapshot;
  events: AcpEventsStream;
  onRestart: () => void;
  ensurePending: boolean;
}) {
  switch (snapshot.status) {
    case 'starting':
      return (
        <FullCenter>
          <CircularProgress size={28} />
          <Typography variant="body2" color="text.secondary" sx={{ mt: '12px' }}>
            正在启动 ACP 会话{snapshot.agentCode ? `（${snapshot.agentCode}）` : ''}…
          </Typography>
          {/* starting 同为闸门开启态：订阅终态失败时启动帧无人送达，视图死转圈——横幅给出
              信号与重试订阅（会话消亡类拒绝重试恒败，整页重挂重新受理兜底）。 */}
          <SubscriptionFailedAlert events={events} sx={{ mt: '12px' }} />
        </FullCenter>
      );
    case 'failed':
    case 'terminated':
      return (
        <FullCenter>
          <Typography variant="body2" color="error">
            {snapshot.status === 'failed' ? '会话启动失败' : '会话已结束'}{snapshot.error ? `：${snapshot.error}` : ''}
          </Typography>
          {snapshot.status === 'failed' && (
            /**
             * failed 缓存闸门开着：重启的 setStarting 帧依赖订阅送达，订阅终态失败时须先
             * 重试订阅恢复链路（terminated 闸门已关，ensure 回包翻闸自动重订，无需此横幅）。
             */
            <SubscriptionFailedAlert events={events} sx={{ mt: '12px' }} />
          )}
          <Button variant="outlined" size="small" onClick={onRestart} loading={ensurePending} sx={{ mt: '12px' }}>
            重新启动
          </Button>
        </FullCenter>
      );
    case 'idle':
    case 'ready':
      return (
        <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
          {/* 订阅失败横幅不阻断会话视图（REST 快照已呈现历史）；终态（terminated/failed）
              呈现仍由快照 status 分支驱动，此处只管订阅通道本身的失败。 */}
          <SubscriptionFailedAlert events={events} sx={{ flexShrink: 0 }} />
          <MessageList entries={snapshot.entries} />
          {/* 回合即时占位：发送后即刻反馈，首个内容条目（思考/正文/工具）到达即让位 */}
          {snapshot.turnActive && showTurnPendingHint(snapshot.entries) && <TurnPendingRow />}
          {snapshot.pendings.length > 0 && (
            /**
             * 固定挂起区（MessageList 与输入区之间）：pending 逐项平铺卡片，全部应答
             * 后（pendings 清空）整区消失。
             */
            <Box sx={{ flexShrink: 0, p: '8px', display: 'flex', flexDirection: 'column', gap: '8px', borderTop: '1px solid', borderColor: 'divider' }}>
              {snapshot.pendings.map(pending => (
                <PendingCard key={pending.pendingId} conversationId={conversationId} pending={pending} />
              ))}
            </Box>
          )}
          <PromptComposer conversationId={conversationId} turnActive={snapshot.turnActive} />
        </Box>
      );
  }
}

// 回合即时占位判定：回合进行中且尚无任何内容条目（最后一条是 user 输入）——首个内容块到达即让位。
function showTurnPendingHint(entries: AcpConversationEntry[]): boolean {
  return entries.length > 0 && entries[entries.length - 1].kind === 'user';
}

/**
 * 回合即时占位行：填充发送到首个流式内容之间的空白死区（受理/冷启动期无任何可见反馈）。
 * 固定行不随滚动，宽度对齐消息列容器。
 */
function TurnPendingRow() {
  return (
    <Box sx={{ flexShrink: 0, display: 'flex', justifyContent: 'center', py: '6px' }}>
      <Box sx={{ maxWidth: '860px', width: '100%', px: '16px', display: 'flex', alignItems: 'center', gap: '8px' }}>
        <CircularProgress size={14} sx={{ color: 'text.secondary' }} />
        <Typography variant="body2" color="text.secondary" sx={{ fontSize: '12px' }}>收到，处理中...</Typography>
      </Box>
    </Box>
  );
}

/** 全区居中外壳（启动/失败/加载态共用）。 */
function FullCenter({ children }: { children: React.ReactNode }) {
  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', p: '16px' }}>
      {children}
    </Box>
  );
}

/** 订阅失败横幅（闸门开启态分支共用）：订阅通道终态失败的信号 + 重试订阅。 */
function SubscriptionFailedAlert({ events, sx }: { events: AcpEventsStream; sx: React.ComponentProps<typeof Alert>['sx'] }) {
  if (events.phase !== 'failed') {
    return null;
  }
  return (
    <Alert
      severity="error"
      sx={sx}
      action={(
        <Button color="inherit" size="small" onClick={events.retry}>
          重试订阅
        </Button>
      )}
    >
      {`实时消息订阅失败：${events.failedMessage ?? '请稍后重试'}`}
    </Alert>
  );
}
