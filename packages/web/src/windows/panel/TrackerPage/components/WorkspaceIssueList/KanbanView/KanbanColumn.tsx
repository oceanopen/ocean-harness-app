import type { WorkspaceIssueResponseData } from '@src/services';
import type { StateCode } from '@src/state/tracker';
import { Draggable, Droppable } from '@hello-pangea/dnd';
import { Box, Paper } from '@mui/material';
import IssueCard from '@src/components/issueCard/IssueCard';
import { STATE_MAP } from '@src/state/tracker';
import StateGroupCard from '../StateGroupCard';

interface KanbanColumnProps {
  stateCode: StateCode;
  workspaceIssues: WorkspaceIssueResponseData[];
  onAdd: () => void;
  onEdit: (workspaceIssue: WorkspaceIssueResponseData) => void;
}

// 看板列（Droppable，列 = 状态）：列头复用 StateGroupCard（状态色点+名称+计数+新增icon）；
// 卡片列表纵向可滚；拖入时背景高亮。列内卡片复用统一 IssueCard，由 Draggable 注入 dnd 透传。
function KanbanColumn({ stateCode, workspaceIssues, onAdd, onEdit }: KanbanColumnProps) {
  const meta = STATE_MAP.get(stateCode);
  return (
    <Droppable droppableId={stateCode}>
      {(provided, snapshot) => (
        <Paper
          ref={provided.innerRef}
          {...provided.droppableProps}
          variant="outlined"
          sx={{
            width: '360px',
            flexShrink: 0,
            display: 'flex',
            flexDirection: 'column',
            maxHeight: '100%',
            bgcolor: snapshot.isDraggingOver ? 'action.hover' : 'background.default',
          }}
        >
          <Box sx={{ px: '12px', py: '8px' }}>
            <StateGroupCard
              color={meta?.color}
              name={meta?.name ?? stateCode}
              count={workspaceIssues.length}
              onAdd={onAdd}
            />
          </Box>
          <Box sx={{ flex: 1, overflow: 'auto', p: '8px' }}>
            {workspaceIssues.map((workspaceIssue, index) => (
              <Draggable key={workspaceIssue.id} draggableId={workspaceIssue.id} index={index}>
                {(dragProvided, dragSnapshot) => (
                  <IssueCard
                    issue={workspaceIssue}
                    onEdit={onEdit}
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
