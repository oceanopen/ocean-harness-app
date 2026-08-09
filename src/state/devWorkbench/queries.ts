import type { ProjectIssueResponseData, StateGroup } from '@src/services';
import type { ProjectStateView } from '@src/state/tracker';
import { ProjectIssueService } from '@src/services';
import { useToast } from '@src/shared/useToast';
import { trackerKeys } from '@src/state/tracker';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

// devWorkbench 域派生工具 + 推进 hook。
// dev issue = 顶级（parentId===0）且 stateId 落在 started 组（含进行中 in_progress 与各开发步骤）。
// 左树按 stateGroup 聚合 started 组全部任务；子状态切换在右栏步骤内容区进行（推进走 useAdvanceDevStep）。
// stateView 由调用方从 tracker 的 useProjectStateViews() 提供（views/viewMap，已 join 目录）。

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

/** 取某 stateGroup 组的首个 stateId（按 sortOrder 升序）。用于 D4 [仅停止]→cancelled 首个。 */
export function getFirstStateIdOfGroup(
  group: StateGroup,
  views: ProjectStateView[],
): number | null {
  const first = views
    .filter(v => v.stateGroupCode === group)
    .sort((a, b) => a.sortOrder - b.sortOrder)[0];
  return first?.id ?? null;
}

/**
 * 开发步骤序列（started 组排除进行中 in_progress，按 sortOrder 升序）。
 * wt_init→developing→pr_open→cleanup；SSOT 供 DevStepper 渲染、getNextDevStepStateId 推进、
 * DevWorkbenchPage 默认选中第一步复用，避免过滤逻辑多处漂移。
 */
export function getDevSteps(views: ProjectStateView[]): ProjectStateView[] {
  return views
    .filter(v => v.stateGroupCode === 'started' && v.stateCode !== 'in_progress')
    .sort((a, b) => a.sortOrder - b.sortOrder);
}

/**
 * 取 issue 推进到的下一个开发步骤 stateId：
 * - 当前在开发步骤序列：下一个开发步骤（wt_init→developing→pr_open→cleanup，排除进行中 in_progress）
 * - 已是最后一步（cleanup）：completed 组首个 state（自动归档）
 * - 当前不在开发步骤序列（如 in_progress）：返回 null（不推进，调用方不显示推进按钮）
 */
export function getNextDevStepStateId(
  currentStateId: number,
  views: ProjectStateView[],
): number | null {
  const steps = getDevSteps(views);
  const idx = steps.findIndex(s => s.id === currentStateId);
  if (idx < 0) {
    return null;
  }
  const next = steps[idx + 1];
  if (next) {
    return next.id;
  }
  // cleanup（最后一步）→ completed 组首个（自动归档）
  return getFirstStateIdOfGroup('completed', views);
}

/**
 * 推进 issue stateId 的 hook：调 ProjectIssueService.move（保留原 sortOrder）+ invalidate 该 project 的 issues 缓存。
 * 不做乐观更新（move 成功后 invalidate 自动刷新 DevTaskTree/右栏；advancing=true 防双击）。
 * 失败时弹 toast（硬编码中文报错，含后端错误信息——按 i18n 策略，报错类不走 key 便于排查）；snack 由调用方渲染（照 useToast「hook 返回 snack 元素」范式）。
 */
export function useAdvanceDevStep(projectId: number) {
  const qc = useQueryClient();
  const { show: showToast, snack } = useToast();
  const [advancing, setAdvancing] = useState(false);
  const advance = async (issue: ProjectIssueResponseData, targetStateId: number) => {
    if (advancing || targetStateId == null) {
      return;
    }
    setAdvancing(true);
    try {
      await ProjectIssueService.move({
        id: issue.id,
        stateId: targetStateId,
        sortOrder: issue.sortOrder,
      });
      await qc.invalidateQueries({ queryKey: trackerKeys.projectIssues(projectId) });
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      showToast(`推进失败：${msg}`, 'error');
    } finally {
      setAdvancing(false);
    }
  };
  return { advance, advancing, snack };
}
