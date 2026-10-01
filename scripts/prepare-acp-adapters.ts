#!/usr/bin/env node
/**
 * prepare-acp-adapters.ts — 构建期安装 ACP adapter 到 Tauri resources staging
 *
 * 读内嵌 agent catalog（SSOT）的 enabled + npx-adapter 条目，npm install（--omit=dev）
 * 到 app/resources/acp-adapters/<id>/<version>/，随 bundle.resources 打进应用。运行期
 * sidecar EnsureVendored 检测受管目录缺失时从此处复制——终端用户机零 npm/零 registry
 * 依赖（公司网络无需代理），版本由构建期锁死 = catalog pin。
 *
 * 幂等：目标已存在且包版本匹配则跳过（秒回），新版本自动新增目录；安装走「临时目录 +
 * rename」原子落地（npm 中断不产生以目标名存在的半成品，bundle.resources 不打包残缺品）。
 * 需联网（开发机/CI）。
 */

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readdirSync, readFileSync, renameSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

/** agent catalog 条目（本脚本只消费这些字段）。 */
interface CatalogEntry {
  id: string;
  args: string[];
  enabled?: boolean;
  strategy?: string;
}

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const CATALOG_PATH = resolve(repoRoot, 'server/internal/agentcatalog/agent-catalog.json');
const STAGING_ROOT = resolve(repoRoot, 'app/resources/acp-adapters');

function fail(message: string): never {
  console.error(`prepare-acp-adapters: ${message}`);
  process.exit(1);
}

/** 从 catalog 条目 args 取包 spec（非 flag 项，形如 @scope/pkg@1.2.3）。 */
function adapterSpec(entry: CatalogEntry): string {
  const spec = entry.args.filter(item => !item.startsWith('-')).pop();
  if (!spec) {
    fail(`catalog 条目 ${entry.id} 无 npx 包 spec（args: ${JSON.stringify(entry.args)}）`);
  }
  return spec;
}

/** staging 是否已就绪（目标包 package.json 存在且 version 匹配）。 */
function stagingReady(dir: string, pkg: string, version: string): boolean {
  const pkgJsonPath = resolve(dir, 'node_modules', ...pkg.split('/'), 'package.json');
  if (!existsSync(pkgJsonPath)) {
    return false;
  }
  try {
    return JSON.parse(readFileSync(pkgJsonPath, 'utf8')).version === version;
  } catch {
    return false;
  }
}

/**
 * 清理同 id 下历史中断残留的 .<version>.tmp-* 临时目录（与运行期 EnsureVendored 的
 * 同名卫生规则对齐；当前 pid 的临时目录在本函数之后才创建，不受误伤）。
 */
function cleanStaleTmp(idDir: string): void {
  if (!existsSync(idDir)) {
    return;
  }
  for (const name of readdirSync(idDir)) {
    if (name.startsWith('.') && name.includes('.tmp-')) {
      rmSync(resolve(idDir, name), { recursive: true, force: true });
    }
  }
}

const catalog = JSON.parse(readFileSync(CATALOG_PATH, 'utf8')) as { agents?: CatalogEntry[] };
const entries = (catalog.agents ?? []).filter(
  entry => entry.enabled && entry.strategy === 'npx-adapter',
);
if (entries.length === 0) {
  console.log('prepare-acp-adapters: 无 npx-adapter 启用条目，跳过');
  process.exit(0);
}

for (const entry of entries) {
  const spec = adapterSpec(entry);
  const at = spec.lastIndexOf('@');
  if (at <= 0) {
    fail(`包 spec 形态非法（应为 pkg@version）: ${spec}`);
  }
  const pkg = spec.slice(0, at);
  const version = spec.slice(at + 1);
  const dir = resolve(STAGING_ROOT, entry.id, version);

  if (stagingReady(dir, pkg, version)) {
    console.log(`prepare-acp-adapters: ${spec} 已就绪，跳过（${dir}）`);
    continue;
  }

  cleanStaleTmp(resolve(STAGING_ROOT, entry.id));
  // 原子落地：先装到 .<version>.tmp-<pid> 临时目录，校验通过后 rename 成目标名——
  // npm 中断（网络断/杀进程）留下的半成品不以目标名存在，不会被打包资源收编。
  const tmpDir = resolve(STAGING_ROOT, entry.id, `.${version}.tmp-${process.pid}`);
  rmSync(tmpDir, { recursive: true, force: true });
  mkdirSync(tmpDir, { recursive: true });
  writeFileSync(resolve(tmpDir, 'package.json'), `${JSON.stringify({ private: true })}\n`);

  console.log(`prepare-acp-adapters: npm install ${spec} → ${tmpDir}`);
  try {
    execFileSync('npm', ['install', '--omit=dev', '--no-audit', '--no-fund', spec], {
      cwd: tmpDir,
      stdio: 'inherit',
      shell: process.platform === 'win32',
    });
  } catch (err) {
    rmSync(tmpDir, { recursive: true, force: true });
    fail(`npm install ${spec} 失败: ${err instanceof Error ? err.message : String(err)}`);
  }
  if (!stagingReady(tmpDir, pkg, version)) {
    rmSync(tmpDir, { recursive: true, force: true });
    fail(`安装后校验失败：${tmpDir} 下未见 ${pkg}@${version}`);
  }
  rmSync(dir, { recursive: true, force: true });
  renameSync(tmpDir, dir);
}

console.log('prepare-acp-adapters: 完成');
