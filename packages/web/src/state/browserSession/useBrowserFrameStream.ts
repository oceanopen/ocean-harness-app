import type { BrowserLiveEvent, BrowserLiveFrameMeta } from '@src/services';
import { serverUrl } from '@src/services/http';
import { useEffect, useRef, useState } from 'react';

/**
 * 视口帧（keep-latest 视频流语义）：jpeg base64 + screencast metadata——仅最新帧有意义，
 * 慢消费跳帧不断连；落 ref 不进 TanStack Query 缓存（高频帧不配缓存写放大），tick 驱动
 * 重绘。
 */
export interface LiveViewportFrame {
  data: string;
  metadata?: BrowserLiveFrameMeta;
}

/**
 * 帧流相位：connecting 订阅中未首帧 → live 推帧中；healing 流内 error（后端自愈，
 * 连接保持，新帧到达即回 live）；closed 终态（页面关闭/管理面强拆），不重订。
 */
export type LiveStreamPhase = 'connecting' | 'live' | 'healing' | 'closed';

export interface BrowserFrameStream {
  /** keep-latest 帧箱（终态保留末帧不清屏）。 */
  frameRef: React.RefObject<LiveViewportFrame | null>;
  /** 帧到达序号：每帧 +1（含重订清帧），视口以它驱动重绘 effect。 */
  tick: number;
  phase: LiveStreamPhase;
  /** 最近一次 target 回显（实际投送目标 url/title，多开同址消歧）。 */
  target: { url: string; title: string } | null;
  /** healing 呈现文案（流内 error 事件 message）。 */
  healingMessage: string | null;
  /** 终态原因（closed 事件 reason；非终态为 null）。 */
  closedReason: string | null;
}

/**
 * useBrowserFrameStream 订阅单页面实时画面 SSE（/api/browserSession/stream，per
 * (profile, pageId)：页码变更即重建，旧流经 done 感知重订）。事件四型分派见
 * BrowserLiveEvent 契约；连接层断开交 EventSource 自动重连（后端自愈中），仅 closed
 * 终态主动停订。生命周期：enabled 为闸门链（expanded && panelVisible && ready &&
 * !restoreInFlight）——收起/恢复编排即断流，重开重订（重订即清帧回到占位态，帧箱
 * 不跨订阅保留旧页面残影）。
 */
export function useBrowserFrameStream(profile: string, pageId: number, enabled: boolean): BrowserFrameStream {
  const frameRef = useRef<LiveViewportFrame | null>(null);
  const [tick, setTick] = useState(0);
  const [phase, setPhase] = useState<LiveStreamPhase>('connecting');
  const [target, setTarget] = useState<{ url: string; title: string } | null>(null);
  const [healingMessage, setHealingMessage] = useState<string | null>(null);
  const [closedReason, setClosedReason] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled) {
      return;
    }
    let es: EventSource | null = null;
    let disposed = false;

    const close = () => {
      es?.close();
      es = null;
    };

    // 命名 handler + 函数声明（互引，范式同 useBrowserSessionEvents）；cleanup 显式
    // removeEventListener 防御旧实例引用残留。
    function onMessage(ev: MessageEvent) {
      let evt: BrowserLiveEvent;
      try {
        evt = JSON.parse(ev.data) as BrowserLiveEvent;
      } catch {
        console.warn('[browserSession] 无法解析的帧流事件:', ev.data);
        return;
      }
      switch (evt.type) {
        case 'frame':
          frameRef.current = { data: evt.data ?? '', metadata: evt.metadata };
          setTick(t => t + 1);
          setPhase('live');
          setHealingMessage(null);
          break;
        case 'target':
          // target(closed:true) 是页面关闭通知，后端必随补发 closed 终态——此处不置终态。
          if (!evt.closed) {
            setTarget({ url: evt.url ?? '', title: evt.title ?? '' });
          }
          break;
        case 'error':
          setPhase('healing');
          setHealingMessage(evt.message ?? '实时画面连接中断');
          break;
        case 'closed':
          setPhase('closed');
          setClosedReason(evt.reason ?? '');
          close(); // 终态停订：EventSource 不再自动重连，重开闸门才重订
          break;
      }
    }

    async function connect() {
      let url: string;
      try {
        url = await serverUrl('/api/browserSession/stream', { profile, pageId: String(pageId) });
      } catch {
        // go-server 未运行：不建连（会话投影读端同报错，由其错误态呈现）。
        return;
      }
      if (disposed) {
        return;
      }
      // （重）订阅激活前清帧复位：新页码/重开闸门不允许旧页残影与上一轮终态残留。
      frameRef.current = null;
      setTick(t => t + 1);
      setPhase('connecting');
      setTarget(null);
      setHealingMessage(null);
      setClosedReason(null);
      es = new EventSource(url);
      es.addEventListener('message', onMessage);
    }

    void connect();
    return () => {
      disposed = true;
      es?.removeEventListener('message', onMessage);
      close();
    };
  }, [profile, pageId, enabled]);

  return { frameRef, tick, phase, target, healingMessage, closedReason };
}
