// chat 乐观 echo pending 发送（T3.1，简化版 orca native-chat-pending.ts）：
// 发送时立即以合成 user 气泡回显，transcript 真实 user turn 落地后剪除。
//
// 与 orca 的差异（据源码裁剪，队列串行化后场景不成立）：
//   - 不做 glue 匹配（丢回车正文粘行场景——串行队列防粘行，见 chatSendQueue）。
//   - 匹配规则保留三要素：归一化内容 + 边界（发送时最后一条消息 id 之后的
//     消息才可绑定，防重复文本绑旧 turn）+ 出现次数（同文本连发各绑各的）。
//
// 两层剪除（对齐 orca 双层语义，防「claude TUI 排队未提交」的 echo 被误清）：
//   - 渲染层（pendingSendsAsMessages，matching 计数）：同文本 user 行落地即
//     隐藏 echo——视觉无双份，不等回复。
//   - 缓存层（prunePendingSends，advanced 计数）：user 行**其后出现非 user
//     消息**（回复开始落地）才剪除缓存条目——排队窗口内（回车已写、claude
//     尚未消费）不误清；被吞/取消的 echo 由发送队列 cancel 显式清除
//     （clearLastPendingSendByText，替代 orca 的 cancel↔echo 事件耦合）。
//
// 模块级缓存 keyed by sessionId：chat overlay 开关会卸载重挂 NativeChatView，
// 组件态会丢在途 echo；模块态跨挂载存续（orca 同款取舍）。

import type { TranscriptMessage } from '@src/shared/bindings';
import { messageText } from './chatStreaming';

/** 一条乐观 echo（未确认的发送）。 */
export interface PendingSend {
  /**
   * 前端铸造 id（唯一 per 发送），合成气泡 key 用 `pending:<id>`。
   */
  id: string;
  /** 用户提交的原文。 */
  text: string;
  /** 发送时刻（epoch ms），合成气泡 timestamp（排序靠尾）。 */
  sentAt: number;
  /**
   * 边界：发送时最后一条 transcript 消息 id；null = 发送时无消息（空会话）。
   * 匹配只认边界之后的消息，防重复文本绑到旧 turn。
   */
  afterMessageId: string | null;
  /** 同文本（同边界）第几次发送（1 起）——连发同文本各绑各的 turn。 */
  occurrence: number;
}

// 上限防御：异常场景（prune 永不触发）缓存不无限增长。
const PENDING_LIMIT = 8;
const pendingCache = new Map<string, PendingSend[]>();
let pendingCounter = 0;

/**
 * 归一化匹配键：trim + 折叠连续空白（transcript 落地文本与发送原文的
 * 差异容忍——bracketed paste 换行归一、TUI 首尾空白）。
 */
function contentKey(text: string): string {
  return text.trim().replace(/\s+/g, ' ');
}

/**
 * 边界之后的消息集（匹配/剪除共用的窗口）。边界找到 → 其后切片；边界缺失
 * 或 null（bounded read 裁剪、快照未落地即发送）→ 退化全量但按发送时间戳
 * 过滤（对齐 orca 回落）：早于发送时刻的行不可代表本次发送，防历史重复
 * 文本误剪；无时间戳的行包含（排除会让 echo 永不可匹配、滞留列表尾）。
 */
function messagesAfterBoundary(
  messages: readonly TranscriptMessage[],
  pending: PendingSend,
): readonly TranscriptMessage[] {
  const boundary = messages.findIndex(m => m.id === pending.afterMessageId);
  if (boundary !== -1) {
    return messages.slice(boundary + 1);
  }
  return messages.filter(m => m.timestamp == null || m.timestamp >= pending.sentAt);
}

/**
 * 边界后「已定局」的同文本 user 行数。onlyAdvanced：只数其后出现非 user
 * 消息（回复开始落地）的行——orca advanced 语义，缓存层剪除判据；false 时
 * 连「已落地未回复」的行一起数（matching），渲染层即时隐藏判据。
 */
function userRowCountsAfter(
  messages: readonly TranscriptMessage[],
  pending: PendingSend,
  onlyAdvanced: boolean,
): number {
  const target = contentKey(pending.text);
  let waiting = 0; // 同文本 user 行，尚未等到非 user 转折
  let advanced = 0; // 已等到（回复开始落地）
  for (const m of messagesAfterBoundary(messages, pending)) {
    if (m.role === 'User') {
      if (contentKey(messageText(m)) === target) {
        waiting += 1;
      }
      continue;
    }
    advanced += waiting;
    waiting = 0;
  }
  return onlyAdvanced ? advanced : advanced + waiting;
}

function writeCache(sessionId: string, pending: PendingSend[]): void {
  if (pending.length === 0) {
    pendingCache.delete(sessionId);
  } else {
    pendingCache.set(sessionId, pending.slice(-PENDING_LIMIT));
  }
}

/** 发送时调用：登记一条 echo（同文本连发 occurrence 递增）。 */
export function appendPendingSend(sessionId: string, text: string, sentAt: number, afterMessageId: string | null): PendingSend {
  pendingCounter += 1;
  const existing = pendingCache.get(sessionId) ?? [];
  const occurrence
    = existing.filter(p => p.afterMessageId === afterMessageId && contentKey(p.text) === contentKey(text)).length + 1;
  const entry: PendingSend = {
    id: `${sentAt}-${pendingCounter}`,
    text,
    sentAt,
    afterMessageId,
    occurrence,
  };
  writeCache(sessionId, [...existing, entry]);
  return entry;
}

/**
 * transcript 消息更新后调用：剪除缓存中「已定局」（advanced）的 echo。
 * 保留返回原数组引用（无变化时），调用方 setState 幂等。
 */
export function prunePendingSends(
  sessionId: string,
  messages: readonly TranscriptMessage[],
): PendingSend[] {
  const pending = pendingCache.get(sessionId);
  if (!pending || pending.length === 0) {
    return pending ?? [];
  }
  const next = pending.filter(p => userRowCountsAfter(messages, p, true) < p.occurrence);
  if (next.length === pending.length) {
    return pending;
  }
  writeCache(sessionId, next);
  return next;
}

/** 当前 echo 列表（幂等快照）。 */
export function readPendingSends(sessionId: string): PendingSend[] {
  return pendingCache.get(sessionId) ?? [];
}

/**
 * 显式清除最近一条同文本 echo（发送队列 cancel 未提交序列时调用——正文被
 * Ctrl+U 放弃，真实 turn 不会落地，须主动清避免永驻显示）。
 */
export function clearLastPendingSendByText(sessionId: string, text: string): void {
  const pending = pendingCache.get(sessionId);
  if (!pending || pending.length === 0) {
    return;
  }
  const key = contentKey(text);
  for (let i = pending.length - 1; i >= 0; i -= 1) {
    if (contentKey(pending[i].text) === key) {
      writeCache(sessionId, pending.filter((_, idx) => idx !== i));
      return;
    }
  }
}

/**
 * echo → 合成 user 消息（与 TranscriptMessage 同形）。渲染层 matching 隐藏：
 * 真实 user 行已落地（回复未落地）的 echo 即刻不出现在列表——视觉无双份；
 * 缓存条目仍保留到回复落地（prunePendingSends advanced 判据）。
 */
export function pendingSendsAsMessages(
  pending: readonly PendingSend[],
  messages: readonly TranscriptMessage[],
): TranscriptMessage[] {
  return pending
    .filter(p => userRowCountsAfter(messages, p, false) < p.occurrence)
    .map(p => ({
      id: `pending:${p.id}`,
      role: 'User' as const,
      blocks: [{ type: 'text' as const, text: p.text }],
      timestamp: p.sentAt,
    }));
}
