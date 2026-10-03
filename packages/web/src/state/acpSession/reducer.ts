import type { AcpFrame, AcpViewSnapshot } from '@src/services';

// applyFrame 将一帧 SSE 增量归约进当前快照（纯函数，返回新快照，不 mutate 入参）。
// 帧序注记（T1.5 定稿）：内容帧经 pump、回合终态帧经收尾 goroutine 并发广播——归约只
// 依赖单帧自洽语义（entry 覆写式 upsert、快照类最新替换、回合两端置位），不依赖帧间
// 顺序；乱序只允许发生在语义无因果的重排上（见 reducer.test.ts 的乱序一致性用例）。
export function applyFrame(snapshot: AcpViewSnapshot, frame: AcpFrame): AcpViewSnapshot {
  switch (frame.type) {
    case 'snapshot':
      // 建连首帧：整体替换（快照即全量投影，无合并语义）。
      return frame.snapshot ?? snapshot;
    case 'sessionStatus': {
      // 状态迁移（starting/ready/failed）：身份字段以载荷为准（defined 覆盖）；迁入
      // 非 failed 态清残留 error（failed-terminated 重建链路的旧因不带到新会话）。
      const payload = frame.status;
      if (!payload) {
        return snapshot;
      }
      const base: AcpViewSnapshot = { ...snapshot, status: payload.status };
      const cleared = payload.status === 'failed' ? base : { ...base, error: undefined };
      return applyDefined(cleared, {
        agentCode: payload.agentCode,
        acpSessionId: payload.acpSessionId,
        error: payload.error,
      });
    }
    case 'entry': {
      // 条目 upsert：按 entryId 原位覆写（chunk 聚合与工具调用部分更新均整条下发），
      // 新条目追加尾部——与后端 entries 顺序保持一致。
      const entry = frame.entry;
      if (!entry) {
        return snapshot;
      }
      const idx = snapshot.entries.findIndex(e => e.entryId === entry.entryId);
      if (idx < 0) {
        return { ...snapshot, entries: [...snapshot.entries, entry] };
      }
      const entries = snapshot.entries.slice();
      entries[idx] = entry;
      return { ...snapshot, entries };
    }
    case 'planUpdated':
      return frame.plan ? { ...snapshot, plan: frame.plan } : snapshot;
    case 'usageUpdated':
      return frame.usage ? { ...snapshot, usage: frame.usage } : snapshot;
    case 'commandsUpdated':
      return frame.commands ? { ...snapshot, availableCommands: frame.commands } : snapshot;
    case 'modeUpdated':
      // 模式目录 / 当前模式最新替换（载荷字段各自 defined 覆盖，对齐后端替换式赋值）。
      return applyDefined(snapshot, {
        availableModes: frame.mode?.availableModes,
        currentModeId: frame.mode?.currentModeId,
      });
    case 'pendingOpened': {
      // 挂起开启：按 pendingId upsert（应答失败回填会重开同一 pendingId）。
      const pending = frame.pending;
      if (!pending) {
        return snapshot;
      }
      const idx = snapshot.pendings.findIndex(p => p.pendingId === pending.pendingId);
      if (idx < 0) {
        return { ...snapshot, pendings: [...snapshot.pendings, pending] };
      }
      const pendings = snapshot.pendings.slice();
      pendings[idx] = pending;
      return { ...snapshot, pendings };
    }
    case 'pendingClosed':
      // 挂起关闭（应答/回合收尾结算）：按 pendingId 移除（后端自 1 起，无 0 值歧义）。
      return { ...snapshot, pendings: snapshot.pendings.filter(p => p.pendingId !== frame.pendingId) };
    case 'turnStarted':
      return { ...snapshot, turnActive: frame.turn?.active ?? true };
    case 'turnEnded': {
      // 回合终态：置停 + stopReason/error defined 覆盖（「最近回合终态」最新替换语义）。
      const turn = frame.turn;
      return applyDefined({ ...snapshot, turnActive: turn?.active ?? false }, {
        stopReason: turn?.stopReason,
        error: turn?.error,
      });
    }
    case 'terminated': {
      // 会话终结：终态 + 原因；未决回合随进程消亡一并置停。
      return applyDefined({ ...snapshot, status: 'terminated', turnActive: false }, {
        acpSessionId: frame.status?.acpSessionId,
        error: frame.status?.error,
      });
    }
  }
}

// isFrameSequenceBroken 判定帧流是否断档（seq gap）：prevSeq 为 null 表示建连首帧
// （hub 保证首帧即快照，seq 任意值都接受）；此后 seq 必须严格 +1，断档即由调用方重建
// 连接取全新快照（无 Last-Event-ID 续传，后端契约 hub.go）。
export function isFrameSequenceBroken(prevSeq: number | null, frame: AcpFrame): boolean {
  return prevSeq !== null && frame.seq !== prevSeq + 1;
}

// applyDefined 将 patch 中非 undefined 字段覆盖进 base（浅合并，返回新对象）——对齐
// Go omitempty 载荷「未携带 = 无变化」的语义，避免空字段误清已有值。
function applyDefined<T extends object>(base: T, patch: Partial<T>): T {
  const out = { ...base };
  for (const [key, value] of Object.entries(patch)) {
    if (value !== undefined) {
      (out as Record<string, unknown>)[key] = value;
    }
  }
  return out;
}
