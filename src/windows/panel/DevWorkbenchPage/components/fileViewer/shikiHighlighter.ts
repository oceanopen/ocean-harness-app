// md 代码块 shiki 静态高亮（halo 同款高亮内核——@streamdown/code 的底层引擎）：
// VS Code 同源 TextMate 语法/主题，输出纯静态 HTML（token 颜色内联 CSS var），生成后
// 零运行时开销。单例懒加载模式同 streamdownPlugins.ts 的 katex 插件：动态 import 不进
// 首屏 bundle，会话内至多构建一次（多块共享缓存）；语言语法按需懒加载——shiki 内部
// 幂等守卫（已加载语法短路）+ ES module import 缓存天然去重，无需额外簿记。
//
// 失败可重试语义：单例与语言加载失败均向上抛（调用方 catch 回退纯文本），单例缓存
// 置空，下次调用重建/重载——rejected promise 滞留模块级变量会让本会话高亮静默失效。
//
// 背景：原方案每块挂一个 CM6 EditorView（折叠/搜索全家桶），切 tab 整层重挂载时
// N 个编辑器同步重建卡主线程数秒——只读块体改静态高亮后成本降约两个数量级，折叠/
// 搜索保留在全屏 Dialog 的按需 CM6 实例里（见 MarkdownViewer MdCodeBlock）。

import type { BundledLanguage, Highlighter } from 'shiki';

// 无着色意义的围栏语言直接走纯文本兜底（不加载任何语法，视觉与 shiki text 等价）。
const PLAIN_LANGS = new Set(['', 'text', 'plaintext', 'txt']);

let highlighterPromise: Promise<Highlighter> | null = null;

function getHighlighter(): Promise<Highlighter> {
  highlighterPromise ??= import('shiki')
    .then(shiki =>
      shiki.createHighlighter({
        themes: ['github-light', 'github-dark'],
        langs: [],
      }),
    )
    .catch((e) => {
      highlighterPromise = null;
      throw e;
    });
  return highlighterPromise;
}

// 围栏语言标识 → shiki 语言 id（bundledLanguagesInfo 归一 id 与 aliases，如 'c++'→'cpp'、
// 'zsh'→'shellscript'）；纯文本与未收录语言返回 null（调用方回退纯文本 pre）。
async function resolveShikiLangId(fenceLang: string): Promise<BundledLanguage | null> {
  const token = fenceLang.trim().toLowerCase();
  if (PLAIN_LANGS.has(token)) {
    return null;
  }
  const shiki = await import('shiki');
  const info = shiki.bundledLanguagesInfo.find(
    i => i.id === token || i.aliases?.includes(token),
  );
  return (info?.id ?? null) as BundledLanguage | null;
}

/// 高亮为双主题静态 HTML。defaultColor:false → 颜色不落内联，token 走 --shiki-light /
/// --shiki-dark CSS var（含 font-style/weight/text-decoration 变体），明暗切换由容器
/// sx 按 MUI palette mode 选取对应 var，无需重新高亮。任何失败（未知语言/语法加载异常）
/// 返回 null，调用方回退纯文本 pre。
export async function highlightCodeBlock(code: string, fenceLang: string): Promise<string | null> {
  try {
    const langId = await resolveShikiLangId(fenceLang);
    if (langId == null) {
      return null;
    }
    const highlighter = await getHighlighter();
    await highlighter.loadLanguage(langId);
    return highlighter.codeToHtml(code, {
      lang: langId,
      themes: { light: 'github-light', dark: 'github-dark' },
      defaultColor: false,
    });
  } catch (e) {
    console.warn('[shikiHighlighter] highlight failed, fallback to plain:', fenceLang, e);
    return null;
  }
}
