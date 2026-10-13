import { describe, expect, it } from 'vitest';
import { prettyPayloadText, tcPayloadMaxChars, truncatePayloadText } from './payloadText';

describe('prettyPayloadText', () => {
  it('字符串值直显（不加引号）', () => {
    expect(prettyPayloadText('第一行\n第二行')).toBe('第一行\n第二行');
  });

  it('对象 pretty 化两格缩进', () => {
    expect(prettyPayloadText({ a: 1 })).toBe('{\n  "a": 1\n}');
  });

  it('循环引用与 stringify 返回 undefined 时兜底 String', () => {
    const circular: Record<string, unknown> = {};
    circular.self = circular;
    expect(prettyPayloadText(circular)).toBe('[object Object]');
    expect(prettyPayloadText(undefined)).toBe('undefined');
  });
});

describe('truncatePayloadText', () => {
  it('未超限原样返回且不标记截断', () => {
    const text = 'a'.repeat(tcPayloadMaxChars);
    expect(truncatePayloadText(text)).toEqual({ text, truncated: false });
  });

  it('超限截断到上限码元并标记 truncated', () => {
    const r = truncatePayloadText('x'.repeat(tcPayloadMaxChars + 100));
    expect(r.truncated).toBe(true);
    expect(r.text.length).toBe(tcPayloadMaxChars);
  });

  it('截断点劈开代理对时回退一位保持成对', () => {
    // '😀' 占 2 个码元，让截断点恰好落在其高代理上。
    const r = truncatePayloadText(`${'a'.repeat(tcPayloadMaxChars - 1)}😀bc`);
    expect(r.truncated).toBe(true);
    expect(r.text).toBe('a'.repeat(tcPayloadMaxChars - 1));
  });
});
