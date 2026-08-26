// chatStreaming 单测（T3.1）：working 门槛 / 被包含或更短隐藏 / 领先显示。

import type { TranscriptMessage } from '@src/shared/bindings';
import { describe, expect, it } from 'vitest';
import { CHAT_STREAMING_ID, deriveStreamingText, streamingMessage } from './chatStreaming';

function assistant(id: string, text: string): TranscriptMessage {
  return {
    id,
    role: 'Assistant',
    blocks: [{ type: 'text', text }],
    timestamp: 1,
  };
}

describe('deriveStreamingText', () => {
  it('非 working 恒不显示（残留 preview 不外漏）', () => {
    expect(deriveStreamingText({ messages: [], previewText: '生成中文本', working: false })).toBeNull();
  });

  it('preview 为空不显示', () => {
    expect(deriveStreamingText({ messages: [], previewText: null, working: true })).toBeNull();
    expect(deriveStreamingText({ messages: [], previewText: '   ', working: true })).toBeNull();
  });

  it('无 assistant 历史：working + 非空预览即显示', () => {
    expect(deriveStreamingText({ messages: [], previewText: 'Hel', working: true })).toBe('Hel');
  });

  it('预览被最后 assistant 文本包含 → 隐藏（真实 turn 已落地，防双份）', () => {
    const messages = [assistant('a1', 'Hello 世界 完整回复')];
    expect(deriveStreamingText({ messages, previewText: 'Hello 世界', working: true })).toBeNull();
    expect(deriveStreamingText({ messages, previewText: 'Hello 世界 完整回复', working: true })).toBeNull();
  });

  it('预览更长（领先）→ 显示', () => {
    const messages = [assistant('a1', 'Hel')];
    expect(deriveStreamingText({ messages, previewText: 'Hello 世界', working: true })).toBe('Hello 世界');
  });

  it('末条非 assistant（user turn / 乐观 echo 在尾）→ 空基准，preview 即显示', () => {
    // working 起步时末条通常是 user（回复未落地）：从首个 delta 起显示，不与
    // 上一回合 assistant 比较（回扫会把「短于上一条回复」的流式整回合抑制）。
    const messages: TranscriptMessage[] = [
      assistant('a1', '很长很长的第一轮回复'),
      { id: 'u1', role: 'User', blocks: [{ type: 'text', text: '继续' }], timestamp: 2 },
    ];
    expect(deriveStreamingText({ messages, previewText: '短', working: true })).toBe('短');
    expect(deriveStreamingText({ messages, previewText: '很长很长的第一轮回复', working: true })).toBe('很长很长的第一轮回复');
  });
});

describe('streamingMessage', () => {
  it('合成 assistant 消息形态', () => {
    const m = streamingMessage('流式文本');
    expect(m.id).toBe(CHAT_STREAMING_ID);
    expect(m.role).toBe('Assistant');
    expect(m.blocks).toEqual([{ type: 'text', text: '流式文本' }]);
  });
});
