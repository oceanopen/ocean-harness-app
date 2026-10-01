#!/usr/bin/env node
/**
 * prepare-agent-catalog.mjs — 生成 server/internal/agentcatalog/agent-catalog.json
 *
 * 从官方 ACP registry 拉取快照，按白名单过滤 + npx distribution 翻译 + 版本 pin +
 * 项目侧覆盖，产出 sidecar go:embed 内嵌的 agent catalog；registry 原始快照同写入库
 * 支持 --offline 可复现重建。运行时只读内嵌产物，本脚本属开发期工具（需联网）。
 *
 * 用法：
 *   pnpm server:catalog:refresh                                    # 在线刷新
 *   node scripts/prepare-agent-catalog.mjs --offline               # 离线：读快照重建
 *   node scripts/prepare-agent-catalog.mjs --pin @scope/pkg@1.2.3  # 覆盖 pin（同步在线校验）
 *
 * 升级验证流程（风险 §5.2）：`pnpm up <pkg>@<新版> --save-exact`（版本住 root
 * package.json devDependencies）→ 跑本脚本 → OCEAN_ACP_REAL_HANDSHAKE=1
 * go test ./internal/acp -run TestRealHandshake（缺省命令即 catalog 条目）→ 提交。
 * 扩展 agent：BUILTIN_AGENT_IDS 与 PROJECT_OVERRIDES 各加一条目 + 补 registry 快照，
 * 代码零改动（native-acp 条目无需 pin）。
 */

import { createHash } from 'node:crypto';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const OUT_DIR = resolve(repoRoot, 'server/internal/agentcatalog');
const rootPkg = JSON.parse(readFileSync(resolve(repoRoot, 'package.json'), 'utf8'));
const CATALOG_PATH = resolve(OUT_DIR, 'agent-catalog.json');
const SNAPSHOT_PATH = resolve(OUT_DIR, 'acp-registry.snapshot.json');
const ACP_REGISTRY_URL = 'https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json';
const NPM_REGISTRY_URL = 'https://registry.npmjs.org';
const NPM_TIMEOUT_MS = 30_000;

/** 一期白名单（P4 只启用 claude-acp；扩展 = 此处加 id + PROJECT_OVERRIDES 补条目）。 */
const BUILTIN_AGENT_IDS = ['claude-acp'];

/**
 * pin 包名表：id → npm 包名（版本不住这里——住 root package.json devDependencies，
 * 本地升级 `pnpm up <pkg>@<新版> --save-exact` 即改）。包名不跟随 registry 的包名迁移，
 * 以本项目真握手验证结论为准；版本须为精确版本（拒绝 range/latest，脚本强校验）。
 * native-acp 条目（直连 CLI）不进此表。
 */
const ADAPTER_PINS = {
  'claude-acp': '@agentclientprotocol/claude-agent-acp',
};

/**
 * 项目侧覆盖：registry 元数据之外由本项目定义的字段。
 * label 对齐前端展示名约定（Claude Code，非 registry 品牌名 Claude）。
 */
const PROJECT_OVERRIDES = {
  'claude-acp': { label: 'Claude Code', strategy: 'npx-adapter', enabled: true },
};

const args = process.argv.slice(2);
const offline = args.includes('--offline');
const pinFlagIdx = args.indexOf('--pin');
const pinOverride = pinFlagIdx >= 0 ? args[pinFlagIdx + 1] : undefined;

function fail(message) {
  console.error(`prepare-agent-catalog: ${message}`);
  process.exit(1);
}

/** 拆 pkg@version（scoped 包名含 @，从最后一个 @ 分隔）。 */
function splitPkgSpec(spec) {
  const at = spec.lastIndexOf('@');
  if (at <= 0) {
    fail(`pin 形态非法（应为 pkg@version）: ${spec}`);
  }
  return { pkg: spec.slice(0, at), version: spec.slice(at + 1) };
}

/** 包名基名（registry 的 npx package 可能自带 @version 后缀）。 */
function pkgBaseName(spec) {
  const at = spec.lastIndexOf('@');
  return at > 0 ? spec.slice(0, at) : spec;
}

/** 精确版本校验：拒绝 range / latest / dist-tag（升级必须钉死可复现版本）。 */
function assertExactVersion(version) {
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Z.-]+)?(?:\+[0-9A-Z.-]+)?$/i.test(version)) {
    fail(`pin 版本必须为精确版本（拒绝 range/latest）: ${version}`);
  }
}

async function fetchJson(url) {
  const res = await fetch(url, { signal: AbortSignal.timeout(NPM_TIMEOUT_MS) });
  if (!res.ok) {
    fail(`GET ${url} → ${res.status}`);
  }
  return res.json();
}

async function loadRegistry() {
  if (offline) {
    console.log(`离线模式：读快照 ${SNAPSHOT_PATH}`);
    return JSON.parse(readFileSync(SNAPSHOT_PATH, 'utf8'));
  }
  console.log(`拉取官方 registry: ${ACP_REGISTRY_URL}`);
  const registry = await fetchJson(ACP_REGISTRY_URL);
  writeFileSync(SNAPSHOT_PATH, `${JSON.stringify(registry, null, 2)}\n`);
  const digest = createHash('sha256').update(JSON.stringify(registry)).digest('hex').slice(0, 12);
  console.log(`registry sha256 前 12 位: ${digest}（快照已入库）`);
  return registry;
}

/** distribution.npx → { command: 'npx', args: ['-y', pkg] }；无 npx 分发 → null（native 直连条目由覆盖表给 command/args）。 */
function resolveNpxDistribution(entry) {
  const pkg = entry.distribution?.npx?.package;
  if (!pkg) {
    return null;
  }
  return { command: 'npx', args: ['-y', pkg] };
}

/** npm manifest 校验 pin 包版本存在 + 取 engines.node 主版本下限（T1.4 doctor 消费）。 */
async function resolveNodeMinVersion(pkg, version) {
  const manifest = await fetchJson(`${NPM_REGISTRY_URL}/${encodeURIComponent(pkg)}/${version}`);
  const engines = manifest.engines?.node ?? '';
  const major = engines.match(/\d+/)?.[0] ?? '';
  if (!major) {
    console.warn(`  ⚠ ${pkg}@${version} 未声明 engines.node，nodeMinVersion 留空（doctor 不校验）`);
  }
  return major;
}

async function main() {
  mkdirSync(OUT_DIR, { recursive: true });
  const registry = await loadRegistry();
  const byId = new Map((registry.agents ?? []).map(entry => [entry.id, entry]));

  const agents = [];
  for (const id of BUILTIN_AGENT_IDS) {
    const registryEntry = byId.get(id);
    if (!registryEntry) {
      fail(`官方 registry 缺白名单条目 ${id}（宁可失败不静默缺刊）`);
    }
    const overrides = PROJECT_OVERRIDES[id];
    if (!overrides) {
      fail(`白名单条目 ${id} 缺 PROJECT_OVERRIDES（label/strategy/enabled 必填）`);
    }

    // pin spec 组装：默认 = pin 包名表 + root package.json 的精确版本；--pin 以完整
    // spec 覆盖（临时测试用，仍过精确版本校验）。
    let pinSpec;
    if (ADAPTER_PINS[id] !== undefined) {
      if (pinOverride) {
        pinSpec = pinOverride;
      } else {
        const version = rootPkg.devDependencies?.[ADAPTER_PINS[id]];
        if (!version) {
          fail(`root package.json devDependencies 缺 ${ADAPTER_PINS[id]} 精确版本（升级：pnpm up ${ADAPTER_PINS[id]}@<新版> --save-exact）`);
        }
        pinSpec = `${ADAPTER_PINS[id]}@${version}`;
      }
    }
    const dist = resolveNpxDistribution(registryEntry);
    let command = '';
    let cmdArgs = [];
    let version = registryEntry.version ?? '';
    let nodeMinVersion = '';

    if (dist) {
      ({ command, args: cmdArgs } = dist);
      if (pinSpec) {
        const { pkg, version: pinVersion } = splitPkgSpec(pinSpec);
        assertExactVersion(pinVersion);
        if (pkg !== pkgBaseName(cmdArgs[1])) {
          console.log(`  pin 包名以本项目拍板为准: ${cmdArgs[1]} → ${pkg}`);
        }
        cmdArgs[1] = `${pkg}@${pinVersion}`;
        version = pinVersion;
        if (!offline) {
          console.log(`  校验 ${pkg}@${pinVersion} ...`);
          nodeMinVersion = await resolveNodeMinVersion(pkg, pinVersion);
        }
      }
    } else if (overrides.command) {
      // native-acp 条目：command/args 由覆盖表全量给出（TODO: 随 P4 扩展补 codex/opencode/pi 条目）。
      ({ command, args: cmdArgs } = overrides);
    } else {
      fail(`条目 ${id} 无 npx 分发且覆盖表未给 command/args`);
    }

    agents.push({
      id,
      code: id, // agentCode（launch_settings.agentCode 取值；与 id 一致，显式落盘以保链路语义）
      label: overrides.label,
      version,
      description: registryEntry.description ?? '',
      strategy: overrides.strategy,
      command,
      args: cmdArgs,
      env: registryEntry.env ?? {},
      enabled: overrides.enabled,
      nodeMinVersion,
    });
  }

  const catalog = {
    schemaVersion: 1,
    source: {
      url: ACP_REGISTRY_URL,
      registryVersion: registry.version ?? 'unknown',
      // SOURCE_DATE_EPOCH 可复现构建（对齐 Gold-Band 惯例）。
      fetchedAt: process.env.SOURCE_DATE_EPOCH
        ? new Date(Number(process.env.SOURCE_DATE_EPOCH) * 1000).toISOString()
        : new Date().toISOString(),
    },
    agents,
  };

  writeFileSync(CATALOG_PATH, `${JSON.stringify(catalog, null, 2)}\n`);
  console.log(`写出 ${CATALOG_PATH}（${agents.length} 条目，registry ${catalog.source.registryVersion}）`);
}

await main();
