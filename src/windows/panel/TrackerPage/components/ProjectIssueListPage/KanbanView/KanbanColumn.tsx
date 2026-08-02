import type { ProjectIssueResponseData, ProjectStateModel } from '@src/services';
import type { SubtaskStats } from '../shared';
import { Draggable, Droppable } from '@hello-pangea/dnd';
import { Box, Chip, Paper, Typography } from '@mui/material';
import { stateDisplayName } from '@src/windows/panel/TrackerPage/components/stateDisplayName';
import IssueCard from '../IssueCard';
import { truncateSx } from '../shared';

interface KanbanColumnProps {
  state: ProjectStateModel;
  projectIssues: ProjectIssueResponseData[];
  stateMap: Map<number, ProjectStateModel>;
  subtaskStats: SubtaskStats;
  childrenByParent: Map<number, ProjectIssueResponseData[]>;
  expandedParents: Set<number>;
  onEdit: (projectIssue: ProjectIssueResponseData) => void;
  onAddChild: (parent: ProjectIssueResponseData) => void;
  onToggleExpand: (id: number) => void;
}

// 看板列（Droppable）：状态色点 + 名称 + 计数；卡片列表纵向可滚；拖入时背景高亮。
// 列内卡片复用统一 IssueCard（variant=kanban），由 Draggable 注入 dnd 透传。
function KanbanColumn({ state, projectIssues, stateMap, subtaskStats, childrenByParent, expandedParents, onEdit, onAddChild, onToggleExpand }: KanbanColumnProps) {
  return (
    <Droppable droppableId={String(state.id)}>
      {(provided, snapshot) => (
        <Paper
          ref={provided.innerRef}
          {...provided.droppableProps}
          variant="outlined"
          sx={{
            width: 280,
            flexShrink: 0,
            display: 'flex',
            flexDirection: 'column',
            maxHeight: '100%',
            bgcolor: snapshot.isDraggingOver ? 'action.hover' : 'background.default',
          }}
        >
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, px: 1.5, py: 1 }}>
            <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: state.color, flexShrink: 0 }} />
            <Typography variant="subtitle2" sx={{ flex: 1, fontWeight: 600, ...truncateSx }}>{stateDisplayName(state.name)}</Typography>
            <Chip label={projectIssues.length} size="small" sx={{ height: 18, fontSize: '0.7rem' }} />
          </Box>
          <Box sx={{ flex: 1, overflow: 'auto', p: 1 }}>
            {projectIssues.map((projectIssue, index) => (
              <Draggable key={projectIssue.id} draggableId={String(projectIssue.id)} index={index}>
                {(dragProvided, dragSnapshot) => (
                  <IssueCard
                    issue={projectIssue}
                    variant="kanban"
                    stateMap={stateMap}
                    subtaskStats={subtaskStats}
                    childIssues={childrenByParent.get(projectIssue.id) ?? []}
                    expanded={expandedParents.has(projectIssue.id)}
                    onToggleExpand={onToggleExpand}
                    onEdit={onEdit}
                    onAddChild={onAddChild}
                    dnd={{ provided: dragProvided, snapshot: dragSnapshot }}
                  />
                )}
              </Draggable>
            ))}
            {provided.placeholder}
          </Box>
        </Paper>
      )}
    </Droppable>
  );
}

export default KanbanColumn;
