/**
 * vendor-common.ts — vendor staging 公共设施（被 prepare-vendor-staging.ts 编排，两域模块复用）
 *
 * 总体机制见入口 scripts/prepare-vendor-staging.ts 头注释；本模块收口三类共享物：
 * - 基准解析：根 package.json 声明（rootPkg）、pnpm-workspace.yaml 顶层 catalog 段版本
 *   （catalogVersionOf，版本 SSOT 同款机制）、staging 的 pnpm 版本注入（packageManagerSpec）。
 * - 平台与 staging 形态：构建目标平台解析（targetPlatform）、staging 三件套
 *   （package.json/.npmrc/pnpm-workspace.yaml）、就绪判定（stagingReady）、临时目录卫生
 *   （cleanStaleTmp）、pnpm add 封装（pnpmAdd）。
 * - 统一安装驱动 installSpec：快路径 → 临时目录 → 安装 → 条目收尾 → 终验 → 原子 rename。
 *   失败清理（rm 临时目录 + fail）单点收敛于此——域侧 postInstall 抛错即安装失败，
 *   域文件只写各自的差异步骤，不再各自处理半成品清理。
 *
 * 安装形态（勿破坏）：staging 内置 .npmrc（node-linker=hoisted）+ 自身 pnpm-workspace.yaml
 * （staging 即工作区根，天然不向上吸附；supportedArchitectures 声明构建目标平台），产出
 * npm 同构的扁平真实目录树——Go VendoredClaudeBin / Rust cli_bin 按扁平路径直取平台
 * 二进制，browser 引擎入口亦按扁平路径解析，Tauri bundle.resources 打包亦要求真实文件；
 * pnpm 默认 isolated（symlink）布局会整链路打穿。
 *
 * 目标平台：CI 交叉构建时 env GOOS/GOARCH（release-assets.yml 矩阵按目标注入，
 * beforeBuildCommand 继承）决定 staging 平台包架构，经 staging .npmrc 的
 * supported-architectures 让 pnpm 装出目标平台 optional 依赖——不信 pnpm 的本机过滤
 * （arm64 runner 打 x86_64 资产必须装出 darwin-x64，与 amd64 sidecar 运行时定位一致；
 * 错配即运行期「vendored claude 二进制缺失」）；本地无 env 回退当前机器。
 */

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readdirSync, readFileSync, renameSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

export const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const STAGING_ROOT = resolve(repoRoot, 'app/resources/acp-adapters');

const WORKSPACE_YAML = resolve(repoRoot, 'pnpm-workspace.yaml');

export const rootPkg = JSON.parse(readFileSync(resolve(repoRoot, 'package.json'), 'utf8')) as {
  packageManager?: string;
  devDependencies?: Record<string, string>;
};

export function log(message: string): void {
  console.log(`prepare-vendor-staging: ${message}`);
}

export function fail(message: string): never {
  console.error(`prepare-vendor-staging: ${message}`);
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

/** pnpm-workspace.yaml 顶层 catalog: 段内指定包的声明版本（单/双引号与裸值、行尾注释均兼容；段结束/缺失返回 null）。 */
export function catalogVersionOf(pkg: string): string | null {
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
export interface TargetPlatform {
  os: string;
  cpu: string;
}

export function targetPlatform(): TargetPlatform {
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
    '# 由 scripts/prepare-vendor-staging.ts 生成：平台包按构建目标架构装出（勿手改）。',
    'supportedArchitectures:',
    '  os:',
    `    - ${platform.os}`,
    '  cpu:',
    `    - ${platform.cpu}`,
    '',
  ].join('\n');
}

/** 附加就绪判据的包版本 pin（如 claude 域的 SDK 基准——快路径与终验同验）。 */
export interface ReadyPin {
  pkg: string;
  version: string;
}

/**
 * staging 是否已就绪：目标包就位且版本匹配，readyPins 逐项同验（版本不符或读不到即
 * 未就绪）；platformBinRel 非 null 时还要求该相对路径文件存在（claude 域传目标平台
 * claude 二进制——跨架构错配的旧 staging（如换 GOARCH 重跑）不复用，整目录重装；
 * browser 引擎为纯 JS 闭包无平台包，传 null 只验版本）。
 */
function stagingReady(dir: string, pkg: string, version: string, platformBinRel: string[] | null, readyPins: ReadyPin[]): boolean {
  try {
    const versionOf = (name: string): string =>
      JSON.parse(readFileSync(resolve(dir, 'node_modules', ...name.split('/'), 'package.json'), 'utf8')).version;
    if (versionOf(pkg) !== version) {
      return false;
    }
    for (const pin of readyPins) {
      if (versionOf(pin.pkg) !== pin.version) {
        return false;
      }
    }
  } catch {
    return false;
  }
  return platformBinRel === null || existsSync(resolve(dir, ...platformBinRel));
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
 * extraEnv：按条目追加环境变量（browser 引擎条目注入 PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1，
 * 见 vendor-browser-engine.ts）。
 */
export function pnpmAdd(args: string[], cwd: string, extraEnv?: Record<string, string>): void {
  execFileSync('pnpm', ['add', ...args], {
    cwd,
    stdio: 'inherit',
    shell: process.platform === 'win32',
    env: extraEnv ? { ...process.env, ...extraEnv } : undefined,
  });
}

/** installSpec 的安装条目：id/spec/platform 判据 + 条目级差异（环境变量、专属收尾）。 */
export interface InstallSpecItem {
  /** staging 目录名（claude 条目取 catalog id；browser 引擎 = Go 侧 agentcatalog Entry.ID 同源）。 */
  id: string;
  /** 安装 spec（形如 pkg@version）。 */
  spec: string;
  platform: TargetPlatform;
  /** 就绪判据的目标平台二进制相对路径；null = 纯版本判据（纯 JS 闭包条目）。 */
  readyBinRel: string[] | null;
  /** 附加就绪判据的包版本 pin（如 claude SDK 基准——快路径复验防静默跳过；可省）。 */
  readyPins?: ReadyPin[];
  /** 条目级安装环境变量（可省）。 */
  extraEnv?: Record<string, string>;
  /** 条目专属收尾（如 claude SDK 校验链）；抛错即安装失败，驱动统一清理半成品。 */
  postInstall?: (tmpDir: string) => void;
}

/**
 * 统一安装驱动：快路径跳过（秒回）→ 清残留 → 建临时目录写三件套 → pnpm add →
 * postInstall → 终验 → 原子 rename 落地。任何一步失败（pnpm 失败/postInstall 抛错/
 * 终验不过）都走同一清理路径 rm 临时目录后退出——半成品不以目标名存在，不进打包资源。
 */
export function installSpec(item: InstallSpecItem): void {
  const { spec } = item;
  const at = spec.lastIndexOf('@');
  if (at <= 0) {
    fail(`包 spec 形态非法（应为 pkg@version）: ${spec}`);
  }
  const pkg = spec.slice(0, at);
  const version = spec.slice(at + 1);
  const dir = resolve(STAGING_ROOT, item.id, version);

  if (stagingReady(dir, pkg, version, item.readyBinRel, item.readyPins ?? [])) {
    log(`${spec} 已就绪（${item.readyBinRel ? `${item.platform.os}-${item.platform.cpu} 平台二进制在位` : '版本匹配'}），跳过（${dir}）`);
    return;
  }

  cleanStaleTmp(resolve(STAGING_ROOT, item.id));
  // 原子落地：先装到 .<version>.tmp-<pid> 临时目录，校验通过后 rename 成目标名——
  // 安装中断（网络断/杀进程）留下的半成品不以目标名存在，不会被打包资源收编。
  const tmpDir = resolve(STAGING_ROOT, item.id, `.${version}.tmp-${process.pid}`);
  rmSync(tmpDir, { recursive: true, force: true });
  mkdirSync(tmpDir, { recursive: true });
  writeFileSync(
    resolve(tmpDir, 'package.json'),
    `${JSON.stringify({ private: true, packageManager: packageManagerSpec() }, null, 2)}\n`,
  );
  writeFileSync(resolve(tmpDir, '.npmrc'), STAGING_NPMRC);
  writeFileSync(resolve(tmpDir, 'pnpm-workspace.yaml'), stagingWorkspaceYaml(item.platform));

  // 第一段：装条目本体（pnpm save 精确版本——目录名/manifest/lockfile 三方一致）。
  // claude 条目的 SDK 作传递依赖随装；browser 引擎连带 playwright 闭包（SKIP env 保险）。
  log(`pnpm add ${spec} → ${tmpDir}`);
  try {
    pnpmAdd([spec], tmpDir, item.extraEnv);
    item.postInstall?.(tmpDir);
    if (!stagingReady(tmpDir, pkg, version, item.readyBinRel, item.readyPins ?? [])) {
      throw new Error(`安装后校验失败：${tmpDir} 下未见 ${pkg}@${version}`);
    }
  } catch (err) {
    // 失败清理单点：pnpm 失败 / postInstall 抛错 / 终验不过，同路径清半成品后退出。
    rmSync(tmpDir, { recursive: true, force: true });
    fail(`安装 ${spec} 失败: ${err instanceof Error ? err.message : String(err)}`);
  }

  rmSync(dir, { recursive: true, force: true });
  renameSync(tmpDir, dir);
}
