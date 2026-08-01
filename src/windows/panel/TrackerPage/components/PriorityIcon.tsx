import type { Priority } from '@src/services';
import {
  DragHandleRounded as DragHandleRoundedIcon,
  KeyboardArrowDownRounded as KeyboardArrowDownRoundedIcon,
  KeyboardArrowUpRounded as KeyboardArrowUpRoundedIcon,
  KeyboardDoubleArrowUpRounded as KeyboardDoubleArrowUpRoundedIcon,
  RemoveOutlined as RemoveOutlinedIcon,
} from '@mui/icons-material';

// 优先级图标：urgent 双上箭头(红) / high 上箭头(橙) / medium 横杠(蓝) / low 下箭头(灰) / none 减号(浅灰)。
// tracker 域跨视图共享（列表行 + 看板卡片），从 ProjectIssueListPage 抽出独立成文件，
// 避免看板卡片反向跨层引用父页面导出（破坏组件树单向依赖）。
export function PriorityIcon({ priority }: { priority: Priority }) {
  switch (priority) {
    case 'urgent':
      return <KeyboardDoubleArrowUpRoundedIcon sx={{ fontSize: '1rem', color: 'error.main' }} />;
    case 'high':
      return <KeyboardArrowUpRoundedIcon sx={{ fontSize: '1rem', color: 'warning.main' }} />;
    case 'medium':
      return <DragHandleRoundedIcon sx={{ fontSize: '1rem', color: 'info.main' }} />;
    case 'low':
      return <KeyboardArrowDownRoundedIcon sx={{ fontSize: '1rem', color: 'text.secondary' }} />;
    default:
      return <RemoveOutlinedIcon sx={{ fontSize: '1rem', color: 'text.disabled' }} />;
  }
}
