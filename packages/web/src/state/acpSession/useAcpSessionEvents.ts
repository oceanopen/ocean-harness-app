import type { AcpFrame, AcpViewSnapshot } from '@src/services';
import { serverUrl } from '@src/services/http';
import { useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import { acpSessionKeys } from './keys';
import { applyFrame, isFrameSequenceBroken } from './reducer';

/**
 * useAcpSessionEvents 订阅 issue 的 ACP 会话 SSE 事件流：建连首帧即全量快照写入 query
 * 缓存（acpSessionKeys.view），后续增量帧经 applyFrame 归约更新同一缓存。生命周期与
 * 挂载组件一致——卸载即断开，重挂载重建连、首帧快照即完成回放（对齐 PTY scrollback
 * 重挂载语义，后端会话常驻）。
 *
 * 断线语义（后端契约 hub.go）：连接层错误交 EventSource 自动重连（重连即新一次订阅，
 * 首帧为新快照，作为 seq 重同步点）；增量帧 seq 断档（慢订户被服务端断开等）主动
 * close 重建，无补帧逻辑；terminated 为终态帧但订阅保持——后续「重新启动」重建会话的
 * 帧继续到达同一订阅，无需重建连接。
 */
export function useAcpSessionEvents(issueId: string): void {
  const queryClient = useQueryClient();
  useEffect(() => {
    let es: EventSource | null = null;
    let disposed = false;
    let lastSeq: number | null = null;

    const close = () => {
      es?.close();
      es = null;
    };

    const applyAndSet = (frame: AcpFrame) => {
      // 缓存尚未建立（getInfo 在飞）时：快照帧直接落缓存；增量帧 updater 返回
      // undefined 即不写（TanStack Query 语义），等快照/查询先到。
      queryClient.setQueryData<AcpViewSnapshot>(acpSessionKeys.view(issueId), prev =>
        prev ? applyFrame(prev, frame) : frame.snapshot);
    };

    // 命名 handler + 函数声明（互引：gap 重建回调 connect，connect 注册 onMessage；
    // 声明提升规避定义顺序约束）。cleanup 显式 removeEventListener——close() 本身已
    // 移除全部监听，显式解除是防御 EventSource 实例被替换后旧引用仍持监听的边缘态。
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
        return;
      }
      if (isFrameSequenceBroken(lastSeq, frame)) {
        close();
        lastSeq = null;
        void connect();
        return;
      }
      lastSeq = frame.seq;
      applyAndSet(frame);
    }

    async function connect() {
      let url: string;
      try {
        url = await serverUrl('/api/acpSession/events', { issueId });
      } catch {
        // go-server 未运行：不建连（getInfo 查询端同样报错，由其错误态呈现）。
        return;
      }
      if (disposed) {
        return;
      }
      es = new EventSource(url);
      es.addEventListener('message', onMessage);
    }

    void connect();
    return () => {
      disposed = true;
      es?.removeEventListener('message', onMessage);
      close();
    };
  }, [issueId, queryClient]);
}
