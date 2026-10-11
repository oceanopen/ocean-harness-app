/**
 * vendor-browser-engine.ts — browser 引擎域安装（伪条目 playwright-mcp → staging）
 *
 * 版本 SSOT 在 pnpm-workspace.yaml 顶层 catalog 段（与 acp 包同款机制，用户拍板）：根
 * devDependencies 以 `catalog:` 引用（字面版本不接受——统一维护语义下禁止第二处版本）；
 * browser-mcp.json 为 Go 侧 go:embed 投影（Go 不读 yaml，运行期构造 Entry 消费），
 * 安装前防漂移断言两者一致（镜像 claude SDK 漂移检查；升级流程 = 改 catalog →
 * pnpm install → 同步投影 → 重跑 pnpm server:vendor:prepare）。
 * 纯 JS 闭包（薄壳 @playwright/mcp，实现在 playwright-core，闭包由 pnpm lockfile 自动
 * 携带），claude 专属校验整段跳过，ready 判据 = 包版本匹配。
 */

import type { TargetPlatform } from './vendor-common.ts';
import { readFileSync } from 'node:fs';

import { resolve } from 'node:path';
import { catalogVersionOf, fail, installSpec, repoRoot, rootPkg } from './vendor-common.ts';

/** browser 引擎包名：伪条目安装对象，与 Go 侧 browser.EngineEntry 的 spawn spec 同源。 */
const BROWSER_ENGINE_PKG = '@playwright/mcp';

/** browser 引擎 staging 目录名 = Go 侧 agentcatalog Entry.ID（server/internal/browser/pin.go 硬编码同源）。 */
const BROWSER_ENGINE_ID = 'playwright-mcp';

const BROWSER_PIN_PATH = resolve(repoRoot, 'server/internal/browser/browser-mcp.json');

/**
 * browser 引擎安装基准：根 devDependencies 的 `catalog:` 引用解析出版本（pnpm-workspace.yaml
 * catalog 段 = 版本 SSOT，acp 包同款机制；字面版本不接受），并与 browser-mcp.json 做防漂移
 * 断言——不一致 fail 并给同步指引。fail fast：入口在任何安装动作前调用本函数。
 */
export function browserEnginePin(): { pkg: string; version: string } {
  const pin = rootPkg.devDependencies?.[BROWSER_ENGINE_PKG];
  if (!pin) {
    fail(`根 package.json devDependencies 缺 ${BROWSER_ENGINE_PKG} 声明（catalog: 引用，版本 SSOT 在 pnpm-workspace.yaml catalog 段，与 acp 包同款机制）`);
  }
  if (pin !== 'catalog:') {
    fail(`根 devDependencies 的 ${BROWSER_ENGINE_PKG}@${pin} 应为 catalog: 引用（版本 SSOT 在 pnpm-workspace.yaml catalog 段统一维护，勿写字面版本）`);
  }
  const version = catalogVersionOf(BROWSER_ENGINE_PKG);
  if (!version) {
    fail(`根 devDependencies 声明 ${BROWSER_ENGINE_PKG}@catalog: 但 pnpm-workspace.yaml 顶层 catalog 段无此条目——补精确版本条目后重跑 pnpm server:vendor:prepare`);
  }
  let goPin: { package?: string; version?: string };
  try {
    goPin = JSON.parse(readFileSync(BROWSER_PIN_PATH, 'utf8'));
  } catch (err) {
    return fail(`读取 browser 引擎投影失败: ${BROWSER_PIN_PATH}（${err instanceof Error ? err.message : String(err)}）`);
  }
  if (!goPin.package || !goPin.version) {
    fail(`${BROWSER_PIN_PATH} 的 package/version 不可为空（Go 侧 go:embed 投影，须与 catalog 版本 SSOT 对齐）`);
  }
  if (goPin.package !== BROWSER_ENGINE_PKG || goPin.version !== version) {
    fail(
      `browser 引擎版本漂移：pnpm-workspace.yaml catalog 声明 ${BROWSER_ENGINE_PKG}@${version}，`
      + `Go 侧投影 ${BROWSER_PIN_PATH} 为 ${goPin.package}@${goPin.version}。`
      + `同步 browser-mcp.json 为 {"package":"${BROWSER_ENGINE_PKG}","version":"${version}"} 后重跑 pnpm server:vendor:prepare`,
    );
  }
  return { pkg: BROWSER_ENGINE_PKG, version };
}

/** browser 引擎域安装入口：纯版本判据（readyBinRel = null），SKIP env 保险注入。 */
export function installBrowserEngine(engine: { pkg: string; version: string }, platform: TargetPlatform): void {
  installSpec({
    id: BROWSER_ENGINE_ID,
    spec: `${engine.pkg}@${engine.version}`,
    platform,
    readyBinRel: null,
    // 发布包无 postinstall 本不拉浏览器，SKIP_BROWSER_DOWNLOAD 防上游形态变化的保险。
    extraEnv: { PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD: '1' },
  });
}
