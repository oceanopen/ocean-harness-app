import type { AcpViewSnapshot } from '@src/services';
import { Box, Button, CircularProgress, Typography } from '@mui/material';
import { useAcpSessionEvents, useAcpSessionView, useEnsureAcpSession } from '@src/state/acpSession';
import { useEffect, useRef } from 'react';
import MessageList from './MessageList';
import PendingCard from './PendingCard';
import PromptComposer from './PromptComposer';

/**
 * AcpSessionView：issue 主窗口 ACP 模式的会话视图（TerminalLaunchFlow acp 分支渲染，
 * 与终端树完全同位）。编排时序（编码规则 1）：挂载即幂等受理 ensure（starting/ready
 * join 返现状，failed/terminated 重建；受理显式携带 pickedLaunchMode='acp'——视图即
 * acp 意图载体，配置 acp 与临场选择 acp 统一声明，后端门禁以此放行且不落库）→ SSE
 * 订阅建连首帧全量快照 → 视图按快照 status 分支渲染；受理同步失败（配置校验拒绝等）
 * 先于快照分支渲染失败态 + 重新启动，绝不落死输入框。挂起审批/表单在 MessageList 与
 * 输入区之间的固定挂起区平铺 PendingCard 应答（卡片移除由 SSE pendingClosed 帧驱动）。
 */
export default function AcpSessionView({ issueId }: { issueId: string }) {
  const ensure = useEnsureAcpSession();
  const ensuredRef = useRef(false);

  // 挂载即受理（幂等）：ref 防重入（effect 依赖的 mutation 对象随渲染更新，ref 在前
  // 挡住重复受理；StrictMode 双跑下 ensure 后端幂等，ref 仅省一次无效请求）。
  useEffect(() => {
    if (ensuredRef.current) {
      return;
    }
    ensuredRef.current = true;
    ensure.mutate({ issueId, pickedLaunchMode: 'acp' });
  }, [ensure, issueId]);

  // SSE 订阅：首帧快照入缓存，增量帧归约更新；卸载即断，重挂载重建连自愈。
  useAcpSessionEvents(issueId);

  const { data: snapshot, isPending, error, refetch } = useAcpSessionView(issueId);

  // 受理同步失败优先呈现（配置校验拒绝等）：先于快照分支，杜绝 idle 死输入框。
  if (ensure.isError) {
    return (
      <FullCenter>
        <Typography variant="body2" color="error">会话创建失败：{ensure.error.message}</Typography>
        <Button
          variant="outlined"
          size="small"
          onClick={() => ensure.mutate({ issueId, pickedLaunchMode: 'acp' })}
          loading={ensure.isPending}
          sx={{ mt: 1.5 }}
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
        <Button variant="outlined" size="small" onClick={() => void refetch()} sx={{ mt: 1.5 }}>
          重试
        </Button>
      </FullCenter>
    );
  }
  return <StatusBranch issueId={issueId} snapshot={snapshot} onRestart={() => ensure.mutate({ issueId, pickedLaunchMode: 'acp' })} ensurePending={ensure.isPending} />;
}

/** 按快照 status 分支：starting 启动中 / failed+terminated 原因与重启 / idle+ready 会话视图。 */
function StatusBranch({ issueId, snapshot, onRestart, ensurePending }: {
  issueId: string;
  snapshot: AcpViewSnapshot;
  onRestart: () => void;
  ensurePending: boolean;
}) {
  switch (snapshot.status) {
    case 'starting':
      return (
        <FullCenter>
          <CircularProgress size={28} />
          <Typography variant="body2" color="text.secondary" sx={{ mt: 1.5 }}>
            正在启动 ACP 会话{snapshot.agentCode ? `（${snapshot.agentCode}）` : ''}…
          </Typography>
        </FullCenter>
      );
    case 'failed':
    case 'terminated':
      return (
        <FullCenter>
          <Typography variant="body2" color="error">
            {snapshot.status === 'failed' ? '会话启动失败' : '会话已结束'}{snapshot.error ? `：${snapshot.error}` : ''}
          </Typography>
          <Button variant="outlined" size="small" onClick={onRestart} loading={ensurePending} sx={{ mt: 1.5 }}>
            重新启动
          </Button>
        </FullCenter>
      );
    case 'idle':
    case 'ready':
      return (
        <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
          <MessageList entries={snapshot.entries} />
          {snapshot.pendings.length > 0 && (
            // 固定挂起区（MessageList 与输入区之间）：pending 逐项平铺卡片，全部应答
            // 后（pendings 清空）整区消失。
            <Box sx={{ flexShrink: 0, p: 1, display: 'flex', flexDirection: 'column', gap: 1, borderTop: 1, borderColor: 'divider' }}>
              {snapshot.pendings.map(pending => (
                <PendingCard key={pending.pendingId} issueId={issueId} pending={pending} />
              ))}
            </Box>
          )}
          <PromptComposer issueId={issueId} turnActive={snapshot.turnActive} />
        </Box>
      );
  }
}

/** 全区居中外壳（启动/失败/加载态共用）。 */
function FullCenter({ children }: { children: React.ReactNode }) {
  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', p: 2 }}>
      {children}
    </Box>
  );
}
