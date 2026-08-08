import type { ProjectIssueResponseData } from '@src/services';
import type { ProjectStateView } from '@src/state/tracker';

// devWorkbench 域派生工具：判断/过滤进入开发工作台左树的 issue。
// dev issue = 顶级（parentId===0，排除子任务）且 stateId 落在 started 组（含进行中 in_progress 与各开发步骤子状态）。
// 左树按 stateGroup 聚合 started 组全部任务；子状态切换（in_progress ↔ wt_init ↔ developing …）在右栏操作区进行。
// stateView 由调用方从 tracker 的 useProjectStateViews().viewMap 提供（已 join 目录，带 name/color）。

/** 判断 issue 是否进入开发工作台左树（started 组顶级 issue，含进行中）。 */
export function isDevIssue(
  issue: ProjectIssueResponseData,
  viewMap: Map<number, ProjectStateView>,
): boolean {
  if (issue.parentId !== 0) {
    return false;
  } // 子任务不进开发工作台树
  const view = viewMap.get(issue.stateId);
  return !!view && view.stateGroupCode === 'started';
}

/** 过滤出进入开发工作台左树的 issue（started 组顶级，含进行中）。 */
export function filterDevIssues(
  issues: ProjectIssueResponseData[],
  viewMap: Map<number, ProjectStateView>,
): ProjectIssueResponseData[] {
  return issues.filter(i => isDevIssue(i, viewMap));
}
