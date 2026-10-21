import type { WorkspaceModel } from '@src/services';
import { create } from 'zustand';

/**
 * tracker 域 client 状态：工作空间选中态。命令面板「跳到工作空间」与主页面共享同一份选中态。
 * 不启用 persist：选中态仅会话内有效，重启重新初始化（保持原行为）。
 */
interface TrackerSelectionState {
  selectedWorkspace: WorkspaceModel | null;
  /**
   * 当前活动工作空间 id（粘性）：selectWorkspace(ws) 时记录 ws.id，selectWorkspace(null)（点切换回到网格页）时保留，
   * 供 WorkspacesView 在卡片上标记「当前工作空间」对勾（该页仅在未选中态渲染，需靠此粘性 id 才知道上个工作空间）。
   */
  activeWorkspaceId: string | null;
  selectWorkspace: (ws: WorkspaceModel | null) => void;
}

export const useTrackerStore = create<TrackerSelectionState>()(set => ({
  selectedWorkspace: null,
  activeWorkspaceId: null,
  /**
   * 入参 null 表示清空选中（如 TrackerPage 切换按钮回到列表），非空表示回写（如命令面板选中实体）。
   * activeWorkspaceId 粘性：非空回写时记录，清空选中时保留原值（不随之清空）。
   */
  selectWorkspace: ws => set(state => ({
    selectedWorkspace: ws,
    activeWorkspaceId: ws ? ws.id : state.activeWorkspaceId,
  })),
}));
