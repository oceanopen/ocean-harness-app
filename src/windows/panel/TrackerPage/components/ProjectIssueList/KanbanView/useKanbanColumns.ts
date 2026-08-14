import type { ProjectIssueResponseData } from '@src/services';
import type { StateCode } from '@src/state/tracker';
import { STATE_ORDER } from '@src/state/tracker';
import { useMemo } from 'react';

export interface KanbanColumns {
  // stateCode → 该状态下的 projectIssue 列表（列内按 sortOrder 升序）。
  columnsByState: Map<StateCode, ProjectIssueResponseData[]>;
  // 看板列的展示顺序（固定 5 列，STATE_ORDER）。
  orderedStates: StateCode[];
}

// useKanbanColumns 把扁平 projectIssues 按 stateCode 分列，供看板视图渲染（与列表模式分组一致）。
// 列固定为 5 个状态（含 0 issue 的空列，否则无法拖入）；列内按纯 sortOrder 升序——看板位置完全由用户拖动决定。
// 子任务（parentId≠0）不入列。
export function useKanbanColumns(projectIssues: ProjectIssueResponseData[]): KanbanColumns {
  return useMemo(() => {
    const columnsByState = new Map<StateCode, ProjectIssueResponseData[]>();
    STATE_ORDER.forEach(c => columnsByState.set(c, []));
    projectIssues.forEach((i) => {
      if (i.parentId !== 0) {
        return; // 子任务不进看板列（仅在父抽屉管理）
      }
      columnsByState.get(i.stateCode)?.push(i);
    });
    columnsByState.forEach((arr) => {
      arr.sort((a, b) => a.sortOrder - b.sortOrder);
    });
    return { columnsByState, orderedStates: STATE_ORDER };
  }, [projectIssues]);
}
