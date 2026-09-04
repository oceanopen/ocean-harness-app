import type { FileTreeNode } from './buildFileTree';
import {
  ChevronRight as ChevronRightIcon,
  FolderOutlined as FolderOutlinedIcon,
  InsertDriveFileOutlined as InsertDriveFileOutlinedIcon,
} from '@mui/icons-material';
import { Box, Collapse, Typography } from '@mui/material';

interface FileTreeNodeRowProps {
  entry: FileTreeNode;
  depth: number;
  /// 展开目录集（域 store，消费方回落默认集）。
  expandedDirs: ReadonlySet<string>;
  /// 已打开预览 tab 的文件路径集（行高亮；tab id = 文件 path）。
  openPaths: ReadonlySet<string>;
  onToggleDir: (dirPath: string) => void;
  onOpenFile: (path: string) => void;
}

/// 单个树节点行：缩进 depth×16 + caret/图标 + 名称。目录行 click 切换展开（caret 随
/// 展开旋转 90°）；文件行 click 打开预览 tab（浮层联动）。已打开预览的文件行高亮。
function FileTreeNodeRow({ entry, depth, expandedDirs, openPaths, onToggleDir, onOpenFile }: FileTreeNodeRowProps) {
  const { node, children } = entry;
  const expanded = expandedDirs.has(node.path);
  const opened = !node.isDir && openPaths.has(node.path);

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
          'color': opened ? 'primary.main' : 'text.primary',
          'bgcolor': opened ? 'action.selected' : 'transparent',
          '&:hover': { bgcolor: 'action.hover' },
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
          ? <FolderOutlinedIcon sx={{ fontSize: 18, color: 'text.secondary' }} />
          : <InsertDriveFileOutlinedIcon sx={{ fontSize: 18, color: 'text.secondary' }} />}
        <Typography variant="body2" noWrap title={node.path} sx={{ minWidth: 0 }}>
          {node.name}
        </Typography>
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
  onToggleDir: (dirPath: string) => void;
  onOpenFile: (path: string) => void;
}

/// 工作空间文件树（自绘递归，不引 @mui/x-tree-view——全树已在手、无异步子节点/拖拽，
/// 递归渲染约百行且零集成 hack）。折叠分支 unmountOnExit 不进 DOM，渲染层无压力。
export default function FileTree({ roots, expandedDirs, openPaths, onToggleDir, onOpenFile }: FileTreeProps) {
  return (
    <Box sx={{ flex: 1, minHeight: 0, overflow: 'auto', py: 0.5 }}>
      {roots.map(root => (
        <FileTreeNodeRow
          key={root.node.path}
          entry={root}
          depth={0}
          expandedDirs={expandedDirs}
          openPaths={openPaths}
          onToggleDir={onToggleDir}
          onOpenFile={onOpenFile}
        />
      ))}
    </Box>
  );
}
