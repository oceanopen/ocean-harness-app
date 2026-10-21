import type { WorkspaceIssueResponseData } from '@src/services';

/**
 * devWorkbench 域派生工具。
 * dev issue = 非终态 issue（BACKLOG/TODO/IN_PROGRESS 进左树，DONE/CANCELLED 终态已收尾不再提供入口）。
 */

/** 判断 issue 是否进入开发工作台左树（非终态）。 */
export function isDevIssue(issue: WorkspaceIssueResponseData): boolean {
  return issue.stateCode !== 'DONE' && issue.stateCode !== 'CANCELLED';
}

/** 过滤出进入开发工作台左树的 issue（非终态）。 */
export function filterDevIssues(issues: WorkspaceIssueResponseData[]): WorkspaceIssueResponseData[] {
  return issues.filter(i => isDevIssue(i));
}
