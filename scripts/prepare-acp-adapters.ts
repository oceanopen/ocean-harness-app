#!/usr/bin/env node
/**
 * prepare-acp-adapters.ts — 构建期安装 ACP adapter 到 Tauri resources staging
 *
 * 读内嵌 agent catalog（SSOT）的 enabled + npx-adapter 条目，pnpm 安装到
 * app/resources/acp-adapters/<id>/<version>/，随 bundle.resources 打进应用。运行期
 * sidecar EnsureVendored 检测受管目录缺失时从此处复制——终端用户机零 npm/零 registry
 * 依赖（公司网络无需代理），版本由构建期锁死 = catalog pin。
 *
 * 安装形态（勿破坏）：staging 内置 .npmrc（node-linker=hoisted）+ 自身 pnpm-workspace.yaml
 * （staging 即工作区根，天然不向上吸附；supportedArchitectures 声明构建目标平台），产出
 * npm 同构的扁平真实目录树——Go VendoredClaudeBin / Rust cli_bin 按扁平路径直取平台
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
 *
 * 目标平台：CI 交叉构建时 env GOOS/GOARCH（release-assets.yml 矩阵按目标注入，
 * beforeBuildCommand 继承）决定 staging 平台包架构，经 staging .npmrc 的
 * supported-architectures 让 pnpm 装出目标平台 optional 依赖——不信 pnpm 的本机过滤
 * （arm64 runner 打 x86_64 资产必须装出 darwin-x64，与 amd64 sidecar 运行时定位一致；
 * 错配即运行期「vendored claude 二进制缺失」）；本地无 env 回退当前机器。staging 就绪
 * 判定与安装后断言均校验目标平台 claude 二进制存在，缺失即 fail（残缺品不落地）。
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

/**
 * 构建目标平台（npm os/cpu 命名）：CI 交叉构建读 GOOS/GOARCH（两者须成对出现），
 * 本地开发无 env 回退当前机器——staging 平台包与消费方 sidecar 架构强一致的 SSOT。
 *
 * Go → npm 词汇翻译唯一收口：os（windows→win32）与 cpu（amd64→x64；arm64 两套词汇
 * 同名）。npm/pnpm 的 cpu 词汇表没有 amd64，不翻译则 supportedArchitectures 匹配不到
 * 任何平台包（SA 生效期连本机平台都不装出），平台包名/二进制断言路径亦全错（npm 真名
 * 形如 claude-agent-sdk-darwin-x64，与 Go claudePlatformTriple / Rust platform_triple
 * 的运行时定位同口径）。回退分支的 process.platform/process.arch 本就是 npm 词汇。
 */
interface TargetPlatform {
  os: string;
  cpu: string;
}

function targetPlatform(): TargetPlatform {
  const goos = process.env.GOOS;
  const goarch = process.env.GOARCH;
  if (!goos && !goarch) {
    return { os: process.platform, cpu: process.arch };
  }
  if (goos !== 'darwin' && goos !== 'windows' && goos !== 'linux') {
    fail(`env GOOS=${goos ?? '缺失'} 非法或与 GOARCH 未成对注入（GOOS ∈ darwin/windows/linux，GOARCH ∈ amd64/arm64）`);
  }
  if (goarch !== 'amd64' && goarch !== 'arm64') {
    fail(`env GOARCH=${goarch ?? '缺失'} 不支持（∈ amd64/arm64，须与 GOOS 成对注入）`);
  }
  return { os: goos === 'windows' ? 'win32' : goos, cpu: goarch === 'amd64' ? 'x64' : goarch };
}

/** 目标平台的 SDK claude 二进制相对路径（与 Go agentcatalog.VendoredClaudeBin 的定位同口径）。 */
function claudePlatBinRel(platform: TargetPlatform): string[] {
  return [
    'node_modules',
    '@anthropic-ai',
    `claude-agent-sdk-${platform.os}-${platform.cpu}`,
    platform.os === 'win32' ? 'claude.exe' : 'claude',
  ];
}

/**
 * staging 是否已就绪：目标包就位且版本匹配，且目标平台 claude 二进制存在——
 * 跨架构错配的旧 staging（如换 GOARCH 重跑）不复用，整目录重装。
 */
function stagingReady(dir: string, pkg: string, version: string, platformBinRel: string[]): boolean {
  try {
    const pkgJsonPath = resolve(dir, 'node_modules', ...pkg.split('/'), 'package.json');
    if (JSON.parse(readFileSync(pkgJsonPath, 'utf8')).version !== version) {
      return false;
    }
  } catch {
    return false;
  }
  return existsSync(resolve(dir, ...platformBinRel));
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

/**
 * pnpm add（staging 自带 pnpm-workspace.yaml 即工作区根，天然隔离、不向上吸附。
 * 勿加 --ignore-workspace：该 flag 会连 supportedArchitectures 等工作区设置一起忽略——
 * 实测其存在时平台包退回「仅当前机器」过滤，CI 交叉目标装不出对应平台包）。
 */
function pnpmAdd(args: string[], cwd: string): void {
  execFileSync('pnpm', ['add', ...args], {
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
const platform = targetPlatform();
const platformBinRel = claudePlatBinRel(platform);
// .npmrc 随 staging 落地：人工 cd 进 staging 手动 pnpm install 也得到同构布局（可复现）。
const STAGING_NPMRC = [
  '# hoisted = npm 同构扁平真实目录树（无 symlink）：扁平路径供 Go/Rust 直取平台二进制，',
  '# Tauri bundle.resources 打包亦要求真实文件。禁改回默认 isolated（symlink）布局。',
  'node-linker=hoisted',
  '',
].join('\n');
// pnpm-workspace.yaml 随 staging 落地：其一，staging 成为自身工作区根（pnpm 向上寻根到此
// 为止，防吸附进外层 workspace）；其二，supportedArchitectures 让平台包按构建目标装出
// （不信 pnpm 的本机 optional 过滤）——CI arm64 runner 打 x86_64 资产时靠它装出 darwin-x64，
// 与 amd64 sidecar 运行时定位一致。注：该设置仅 pnpm-workspace.yaml 形态生效
// （.npmrc 在现代 pnpm 只读 auth/registry 类设置），且被 --ignore-workspace 连带忽略。
function stagingWorkspaceYaml(platform: TargetPlatform): string {
  return [
    '# 由 scripts/prepare-acp-adapters.ts 生成：平台包按构建目标架构装出（勿手改）。',
    'supportedArchitectures:',
    '  os:',
    `    - ${platform.os}`,
    '  cpu:',
    `    - ${platform.cpu}`,
    '',
  ].join('\n');
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

  if (stagingReady(dir, pkg, version, platformBinRel)) {
    console.log(`prepare-acp-adapters: ${spec} 已就绪（${platform.os}-${platform.cpu} 平台二进制在位），跳过（${dir}）`);
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
  writeFileSync(resolve(tmpDir, 'pnpm-workspace.yaml'), stagingWorkspaceYaml(platform));

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

  // 目标平台二进制断言（bundle.resources 不打包与 sidecar 架构错配的残缺品）。
  const platBinPath = resolve(tmpDir, ...platformBinRel);
  if (!existsSync(platBinPath)) {
    rmSync(tmpDir, { recursive: true, force: true });
    fail(`目标平台 claude 二进制缺失: ${platBinPath}（pnpm 未按构建目标 ${platform.os}-${platform.cpu} 装出平台包，检查 supported-architectures 注入）`);
  }

  if (!stagingReady(tmpDir, pkg, version, platformBinRel)) {
    rmSync(tmpDir, { recursive: true, force: true });
    fail(`安装后校验失败：${tmpDir} 下未见 ${pkg}@${version}`);
  }
  rmSync(dir, { recursive: true, force: true });
  renameSync(tmpDir, dir);
}

console.log('prepare-acp-adapters: 完成');
