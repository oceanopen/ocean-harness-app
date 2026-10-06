import type { AcpViewSnapshotWire } from './AcpSessionService';
import { describe, expect, it } from 'vitest';
import { normalizeAcpViewSnapshot } from './AcpSessionService';

// normalizeAcpViewSnapshot 归一化单测：旧 sidecar wire 的 null 数组归一为 []（零条目
// 快照缺陷窗口的混跑防御），非空数组与其余字段原样保留。

function wireSnapshot(overrides: Partial<AcpViewSnapshotWire> = {}): AcpViewSnapshotWire {
  return {
    status: 'ready',
    entries: null,
    pendings: null,
    turnActive: false,
    ...overrides,
  };
}

describe('normalizeAcpViewSnapshot', () => {
  it('null entries/pendings 归一为空数组（旧 sidecar 零条目快照）', () => {
    const out = normalizeAcpViewSnapshot(wireSnapshot());
    expect(out.entries).toEqual([]);
    expect(out.pendings).toEqual([]);
    expect(out.status).toBe('ready');
  });

  it('非空数组与其余字段原样保留（引用不变）', () => {
    const entries = [{ entryId: 'e1', kind: 'user' as const, text: '第一问' }];
    const pendings = [{ pendingId: 1, kind: 'permission' as const }];
    const out = normalizeAcpViewSnapshot(wireSnapshot({ entries, pendings, agentCode: 'claude-acp' }));
    expect(out.entries).toBe(entries);
    expect(out.pendings).toBe(pendings);
    expect(out.agentCode).toBe('claude-acp');
  });
});
