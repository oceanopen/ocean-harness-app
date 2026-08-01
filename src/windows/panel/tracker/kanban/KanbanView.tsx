import type { ProjectIssueResponseData, ProjectStateModel } from '@src/service';
import type { Dispatch, SetStateAction } from 'react';
import { DragDropContext } from '@hello-pangea/dnd';
import { Box, CircularProgress } from '@mui/material';
import { useTranslation } from 'react-i18next';
import KanbanColumn from './KanbanColumn';
import { useKanbanColumns } from './useKanbanColumns';
import { useKanbanDnd } from './useKanbanDnd';

interface KanbanViewProps {
  issues: ProjectIssueResponseData[];
  states: ProjectStateModel[];
  stateMap: Map<number, ProjectStateModel>;
  setIssues: Dispatch<SetStateAction<ProjectIssueResponseData[]>>;
  onOpen: (issue: ProjectIssueResponseData) => void;
  showToast: (text: string, severity: 'success' | 'error') => void;
}

// 看板视图：DragDropContext 容器 + 横向铺列（固定列宽 + 横向滚动）。
// 分列用 useKanbanColumns（按 stateId），拖拽逻辑用 useKanbanDnd（与渲染解耦）。
// 注：看板展示全量 issues（不应用列表的筛选，避免破坏列结构与拖拽排序基准）。
function KanbanView({ issues, states, stateMap, setIssues, onOpen, showToast }: KanbanViewProps) {
  const { t } = useTranslation();
  const { columnsByState, orderedStates } = useKanbanColumns(issues, states);
  const onDragEnd = useKanbanDnd({
    columnsByState,
    stateMap,
    setIssues,
    showToast,
    moveFailedText: message => t('tracker:issue.toast.moveFailed', { message }),
  });

  if (states.length === 0) {
    return (
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <DragDropContext onDragEnd={onDragEnd}>
      <Box sx={{ display: 'flex', gap: 1.5, height: '100%', overflow: 'auto', p: 1.5 }}>
        {orderedStates.map(state => (
          <KanbanColumn
            key={state.id}
            state={state}
            issues={columnsByState.get(state.id) ?? []}
            onOpen={onOpen}
          />
        ))}
      </Box>
    </DragDropContext>
  );
}

export default KanbanView;
