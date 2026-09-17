import type { SubtaskStats } from '@src/components/issueCard/shared';
// tracker 域派生函数：从全量扁平 issue 列表派生的纯函数（无副作用，供各页面 useMemo 复用）。
import type { ProjectIssueResponseData } from '@src/services';

// 各父 issue 的子任务统计（done/total）：列表/看板卡片进度小标与开发工作台左树共用。
// done 以 completedAt 判定（DONE 状态流转时后端写入），与 ProjectIssueList 原内联实现一致。
export function buildSubtaskStats(issues: ProjectIssueResponseData[]): SubtaskStats {
  const m: SubtaskStats = new Map();
  for (const i of issues) {
    if (i.parentId === '') {
      continue;
    }
    const s = m.get(i.parentId) ?? { done: 0, total: 0 };
    s.total += 1;
    if (i.completedAt) {
      s.done += 1;
    }
    m.set(i.parentId, s);
  }
  return m;
}
