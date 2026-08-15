import type { DropResult } from '@hello-pangea/dnd';
import type { ProjectIssueResponseData } from '@src/services';
import type { StateCode } from '@src/state/tracker';
import type { Dispatch, SetStateAction } from 'react';
import { ProjectIssueService } from '@src/services';
import { useCallback, useRef } from 'react';

type ToastSeverity = 'success' | 'error';

const STEP = 10000;
const EPSILON = 1e-6;

export interface UseKanbanDndOptions {
  columnsByState: Map<StateCode, ProjectIssueResponseData[]>;
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

// useKanbanDnd 封装看板拖拽的 onDragEnd（列 = 状态）：
// 目标列 stateCode 即卡片新状态。按 destState 预估 completedAt（DONE 置当前时间，与后端规则字面对齐；后端二次校正）。
// 乐观更新即时反馈，失败用 setIssues 前的快照整表回滚 + toast；用后端权威返回值二次校正。
export function useKanbanDnd(opts: UseKanbanDndOptions) {
  const { columnsByState, setIssues, showToast, moveFailedText } = opts;
  // 回滚快照：在 setIssues 的 updater 内捕获更新前的最新 state，失败时整表恢复。
  const snapshotRef = useRef<ProjectIssueResponseData[] | null>(null);
  // 同卡飞行中串行化：避免快速连拖同一张卡导致 fetch 响应乱序覆盖乐观状态。
  const inFlightRef = useRef<Set<string>>(new Set());

  return useCallback((result: DropResult) => {
    const { source, destination, draggableId } = result;
    if (!destination) {
      return;
    }
    if (source.droppableId === destination.droppableId && source.index === destination.index) {
      return;
    }

    // issue id 为 uuid 字符串，draggableId 直接即 id（不再数值化）。
    const issueId = draggableId;
    // 看板列 droppableId 即状态 code（source/destination 同源）。
    const sourceState = source.droppableId as StateCode;
    const destState = destination.droppableId as StateCode;
    const isCrossState = sourceState !== destState;

    // 同卡飞行中：忽略本次拖拽，防止与在途请求乱序叠加（本地 server 瞬时，正常交互不触发）。
    if (inFlightRef.current.has(issueId)) {
      return;
    }

    // 目标列快照剔除自身（同列下拖时 self 仍在列内，需排除以免 index 错位）。
    const destCol = (columnsByState.get(destState) ?? []).filter(i => i.id !== issueId);
    const newSortOrder = computeSortOrder(destCol, destination.index);

    setIssues((prev) => {
      snapshotRef.current = prev;
      const projectIssue = prev.find(i => i.id === issueId);
      if (!projectIssue) {
        return prev;
      }
      const moved: ProjectIssueResponseData = {
        ...projectIssue,
        stateCode: destState,
        sortOrder: newSortOrder,
        completedAt: destState === 'DONE' ? new Date().toISOString() : null,
      };
      // 跨列拖父：子乐观继承父新状态（后端 maybeSyncChildrenState 会同步，缓存跟进避免手动刷新）。
      return prev.map((i) => {
        if (i.id === issueId) {
          return moved;
        }
        if (isCrossState && i.parentId === issueId) {
          return { ...i, stateCode: destState, completedAt: moved.completedAt };
        }
        return i;
      });
    });

    inFlightRef.current.add(issueId);
    ProjectIssueService.move({
      id: issueId,
      stateCode: destState,
      sortOrder: newSortOrder,
    })
      .then((updated) => {
        snapshotRef.current = null;
        // 用后端权威返回值二次校正（completedAt/sortOrder/stateCode 实际写入值）。
        // 跨列拖父时后端已同步子（maybeSyncChildrenState），缓存也跟进子。
        setIssues(prev => prev.map((i) => {
          if (i.id === updated.id) {
            return updated;
          }
          if (isCrossState && i.parentId === updated.id) {
            return { ...i, stateCode: updated.stateCode, completedAt: updated.completedAt };
          }
          return i;
        }));
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
  }, [columnsByState, setIssues, showToast, moveFailedText]);
}
