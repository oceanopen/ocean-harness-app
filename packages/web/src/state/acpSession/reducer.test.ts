import type { AcpFrame, AcpViewSnapshot } from '@src/services';
import { describe, expect, it } from 'vitest';
import { applyFrame, isFrameSequenceBroken } from './reducer';

// applyFrame 归约单测：12 帧型语义 + 帧序无关性（内容帧与回合终态帧并发交错的两种顺序
// 收敛同一终态）+ seq 断档判定。chat 模式退役后前端测试自此重建（vite.config test 段）。

let seqSource = 0;

// frameOf 构造自增 seq 的帧（seq 单调性由后端 hub 保证，测试内同样递增模拟合法流）。
function frameOf(partial: Omit<AcpFrame, 'seq' | 'issueId'>): AcpFrame {
  seqSource += 1;
  return { seq: seqSource, issueId: 'i-1', ...partial };
}

function baseSnapshot(): AcpViewSnapshot {
  return {
    status: 'ready',
    agentCode: 'claude-acp',
    acpSessionId: 'sess-1',
    entries: [],
    pendings: [],
    turnActive: false,
  };
}

describe('applyFrame', () => {
  it('snapshot 帧整体替换', () => {
    const next: AcpViewSnapshot = { ...baseSnapshot(), entries: [{ entryId: 'e1', kind: 'user', text: 'hi' }] };
    const out = applyFrame(baseSnapshot(), frameOf({ type: 'snapshot', snapshot: next }));
    expect(out).toBe(next);
  });

  it('entry 新条目追加尾部', () => {
    const base = baseSnapshot();
    const out = applyFrame(base, frameOf({ type: 'entry', entry: { entryId: 'u1', kind: 'user', text: '第一问' } }));
    expect(out.entries).toHaveLength(1);
    expect(out.entries[0].entryId).toBe('u1');
  });

  it('entry 已有条目原位覆写（chunk 聚合 / 工具调用更新）', () => {
    let snap = applyFrame(baseSnapshot(), frameOf({ type: 'entry', entry: { entryId: 'a1', kind: 'agentMessage', text: '第一' } }));
    snap = applyFrame(snap, frameOf({ type: 'entry', entry: { entryId: 'a1', kind: 'agentMessage', text: '第一段合并' } }));
    expect(snap.entries).toHaveLength(1);
    expect(snap.entries[0].text).toBe('第一段合并');

    const toolCall = { toolCallId: 'tc-1', title: 'Bash', status: 'in_progress' as const };
    snap = applyFrame(snap, frameOf({ type: 'entry', entry: { entryId: 'tool:tc-1', kind: 'toolCall', toolCall } }));
    expect(snap.entries).toHaveLength(2);
    // 工具调用条目覆写不改变位置（前两条目顺序保持）。
    const updated = { ...snap.entries[1], toolCall: { ...toolCall, status: 'completed' as const } };
    snap = applyFrame(snap, frameOf({ type: 'entry', entry: updated }));
    expect(snap.entries).toHaveLength(2);
    expect(snap.entries[1].toolCall?.status).toBe('completed');
    expect(snap.entries[0].entryId).toBe('a1');
  });

  it('pendingOpened upsert 与 pendingClosed 移除', () => {
    let snap = applyFrame(baseSnapshot(), frameOf({ type: 'pendingOpened', pending: { pendingId: 1, kind: 'permission' } }));
    snap = applyFrame(snap, frameOf({ type: 'pendingOpened', pending: { pendingId: 2, kind: 'elicitation' } }));
    expect(snap.pendings).toHaveLength(2);
    // 同 pendingId 重开（应答失败回填）覆写不重复。
    snap = applyFrame(snap, frameOf({ type: 'pendingOpened', pending: { pendingId: 1, kind: 'permission' } }));
    expect(snap.pendings).toHaveLength(2);
    snap = applyFrame(snap, frameOf({ type: 'pendingClosed', pendingId: 1 }));
    expect(snap.pendings.map(p => p.pendingId)).toEqual([2]);
  });

  it('快照类帧（plan/usage/commands/mode）最新替换', () => {
    let snap = applyFrame(baseSnapshot(), frameOf({ type: 'planUpdated', plan: { entries: [] } }));
    expect(snap.plan).toEqual({ entries: [] });
    snap = applyFrame(snap, frameOf({ type: 'usageUpdated', usage: { used: 42 } }));
    snap = applyFrame(snap, frameOf({ type: 'commandsUpdated', commands: [{ name: 'create_plan' }] }));
    snap = applyFrame(snap, frameOf({ type: 'modeUpdated', mode: { currentModeId: 'acceptEdits' } }));
    expect(snap.usage).toEqual({ used: 42 });
    expect(snap.availableCommands).toEqual([{ name: 'create_plan' }]);
    expect(snap.currentModeId).toBe('acceptEdits');
    // 二次推送覆盖。
    snap = applyFrame(snap, frameOf({ type: 'planUpdated', plan: { entries: [{}] } }));
    expect(snap.plan).toEqual({ entries: [{}] });
    // modeUpdated 带目录时一并替换。
    snap = applyFrame(snap, frameOf({ type: 'modeUpdated', mode: { currentModeId: 'bypassPermissions', availableModes: [{ id: 'bypassPermissions', name: 'Bypass' }] } }));
    expect(snap.currentModeId).toBe('bypassPermissions');
    expect(snap.availableModes).toEqual([{ id: 'bypassPermissions', name: 'Bypass' }]);
  });

  it('回合两端置位 turnActive 与 stopReason', () => {
    let snap = applyFrame(baseSnapshot(), frameOf({ type: 'turnStarted', turn: { active: true } }));
    expect(snap.turnActive).toBe(true);
    snap = applyFrame(snap, frameOf({ type: 'turnEnded', turn: { active: false, stopReason: 'end_turn' } }));
    expect(snap.turnActive).toBe(false);
    expect(snap.stopReason).toBe('end_turn');
    // 二回合终态覆盖最近 stopReason。
    snap = applyFrame(snap, frameOf({ type: 'turnStarted', turn: { active: true } }));
    snap = applyFrame(snap, frameOf({ type: 'turnEnded', turn: { active: false, stopReason: 'cancelled' } }));
    expect(snap.stopReason).toBe('cancelled');
  });

  it('sessionStatus 迁移：ready 清残留 error，failed 带因', () => {
    const failed = applyFrame(baseSnapshot(), frameOf({ type: 'sessionStatus', status: { status: 'failed', error: 'claude 未登录' } }));
    expect(failed.status).toBe('failed');
    expect(failed.error).toBe('claude 未登录');
    // 重建链路：failed → starting/ready 旧因不带入。
    const restarted = applyFrame(failed, frameOf({ type: 'sessionStatus', status: { status: 'starting', agentCode: 'claude-acp' } }));
    expect(restarted.status).toBe('starting');
    expect(restarted.error).toBeUndefined();
    const ready = applyFrame(restarted, frameOf({ type: 'sessionStatus', status: { status: 'ready', acpSessionId: 'sess-2' } }));
    expect(ready.acpSessionId).toBe('sess-2');
    expect(ready.error).toBeUndefined();
  });

  it('terminated 置终态并置停未决回合', () => {
    const active = applyFrame(baseSnapshot(), frameOf({ type: 'turnStarted', turn: { active: true } }));
    const out = applyFrame(active, frameOf({ type: 'terminated', status: { status: 'terminated', error: '进程异常退出' } }));
    expect(out.status).toBe('terminated');
    expect(out.turnActive).toBe(false);
    expect(out.error).toBe('进程异常退出');
  });

  it('内容帧与回合终态帧乱序交错收敛同一终态（帧序注记）', () => {
    // pump 内容帧与收尾 turnEnded 并发：两种到达顺序终态必须一致（entry 覆写 + 快照类
    // 最新替换均无帧间因果）。
    const entry = { entryId: 'a1', kind: 'agentMessage' as const, text: '回答' };
    const turnEnded = frameOf({ type: 'turnEnded', turn: { active: false, stopReason: 'end_turn' } });
    const forward = [frameOf({ type: 'entry', entry }), turnEnded];
    const backward = [turnEnded, frameOf({ type: 'entry', entry })];
    const feed = (frames: AcpFrame[]) => frames.reduce(applyFrame, baseSnapshot());
    const a = feed(forward);
    const b = feed(backward);
    expect(a.entries).toEqual(b.entries);
    expect(a.turnActive).toBe(b.turnActive);
    expect(a.stopReason).toBe(b.stopReason);
  });

  it('归约不 mutate 入参快照', () => {
    const base = baseSnapshot();
    const frozen = Object.freeze(base);
    applyFrame(frozen, frameOf({ type: 'entry', entry: { entryId: 'u1', kind: 'user', text: 'x' } }));
    expect(base.entries).toHaveLength(0);
  });
});

describe('isFrameSequenceBroken', () => {
  it('建连首帧（prevSeq null）接受任意 seq', () => {
    expect(isFrameSequenceBroken(null, { seq: 7, issueId: 'i-1', type: 'snapshot' })).toBe(false);
  });

  it('连续 seq 正常，断档报断', () => {
    expect(isFrameSequenceBroken(7, { seq: 8, issueId: 'i-1', type: 'entry' })).toBe(false);
    expect(isFrameSequenceBroken(7, { seq: 9, issueId: 'i-1', type: 'entry' })).toBe(true);
  });
});
