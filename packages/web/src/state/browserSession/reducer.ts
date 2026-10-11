import type { BrowserFrame, BrowserSessionSnapshot, BrowserSessionView } from '@src/services';

// applyFrame 将一帧 SSE 增量归约进当前快照（纯函数，返回新快照，不 mutate 入参）。
// 帧型只有两类（view.go D6）：snapshot 整体替换、sessions 按 profile 覆写式 upsert——
// 后端行级变更整行下发，归约只依赖单帧自洽语义，不依赖帧间顺序。
export function applyFrame(snapshot: BrowserSessionSnapshot, frame: BrowserFrame): BrowserSessionSnapshot {
  switch (frame.type) {
    case 'snapshot':
      // 建连首帧：整体替换（快照即全量投影，无合并语义）。
      return frame.snapshot ?? snapshot;
    case 'sessions':
      return frame.sessions ? upsertSessions(snapshot, frame.sessions) : snapshot;
  }
}

// upsertSessions 按 profile 整行覆写；未提及的行保持原位（帧只携带变更行），新 profile
// 追加尾部（建连快照按 profile 字典序，后续新会话排尾对 UI 顺序稳定）。
export function upsertSessions(snapshot: BrowserSessionSnapshot, rows: BrowserSessionView[]): BrowserSessionSnapshot {
  const byProfile = new Map(snapshot.sessions.map(row => [row.profile, row]));
  for (const row of rows) {
    byProfile.set(row.profile, row);
  }
  return { sessions: [...byProfile.values()] };
}

// isFrameSequenceBroken 判定帧流是否断档（seq gap）：prevSeq 为 null 表示建连首帧
// （hub 保证首帧即快照，seq 任意值都接受）；此后 seq 必须严格 +1，断档即由调用方重建
// 连接取全新快照（无 Last-Event-ID 续传，后端契约 hub.go）。
export function isFrameSequenceBroken(prevSeq: number | null, frame: BrowserFrame): boolean {
  return prevSeq !== null && frame.seq !== prevSeq + 1;
}
