import type { DropResult } from '@hello-pangea/dnd';
import type { ProjectIssueResponseData, StateGroup } from '@src/services';
import type { Dispatch, SetStateAction } from 'react';
import { ProjectIssueService } from '@src/services';
import { useCallback, useRef } from 'react';

type ToastSeverity = 'success' | 'error';

const STEP = 10000;
const EPSILON = 1e-6;

export interface UseKanbanDndOptions {
  columnsByGroup: Map<StateGroup, ProjectIssueResponseData[]>;
  // 各状态组的首个子状态 id（跨组拖拽的目标 stateId）。
  firstStateIdByGroup: Partial<Record<StateGroup, number>>;
  setIssues: Dispatch<SetStateAction<ProjectIssueResponseData[]>>;
  showToast: (text: string, severity: ToastSeverity) => void;
  moveFailedText: (message: string) => string;
}

// computeSortOrder 按分数插值算落点 sortOrder：列空/列首/列尾/列中四情形；
// 邻值差 < EPSILON 时降级为整数步进，避免反复对半后浮点精度塌缩。
// 入参 col 应已剔除被拖卡片自身（同列下拖时避免把自己算进邻居导致 index 偏移）。
export function computeSortOrder(col: ProjectIssueResponseData[], index: number): number {
  if (col.length === 0) {
    return STEP;
  }
  if (index <= 0) {
    return col[0].sortOrder - STEP;
  }
  if (index >= col.length) {
    return col[col.length - 1].sortOrder + STEP;
  }
  const lower = col[index - 1].sortOrder;
  const upper = col[index].sortOrder;
  if (Math.abs(upper - lower) < EPSILON) {
    return Math.floor(lower) + 1; // 精度退化降级
  }
  return (lower + upper) / 2;
}

// useKanbanDnd 封装看板拖拽的 onDragEnd（列 = 状态组）：
// 同组拖拽 → 保留子状态 stateId、只重排 sortOrder；跨组拖拽 → stateId 落到目标组首个子状态。
// 按 destGroup 预估 completedAt（completed 组置当前时间，与后端规则字面对齐；后端二次校正）。
// 乐观更新即时反馈，失败用 setIssues 前的快照整表回滚 + toast；用后端权威返回值二次校正。
export function useKanbanDnd(opts: UseKanbanDndOptions) {
  const { columnsByGroup, firstStateIdByGroup, setIssues, showToast, moveFailedText } = opts;
  // 回滚快照：在 setIssues 的 updater 内捕获更新前的最新 state，失败时整表恢复。
  const snapshotRef = useRef<ProjectIssueResponseData[] | null>(null);
  // 同卡飞行中串行化：避免快速连拖同一张卡导致 fetch 响应乱序覆盖乐观状态。
  const inFlightRef = useRef<Set<number>>(new Set());

  return useCallback((result: DropResult) => {
    const { source, destination, draggableId } = result;
    if (!destination) {
      return;
    }
    if (source.droppableId === destination.droppableId && source.index === destination.index) {
      return;
    }

    const issueId = Number(draggableId);
    // 看板列 droppableId 即状态组 code（source/destination 同源）。
    const sourceGroup = source.droppableId as StateGroup;
    const destGroup = destination.droppableId as StateGroup;
    const isCrossGroup = sourceGroup !== destGroup;

    // 同卡飞行中：忽略本次拖拽，防止与在途请求乱序叠加（本地 server 瞬时，正常交互不触发）。
    if (inFlightRef.current.has(issueId)) {
      return;
    }

    // 目标列快照剔除自身（同组下拖时 self 仍在列内，需排除以免 index 错位）。
    const destCol = (columnsByGroup.get(destGroup) ?? []).filter(i => i.id !== issueId);
    const newSortOrder = computeSortOrder(destCol, destination.index);

    // issue 当前 stateId：从源列查（同组拖拽时保留它）；跨组则取目标组首个子状态。
    const sourceIssue = (columnsByGroup.get(sourceGroup) ?? []).find(i => i.id === issueId);
    const currentStateId = sourceIssue?.stateId ?? 0;
    const targetStateId = isCrossGroup
      ? (firstStateIdByGroup[destGroup] ?? currentStateId)
      : currentStateId;

    setIssues((prev) => {
      snapshotRef.current = prev;
      const projectIssue = prev.find(i => i.id === issueId);
      if (!projectIssue) {
        return prev;
      }
      const moved: ProjectIssueResponseData = {
        ...projectIssue,
        stateId: targetStateId,
        sortOrder: newSortOrder,
        completedAt: destGroup === 'completed' ? new Date().toISOString() : null,
      };
      return prev.map(i => (i.id === issueId ? moved : i));
    });

    inFlightRef.current.add(issueId);
    ProjectIssueService.move({
      id: issueId,
      stateId: targetStateId,
      sortOrder: newSortOrder,
    })
      .then((updated) => {
        snapshotRef.current = null;
        // 用后端权威返回值二次校正（completedAt/sortOrder/stateId 实际写入值）。
        setIssues(prev => prev.map(i => (i.id === updated.id ? updated : i)));
      })
      .catch((e) => {
        if (snapshotRef.current) {
          setIssues(snapshotRef.current);
          snapshotRef.current = null;
        }
        const msg = e instanceof Error ? e.message : String(e);
        showToast(moveFailedText(msg), 'error');
      })
      .finally(() => {
        inFlightRef.current.delete(issueId);
      });
  }, [columnsByGroup, firstStateIdByGroup, setIssues, showToast, moveFailedText]);
}
