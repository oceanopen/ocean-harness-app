// 贴底判定边界覆盖：贴底/容差内/恰在阈值/远离。
import { describe, expect, it } from 'vitest';
import { isStickToBottom } from './stickToBottom';

const measure = (scrollHeight: number, scrollTop: number, clientHeight: number) => ({ scrollHeight, scrollTop, clientHeight });

describe('isStickToBottom', () => {
  it('完全贴底（scrollTop = scrollHeight - clientHeight）为贴底', () => {
    expect(isStickToBottom(measure(1000, 852, 148))).toBe(true);
  });

  it('距底不足 48px 为贴底', () => {
    expect(isStickToBottom(measure(1000, 810, 148))).toBe(true);
  });

  it('距底恰好 48px 不算贴底（严格小于）', () => {
    expect(isStickToBottom(measure(1000, 804, 148))).toBe(false);
  });

  it('远离底部不算贴底', () => {
    expect(isStickToBottom(measure(5000, 0, 500))).toBe(false);
  });
});
