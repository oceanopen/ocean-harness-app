/**
 * vendor-claude-adapters.ts — claude adapter 域安装（catalog npx-adapter 条目 → staging）
 *
 * 消费内嵌 agent catalog（SSOT）的 enabled + npx-adapter 条目，安装走公共驱动
 * installSpec；claude 专属收尾链（postInstall）：安装树内 SDK 存在性断言 → 与根声明
 * 防漂移断言（catalog: 引用时解析 pnpm-workspace.yaml catalog 段 SSOT；漂移即 fail 并给
 * 同步指引）→ SDK 提升明文依赖（第二段 --save-exact 安装）→ 目标平台 claude 二进制
 * 断言。SDK 基准在有启用条目时安装前解析一次，作为就绪判据（readyPins，快路径亦复验）
 * 与漂移断言的共同基准；纯 browser 运行（catalog 无启用 claude 条目）不触发其中任何断言。
 */

import type { TargetPlatform } from './vendor-common.ts';
import { existsSync, readFileSync } from 'node:fs';

import { resolve } from 'node:path';
import { catalogVersionOf, fail, installSpec, log, pnpmAdd, repoRoot, rootPkg } from './vendor-common.ts';

/** claude SDK 包名：adapter 的传递依赖，staging 明文化 + 根声明防漂移的校验对象。 */
const CLAUDE_SDK_PKG = '@anthropic-ai/claude-agent-sdk';

/** patch 目录（scripts/patches/）：patch 文件名与安装 spec 同形（pkg@version.patch），与 pin 版本强绑定。 */
const PATCHES_DIR = resolve(repoRoot, 'scripts/patches');

const CATALOG_PATH = resolve(repoRoot, 'server/internal/agentcatalog/agent-catalog.json');

/** agent catalog 条目（本模块只消费这些字段）。 */
interface CatalogEntry {
  id: string;
  args: string[];
  enabled?: boolean;
  strategy?: string;
}

/** claude SDK 防漂移断言基准：version 为解析后的精确版本；fromCatalog 标记取自 workspace catalog 段（漂移提示分流用）。 */
interface SdkPin {
  version: string;
  fromCatalog: boolean;
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

/** 目标平台的 SDK claude 二进制相对路径（与 Go agentcatalog.VendoredClaudeBin 的定位同口径）。 */
function claudePlatBinRel(platform: TargetPlatform): string[] {
  return [
    'node_modules',
    '@anthropic-ai',
    `claude-agent-sdk-${platform.os}-${platform.cpu}`,
    platform.os === 'win32' ? 'claude.exe' : 'claude',
  ];
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

/** 从 catalog 条目 args 取包 spec（非 flag 项，形如 @scope/pkg@1.2.3）。 */
function adapterSpec(entry: CatalogEntry): string {
  const spec = entry.args.filter(item => !item.startsWith('-')).pop();
  if (!spec) {
    fail(`catalog 条目 ${entry.id} 无 npx 包 spec（args: ${JSON.stringify(entry.args)}）`);
  }
  return spec;
}

/** 条目 patch 声明：scripts/patches/<包尾段>@<版本>.patch 存在即启用（升级 pin 时须同步重放 patch）。 */
function adapterPatch(spec: string): { key: string; file: string } | undefined {
  const at = spec.lastIndexOf('@');
  const base = spec.slice(0, at).split('/').pop() ?? spec;
  const file = resolve(PATCHES_DIR, `${base}${spec.slice(at)}.patch`);
  return existsSync(file) ? { key: spec, file } : undefined;
}

/**
 * claude 专属收尾（installSpec postInstall）：抛错即安装失败（驱动统一清理半成品）。
 * 链条：SDK 存在性 → 根声明防漂移 → 明文提升（--save-exact 精确版本，lockfile 同步收敛）
 * → 目标平台二进制断言（bundle.resources 不打包与 sidecar 架构错配的残缺品）。
 */
function claudePostInstall(tmpDir: string, spec: string, platform: TargetPlatform, sdkPin: SdkPin): void {
  const sdkActual = installedClaudeSdkVersion(tmpDir);
  if (!sdkActual) {
    throw new Error(`安装树内未发现 ${CLAUDE_SDK_PKG}（adapter ${spec} 未携带 claude SDK？复核 catalog pin 与本脚本假设）`);
  }
  if (sdkActual !== sdkPin.version) {
    throw new Error(
      sdkPin.fromCatalog
        ? `claude SDK 版本漂移：pnpm-workspace.yaml catalog 声明 ${CLAUDE_SDK_PKG}@${sdkPin.version}（根 devDependencies 引用 catalog:），`
        + `adapter ${spec} 实际依赖 ${sdkActual}。同步 catalog 条目为 ${sdkActual} 后重跑 pnpm server:vendor:prepare`
        : `claude SDK 版本漂移：根 devDependencies 声明 ${CLAUDE_SDK_PKG}@${sdkPin.version}，`
          + `adapter ${spec} 实际依赖 ${sdkActual}。同步根声明后重跑：`
          + `pnpm up ${CLAUDE_SDK_PKG}@${sdkActual} --save-exact`,
    );
  }

  log(`回写明文依赖 ${CLAUDE_SDK_PKG}@${sdkActual}`);
  pnpmAdd(['--save-exact', `${CLAUDE_SDK_PKG}@${sdkActual}`], tmpDir);

  const platBinPath = resolve(tmpDir, ...claudePlatBinRel(platform));
  if (!existsSync(platBinPath)) {
    throw new Error(`目标平台 claude 二进制缺失: ${platBinPath}（pnpm 未按构建目标 ${platform.os}-${platform.cpu} 装出平台包，检查 supported-architectures 注入）`);
  }
}

/** claude adapter 域安装入口：catalog 启用条目逐个走统一驱动（平台二进制与 SDK pin 均在位即跳过秒回）。 */
export function installClaudeAdapters(platform: TargetPlatform): void {
  const catalog = JSON.parse(readFileSync(CATALOG_PATH, 'utf8')) as { agents?: CatalogEntry[] };
  const entries = (catalog.agents ?? []).filter(item => item.enabled && item.strategy === 'npx-adapter');
  if (entries.length === 0) {
    return; // 纯 browser 运行：不执行 claude 专属根声明断言（惰性语义）
  }
  const sdkPin = rootClaudeSdkPin();
  for (const entry of entries) {
    const spec = adapterSpec(entry);
    installSpec({
      id: entry.id,
      spec,
      platform,
      readyBinRel: claudePlatBinRel(platform),
      // 快路径亦复验 SDK pin：staging 在位但根声明被改错时不再静默跳过（重装后由
      // postInstall 漂移断言响亮报错）。
      readyPins: [{ pkg: CLAUDE_SDK_PKG, version: sdkPin.version }],
      patch: adapterPatch(spec),
      postInstall: tmpDir => claudePostInstall(tmpDir, spec, platform, sdkPin),
    });
  }
}
