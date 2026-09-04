// Streamdown 插件懒加载（模式移植自 hello-halo 的 streamdown-plugins.ts）：
// KaTeX 属重依赖，动态 import 首用加载并模块级缓存——不进首屏 bundle，会话内至多
// 构建一次（多渲染器共享缓存）。
// 注：Shiki 代码高亮插件（@streamdown/code）已弃用——streamdown 2.6 的代码块配色/背景
// 只通过 tailwind 任意值类生效（本项目无 tailwind，全失效），且 context 未给 shikiTheme
// 时其 highlight 会同步崩溃；md 代码块改走 renderers 自绘（见 MarkdownViewer），配色
// 复用 CM6 主题，与源码模式同观感。
import type { MathPlugin } from 'streamdown';
import { useEffect, useState } from 'react';

function createLazyPluginHook<T>(loader: () => Promise<T>): () => T | undefined {
  let cached: T | null = null;
  let loadPromise: Promise<T> | null = null;

  const load = (): Promise<T> => {
    if (!loadPromise) {
      loadPromise = loader().then((plugin) => {
        cached = plugin;
        return plugin;
      });
    }
    return loadPromise;
  };

  return function useLazyPlugin(): T | undefined {
    // 初值懒读缓存（本会话已加载过的实例首帧即终值）；未缓存时 effect 发起异步加载，
    // 回填 setState 在异步回调中（非 effect 同步段）。
    const [plugin, setPlugin] = useState<T | undefined>(() => cached ?? undefined);
    useEffect(() => {
      if (cached != null) {
        return;
      }
      void load().then(setPlugin);
    }, []);
    return plugin;
  };
}

/// KaTeX 数学插件（$...$ 行内与 $$...$$ 块级公式）。Streamdown 声明了 MathPlugin 却不注入
/// 样式——katex css 随插件一并懒加载引入（halo 验证过的必要补充）。
export const useMathPlugin = createLazyPluginHook<MathPlugin>(async () => {
  await import('katex/dist/katex.min.css');
  const [remarkMath, rehypeKatex] = await Promise.all([
    import('remark-math').then(m => m.default),
    import('rehype-katex').then(m => m.default),
  ]);
  return { name: 'katex', type: 'math', remarkPlugin: remarkMath, rehypePlugin: rehypeKatex };
});
