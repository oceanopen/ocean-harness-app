import type { ProjectIssueResponseData } from '@src/services';
import { create } from 'zustand';

// devWorkbench 域 client 状态：左任务树的 issue 级选中态（issue id + 其 project id）。
// 存 projectId 供右栏按 project 查询 issue/开发步骤（实时派生，避免 store 快照陈旧——issue 变更后 query 自动刷新）。
// workspace/project 级选择复用 tracker store（issue 数据挂 workspace→project，共享最省事）。
// 不启用 persist：选中态仅会话内有效（与 tracker 一致，重启重新初始化）。
interface DevWorkbenchSelectionState {
  // 当前选中的开发任务 issue id + 其所属 project id（右栏步骤条据此派生）。
  selectedIssueId: number | null;
  selectedProjectId: number | null;
  selectIssue: (issue: ProjectIssueResponseData | null) => void;
}

export const useDevWorkbenchStore = create<DevWorkbenchSelectionState>()(set => ({
  selectedIssueId: null,
  selectedProjectId: null,
  selectIssue: issue => set({
    selectedIssueId: issue?.id ?? null,
    selectedProjectId: issue?.projectId ?? null,
  }),
}));
