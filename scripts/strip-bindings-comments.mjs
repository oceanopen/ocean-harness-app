// 剥离 bindings.ts 中 tauri-specta 生成器注入的块注释（/** ... */），使生成物除文件头 // 注释外零注释。
// 挂在 gen:bindings 流程末尾执行，verify:bindings 复用同一脚本，保证生成物与仓内版本始终一致。
import { readFileSync, writeFileSync } from 'node:fs';

const file = new URL('../packages/web/src/shared/bindings.ts', import.meta.url);
const out = readFileSync(file, 'utf8').replace(/\/\*\*[\s\S]*?\*\//g, '').replace(/^\n+/, '');
writeFileSync(file, out);
