import type { SvgIconProps } from '@mui/material/SvgIcon';
import { SvgIcon } from '@mui/material';

/**
 * Chrome 品牌图标（SVG 内联组件，currentColor 着色）：任务卡片浏览器会话徽标用。
 * 路径取自 Simple Icons googlechrome（viewBox 0 0 24 24，evenodd 单色形）。
 */
export default function ChromeIcon(props: SvgIconProps) {
  return (
    <SvgIcon viewBox="0 0 24 24" {...props}>
      <path d="M12 0C8.21 0 4.831 1.757 2.632 4.501l3.953 6.848A5.454 5.454 0 0 1 12 6.545h10.691A12 12 0 0 0 12 0zM1.931 5.47A11.943 11.943 0 0 0 0 12c0 6.012 4.42 10.991 10.189 11.864l3.953-6.847a5.45 5.45 0 0 1-6.865-2.29zm13.342 2.166a5.446 5.446 0 0 1 1.45 7.09l.002.001h-.002l-5.344 9.257c.206.01.413.016.621.016 6.627 0 12-5.373 12-12 0-1.54-.29-3.011-.818-4.364zM12 16.364a4.364 4.364 0 1 1 0-8.728 4.364 4.364 0 0 1 0 8.728Z" />
    </SvgIcon>
  );
}
