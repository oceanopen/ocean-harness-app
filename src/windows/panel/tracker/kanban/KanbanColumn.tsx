import type { Issue, ProjectState } from '../IssueListPage';
import { Droppable } from '@hello-pangea/dnd';
import { Box, Chip, Paper, Typography } from '@mui/material';
import KanbanCard from './KanbanCard';
import { truncateSx } from './shared';

interface KanbanColumnProps {
  state: ProjectState;
  issues: Issue[];
  onOpen: (issue: Issue) => void;
}

// 看板列（Droppable）：状态色点 + 名称 + 计数；卡片列表纵向可滚；拖入时背景高亮。
function KanbanColumn({ state, issues, onOpen }: KanbanColumnProps) {
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
            <Typography variant="subtitle2" sx={{ flex: 1, fontWeight: 600, ...truncateSx }}>{state.name}</Typography>
            <Chip label={issues.length} size="small" sx={{ height: 18, fontSize: '0.7rem' }} />
          </Box>
          <Box sx={{ flex: 1, overflow: 'auto', p: 1 }}>
            {issues.map((issue, index) => (
              <KanbanCard key={issue.id} issue={issue} index={index} onOpen={onOpen} />
            ))}
            {provided.placeholder}
          </Box>
        </Paper>
      )}
    </Droppable>
  );
}

export default KanbanColumn;
