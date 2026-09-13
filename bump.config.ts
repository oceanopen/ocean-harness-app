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

export default defineConfig({
  files: ['package.json', 'app/tauri.conf.json'],
  execute: (operation) => {
    const { newVersion } = operation.state;
    bumpCargoVersion('app/Cargo.toml', newVersion);
    bumpCargoVersion('app/Cargo.lock', newVersion);
    operation.state.updatedFiles.push('app/Cargo.toml', 'app/Cargo.lock');
  },
});
