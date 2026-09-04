// 扁平节点表 → 嵌套树（纯函数，无副作用——getFileTree 返回 WalkDir 词法序扁平表，
// 本函数按 path 前缀归类组树并排序：目录在前、同级名称 localeCompare 升序）。
import type { IssueWorkspaceFileNode } from '@src/services';

/// 树渲染节点：原始 node + 排序后的子节点。
export interface FileTreeNode {
  node: IssueWorkspaceFileNode;
  children: FileTreeNode[];
}

export function buildFileTree(nodes: IssueWorkspaceFileNode[]): FileTreeNode[] {
  // 全量建壳后再挂接：使函数对任意输入顺序稳定（WalkDir 已保证父先于子，此为防御）。
  const byPath = new Map<string, FileTreeNode>();
  for (const n of nodes) {
    byPath.set(n.path, { node: n, children: [] });
  }
  const roots: FileTreeNode[] = [];
  for (const n of nodes) {
    const treeNode = byPath.get(n.path)!;
    const slash = n.path.lastIndexOf('/');
    const parent = slash < 0 ? undefined : byPath.get(n.path.slice(0, slash));
    if (parent != null) {
      parent.children.push(treeNode);
    } else {
      roots.push(treeNode);
    }
  }
  sortTreeNodes(roots);
  return roots;
}

/// 递归排序：目录在前（产物浏览以目录导航为主），同级名称 localeCompare 升序。
function sortTreeNodes(list: FileTreeNode[]): void {
  list.sort((a, b) => (a.node.isDir === b.node.isDir
    ? a.node.name.localeCompare(b.node.name)
    : (a.node.isDir ? -1 : 1)));
  for (const t of list) {
    sortTreeNodes(t.children);
  }
}
