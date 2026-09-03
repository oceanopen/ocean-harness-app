import type { ProjectIssueResponseData } from '@src/services';

// devWorkbench 域派生工具。
// dev issue = 顶级（parentId 为空串）且非终态（DONE/CANCELLED 除外）的 issue
// （T3.3 放宽：BACKLOG/TODO/IN_PROGRESS 均进左树——工作台覆盖润色→开发全生命周期，
// 「AI 润色」跳转与「进入开发」共用同一条 URL 选中链路；终态任务已收尾且工作空间
// 多已被归档清理，不再提供入口）。与「AI 润色」按钮门槛（仅终态禁用）同一判定语义。

/** 判断 issue 是否进入开发工作台左树（非终态的顶级 issue）。 */
export function isDevIssue(issue: ProjectIssueResponseData): boolean {
  if (issue.parentId !== '') {
    return false; // 子任务不进开发工作台树
  }
  return issue.stateCode !== 'DONE' && issue.stateCode !== 'CANCELLED';
}

/** 过滤出进入开发工作台左树的 issue（非终态的顶级）。 */
export function filterDevIssues(issues: ProjectIssueResponseData[]): ProjectIssueResponseData[] {
  return issues.filter(i => isDevIssue(i));
}

/** 过滤出指定 issue 的子任务（parentId 指向该 issue），按 sortOrder 升序（右侧子任务面板用）。 */
export function filterIssueSubTasks(
  issues: ProjectIssueResponseData[],
  issueId: string,
): ProjectIssueResponseData[] {
  return issues
    .filter(i => i.parentId === issueId)
    .sort((a, b) => a.sortOrder - b.sortOrder);
}
