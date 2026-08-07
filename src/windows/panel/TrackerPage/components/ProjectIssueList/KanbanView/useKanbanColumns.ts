import type { ProjectIssueResponseData, StateGroup } from '@src/services';
import type { ProjectStateView } from '@src/state/tracker';
import { useMemo } from 'react';
import { GROUP_ORDER } from '../shared';

export interface KanbanColumns {
  // stateGroupCode → 该组下的 projectIssue 列表（列内按 sortOrder 升序）。
  columnsByGroup: Map<StateGroup, ProjectIssueResponseData[]>;
  // 看板列的展示顺序（GROUP_ORDER 中项目已有状态的组，含空组以便拖入）。
  orderedGroups: StateGroup[];
}

// useKanbanColumns 把扁平 projectIssues 按 stateGroupCode 分列，供看板视图渲染（与列表模式分组一致）。
// 列基于"项目已有该组状态"生成（含 0 issue 的空组，否则无法拖入）；列内按纯 sortOrder 升序——
// 看板位置完全由用户拖动决定。子任务（parentId≠0）不入列。stateId 指向已删状态的 issue 无法定位组 → 不入列。
export function useKanbanColumns(projectIssues: ProjectIssueResponseData[], projectStates: ProjectStateView[]): KanbanColumns {
  return useMemo(() => {
    // stateId → stateGroupCode，用于把 issue 归入状态组列。
    const groupOfState = new Map<number, StateGroup>();
    const groupsWithStates = new Set<StateGroup>();
    projectStates.forEach((s) => {
      groupOfState.set(s.id, s.stateGroupCode);
      groupsWithStates.add(s.stateGroupCode);
    });
    const columnsByGroup = new Map<StateGroup, ProjectIssueResponseData[]>();
    GROUP_ORDER.forEach(g => columnsByGroup.set(g, []));
    projectIssues.forEach((i) => {
      if (i.parentId !== 0) {
        return; // 子任务不进看板列（仅在父抽屉管理）
      }
      const g = groupOfState.get(i.stateId);
      if (g) {
        columnsByGroup.get(g)?.push(i);
      }
    });
    columnsByGroup.forEach((arr) => {
      arr.sort((a, b) => a.sortOrder - b.sortOrder);
    });
    const orderedGroups = GROUP_ORDER.filter(g => groupsWithStates.has(g));
    return { columnsByGroup, orderedGroups };
  }, [projectIssues, projectStates]);
}
