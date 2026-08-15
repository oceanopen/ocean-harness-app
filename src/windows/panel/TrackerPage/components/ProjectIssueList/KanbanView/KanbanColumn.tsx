import type { ProjectIssueResponseData } from '@src/services';
import type { StateCode } from '@src/state/tracker';
import type { SubtaskStats } from '../shared';
import { Draggable, Droppable } from '@hello-pangea/dnd';
import { Box, Paper } from '@mui/material';
import { STATE_MAP } from '@src/state/tracker';
import IssueCard from '../IssueCard';
import StateGroupCard from '../StateGroupCard';

interface KanbanColumnProps {
  stateCode: StateCode;
  projectIssues: ProjectIssueResponseData[];
  subtaskStats: SubtaskStats;
  childrenByParent: Map<string, ProjectIssueResponseData[]>;
  expandedParents: Set<string>;
  onAdd: () => void;
  onEdit: (projectIssue: ProjectIssueResponseData) => void;
  onAddChild: (parent: ProjectIssueResponseData) => void;
  onToggleExpand: (id: string) => void;
}

// 看板列（Droppable，列 = 状态）：列头复用 StateGroupCard（状态色点+名称+计数+新增icon）；
// 卡片列表纵向可滚；拖入时背景高亮。列内卡片复用统一 IssueCard，由 Draggable 注入 dnd 透传。
function KanbanColumn({ stateCode, projectIssues, subtaskStats, childrenByParent, expandedParents, onAdd, onEdit, onAddChild, onToggleExpand }: KanbanColumnProps) {
  const meta = STATE_MAP.get(stateCode);
  return (
    <Droppable droppableId={stateCode}>
      {(provided, snapshot) => (
        <Paper
          ref={provided.innerRef}
          {...provided.droppableProps}
          variant="outlined"
          sx={{
            width: 360,
            flexShrink: 0,
            display: 'flex',
            flexDirection: 'column',
            maxHeight: '100%',
            bgcolor: snapshot.isDraggingOver ? 'action.hover' : 'background.default',
          }}
        >
          <Box sx={{ px: 1.5, py: 1 }}>
            <StateGroupCard
              color={meta?.color}
              name={meta?.name ?? stateCode}
              count={projectIssues.length}
              onAdd={onAdd}
            />
          </Box>
          <Box sx={{ flex: 1, overflow: 'auto', p: 1 }}>
            {projectIssues.map((projectIssue, index) => (
              <Draggable key={projectIssue.id} draggableId={projectIssue.id} index={index}>
                {(dragProvided, dragSnapshot) => (
                  <IssueCard
                    issue={projectIssue}
                    subtaskStats={subtaskStats}
                    childIssues={childrenByParent.get(projectIssue.id) ?? []}
                    expanded={expandedParents.has(projectIssue.id)}
                    onToggleExpand={onToggleExpand}
                    onEdit={onEdit}
                    onAddChild={onAddChild}
                    kanban
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
