// 任务工作目录路径派生：{工作空间目录}/{issueId}。终端 cwd 与打开目录按钮共用，
// 与服务端 filepath.Join(baseDir, issueId) 同构。
export function issueWorkspacePath(workspaceDir: string, issueId: string): string {
  return `${workspaceDir}/${issueId}`;
}
