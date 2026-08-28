import { resolve } from 'node:path';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';
import pkg from './package.json';

export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version),
  },
  plugins: [react()],
  // chat 模式退役后前端暂无测试文件（原 5 个 vitest 全属 chat 工具链已删），
  // passWithNoTests 让 web:test 保持可用，后续模块补测试自然恢复。
  test: {
    passWithNoTests: true,
  },
  // strictPort: 端口被占时直接报错而非递增，避免 tauri.devUrl 连不上前端。
  server: {
    port: 7102,
    strictPort: true,
  },
  resolve: {
    alias: {
      '@src': resolve(__dirname, 'src'),
    },
  },
  build: {
    rollupOptions: {
      input: {
        panel: resolve(__dirname, 'panel.html'),
        petClaudeSessionsSummary: resolve(__dirname, 'pet-claude-sessions-summary.html'),
        petClaudeSessionsTask: resolve(__dirname, 'pet-claude-sessions-task.html'),
        settings: resolve(__dirname, 'settings.html'),
      },
    },
  },
});
