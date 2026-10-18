/**
 * vendor-node-runtime.ts — node 运行时域安装（内嵌 node 二进制 → staging）
 *
 * 应用自持 node 运行时（消除用户自装 node / 版本过旧 / GUI PATH 缺 nvm 整类失败）：
 * 官方 dist 二进制（仅 node 可执行文件，npm/corepack 运行期用不上）落 staging 的
 * runtime/node/<version>/node[.exe]，随 acp-adapters 树打包；Go 侧 ResolveNodeBin 扫
 * 该目录取 semver 最高（版本号对 Go 不可见，无跨语言投影）。非 pnpm 条目，不走
 * installSpec，自带下载 → 选择性解出 → 实测校验 → 原子落地链路（卫生规则同源）。
 *
 * 多平台：dist 平台段由 targetPlatform() 派生（CI 矩阵 GOOS/GOARCH 注入，与 pnpm
 * supportedArchitectures 同源）；darwin 产物名 node、windows 产物名 node.exe，同一版本
 * 目录可共存（CI 单 runner 单目标天然纯净，本机交叉验证混入异平台产物仅体积冗余）。
 */

import type { TargetPlatform } from './vendor-common.ts';
import { Buffer } from 'node:buffer';
import { execFileSync } from 'node:child_process';
import { chmodSync, existsSync, mkdirSync, renameSync, rmSync, writeFileSync } from 'node:fs';

import { resolve } from 'node:path';
import process from 'node:process';
import { cleanStaleTmp, fail, log, STAGING_ROOT } from './vendor-common.ts';

/** 内嵌 node 版本 pin（唯一 SSOT）：与根 engines >=24.18 同线。 */
const NODE_VERSION = '24.18.0';

/** 官方 dist 镜像覆盖 env（默认 nodejs.org，受限网络指向镜像后重跑）。 */
const MIRROR_ENV = 'NODEJS_DIST_MIRROR';

/** 默认 dist 源。 */
const DIST_MIRROR_DEFAULT = 'https://nodejs.org/dist';

/** staging 落地目录 runtime/node/<version>（Go 侧 ResolveNodeBin 扫描布局同源）。 */
const RUNTIME_NODE_DIR = resolve(STAGING_ROOT, 'runtime', 'node', NODE_VERSION);

/**
 * 构建目标 → node dist 平台段命名（win32→win；cpu 两套词汇同名）。仅 darwin/windows
 * 有打包形态（bundle ∈ app/dmg/nsis），linux 显式拒绝。
 */
function nodeDistPlatform(platform: TargetPlatform): string {
  if (platform.os === 'darwin') {
    return `darwin-${platform.cpu}`;
  }
  if (platform.os === 'win32') {
    return `win-${platform.cpu}`;
  }
  return fail(`打包目标 ${platform.os} 无 bundle 形态，node 运行时不安装（目标 ∈ darwin/windows）`);
}

/** 目标平台产物名（windows 为 node.exe，其余 node）。 */
function binName(platform: TargetPlatform): string {
  return platform.os === 'win32' ? 'node.exe' : 'node';
}

/**
 * 二进制就绪判据：在位且实测 --version 等于 pin。版本实测仅目标 = 本机可行（CI 交叉
 * 构建的异平台二进制不可本机执行，完整性由 gzip CRC 与 tar 解包错误兜底）。
 */
function nodeReadyAt(bin: string, platform: TargetPlatform): boolean {
  if (!existsSync(bin)) {
    return false;
  }
  if (platform.os !== process.platform || platform.cpu !== process.arch) {
    return true;
  }
  try {
    return execFileSync(bin, ['--version'], { encoding: 'utf8' }).trim() === `v${NODE_VERSION}`;
  } catch {
    return false;
  }
}

/**
 * 下载官方 dist 并选择性解出 node 二进制到 tmpDir 根（tar 路径过滤成员解包，unix tgz
 * 与 windows zip 同一 tar 入口；解包后归位到 tmpDir/binName，残壳目录清除）。
 */
async function downloadAndExtract(platform: TargetPlatform, tmpDir: string): Promise<void> {
  const base = `node-v${NODE_VERSION}-${nodeDistPlatform(platform)}`;
  const isWin = platform.os === 'win32';
  const ext = isWin ? 'zip' : 'tar.gz';
  const mirror = (process.env[MIRROR_ENV] || DIST_MIRROR_DEFAULT).replace(/\/+$/, '');
  const url = `${mirror}/v${NODE_VERSION}/${base}.${ext}`;
  log(`下载 ${url}`);
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}（受限网络可用 ${MIRROR_ENV} 指向镜像后重试）`);
  }
  const archive = resolve(tmpDir, `.${base}.${ext}`);
  writeFileSync(archive, Buffer.from(await res.arrayBuffer()));
  // 成员路径过滤解包：包内仅一个可执行文件被解出，doc/include/npm 等全部留在包内。
  const member = isWin ? `${base}/${binName(platform)}` : `${base}/bin/${binName(platform)}`;
  execFileSync('tar', [isWin ? '-xf' : '-xzf', archive, '-C', tmpDir, member], { stdio: 'inherit' });
  rmSync(archive);
  renameSync(resolve(tmpDir, member), resolve(tmpDir, binName(platform)));
  rmSync(resolve(tmpDir, base), { recursive: true, force: true });
  if (!isWin) {
    chmodSync(resolve(tmpDir, binName(platform)), 0o755);
  }
}

/** node 运行时域安装入口：就绪跳过（秒回）→ 清残留 → 下载解出 → 实测校验 → 原子 rename。 */
export async function installNodeRuntime(platform: TargetPlatform): Promise<void> {
  const target = resolve(RUNTIME_NODE_DIR, binName(platform));
  if (nodeReadyAt(target, platform)) {
    log(`node 运行时 v${NODE_VERSION} 已就绪，跳过（${target}）`);
    return;
  }
  const parentDir = resolve(RUNTIME_NODE_DIR, '..');
  cleanStaleTmp(parentDir);
  const tmpDir = resolve(parentDir, `.${NODE_VERSION}.tmp-${process.pid}`);
  rmSync(tmpDir, { recursive: true, force: true });
  mkdirSync(tmpDir, { recursive: true });
  try {
    await downloadAndExtract(platform, tmpDir);
    if (!nodeReadyAt(resolve(tmpDir, binName(platform)), platform)) {
      throw new Error(`解出二进制就绪校验失败: ${resolve(tmpDir, binName(platform))}`);
    }
  } catch (err) {
    rmSync(tmpDir, { recursive: true, force: true });
    fail(`安装 node 运行时 v${NODE_VERSION} 失败: ${err instanceof Error ? err.message : String(err)}`);
  }
  rmSync(RUNTIME_NODE_DIR, { recursive: true, force: true });
  renameSync(tmpDir, RUNTIME_NODE_DIR);
  log(`node 运行时 v${NODE_VERSION} → ${RUNTIME_NODE_DIR}（${platform.os}-${platform.cpu}）`);
}
