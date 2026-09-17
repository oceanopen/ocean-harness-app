import { readFileSync, writeFileSync } from 'node:fs';
import { defineConfig } from 'bumpp';

// Cargo.toml / Cargo.lock 走精确正则替换
function bumpCargoVersion(filePath: string, newVersion: string): void {
  const content = readFileSync(filePath, 'utf-8');
  const updated = content.replace(
    /(name = "ocean-harness"\nversion = ")[^"]*(")/,
    `$1${newVersion}$2`,
  );
  writeFileSync(filePath, updated);
}

// plugin.json 的 "version": "..." 精确替换（marketplace 更新检测以该版本为准）
function bumpPluginVersion(filePath: string, newVersion: string): void {
  const content = readFileSync(filePath, 'utf-8');
  const updated = content.replace(/("version":\s*")[^"]*(")/, `$1${newVersion}$2`);
  writeFileSync(filePath, updated);
}

export default defineConfig({
  files: [
    'package.json',
    'app/tauri.conf.json',
  ],
  execute: (operation) => {
    const { newVersion } = operation.state;

    bumpCargoVersion('app/Cargo.toml', newVersion);
    bumpCargoVersion('app/Cargo.lock', newVersion);
    bumpPluginVersion('plugins/ocean-harness-plugin/.claude-plugin/plugin.json', newVersion);

    operation.state.updatedFiles.push(
      'app/Cargo.toml',
      'app/Cargo.lock',
      'plugins/ocean-harness-plugin/.claude-plugin/plugin.json',
    );
  },
});
