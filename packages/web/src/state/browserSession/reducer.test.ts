import type { BrowserFrame, BrowserSessionSnapshot, BrowserSessionView } from '@src/services';
import { describe, expect, it } from 'vitest';
import { applyFrame, isFrameSequenceBroken, upsertSessions } from './reducer';

/**
 * applyFrame 归约单测：两帧型语义（snapshot 整体替换 / sessions 覆写式 upsert）+ 不
 * mutate 入参 + seq 断档判定（镜像 acpSession/reducer.test.ts 范式）。
 */

let seqSource = 0;

// frameOf 构造自增 seq 的帧（seq 单调性由后端 hub 保证，测试内同样递增模拟合法流）。
function frameOf(partial: Omit<BrowserFrame, 'seq'>): BrowserFrame {
  seqSource += 1;
  return { seq: seqSource, ...partial };
}

function rowOf(profile: string, overrides: Partial<BrowserSessionView> = {}): BrowserSessionView {
  return { profile, status: 'ready', headless: false, ...overrides };
}

function baseSnapshot(): BrowserSessionSnapshot {
  return { sessions: [rowOf('a'), rowOf('b')] };
}

describe('applyFrame', () => {
  it('snapshot 帧整体替换', () => {
    const next: BrowserSessionSnapshot = { sessions: [rowOf('x', { status: 'starting' })] };
    const out = applyFrame(baseSnapshot(), frameOf({ type: 'snapshot', snapshot: next }));
    expect(out).toBe(next);
  });

  it('sessions 帧新 profile 追加尾部', () => {
    const out = applyFrame(baseSnapshot(), frameOf({ type: 'sessions', sessions: [rowOf('c', { status: 'starting' })] }));
    expect(out.sessions.map(row => row.profile)).toEqual(['a', 'b', 'c']);
  });

  it('sessions 帧已存在 profile 原位整行覆写，未提及行保持', () => {
    const out = applyFrame(baseSnapshot(), frameOf({ type: 'sessions', sessions: [rowOf('a', { status: 'failed', error: '拉起失败' })] }));
    expect(out.sessions).toHaveLength(2);
    expect(out.sessions[0].profile).toBe('a');
    expect(out.sessions[0].status).toBe('failed');
    expect(out.sessions[0].error).toBe('拉起失败');
    expect(out.sessions[1].profile).toBe('b');
  });

  it('sessions 帧载荷缺失原样返回', () => {
    const base = baseSnapshot();
    expect(applyFrame(base, frameOf({ type: 'sessions' }))).toBe(base);
  });

  it('归约不 mutate 入参快照', () => {
    const base = baseSnapshot();
    const frozen = Object.freeze(base);
    applyFrame(frozen, frameOf({ type: 'sessions', sessions: [rowOf('a', { status: 'idle' })] }));
    expect(base.sessions[0].status).toBe('ready');
  });
});

describe('upsertSessions', () => {
  it('空快照起步（ensure 受理写缓存路径）', () => {
    const out = upsertSessions({ sessions: [] }, [rowOf('default')]);
    expect(out.sessions.map(row => row.profile)).toEqual(['default']);
  });
});

describe('isFrameSequenceBroken', () => {
  it('建连首帧（prevSeq null）接受任意 seq', () => {
    expect(isFrameSequenceBroken(null, { seq: 7, type: 'snapshot' })).toBe(false);
  });

  it('连续 seq 正常，断档报断', () => {
    expect(isFrameSequenceBroken(7, { seq: 8, type: 'sessions' })).toBe(false);
    expect(isFrameSequenceBroken(7, { seq: 9, type: 'sessions' })).toBe(true);
  });
});
