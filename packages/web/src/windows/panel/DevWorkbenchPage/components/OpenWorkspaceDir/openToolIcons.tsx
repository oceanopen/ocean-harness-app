import { SiIterm2 } from '@icons-pack/react-simple-icons';
// 「打开工作区目录」工具图标组件：统一 size prop（默认 16px，与标题栏图标同档），
// currentColor 跟随主题。独立成文件且只导出组件——工具目录（openTools.tsx）为纯
// 数据/函数导出，混入组件定义会破坏 react-refresh 边界（同文件不得混导组件与非组件）。
import { Terminal as TerminalAppMuiIcon } from '@mui/icons-material';
import finderIconSvg from '@src/assets/finder.svg?raw';
import vscodeIconSvg from '@src/assets/vscode.svg?raw';
import windowsTerminalIconSvg from '@src/assets/windows-terminal.svg?raw';

export interface ToolIconProps {
  size?: number;
}

// raw SVG 资产注入的通用底座（不导出）：?raw 保留 currentColor；span 定尺寸，svg 无
// 宽高属性随容器铺满。导出项保持函数声明薄包装（工厂 const 形式不被 react-refresh
// 识别为组件，勿改）。
function RawSvgIcon({ svg, size = 16 }: { svg: string } & ToolIconProps) {
  return (
    <span
      style={{ display: 'inline-flex', width: size, height: size }}
      // eslint-disable-next-line react/dom-no-dangerously-set-innerhtml -- 注入项目内静态 SVG 字符串，非外部输入，无 XSS 风险（vscode.svg 同款先例）
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  );
}

// Finder（macOS 系统文件管理器，自绘单色简化脸型图标）。
export function FinderIcon(props: ToolIconProps) {
  return <RawSvgIcon svg={finderIconSvg} {...props} />;
}

// VSCode（官方单色品牌图标，RepositoryCard/ClaudeSessionCard 同款资产）。
export function VsCodeIcon(props: ToolIconProps) {
  return <RawSvgIcon svg={vscodeIconSvg} {...props} />;
}

// Windows Terminal（自绘单色简化四格窗图标，仅 Windows 平台展示）。
export function WindowsTerminalIcon(props: ToolIconProps) {
  return <RawSvgIcon svg={windowsTerminalIconSvg} {...props} />;
}

// iTerm2（品牌图标库单色条目）。
export function ITerm2Icon({ size = 16 }: ToolIconProps) {
  return <SiIterm2 size={size} color="currentColor" />;
}

// Terminal.app：无品牌图标库条目，用 MUI 终端字形（>_ 提示符）示意。
export function TerminalAppIcon({ size = 16 }: ToolIconProps) {
  return <TerminalAppMuiIcon sx={{ fontSize: size }} />;
}
