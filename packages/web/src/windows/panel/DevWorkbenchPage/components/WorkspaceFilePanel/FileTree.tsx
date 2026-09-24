import type { IssueWorkspaceGitChangeFile } from '@src/services';
import type { FileTreeNode } from './buildFileTree';
import { ChevronRight as ChevronRightIcon } from '@mui/icons-material';
import { Box, Collapse, Typography } from '@mui/material';
import { extOf } from '../fileViewer/viewerKind';
import { DefaultFileIcon, FILE_ICONS, FolderClosedIcon, FolderOpenedIcon } from './fileIcon';

/// vscode-icons 系图标（unplugin-icons 虚拟模块组件，svg 尺寸 1em——scale 见 vite.config.ts）：
/// 经 fontSize 控制为 16px 与 caret 对齐；flexShrink 防长文件名挤压（对齐 MUI SvgIcon 默认行为）。
const iconStyle = { fontSize: 16, flexShrink: 0 } as const;

/// 变更状态标记的展示配色（Git 变更模式行尾徽标）：A/U=新增绿、M=修改琥珀、D=删除红。
const GIT_STATUS_META: Record<IssueWorkspaceGitChangeFile['status'], { label: string; color: string }> = {
  A: { label: 'A', color: 'success.main' },
  U: { label: 'N', color: 'success.main' },
  M: { label: 'M', color: 'warning.main' },
  D: { label: 'D', color: 'error.main' },
};

interface FileTreeNodeRowProps {
  entry: FileTreeNode;
  depth: number;
  /// 展开目录集（域 store，消费方回落默认集）。
  expandedDirs: ReadonlySet<string>;
  /// 已打开预览 tab 的文件路径集（行高亮；tab id = 文件 path）。
  openPaths: ReadonlySet<string>;
  /// 激活预览 tab 的文件 path（两级选中态的强高亮档；无激活 tab 时为 undefined）。
  activePath?: string;
  /// Git 变更模式的行尾标记表（path → 变更条目）；undefined = 全部文件模式（不渲染标记）。
  marks?: ReadonlyMap<string, IssueWorkspaceGitChangeFile>;
  onToggleDir: (dirPath: string) => void;
  onOpenFile: (path: string) => void;
}

/// 单个树节点行：缩进 depth×16 + caret/图标 + 名称。目录行 click 切换展开（caret 随
/// 展开旋转 90°，folder 图标开合两态）；文件行 click 打开预览 tab（浮层联动），图标按
/// 扩展名分派。选中态两级：激活 tab 行底色+主色文字，已打开非激活行仅主色文字；
/// hover 一档灰底（均带 4px 圆角）。dotfile（. 名前缀，含目录）整行降透明度弱化。
/// Git 变更模式（marks 提供）：行尾追加状态字母徽标 + `+N/-N` 行数（binary 无数字）。
function FileTreeNodeRow({ entry, depth, expandedDirs, openPaths, activePath, marks, onToggleDir, onOpenFile }: FileTreeNodeRowProps) {
  const { node, children } = entry;
  const expanded = expandedDirs.has(node.path);
  const active = !node.isDir && node.path === activePath;
  const opened = !node.isDir && openPaths.has(node.path);
  const dimmed = node.name.startsWith('.');
  // 文件图标组件：record 纯引用查找（形态理据见 fileIcon.ts 头注释）；目录行不消费。
  const FileIcon = FILE_ICONS[extOf(node.path)] ?? DefaultFileIcon;
  const mark = !node.isDir ? marks?.get(node.path) : undefined;
  const markMeta = mark != null ? GIT_STATUS_META[mark.status] : undefined;

  return (
    <Box>
      <Box
        onClick={() => (node.isDir ? onToggleDir(node.path) : onOpenFile(node.path))}
        sx={{
          'display': 'flex',
          'alignItems': 'center',
          'gap': 0.5,
          'pl': 0.5 + depth * 2,
          'py': 0.25,
          'pr': 1,
          'cursor': 'pointer',
          'borderRadius': 0.5,
          'color': active || opened ? 'primary.main' : 'text.primary',
          'bgcolor': active ? 'action.selected' : 'transparent',
          'opacity': dimmed ? 0.6 : 1,
          // hover 不降级激活行的选中底色（action.hover 比 action.selected 更淡）。
          '&:hover': { bgcolor: active ? 'action.selected' : 'action.hover' },
        }}
      >
        {node.isDir
          ? (
              <ChevronRightIcon
                sx={{
                  fontSize: 16,
                  color: 'text.secondary',
                  transition: theme => theme.transitions.create('transform'),
                  transform: expanded ? 'rotate(90deg)' : 'none',
                }}
              />
            )
          : <Box sx={{ width: 16, flexShrink: 0 }} />}
        {node.isDir
          ? (
              expanded
                ? <FolderOpenedIcon style={iconStyle} />
                : <FolderClosedIcon style={iconStyle} />
            )
          : <FileIcon style={iconStyle} />}
        <Typography variant="body2" noWrap title={node.path} sx={{ minWidth: 0 }}>
          {node.name}
        </Typography>
        {mark != null && markMeta != null && (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, ml: 'auto', flexShrink: 0 }}>
            {mark.ins >= 0 && mark.del >= 0 && (
              <Typography variant="caption" component="span" noWrap sx={{ lineHeight: 1 }}>
                <Box component="span" sx={{ color: 'success.main' }}>+{mark.ins}</Box>
                {' / '}
                <Box component="span" sx={{ color: 'error.main' }}>-{mark.del}</Box>
              </Typography>
            )}
            <Typography variant="caption" component="span" sx={{ color: markMeta.color, fontWeight: 600, lineHeight: 1 }}>
              {markMeta.label}
            </Typography>
          </Box>
        )}
      </Box>
      {node.isDir && (
        <Collapse in={expanded} unmountOnExit sx={{ width: '100%' }}>
          {children.map(child => (
            <FileTreeNodeRow
              key={child.node.path}
              entry={child}
              depth={depth + 1}
              expandedDirs={expandedDirs}
              openPaths={openPaths}
              activePath={activePath}
              marks={marks}
              onToggleDir={onToggleDir}
              onOpenFile={onOpenFile}
            />
          ))}
        </Collapse>
      )}
    </Box>
  );
}

interface FileTreeProps {
  roots: FileTreeNode[];
  expandedDirs: ReadonlySet<string>;
  openPaths: ReadonlySet<string>;
  /// 激活预览 tab 的文件 path（两级选中态的强高亮档；无激活 tab 时为 undefined）。
  activePath?: string;
  /// Git 变更模式的行尾标记表（path → 变更条目）；不传 = 全部文件模式。
  marks?: ReadonlyMap<string, IssueWorkspaceGitChangeFile>;
  onToggleDir: (dirPath: string) => void;
  onOpenFile: (path: string) => void;
}

/// 工作空间文件树（自绘递归，不引 @mui/x-tree-view——全树已在手、无异步子节点/拖拽，
/// 递归渲染约百行且零集成 hack）。折叠分支 unmountOnExit 不进 DOM，渲染层无压力。
export default function FileTree({ roots, expandedDirs, openPaths, activePath, marks, onToggleDir, onOpenFile }: FileTreeProps) {
  return (
    <Box sx={{ flex: 1, minHeight: 0, overflow: 'auto', py: 0.5 }}>
      {roots.map(root => (
        <FileTreeNodeRow
          key={root.node.path}
          entry={root}
          depth={0}
          expandedDirs={expandedDirs}
          openPaths={openPaths}
          activePath={activePath}
          marks={marks}
          onToggleDir={onToggleDir}
          onOpenFile={onOpenFile}
        />
      ))}
    </Box>
  );
}
