import type { ProjectIssueResponseData } from '@src/services';
import { create } from 'zustand';

// devWorkbench 域 client 状态：左任务树的 issue 级选中态。
// 存 projectId 供右栏按 project 查询 issue 详情（实时派生，避免 store 快照陈旧——issue 变更后 query 自动刷新）。
// workspace/project 级选择复用 tracker store（issue 数据挂 workspace→project，共享最省事）。
// 不启用 persist：选中态仅会话内有效（与 tracker 一致，重启重新初始化）。
// 左栏折叠态不在此处——走 appConfig 持久化（PANEL_DEV_TREE_COLLAPSED_KEY，见 DevWorkbenchPage）。
interface DevWorkbenchSelectionState {
  // 当前选中的开发任务 issue id（uuid 字符串）+ 其所属 project id（右栏据此查询 issue 详情）。
  selectedIssueId: string | null;
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
