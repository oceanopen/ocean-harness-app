// git remote URL 解析 + PR compare URL 构造（纯函数，SSOT）。
// D3（生成 PR）用 buildCompareUrl 打开 GitHub/GitLab 风格 compare 页；未来真 PR 创建可复用 parseRemoteUrl。

/** 解析 git remote URL（SSH/HTTPS）为 host + ownerRepo。无法解析返回 null。 */
// SSH:   git@github.com:org/repo.git   → { host: 'github.com', ownerRepo: 'org/repo' }
// HTTPS: https://github.com/org/repo.git → 同上
export function parseRemoteUrl(remoteUrl: string): { host: string; ownerRepo: string } | null {
  if (!remoteUrl) {
    return null;
  }
  // SSH 形式：git@host:owner/repo(.git)?
  const ssh = remoteUrl.match(/^git@([^:]+):(.+?)(?:\.git)?$/);
  if (ssh) {
    return { host: ssh[1], ownerRepo: ssh[2] };
  }
  // HTTPS 形式：https://host/owner/repo(.git)?
  try {
    const u = new URL(remoteUrl);
    const ownerRepo = u.pathname.replace(/^\/+/, '').replace(/\.git$/, '');
    if (u.host && ownerRepo) {
      return { host: u.host, ownerRepo };
    }
  } catch {
    // remoteUrl 非 HTTPS，忽略
  }
  return null;
}

/** 构造 GitHub/GitLab 风格 compare URL：https://<host>/<ownerRepo>/compare/<base>...<head>。无法构造返回 null。 */
export function buildCompareUrl(remoteUrl: string, base: string, head: string): string | null {
  const parsed = parseRemoteUrl(remoteUrl);
  if (!parsed || !base || !head) {
    return null;
  }
  return `https://${parsed.host}/${parsed.ownerRepo}/compare/${base}...${head}`;
}
