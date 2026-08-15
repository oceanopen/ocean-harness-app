import type { ProjectIssueResponseData } from '@src/services';

// devWorkbench 域派生工具。
// dev issue = 顶级（parentId 为空串）且 stateCode 为 IN_PROGRESS 的 issue（左树聚合全部进行中任务）。

/** 判断 issue 是否进入开发工作台左树（进行中的顶级 issue）。 */
export function isDevIssue(issue: ProjectIssueResponseData): boolean {
  if (issue.parentId !== '') {
    return false; // 子任务不进开发工作台树
  }
  return issue.stateCode === 'IN_PROGRESS';
}

/** 过滤出进入开发工作台左树的 issue（进行中的顶级）。 */
export function filterDevIssues(issues: ProjectIssueResponseData[]): ProjectIssueResponseData[] {
  return issues.filter(i => isDevIssue(i));
}
