import type { BrowserLiveEvent, BrowserLiveFrameMeta } from '@src/services';
import { serverUrl } from '@src/services/http';
import { useCallback, useEffect, useRef, useState } from 'react';

/**
 * 视口帧（keep-latest 视频流语义）：jpeg base64 + screencast metadata——仅最新帧有意义，
 * 慢消费跳帧不断连；落 ref 不进 TanStack Query 缓存（高频帧不配缓存写放大），
 * 绘制由 useFramePaint 的 rAF 消费循环驱动（与帧到达事件解耦）。
 */
export interface LiveViewportFrame {
  data: string;
  metadata?: BrowserLiveFrameMeta;
}

/**
 * 帧流相位：connecting 订阅中未首帧 → live 推帧中；healing 流内 error（后端自愈，
 * 连接保持，新帧到达即回 live）；failed 终态（订阅被服务端拒绝/连接永久失败，
 * EventSource 已放弃，待手动重试）；closed 终态（页面关闭/管理面强拆），不重订。
 */
export type LiveStreamPhase = 'connecting' | 'live' | 'healing' | 'failed' | 'closed';

export interface BrowserFrameStream {
  /** keep-latest 帧箱（终态保留末帧不清屏）。 */
  frameRef: React.RefObject<LiveViewportFrame | null>;
  /** 是否已有帧（首帧置 true、重订清帧置 false）：占位态切换驱动；同值 set 不触发重渲染。 */
  hasFrame: boolean;
  phase: LiveStreamPhase;
  /** 最近一次 target 回显（实际投送目标 url/title，多开同址消歧）。 */
  target: { url: string; title: string } | null;
  /** healing 呈现文案（流内 error 事件 message）。 */
  healingMessage: string | null;
  /** 终态原因（closed 事件 reason；非终态为 null）。 */
  closedReason: string | null;
  /** failed 终态原因（EventSource error 事件不暴露 HTTP 状态码，仅通用文案/异常 message）。 */
  failedMessage: string | null;
  /** failed 终态手动重试（attempt 自增触发重订）。 */
  retry: () => void;
}

/**
 * useBrowserFrameStream 订阅单页面实时画面 SSE（/api/browserSession/stream，per (profile, targetId)：目标变更即重建；
 * targetId 空串不订阅，等 SSE 投影首帧——首帧即终值）。事件分派见 BrowserLiveEvent 契约：瞬态断线交 EventSource 自动重连，
 * reject/永久失败置 failed 待手动重试，仅 closed 主动停订；enabled 为闸门链，throttleMs 为帧写侧最小间隔（0/缺省不节流）。
 */
export function useBrowserFrameStream(profile: string, targetId: string, enabled: boolean, throttleMs = 0): BrowserFrameStream {
  const frameRef = useRef<LiveViewportFrame | null>(null);
  const [hasFrame, setHasFrame] = useState(false);
  const [phase, setPhase] = useState<LiveStreamPhase>('connecting');
  const [target, setTarget] = useState<{ url: string; title: string } | null>(null);
  const [healingMessage, setHealingMessage] = useState<string | null>(null);
  const [closedReason, setClosedReason] = useState<string | null>(null);
  const [failedMessage, setFailedMessage] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const retry = useCallback(() => setAttempt(n => n + 1), []);

  useEffect(() => {
    if (!enabled || targetId === '') {
      return;
    }
    let es: EventSource | null = null;
    let disposed = false;

    const close = () => {
      es?.close();
      es = null;
    };

    /**
     * 命名 handler + 函数声明（互引，范式同 useBrowserSessionEvents）；
     * cleanup 显式 removeEventListener 防御旧实例引用残留。
     */
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
          setHasFrame(true); // 同值 set 跳过重渲染：仅首帧真正触发（占位态切换）
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
        case 'reject':
          // 订阅拒绝终态：载荷为后端真实原因（排障可见），EventSource 不再重连。
          setPhase('failed');
          setFailedMessage(evt.message ?? '订阅被服务端拒绝');
          close();
          break;
        case 'closed':
          setPhase('closed');
          setClosedReason(evt.reason ?? '');
          close(); // 终态停订：EventSource 不再自动重连，重开闸门才重订
          break;
      }
    }

    /**
     * 连接层 error：readyState CLOSED = EventSource 已放弃（非 200 订阅拒绝等永久失败，
     * 规范不自动重连）→ failed 终态待手动重试；CONNECTING = 瞬态断线自动重连中（后端自愈期间），不置终态。
     */
    function onError() {
      if (es && es.readyState === EventSource.CLOSED) {
        setPhase('failed');
        setFailedMessage('订阅被服务端拒绝或连接中断');
        close();
      }
    }

    async function connect() {
      let url: string;
      const query: Record<string, string> = { profile, targetId };
      if (throttleMs > 0) {
        query.throttleMs = String(throttleMs);
      }
      try {
        url = await serverUrl('/api/browserSession/stream', query);
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
      // （重）订阅激活前清帧复位：新页码/重开闸门/手动重试不允许旧页残影与上一轮终态残留。
      frameRef.current = null;
      setHasFrame(false);
      setPhase('connecting');
      setTarget(null);
      setHealingMessage(null);
      setClosedReason(null);
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
  }, [profile, targetId, enabled, throttleMs, attempt]);

  return { frameRef, hasFrame, phase, target, healingMessage, closedReason, failedMessage, retry };
}
