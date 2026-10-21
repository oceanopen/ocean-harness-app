import type { WorkspaceIssueResponseData } from '@src/services';
import type { StateCode } from '@src/state/tracker';
import type { Dispatch, SetStateAction } from 'react';
import { DragDropContext } from '@hello-pangea/dnd';
import { Box } from '@mui/material';
import { useTranslation } from 'react-i18next';
import KanbanColumn from './KanbanColumn';
import { useKanbanColumns } from './useKanbanColumns';
import { useKanbanDnd } from './useKanbanDnd';

interface KanbanViewProps {
  workspaceIssues: WorkspaceIssueResponseData[];
  setIssues: Dispatch<SetStateAction<WorkspaceIssueResponseData[]>>;
  onAddIssue: (stateCode: StateCode) => void;
  onEdit: (workspaceIssue: WorkspaceIssueResponseData) => void;
  showToast: (text: string, severity: 'success' | 'error') => void;
}

// 看板视图：DragDropContext 容器 + 横向铺列（固定列宽 + 横向滚动）。
// 列 = 状态（固定 5 列，与列表模式分组一致），分列用 useKanbanColumns（按 stateCode），拖拽逻辑用 useKanbanDnd（与渲染解耦）。
// 注：看板展示全量 workspaceIssues（不应用列表的筛选，避免破坏列结构与拖拽排序基准）。
// 卡片复用统一 IssueCard，交互回调与列表同源。
function KanbanView({ workspaceIssues, setIssues, onAddIssue, onEdit, showToast }: KanbanViewProps) {
  const { t } = useTranslation();
  const { columnsByState, orderedStates } = useKanbanColumns(workspaceIssues);
  const onDragEnd = useKanbanDnd({
    columnsByState,
    setIssues,
    showToast,
    moveFailedText: message => t('tracker:workspaceIssue.toast.moveFailed', { message }),
  });

  return (
    <DragDropContext onDragEnd={onDragEnd}>
      <Box sx={{ display: 'flex', gap: '12px', height: '100%', overflow: 'auto', p: '12px' }}>
        {orderedStates.map(code => (
          <KanbanColumn
            key={code}
            stateCode={code}
            workspaceIssues={columnsByState.get(code) ?? []}
            onAdd={() => onAddIssue(code)}
            onEdit={onEdit}
          />
        ))}
      </Box>
    </DragDropContext>
  );
}

export default KanbanView;
