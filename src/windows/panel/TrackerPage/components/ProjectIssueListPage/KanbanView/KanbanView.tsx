import type { ProjectIssueResponseData, ProjectStateModel } from '@src/services';
import type { Dispatch, SetStateAction } from 'react';
import type { SubtaskStats } from './shared';
import { DragDropContext } from '@hello-pangea/dnd';
import { Box, CircularProgress } from '@mui/material';
import { useTranslation } from 'react-i18next';
import KanbanColumn from './KanbanColumn';
import { useKanbanColumns } from './useKanbanColumns';
import { useKanbanDnd } from './useKanbanDnd';

interface KanbanViewProps {
  projectIssues: ProjectIssueResponseData[];
  projectStates: ProjectStateModel[];
  stateMap: Map<number, ProjectStateModel>;
  subtaskStats: SubtaskStats;
  setIssues: Dispatch<SetStateAction<ProjectIssueResponseData[]>>;
  onOpen: (projectIssue: ProjectIssueResponseData) => void;
  showToast: (text: string, severity: 'success' | 'error') => void;
}

// 看板视图：DragDropContext 容器 + 横向铺列（固定列宽 + 横向滚动）。
// 分列用 useKanbanColumns（按 stateId），拖拽逻辑用 useKanbanDnd（与渲染解耦）。
// 注：看板展示全量 projectIssues（不应用列表的筛选，避免破坏列结构与拖拽排序基准）。
function KanbanView({ projectIssues, projectStates, stateMap, subtaskStats, setIssues, onOpen, showToast }: KanbanViewProps) {
  const { t } = useTranslation();
  const { columnsByState, orderedStates } = useKanbanColumns(projectIssues, projectStates);
  const onDragEnd = useKanbanDnd({
    columnsByState,
    stateMap,
    setIssues,
    showToast,
    moveFailedText: message => t('tracker:projectIssue.toast.moveFailed', { message }),
  });

  if (projectStates.length === 0) {
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
            projectIssues={columnsByState.get(state.id) ?? []}
            subtaskStats={subtaskStats}
            onOpen={onOpen}
          />
        ))}
      </Box>
    </DragDropContext>
  );
}

export default KanbanView;
