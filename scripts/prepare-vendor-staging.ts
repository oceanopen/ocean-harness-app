#!/usr/bin/env node
/**
 * prepare-vendor-staging.ts — 构建期安装 ACP adapter 与 browser 引擎到 Tauri resources staging
 *
 * pnpm 安装到 app/resources/acp-adapters/<id>/<version>/，随 bundle.resources 打进应用。
 * 运行期 sidecar EnsureVendored 检测受管目录缺失时从此处复制——终端用户机零 npm/零
 * registry 依赖（公司网络无需代理），版本由构建期锁死。
 *
 * 安装条目三类（分域模块，公共设施与统一安装驱动在 vendor-common.ts）：
 * - claude-acp：catalog 的 claude adapter 条目（vendor-claude-adapters.ts，SDK 专属校验链）。
 * - browser-engine：browser 引擎伪条目 id=playwright-mcp（vendor-browser-engine.ts，
 *   pnpm-workspace.yaml catalog 段版本 SSOT + browser-mcp.json 投影防漂移）。
 * - node-runtime：内嵌 node 二进制（vendor-node-runtime.ts，官方 dist 直下，应用自持
 *   node 运行时）。
 *
 * 幂等：目标已存在且条目判据满足则跳过（秒回），新版本自动新增目录；安装走「临时目录 +
 * rename」原子落地（安装中断不产生以目标名存在的半成品，bundle.resources 不打包残缺品）。
 * 需联网（开发机/CI）。
 */

import { browserEnginePin, installBrowserEngine } from './vendor-browser-engine.ts';
import { installClaudeAdapters } from './vendor-claude-adapters.ts';
import { log, targetPlatform } from './vendor-common.ts';
import { installNodeRuntime } from './vendor-node-runtime.ts';

// 伪条目基准先行解析（fail fast：catalog 缺条目 / 与 Go 投影漂移，在任何安装动作前报错）。
const engine = browserEnginePin();
const platform = targetPlatform();
installClaudeAdapters(platform);
installBrowserEngine(engine, platform);
await installNodeRuntime(platform);

log('完成');
