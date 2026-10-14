import type { BrowserSessionStatus } from '@src/services';

/**
 * 任务派生 profile 名（任务↔会话关联 SSOT）：`issue-<issueId>` 命名约定，面板过滤、
 * 「+」新建与 MCP ProfileOpen 共用同一派生规则；uuid 含连字符，符合 profile 命名正则。
 */
export function deriveIssueProfile(issueId: string): string {
  return `issue-${issueId}`;
}

// 会话未释放（引擎进程在场）：starting/ready；idle 已释放、failed 拉起失败/意外退出。
export function isSessionLive(status: BrowserSessionStatus | undefined): boolean {
  return status === 'starting' || status === 'ready';
}
