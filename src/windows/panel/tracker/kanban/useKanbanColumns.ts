import type { ProjectIssueResponseData, ProjectStateModel } from '@src/service';
import { useMemo } from 'react';

export interface KanbanColumns {
  // stateId → 该状态下的 issue 列表（列内按 sortOrder 升序）。
  columnsByState: Map<number, ProjectIssueResponseData[]>;
  // 看板列的展示顺序（按 state.sortOrder 升序）。
  orderedStates: ProjectStateModel[];
}

// useKanbanColumns 把扁平 issues 按 stateId 分列，供看板视图渲染。
// 列基于 states 全量生成（含空状态列，否则无法拖入）；列内按纯 sortOrder 升序——
// 看板位置完全由用户拖动决定，priority 仅作卡片视觉标记（与 computeSortOrder 插值假设一致；
// 若按 priority 主排会与只改 sortOrder 的拖拽冲突，导致卡片"弹回"）。
// stateId 指向已删状态的 issue 不入列（罕见边界，可在详情抽屉里改状态）。
export function useKanbanColumns(issues: ProjectIssueResponseData[], states: ProjectStateModel[]): KanbanColumns {
  return useMemo(() => {
    const columnsByState = new Map<number, ProjectIssueResponseData[]>();
    states.forEach((s) => {
      columnsByState.set(s.id, []);
    });
    issues.forEach((i) => {
      const arr = columnsByState.get(i.stateId);
      if (arr) {
        arr.push(i);
      }
    });
    columnsByState.forEach((arr) => {
      arr.sort((a, b) => a.sortOrder - b.sortOrder);
    });
    const orderedStates = [...states].sort((a, b) => a.sortOrder - b.sortOrder);
    return { columnsByState, orderedStates };
  }, [issues, states]);
}
