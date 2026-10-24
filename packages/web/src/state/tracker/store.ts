import type { WorkspaceModel } from '@src/services';
import { create } from 'zustand';

/**
 * tracker 域 client 状态：工作空间选中态。命令面板「跳到工作空间」回写，面板上下文
 * （当前空间 id/名）由此派生。不启用 persist：选中态仅会话内有效，重启重新初始化。
 */
interface TrackerSelectionState {
  selectedWorkspace: WorkspaceModel | null;
  selectWorkspace: (ws: WorkspaceModel | null) => void;
}

export const useTrackerStore = create<TrackerSelectionState>()(set => ({
  selectedWorkspace: null,
  // 入参 null = 清空选中；非空 = 回写（命令面板选中实体）。
  selectWorkspace: ws => set({ selectedWorkspace: ws }),
}));
