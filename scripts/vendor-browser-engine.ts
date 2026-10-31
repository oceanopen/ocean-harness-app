/**
 * vendor-browser-engine.ts — browser 引擎域安装（伪条目 playwright-mcp → staging）
 *
 * 版本 SSOT 在 scripts/vendor-pins.ts（与 acp 包同款机制）；browser-mcp.json 为 Go 侧
 * go:embed 投影（Go 不读 TS，运行期构造 Entry 消费），安装前防漂移断言两者一致（镜像
 * claude SDK 漂移检查；升级流程 = 改 pins → 同步投影 → 重跑 pnpm server:vendor:prepare）。
 * 纯 JS 闭包（薄壳 @playwright/mcp，实现在 playwright-core，闭包由 pnpm lockfile 自动
 * 携带），claude 专属校验整段跳过；ready 判据 = 包版本 + 内置浏览器 marker。
 */

import type { TargetPlatform } from './vendor-common.ts';
import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';

import { resolve } from 'node:path';
import process from 'node:process';
import { fail, installSpec, repoRoot } from './vendor-common.ts';
import { VENDOR_PKG_PINS } from './vendor-pins.ts';

/** browser 引擎包名：伪条目安装对象，与 Go 侧 browser.EngineEntry 的 spawn spec 同源。 */
const BROWSER_ENGINE_PKG = '@playwright/mcp';

/** browser 引擎 staging 目录名 = Go 侧 agentcatalog Entry.ID（server/internal/browser/pin.go 硬编码同源）。 */
const BROWSER_ENGINE_ID = 'playwright-mcp';

const BROWSER_PIN_PATH = resolve(repoRoot, 'server/internal/browser/browser-mcp.json');

/**
 * browser 引擎安装基准：vendor-pins SSOT 解析版本，并与 browser-mcp.json 做防漂移
 * 断言——不一致 fail 并给同步指引。fail fast：入口在任何安装动作前调用本函数。
 */
export function browserEnginePin(): { pkg: string; version: string } {
  const version = VENDOR_PKG_PINS[BROWSER_ENGINE_PKG as keyof typeof VENDOR_PKG_PINS];
  if (!version) {
    fail(`scripts/vendor-pins.ts 缺 ${BROWSER_ENGINE_PKG} 版本 pin（补精确版本条目后重跑 pnpm server:vendor:prepare）`);
  }
  let goPin: { package?: string; version?: string };
  try {
    goPin = JSON.parse(readFileSync(BROWSER_PIN_PATH, 'utf8'));
  } catch (err) {
    return fail(`读取 browser 引擎投影失败: ${BROWSER_PIN_PATH}（${err instanceof Error ? err.message : String(err)}）`);
  }
  if (!goPin.package || !goPin.version) {
    fail(`${BROWSER_PIN_PATH} 的 package/version 不可为空（Go 侧 go:embed 投影，须与 pins 版本 SSOT 对齐）`);
  }
  if (goPin.package !== BROWSER_ENGINE_PKG || goPin.version !== version) {
    fail(
      `browser 引擎版本漂移：vendor-pins 声明 ${BROWSER_ENGINE_PKG}@${version}，`
      + `Go 侧投影 ${BROWSER_PIN_PATH} 为 ${goPin.package}@${goPin.version}。`
      + `同步 browser-mcp.json 为 {"package":"${BROWSER_ENGINE_PKG}","version":"${version}"} 后重跑 pnpm server:vendor:prepare`,
    );
  }
  return { pkg: BROWSER_ENGINE_PKG, version };
}

/**
 * 内置浏览器（T3.5/D2 修订）：playwright 内置 Chromium 嵌入分发——staged playwright 的
 * cli.js install chromium 定向下载进条目目录 browsers/，引擎 node 进程运行期经
 * PLAYWRIGHT_BROWSERS_PATH 同目录消费（revision 与 playwright-core browsers.json 天然
 * 一致）。目录在条目版本目录内 → tauri resources 的 acp-adapters 递归打包自动覆盖，
 * tauri.conf.json 零变更；EnsureVendored 整树复制随之进受管目录。
 */
const BROWSERS_DIR_NAME = 'browsers';

/**
 * 内置浏览器就绪 marker：install 成功后写入 browsers/ 根；readyBinRel 以此为判据——
 * 同版本 staging 无浏览器目录时（D2 修订前的旧 staging / 中断残留）不复用，整目录重装。
 */
const BROWSERS_MARKER = '.ocean-browsers';

/**
 * 交叉构建的浏览器下载 env：playwright install 按宿主机下载，CI 交叉构建（arm64 runner
 * 打 darwin-x64 资产等）必须经 PLAYWRIGHT_HOST_PLATFORM_OVERRIDE 强制目标平台（取值
 * 整串用作 hostPlatform）。取值仿原生形态（macOS 版本号 + 架构后缀 / win64）——下载段
 * 由 playwright toShortPlatform 派生（mac15→mac-x64、mac15-arm64→mac-arm64、
 * win64→win-x64），GOOS/GOARCH 判定与 targetPlatform() 同源。本机构建（无 env）不注入，
 * 宿主机即目标。
 */
function browserDownloadEnv(browsersDir: string): Record<string, string> {
  const base = { PLAYWRIGHT_BROWSERS_PATH: browsersDir };
  const goos = process.env.GOOS;
  const goarch = process.env.GOARCH;
  if (!goos && !goarch) {
    return base;
  }
  if (goos === 'darwin') {
    return { ...base, PLAYWRIGHT_HOST_PLATFORM_OVERRIDE: goarch === 'arm64' ? 'mac15-arm64' : 'mac15' };
  }
  if (goos === 'windows' && goarch === 'amd64') {
    return { ...base, PLAYWRIGHT_HOST_PLATFORM_OVERRIDE: 'win64' };
  }
  return fail(`GOOS=${goos ?? '缺失'}/GOARCH=${goarch ?? '缺失'} 无法派生 playwright 浏览器目标平台（打包目标 ∈ darwin/windows × amd64/arm64）`);
}

/**
 * install chromium（staged cli.js 保版本精确，不 npx 漂移）：--no-shell 跳过 headless-shell
 * （202M，无头模式经引擎 launchOptions.channel="chromium" 走完整版，见 server/internal/browser/pin.go）。
 * SKIP_BROWSER_DOWNLOAD 仅 pnpmAdd 段需要的保险，在此显式失效，不得带进下载步。
 */
function installBrowsers(tmpDir: string): void {
  const cli = resolve(tmpDir, 'node_modules', 'playwright', 'cli.js');
  const browsersDir = resolve(tmpDir, BROWSERS_DIR_NAME);
  const env = { ...process.env };
  delete env.PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD;
  execFileSync(process.execPath, [cli, 'install', 'chromium', '--no-shell'], {
    cwd: tmpDir,
    stdio: 'inherit',
    env: { ...env, ...browserDownloadEnv(browsersDir) },
  });
  writeFileSync(resolve(browsersDir, BROWSERS_MARKER), `${BROWSER_ENGINE_PKG} bundled chromium (no-shell)\n`);
}

/** browser 引擎域安装入口：版本 + 内置浏览器 marker 双判据（快路径与终验同验）。 */
export function installBrowserEngine(engine: { pkg: string; version: string }, platform: TargetPlatform): void {
  installSpec({
    id: BROWSER_ENGINE_ID,
    spec: `${engine.pkg}@${engine.version}`,
    platform,
    readyBinRel: [BROWSERS_DIR_NAME, BROWSERS_MARKER],
    // pnpmAdd 段的保险：发布包无 postinstall 本不拉浏览器；下载步在 postInstall 内
    // 显式失效该变量（installBrowsers）。
    extraEnv: { PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD: '1' },
    postInstall: installBrowsers,
  });
}
