import type { DropResult } from '@hello-pangea/dnd';
import type { Dispatch, SetStateAction } from 'react';
import type { Issue, ProjectState } from '../IssueListPage';
import { useCallback, useRef } from 'react';
import { apiPost } from '../api';

type ToastSeverity = 'success' | 'error';

const STEP = 10000;
const EPSILON = 1e-6;

export interface UseKanbanDndOptions {
  columnsByState: Map<number, Issue[]>;
  stateMap: Map<number, ProjectState>;
  setIssues: Dispatch<SetStateAction<Issue[]>>;
  showToast: (text: string, severity: ToastSeverity) => void;
  moveFailedText: (message: string) => string;
}

// computeSortOrder 按分数插值算落点 sortOrder：列空/列首/列尾/列中四情形；
// 邻值差 < EPSILON 时降级为整数步进，避免反复对半后浮点精度塌缩。
// 入参 col 应已剔除被拖卡片自身（同列下拖时避免把自己算进邻居导致 index 偏移）。
export function computeSortOrder(col: Issue[], index: number): number {
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

// useKanbanDnd 封装看板拖拽的 onDragEnd：改 stateId + sortOrder + 按目标 stateGroup 预估 completedAt。
// 乐观更新即时反馈，失败用 setIssues 前的快照整表回滚 + toast；用后端权威返回值二次校正。
// 与渲染解耦：KanbanView 只需把返回的 onDragEnd 交给 DragDropContext。
export function useKanbanDnd(opts: UseKanbanDndOptions) {
  const { columnsByState, stateMap, setIssues, showToast, moveFailedText } = opts;
  // 回滚快照：在 setIssues 的 updater 内捕获更新前的最新 state，失败时整表恢复。
  const snapshotRef = useRef<Issue[] | null>(null);
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
    const destStateId = Number(destination.droppableId);

    // 同卡飞行中：忽略本次拖拽，防止与在途请求乱序叠加（本地 server 瞬时，正常交互不触发）。
    if (inFlightRef.current.has(issueId)) {
      return;
    }

    // 目标列快照剔除自身（同列下拖时 self 仍在列内，需排除以免 index 错位）。
    const destCol = (columnsByState.get(destStateId) ?? []).filter(i => i.id !== issueId);
    const newSortOrder = computeSortOrder(destCol, destination.index);

    // 按目标 stateGroup 预估 completedAt（与后端 applyStateTransition 规则字面对齐；后端二次校正）。
    const destGroup = stateMap.get(destStateId)?.stateGroup;

    setIssues((prev) => {
      snapshotRef.current = prev;
      const issue = prev.find(i => i.id === issueId);
      if (!issue) {
        return prev;
      }
      const moved: Issue = {
        ...issue,
        stateId: destStateId,
        sortOrder: newSortOrder,
        completedAt: destGroup === 'completed' ? new Date().toISOString() : null,
      };
      return prev.map(i => (i.id === issueId ? moved : i));
    });

    inFlightRef.current.add(issueId);
    apiPost<Issue>('/api/tracker/projectIssue/move', {
      id: issueId,
      stateId: destStateId,
      sortOrder: newSortOrder,
    })
      .then((updated) => {
        snapshotRef.current = null;
        // 用后端权威返回值二次校正（completedAt/sortOrder 实际写入值）。
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
  }, [columnsByState, stateMap, setIssues, showToast, moveFailedText]);
}
