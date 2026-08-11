// 前端 worktree 路径派生工具。
//
// ⚠️ 必须与后端 src-server/internal/service/issue_worktree.go（IssueWorktree.CreateWorktree，
// 约 72-82 行）的派生逻辑保持一致：
//   repoName    = gitutil.RepoNameFromRemoteURL(repo.RemoteURL)，为空回退 filepath.Base(repo.LocalDir)
//   worktreePath = filepath.Join(worktreeRoot, repoName, "workspace_{wid}-project_{pid}-issue_{iid}")
// 后端是真相源（git 写盘）；本工具仅用于「创建前」的 UI 预览。后端公式变更时须同步更新此处。

// 复刻 gitutil.RepoNameFromRemoteURL：trim → 剥 .git 后缀 → 按 / 与 : 切分取末段非空。
//   git@github.com:org/repo.git          → repo
//   https://github.com/org/repo.git       → repo
//   https://gitlab.com/group/sub/repo.git → repo（subgroup 取末段）
// 空串/无法识别返回 ''（调用方决定回退）。
export function repoNameFromRemoteUrl(remoteUrl: string): string {
  let s = remoteUrl.trim();
  if (s.endsWith('.git')) {
    s = s.slice(0, -4);
  }
  // 按 / 与 : 切分，丢弃空段（连续分隔符 / HTTPS 头的 // 产生的空段），取末段。
  const fields = s.split(/[/:]/).filter(Boolean);
  return fields.length > 0 ? fields[fields.length - 1] : '';
}

// 复刻 filepath.Base：取路径末段（先剥尾部斜杠）。仅用于 remote_url 缺失时对 localDir 的兜底。
export function basenameOfPath(path: string): string {
  let s = path.trim();
  while (s.length > 1 && s.endsWith('/')) {
    s = s.slice(0, -1);
  }
  const idx = s.lastIndexOf('/');
  return idx >= 0 ? s.slice(idx + 1) : s;
}

// 复刻 issue_worktree.go:78-82 的 filepath.Join(worktreeRoot, repoName, "workspace_...-issue_...")。
// worktreeRoot 尾部斜杠剥除（对齐 filepath.Join 的 clean 语义）；拼接用 '/'（darwin 路径分隔符）。
export function deriveWorktreePath(
  worktreeRoot: string,
  repoName: string,
  workspaceId: number,
  projectId: number,
  issueId: number,
): string {
  const root = worktreeRoot.replace(/\/+$/, '');
  const leaf = `workspace_${workspaceId}-project_${projectId}-issue_${issueId}`;
  return [root, repoName, leaf].join('/');
}
