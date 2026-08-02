import type { ProjectIssueResponseData, ProjectStateModel } from '@src/services';
import type { Dispatch, SetStateAction } from 'react';
import type { SubtaskStats } from '../shared';
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
  childrenByParent: Map<number, ProjectIssueResponseData[]>;
  expandedParents: Set<number>;
  setIssues: Dispatch<SetStateAction<ProjectIssueResponseData[]>>;
  onAddIssue: (stateId: number) => void;
  onEdit: (projectIssue: ProjectIssueResponseData) => void;
  onAddChild: (parent: ProjectIssueResponseData) => void;
  onToggleExpand: (id: number) => void;
  showToast: (text: string, severity: 'success' | 'error') => void;
}

// 看板视图：DragDropContext 容器 + 横向铺列（固定列宽 + 横向滚动）。
// 分列用 useKanbanColumns（按 stateId），拖拽逻辑用 useKanbanDnd（与渲染解耦）。
// 注：看板展示全量 projectIssues（不应用列表的筛选，避免破坏列结构与拖拽排序基准）。
// 卡片复用统一 IssueCard（variant=kanban），交互回调与列表同源。
function KanbanView({ projectIssues, projectStates, stateMap, subtaskStats, childrenByParent, expandedParents, setIssues, onAddIssue, onEdit, onAddChild, onToggleExpand, showToast }: KanbanViewProps) {
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
            stateMap={stateMap}
            subtaskStats={subtaskStats}
            childrenByParent={childrenByParent}
            expandedParents={expandedParents}
            onAddIssue={onAddIssue}
            onEdit={onEdit}
            onAddChild={onAddChild}
            onToggleExpand={onToggleExpand}
          />
        ))}
      </Box>
    </DragDropContext>
  );
}

export default KanbanView;
