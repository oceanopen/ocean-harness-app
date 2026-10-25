import type { AcpFrame, AcpViewSnapshot } from '@src/services';
import { normalizeAcpViewSnapshot } from '@src/services';
import { serverUrl } from '@src/services/http';
import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useState } from 'react';
import { acpSessionKeys } from './keys';
import { applyFrame, isFrameSequenceBroken } from './reducer';

/**
 * 订阅相位：connecting 订阅中未首帧 → live 推帧中；failed 终态（订阅被服务端拒绝/
 * 连接永久失败，EventSource 已放弃，待手动重试）。
 */
export type AcpEventsPhase = 'connecting' | 'live' | 'failed';

export interface AcpEventsStream {
  phase: AcpEventsPhase;
  /** failed 终态原因（EventSource error 事件不暴露 HTTP 状态码，仅通用文案/异常 message）。 */
  failedMessage: string | null;
  /** failed 终态手动重试（attempt 自增触发重订）。 */
  retry: () => void;
}

/**
 * useAcpSessionEvents 订阅 issue 的 ACP 会话 SSE 事件流：建连首帧即全量快照写入 query 缓存（acpSessionKeys.view），后续增量帧经 applyFrame 归约更新同一缓存。enabled 为订阅闸门（isAcpSessionSubscribable：快照可订阅状态才建连，治首开订阅先于 ensure 的 404 竞态）——terminated 帧落缓存即闭闸断开，取代旧「订阅保持」契约；「重新启动」受理回包把快照迁回 starting，闸门重开自动重订（首帧快照即完成回放）。
 * 断线语义（后端契约 hub.go）：连接层瞬态错误交 EventSource 自动重连（重连即新一次订阅，首帧为新快照，作为 seq 重同步点）；EventSource 永久失败（readyState CLOSED，含服务端 404/409 订阅拒绝——非 200 规范不自动重连）置 failed 终态待手动重试；增量帧 seq 断档（慢订户被服务端断开等）主动 close 重建，无补帧逻辑。
 * 生命周期与闸门一致：闸门关闭/卸载即断开，重开重建连。
 */
export function useAcpSessionEvents(conversationId: string, enabled: boolean): AcpEventsStream {
  const queryClient = useQueryClient();
  const [phase, setPhase] = useState<AcpEventsPhase>('connecting');
  const [failedMessage, setFailedMessage] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const retry = useCallback(() => setAttempt(n => n + 1), []);

  useEffect(() => {
    if (!enabled) {
      return;
    }
    let es: EventSource | null = null;
    let disposed = false;
    let lastSeq: number | null = null;

    const close = () => {
      es?.close();
      es = null;
    };

    const applyAndSet = (rawFrame: AcpFrame) => {
      /**
       * snapshot 帧载荷入站归一化（旧 sidecar 零条目快照 entries 可能为 null）：分支前
       * 先归一，无 prev 首落缓存与 applyFrame 整体替换两条路径同守。
       */
      const frame: AcpFrame = rawFrame.type === 'snapshot' && rawFrame.snapshot
        ? { ...rawFrame, snapshot: normalizeAcpViewSnapshot(rawFrame.snapshot) }
        : rawFrame;
      /**
       * 缓存尚未建立（getInfo 在飞）时：快照帧直接落缓存；增量帧 updater 返回
       * undefined 即不写（TanStack Query 语义），等快照/查询先到。
       */
      queryClient.setQueryData<AcpViewSnapshot>(acpSessionKeys.view(conversationId), prev =>
        prev ? applyFrame(prev, frame) : frame.snapshot);
    };

    /**
     * 命名 handler + 函数声明（互引：gap 重建回调 connect，connect 注册 onMessage；
     * 声明提升规避定义顺序约束）。cleanup 显式 removeEventListener——close() 本身已
     * 移除全部监听，显式解除是防御 EventSource 实例被替换后旧引用仍持监听的边缘态。
     */
    function onMessage(ev: MessageEvent) {
      let frame: AcpFrame;
      try {
        frame = JSON.parse(ev.data) as AcpFrame;
      } catch {
        console.warn('[acpSession] 无法解析的 SSE 帧:', ev.data);
        return;
      }
      if (frame.type === 'snapshot') {
        lastSeq = frame.seq; // 快照即重同步点（建连/重连首帧）
        applyAndSet(frame);
        setPhase('live');
        return;
      }
      if (isFrameSequenceBroken(lastSeq, frame)) {
        // 断档重建：connect 复位相位并取全新快照（首帧即重同步点）。
        close();
        lastSeq = null;
        void connect();
        return;
      }
      lastSeq = frame.seq;
      applyAndSet(frame);
      setPhase('live'); // 同值 set 由 React 跳过重渲染
    }

    /**
     * 连接层 error：readyState CLOSED = EventSource 已放弃（非 200 订阅拒绝等永久
     * 失败，规范不自动重连）→ failed 终态待手动重试；CONNECTING = 瞬态断线自动重连
     * 中（重连首帧即新快照），不置终态。
     */
    function onError() {
      if (es && es.readyState === EventSource.CLOSED) {
        setPhase('failed');
        setFailedMessage('订阅被服务端拒绝或连接中断');
        close();
      }
    }

    async function connect() {
      // 重订（闸门重开/手动重试）复位相位：清除上一轮终态残留，回到订阅中。
      setPhase('connecting');
      let url: string;
      try {
        url = await serverUrl('/api/acpSession/events', { conversationId });
      } catch (e) {
        // go-server 未运行：无自动恢复方，直接 failed 终态待手动重试。
        if (disposed) {
          return;
        }
        setPhase('failed');
        setFailedMessage(e instanceof Error ? e.message : '服务地址获取失败');
        return;
      }
      if (disposed) {
        return;
      }
      setFailedMessage(null);
      es = new EventSource(url);
      es.addEventListener('message', onMessage);
      es.addEventListener('error', onError);
    }

    void connect();
    return () => {
      disposed = true;
      es?.removeEventListener('message', onMessage);
      es?.removeEventListener('error', onError);
      close();
    };
  }, [conversationId, enabled, attempt, queryClient]);

  return { phase, failedMessage, retry };
}
