import type { ProjectIssueResponseData } from '@src/services';
import { Draggable } from '@hello-pangea/dnd';
import { CalendarMonthOutlined as CalendarMonthOutlinedIcon } from '@mui/icons-material';
import { Box, Paper, Tooltip, Typography } from '@mui/material';
import { formatDate } from '@src/shared/time';
import { PriorityIcon } from '@src/windows/panel/TrackerPage/components/PriorityIcon';
import { truncateSx } from './shared';

interface KanbanCardProps {
  projectIssue: ProjectIssueResponseData;
  index: number;
  onOpen: (projectIssue: ProjectIssueResponseData) => void;
}

// 看板卡片：优先级图标 + #id + 标题（截断）+ 标签色块 + 目标日期。
// 整卡作为拖拽手柄（dragHandleProps 绑整卡）；纯点击（无拖动）打开详情。
function KanbanCard({ projectIssue, index, onOpen }: KanbanCardProps) {
  return (
    <Draggable draggableId={String(projectIssue.id)} index={index}>
      {(provided, snapshot) => (
        <Paper
          ref={provided.innerRef}
          {...provided.draggableProps}
          {...provided.dragHandleProps}
          onClick={() => onOpen(projectIssue)}
          sx={{
            p: 1,
            mb: 0.75,
            cursor: 'pointer',
            boxShadow: snapshot.isDragging ? 3 : 1,
            opacity: snapshot.isDragging ? 0.95 : 1,
          }}
        >
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mb: 0.25 }}>
            <PriorityIcon priority={projectIssue.priority} />
            <Typography variant="caption" color="text.disabled">#{projectIssue.id}</Typography>
          </Box>
          <Typography variant="body2" sx={truncateSx} title={projectIssue.name}>
            {projectIssue.name}
          </Typography>
          {projectIssue.labels.length > 0 && (
            <Box sx={{ display: 'flex', gap: 0.25, mt: 0.5, flexWrap: 'wrap' }}>
              {projectIssue.labels.map(l => (
                <Tooltip key={l.id} title={l.name}>
                  <Box sx={{ width: 24, height: 4, borderRadius: 1, bgcolor: l.color }} />
                </Tooltip>
              ))}
            </Box>
          )}
          {projectIssue.targetDate && (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mt: 0.5 }}>
              <CalendarMonthOutlinedIcon sx={{ fontSize: '0.9rem', color: 'text.disabled' }} />
              <Typography variant="caption" color="text.disabled">
                {formatDate(projectIssue.targetDate, 'YYYY-MM-DD')}
              </Typography>
            </Box>
          )}
        </Paper>
      )}
    </Draggable>
  );
}

export default KanbanCard;
