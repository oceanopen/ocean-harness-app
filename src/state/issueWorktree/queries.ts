import type { IssueWorktreeCreateWorktreeRequest, ProjectIssueResponseData } from '@src/services';
import { IssueWorktreeService, ProjectIssueService } from '@src/services';
import { commands } from '@src/shared/bindings';
import { useToast } from '@src/shared/useToast';
import { trackerKeys } from '@src/state/tracker';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { issueWorktreeKeys } from './keys';

// issueWorktree 域：issue 开发流程 worktree 元数据查询 + 多步编排 hook（P1 桩）。
// 编排 hook 照 devWorkbench/useAdvanceDevStep 范式：单 try/catch + toast + loading + snack，
// 任一步失败即中止并 toast（不跳过未完成的后续步骤）。

// useIssueWorktrees：查某 issue 的 active worktree（作 worktreePath/worktreeId 的 SSOT，D1/D2/D4 共用）。
export function useIssueWorktrees(issueId: number) {
  return useQuery({
    queryKey: issueWorktreeKeys.list(issueId),
    queryFn: () => IssueWorktreeService.getList({ issueId }),
    enabled: issueId > 0,
  });
}

// useCreateWorktreeAndAdvance：D1 [创建并开始] 编排——createWorktree（建 worktree 记录）→ updateState 推进 stateId 到首个开发步骤。
export function useCreateWorktreeAndAdvance(projectId: number) {
  const qc = useQueryClient();
  const { show: showToast, snack } = useToast();
  const [running, setRunning] = useState(false);
  const run = async (
    req: IssueWorktreeCreateWorktreeRequest,
    issue: ProjectIssueResponseData,
    targetStateId: number,
  ) => {
    if (running || targetStateId == null) {
      return;
    }
    setRunning(true);
    try {
      await IssueWorktreeService.createWorktree(req);
      await ProjectIssueService.updateState({ id: issue.id, stateId: targetStateId });
      await Promise.all([
        qc.invalidateQueries({ queryKey: issueWorktreeKeys.list(req.issueId) }),
        qc.invalidateQueries({ queryKey: trackerKeys.projectIssues(projectId) }),
      ]);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(`开始开发失败：${msg}`, 'error');
    } finally {
      setRunning(false);
    }
  };
  return { run, running, snack };
}

// useCleanupAndAdvance：D4 [清理并完成] 编排——pty_stop_for_worktree（停 PTY）→ removeWorktree（软删记录）→ updateState 推进 completed。
// §9.3 两阶段强约束：先停 PTY 再删 worktree（否则文件锁）。
export function useCleanupAndAdvance(projectId: number) {
  const qc = useQueryClient();
  const { show: showToast, snack } = useToast();
  const [running, setRunning] = useState(false);
  const run = async (
    worktreeId: string,
    issue: ProjectIssueResponseData,
    targetStateId: number,
  ) => {
    if (running || targetStateId == null) {
      return;
    }
    setRunning(true);
    try {
      await commands.ptyStopForWorktree(worktreeId); // Rust 桩（P1 恒返 0）；invoke reject 时抛入 catch
      await IssueWorktreeService.removeWorktree({ worktreeId });
      await ProjectIssueService.updateState({ id: issue.id, stateId: targetStateId });
      await Promise.all([
        qc.invalidateQueries({ queryKey: issueWorktreeKeys.list(issue.id) }),
        qc.invalidateQueries({ queryKey: trackerKeys.projectIssues(projectId) }),
      ]);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(`清理失败：${msg}`, 'error');
    } finally {
      setRunning(false);
    }
  };
  return { run, running, snack };
}
