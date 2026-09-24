// Git 变更文件表 → 嵌套树（纯函数，无副作用——getGitChanges 返回多仓库扁平变更表，
// 本函数补全中间目录节点后组树，复用 buildFileTree 的 FileTreeNode 形状与排序规则，
// 供 FileTree 同一套渲染）。变更树目录默认全展开（树通常很小，逐层点开无意义）。
import type { IssueWorkspaceFileNode, IssueWorkspaceGitChangeFile } from '@src/services';
import type { FileTreeNode } from './buildFileTree';
import { buildFileTree } from './buildFileTree';

/// 由变更文件路径列表构建渲染树：先把每个文件路径展开为「中间目录 + 文件」的扁平节点表
/// （目录去重），再复用 buildFileTree 组树排序。size 对变更树无意义，恒 0。
export function buildGitChangesTree(files: IssueWorkspaceGitChangeFile[]): FileTreeNode[] {
  const seen = new Set<string>();
  const nodes: IssueWorkspaceFileNode[] = [];
  const pushDir = (path: string) => {
    if (path !== '' && !seen.has(path)) {
      seen.add(path);
      nodes.push({ path, name: path.slice(path.lastIndexOf('/') + 1), isDir: true, size: 0 });
    }
  };
  for (const f of files) {
    const slash = f.path.lastIndexOf('/');
    if (slash >= 0) {
      // 逐级补全中间目录（a/b/c.ts → a、a/b）。
      const segments = f.path.split('/');
      for (let i = 1; i < segments.length; i++) {
        pushDir(segments.slice(0, i).join('/'));
      }
    }
    if (!seen.has(f.path)) {
      seen.add(f.path);
      nodes.push({ path: f.path, name: f.path.slice(slash + 1), isDir: false, size: 0 });
    }
  }
  return buildFileTree(nodes);
}

/// 变更树的展开目录全集（目录默认全展开——与文件树的 toggle 展开态不同源：变更树小且
/// 逐次刷新重建，不接 expandedDirs 持久化，直接以全展开集渲染）。
export function allDirsExpanded(roots: FileTreeNode[]): Set<string> {
  const dirs = new Set<string>();
  const walk = (list: FileTreeNode[]) => {
    for (const t of list) {
      if (t.node.isDir) {
        dirs.add(t.node.path);
        walk(t.children);
      }
    }
  };
  walk(roots);
  return dirs;
}
