import { resolve } from 'node:path';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';
import pkg from './package.json';

export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version),
  },
  plugins: [react()],
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
