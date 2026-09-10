import { resolve } from 'node:path';
import react from '@vitejs/plugin-react';
import Icons from 'unplugin-icons/vite';
import { defineConfig } from 'vitest/config';
import pkg from './package.json';

export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version),
  },
  // unplugin-icons（compiler: 'jsx' 适配 React）：~icons/<集合>/<图标名> 虚拟模块按需
  // 引入图标（文件树按扩展名的 vscode-icons 彩色图标，Halo ui 同款链路），构建期
  // tree-shake 只打进用到的图标。scale: 1 抵消默认 1.2 的尺寸放大（svg 属性 1em，
  // 消费方 fontSize 即精确像素）；类型声明见 tsconfig.app.json 的 types。
  plugins: [react(), Icons({ compiler: 'jsx', scale: 1 })],
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
        petSessionSummary: resolve(__dirname, 'pet-session-summary.html'),
        petSessionTask: resolve(__dirname, 'pet-session-task.html'),
      },
    },
  },
});
