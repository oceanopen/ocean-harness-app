/**
 * vendor-pins.ts — vendored 外部组件版本 pin 唯一 SSOT
 *
 * 仅构建期 vendor 脚本消费（prepare-agent-catalog / vendor-*），随包打进应用而非
 * workspace 依赖：全仓代码不 import 这些包，禁止回迁根 package.json devDependencies
 * 或 pnpm-workspace.yaml catalog 段（幽灵依赖 + lockfile 随 vendored 升级空转）。
 */

/** npm 包 pin 表：包名 → 精确版本（staging 安装 spec、catalog 生成、防漂移基准共同消费）。 */
export const VENDOR_PKG_PINS = {
  '@agentclientprotocol/claude-agent-acp': '0.97.0',
  '@anthropic-ai/claude-agent-sdk': '0.3.310',
  '@playwright/mcp': '0.0.83',
} as const;

/** 内嵌 node 运行时版本：与根 package.json engines >=24.18 同线（人工约定，无机械校验）。 */
export const VENDOR_NODE_VERSION = '24.18.0';
