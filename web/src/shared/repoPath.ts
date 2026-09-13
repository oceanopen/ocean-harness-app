// 本地仓库路径工具：仓库目录与项目子目录的拼接、相对路径计算、basename 提取。
// 表单（子目录剥离前缀）与卡片（拼接后打开目录）共用，避免多处重复字符串处理。

/**
 * 去掉路径**结尾**的路径分隔符（`/` 或 `\`），**保留前导分隔符**。
 * 用于绝对仓库目录：Unix 绝对路径以 `/` 开头，前导分隔符不能丢，否则 `/Users/a` 被削成 `Users/a`
 * 导致前缀匹配失败、拼接出非绝对路径。
 */
function trimTrailingSeparators(p: string): string {
  return p.replace(/[/\\]+$/g, '');
}

/**
 * 去掉相对路径**首尾**的路径分隔符（`/` 或 `\`）。
 * 用于项目子目录：须为相对路径，前导 `/` 会让后端 `Path::join` 用其整体替换 base（拼接出错）。
 */
function normalizeRelativePath(p: string): string {
  return p.replace(/^[/\\]+|[/\\]+$/g, '');
}

/** 取路径最后一段（目录名）。如 `~/Project/MUMBLEFE/UMS-INFOMWEB` → `UMS-INFOMWEB`。 */
export function basename(p: string): string {
  const trimmed = p.replace(/[/\\]+$/, '');
  if (!trimmed) {
    return '';
  }
  const segs = trimmed.split(/[/\\]/);
  return segs[segs.length - 1] ?? '';
}

/**
 * 拼接仓库目录与项目子目录，得到实际系统目录。子目录为空时返回仓库目录原值。
 * 仓库目录仅去尾分隔符（保留前导 `/`），子目录去首尾分隔符后用 `/` 拼接
 * （macOS 原生；Windows 的 explorer/code/open 同样接受 `/`）。
 */
export function joinRepoDir(dir: string, subDir: string): string {
  const sd = normalizeRelativePath(subDir.trim());
  if (!sd) {
    return dir;
  }
  return `${trimTrailingSeparators(dir)}/${sd}`;
}

/**
 * 计算所选目录相对仓库目录的子目录路径（用于表单「项目子目录」字段）。
 * - 所选即仓库目录本身 → 返回 `''`（无子目录）
 * - 所选位于仓库目录之下 → 返回相对路径（分隔符统一为 `/`），如 `packages/ums_infomweb_web_infom`
 * - 所选不在仓库目录之下 → 返回 `null`（调用方据此提示「子目录必须位于仓库目录之下」）
 */
export function relativeSubDir(repoDir: string, selected: string): string | null {
  const root = trimTrailingSeparators(repoDir.trim());
  const sel = selected.trim();
  if (!root || !sel) {
    return null;
  }
  if (trimTrailingSeparators(sel) === root) {
    return '';
  }
  // 严格前缀匹配：必须 root + 分隔符之后才算子目录（避免 /a/b 误匹配 /a/bbb）。
  if (sel.startsWith(`${root}/`) || sel.startsWith(`${root}\\`)) {
    return normalizeRelativePath(sel.slice(root.length + 1)).replace(/\\/g, '/');
  }
  return null;
}
