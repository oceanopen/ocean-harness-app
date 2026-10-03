#!/usr/bin/env node
/**
 * prepare-acp-adapters.ts — 构建期安装 ACP adapter 到 Tauri resources staging
 *
 * 读内嵌 agent catalog（SSOT）的 enabled + npx-adapter 条目，pnpm 安装到
 * app/resources/acp-adapters/<id>/<version>/，随 bundle.resources 打进应用。运行期
 * sidecar EnsureVendored 检测受管目录缺失时从此处复制——终端用户机零 npm/零 registry
 * 依赖（公司网络无需代理），版本由构建期锁死 = catalog pin。
 *
 * 安装形态（勿破坏）：staging 内置 .npmrc（node-linker=hoisted）+ pnpm --ignore-workspace，
 * 产出 npm 同构的扁平真实目录树——Go VendoredClaudeBin / Rust cli_bin 按扁平路径直取平台
 * 二进制（node_modules/@anthropic-ai/claude-agent-sdk-<triple>/<bin>），Tauri bundle.resources
 * 打包亦要求真实文件；pnpm 默认 isolated（symlink）布局会整链路打穿。
 *
 * staging package.json 两类明文依赖（否则 claude 版本不可见）：packageManager 取根
 * package.json SSOT 注入；claude SDK 安装后回写实际落地版本，并与根 devDependencies
 * 的声明做防漂移断言（catalog: 引用时解析 pnpm-workspace.yaml catalog 段 SSOT；
 * 不一致 fail，提示同步后重跑）。
 *
 * 幂等：目标已存在且包版本匹配则跳过（秒回），新版本自动新增目录；安装走「临时目录 +
 * rename」原子落地（安装中断不产生以目标名存在的半成品，bundle.resources 不打包残缺品）。
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

/** claude SDK 包名：adapter 的传递依赖，staging 明文化 + 根声明防漂移的校验对象。 */
const CLAUDE_SDK_PKG = '@anthropic-ai/claude-agent-sdk';

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const CATALOG_PATH = resolve(repoRoot, 'server/internal/agentcatalog/agent-catalog.json');
const STAGING_ROOT = resolve(repoRoot, 'app/resources/acp-adapters');

const rootPkg = JSON.parse(readFileSync(resolve(repoRoot, 'package.json'), 'utf8')) as {
  packageManager?: string;
  devDependencies?: Record<string, string>;
};

function fail(message: string): never {
  console.error(`prepare-acp-adapters: ${message}`);
  process.exit(1);
}

/** 根 packageManager（pnpm@<版本>）：staging 的 pnpm 版本声明 SSOT 在根，禁止第二份硬编码。 */
function packageManagerSpec(): string {
  const pm = rootPkg.packageManager;
  if (!pm?.startsWith('pnpm@')) {
    fail(`根 package.json packageManager 缺失或非 pnpm（实际：${pm ?? '无'}），staging 无法注入 pnpm 版本声明`);
  }
  return pm;
}

/** claude SDK 防漂移断言基准：version 为解析后的精确版本；fromCatalog 标记取自 workspace catalog 段（漂移提示分流用）。 */
interface SdkPin {
  version: string;
  fromCatalog: boolean;
}

const WORKSPACE_YAML = resolve(repoRoot, 'pnpm-workspace.yaml');

/** pnpm-workspace.yaml 顶层 catalog: 段内指定包的声明版本（单/双引号与裸值、行尾注释均兼容；段结束/缺失返回 null）。 */
function catalogVersionOf(pkg: string): string | null {
  const escaped = pkg.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const pattern = new RegExp(`^\\s+['"]?${escaped}['"]?:\\s*(['"]?)(.+?)\\1\\s*(?:#.*)?$`);
  let inCatalog = false;
  for (const line of readFileSync(WORKSPACE_YAML, 'utf8').split('\n')) {
    if (!inCatalog) {
      inCatalog = /^catalog:\s*(?:#.*)?$/.test(line);
      continue;
    }
    if (/^\S/.test(line)) {
      break; // 下一个顶层 key：catalog 段结束（后续缩进行属其他段，禁止误匹配）
    }
    const matched = line.match(pattern);
    if (matched) {
      return matched[2];
    }
  }
  return null;
}

/** 根 devDependencies 对 claude SDK 的声明版本（防漂移断言基准）；catalog: 协议引用时版本 SSOT 在 pnpm-workspace.yaml catalog 段，就地解析。 */
function rootClaudeSdkPin(): SdkPin {
  const pin = rootPkg.devDependencies?.[CLAUDE_SDK_PKG];
  if (!pin) {
    fail(`根 package.json devDependencies 缺 ${CLAUDE_SDK_PKG} 声明（catalog: 引用或字面精确版本均可；升级 adapter 时与其实际依赖同步）`);
  }
  if (!pin.startsWith('catalog:')) {
    return { version: pin, fromCatalog: false };
  }
  // TODO(named-catalog): catalog:<name> 指向 pnpm-workspace.yaml catalogs.<name> 命名段，当前不支持；用到时在此扩展对应段解析。
  if (pin !== 'catalog:') {
    fail(`暂不支持 named catalog 引用（${CLAUDE_SDK_PKG}@${pin}），防漂移基准无法解析——改回 catalog: 或字面精确版本`);
  }
  const version = catalogVersionOf(CLAUDE_SDK_PKG);
  if (!version) {
    fail(`根 devDependencies 声明 ${CLAUDE_SDK_PKG}@catalog: 但 pnpm-workspace.yaml 顶层 catalog 段无此条目——补精确版本条目或改回字面声明`);
  }
  return { version, fromCatalog: true };
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

/** 安装树内实际落地的 claude SDK 版本（读不到 = 安装树异常）。 */
function installedClaudeSdkVersion(dir: string): string | null {
  try {
    const pkgJsonPath = resolve(dir, 'node_modules', ...CLAUDE_SDK_PKG.split('/'), 'package.json');
    return JSON.parse(readFileSync(pkgJsonPath, 'utf8')).version ?? null;
  } catch {
    return null;
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

/** pnpm add（--ignore-workspace 隔离：staging 非根 workspace 成员，防向上吸附）。 */
function pnpmAdd(args: string[], cwd: string): void {
  execFileSync('pnpm', ['add', '--ignore-workspace', ...args], {
    cwd,
    stdio: 'inherit',
    shell: process.platform === 'win32',
  });
}

const catalog = JSON.parse(readFileSync(CATALOG_PATH, 'utf8')) as { agents?: CatalogEntry[] };
const entries = (catalog.agents ?? []).filter(
  entry => entry.enabled && entry.strategy === 'npx-adapter',
);
if (entries.length === 0) {
  console.log('prepare-acp-adapters: 无 npx-adapter 启用条目，跳过');
  process.exit(0);
}

const pmSpec = packageManagerSpec();
const sdkPin = rootClaudeSdkPin();
// .npmrc 随 staging 落地：人工 cd 进 staging 手动 pnpm install 也得到同构布局（可复现）。
const STAGING_NPMRC = [
  '# hoisted = npm 同构扁平真实目录树（无 symlink）：扁平路径供 Go/Rust 直取平台二进制，',
  '# Tauri bundle.resources 打包亦要求真实文件。禁改回默认 isolated（symlink）布局。',
  'node-linker=hoisted',
  '',
].join('\n');

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
  // 安装中断（网络断/杀进程）留下的半成品不以目标名存在，不会被打包资源收编。
  const tmpDir = resolve(STAGING_ROOT, entry.id, `.${version}.tmp-${process.pid}`);
  rmSync(tmpDir, { recursive: true, force: true });
  mkdirSync(tmpDir, { recursive: true });
  writeFileSync(
    resolve(tmpDir, 'package.json'),
    `${JSON.stringify({ private: true, packageManager: pmSpec }, null, 2)}\n`,
  );
  writeFileSync(resolve(tmpDir, '.npmrc'), STAGING_NPMRC);

  console.log(`prepare-acp-adapters: pnpm add ${spec} → ${tmpDir}`);
  try {
    // 第一段：装 adapter（pnpm save 精确版本——目录名/manifest/lockfile 三方一致），claude SDK 作传递依赖随装。
    pnpmAdd([spec], tmpDir);
  } catch (err) {
    rmSync(tmpDir, { recursive: true, force: true });
    fail(`pnpm add ${spec} 失败: ${err instanceof Error ? err.message : String(err)}`);
  }

  const sdkActual = installedClaudeSdkVersion(tmpDir);
  if (!sdkActual) {
    rmSync(tmpDir, { recursive: true, force: true });
    fail(`安装树内未发现 ${CLAUDE_SDK_PKG}（adapter ${spec} 未携带 claude SDK？复核 catalog pin 与本脚本假设）`);
  }
  if (sdkActual !== sdkPin.version) {
    rmSync(tmpDir, { recursive: true, force: true });
    fail(
      sdkPin.fromCatalog
        ? `claude SDK 版本漂移：pnpm-workspace.yaml catalog 声明 ${CLAUDE_SDK_PKG}@${sdkPin.version}（根 devDependencies 引用 catalog:），`
        + `adapter ${spec} 实际依赖 ${sdkActual}。同步 catalog 条目为 ${sdkActual} 后重跑 pnpm server:acp:vendor`
        : `claude SDK 版本漂移：根 devDependencies 声明 ${CLAUDE_SDK_PKG}@${sdkPin.version}，`
          + `adapter ${spec} 实际依赖 ${sdkActual}。同步根声明后重跑：`
          + `pnpm up ${CLAUDE_SDK_PKG}@${sdkActual} --save-exact`,
    );
  }

  console.log(`prepare-acp-adapters: 回写明文依赖 ${CLAUDE_SDK_PKG}@${sdkActual}`);
  try {
    // 第二段：claude SDK 提升为 staging 直接依赖（--save-exact 明文精确版本，lockfile 同步收敛）。
    pnpmAdd(['--save-exact', `${CLAUDE_SDK_PKG}@${sdkActual}`], tmpDir);
  } catch (err) {
    rmSync(tmpDir, { recursive: true, force: true });
    fail(`pnpm add ${CLAUDE_SDK_PKG}@${sdkActual} 失败: ${err instanceof Error ? err.message : String(err)}`);
  }

  if (!stagingReady(tmpDir, pkg, version)) {
    rmSync(tmpDir, { recursive: true, force: true });
    fail(`安装后校验失败：${tmpDir} 下未见 ${pkg}@${version}`);
  }
  rmSync(dir, { recursive: true, force: true });
  renameSync(tmpDir, dir);
}

console.log('prepare-acp-adapters: 完成');
