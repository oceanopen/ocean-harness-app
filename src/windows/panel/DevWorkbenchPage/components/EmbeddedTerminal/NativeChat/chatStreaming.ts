// chat 流式气泡派生（T3.1，对齐 orca native-chat-streaming.ts）：生成中把
// runtime previewText（MessageDisplay delta 拼接，ingest T3.1）显示为合成
// assistant 气泡，真实 turn 落 transcript 后自然消失（预览被包含/不领先时隐藏，
// 不双份不闪烁）。纯函数，与 orca 双端共享 derive 同源规则。

import type { TranscriptMessage } from '@src/shared/bindings';

/**
 * 合成流式气泡稳定 id（跨 tick 恒定，列表 key 稳定；真实 turn 落地后由
 * derive 隐藏自然替换）。
 */
export const CHAT_STREAMING_ID = 'streaming';

/**
 * 消息全部 text block 的拼接文本（不 trim——调用方决定归一方式：
 * 内容匹配用 contentKey 折叠空白，基准比较用 trim）。
 */
export function messageText(message: TranscriptMessage): string {
  return message.blocks
    .filter((b): b is Extract<TranscriptMessage['blocks'][number], { type: 'text' }> => b.type === 'text')
    .map(b => b.text)
    .join('');
}

/**
 * 末条消息若为 assistant 的拼接文本（trim）；末条非 assistant（user turn /
 * 乐观 echo 在尾）为空串。对齐 orca「只看末条」：working 起步时末条通常是
 * user（真实 turn 未落地），空基准让 preview 从首个 delta 即显示——回扫上一
 * 回合 assistant 会把「短于上一条回复」的流式整个回合抑制。
 */
function lastAssistantText(messages: readonly TranscriptMessage[]): string {
  const last = messages[messages.length - 1];
  if (last == null || last.role !== 'Assistant') {
    return '';
  }
  return messageText(last).trim();
}

/**
 * 决定流式气泡显示文本；null = 不显示。
 * working 门槛：非 working（含残留 preview）恒不显示；预览仅在其**领先**
 * transcript 时显示——不被最后一条 assistant 文本包含且更长；真实 turn
 * 落地（含相同或更多文本）后自然隐藏。
 */
export function deriveStreamingText(args: {
  messages: readonly TranscriptMessage[];
  previewText: string | null | undefined;
  working: boolean;
}): string | null {
  const { messages, previewText, working } = args;
  if (!working) {
    return null;
  }
  const text = previewText?.trim();
  if (!text) {
    return null;
  }
  const lastText = lastAssistantText(messages);
  if (lastText.includes(text) || text.length <= lastText.length) {
    return null;
  }
  return text;
}

/**
 * 流式文本 → 合成 assistant 消息（与 TranscriptMessage 同形）。
 */
export function streamingMessage(text: string): TranscriptMessage {
  return {
    id: CHAT_STREAMING_ID,
    role: 'Assistant',
    blocks: [{ type: 'text', text }],
    timestamp: null,
  };
}
