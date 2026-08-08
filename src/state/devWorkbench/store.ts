import { create } from 'zustand';

// devWorkbench 域 client 状态：左任务树的 issue 级选中态。
// workspace/project 级选择复用 tracker store（issue 数据挂 workspace→project，共享最省事、命令面板跳转零改动）。
// 不启用 persist：选中态仅会话内有效（与 tracker 一致，重启重新初始化）。
interface DevWorkbenchSelectionState {
  // 当前选中的开发任务 issue id（右栏步骤条据此渲染；null 表示未选中）。
  selectedIssueId: number | null;
  selectIssue: (id: number | null) => void;
}

export const useDevWorkbenchStore = create<DevWorkbenchSelectionState>()(set => ({
  selectedIssueId: null,
  selectIssue: id => set({ selectedIssueId: id }),
}));
