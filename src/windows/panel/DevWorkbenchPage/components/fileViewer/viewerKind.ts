// 文件预览呈现分派（纯函数）：仅对传输 kind=text 的文件按扩展名细分——markdown 走渲染器，
// 其余（含未知扩展）走 CM6 只读视图（无语言扩展时即纯文本等宽展示，不设第三档）。
// 传输层 kind（text/image/binary/tooLarge）由后端定夺，前端不猜——见 services/IssueWorkspaceService.ts。
import type { Extension } from '@codemirror/state';
import { cpp } from '@codemirror/lang-cpp';
import { css } from '@codemirror/lang-css';
import { go } from '@codemirror/lang-go';
import { html } from '@codemirror/lang-html';
import { java } from '@codemirror/lang-java';
import { javascript } from '@codemirror/lang-javascript';
import { json } from '@codemirror/lang-json';
import { markdown } from '@codemirror/lang-markdown';
import { php } from '@codemirror/lang-php';
import { python } from '@codemirror/lang-python';
import { rust } from '@codemirror/lang-rust';
import { sql } from '@codemirror/lang-sql';
import { vue } from '@codemirror/lang-vue';
import { xml } from '@codemirror/lang-xml';
import { yaml } from '@codemirror/lang-yaml';

export type TextViewerKind
  = | 'markdown'
    | 'code';

function extOf(path: string): string {
  const dot = path.lastIndexOf('.');
  return dot < 0 ? '' : path.slice(dot + 1).toLowerCase();
}

/// token → CM6 语言扩展：token 兼收文件扩展名（ts/go）与 fence 语言名（typescript/golang）。
/// 未命中返回 []——纯文本等宽展示。刻意不引 legacy-modes 做近似映射（halo 的
/// elixir→erlang 类降级）：没有准确高亮就纯文本。
function langByToken(tok: string): Extension {
  switch (tok) {
    case 'js':
    case 'mjs':
    case 'cjs':
    case 'javascript':
      return javascript();
    case 'jsx':
      return javascript({ jsx: true });
    case 'ts':
    case 'mts':
    case 'cts':
    case 'typescript':
      return javascript({ typescript: true });
    case 'tsx':
      return javascript({ typescript: true, jsx: true });
    case 'go':
    case 'golang':
      return go();
    case 'py':
    case 'pyi':
    case 'python':
      return python();
    case 'rs':
    case 'rust':
      return rust();
    case 'java':
      return java();
    case 'php':
      return php();
    case 'json':
    case 'jsonc':
      return json();
    case 'yaml':
    case 'yml':
      return yaml();
    case 'sql':
      return sql();
    case 'c':
    case 'h':
    case 'cc':
    case 'cpp':
    case 'hpp':
    case 'c++':
      return cpp();
    case 'css':
    case 'scss':
    case 'less':
      return css();
    case 'html':
    case 'htm':
    case 'svelte':
      return html();
    case 'vue':
      return vue();
    case 'xml':
    case 'svg':
      return xml();
    case 'md':
    case 'markdown':
      return markdown();
    default:
      return [];
  }
}

/// text 内容的呈现分派：md/markdown → markdown 渲染器；其余 → CM6（未命中语言表时纯文本）。
export function resolveTextViewer(path: string): TextViewerKind {
  const ext = extOf(path);
  return ext === 'md' || ext === 'markdown' ? 'markdown' : 'code';
}

/// 文件路径（取扩展名）→ CM6 语言扩展。
export function codeMirrorLanguage(path: string): Extension {
  return langByToken(extOf(path));
}

/// 语言名（md 代码围栏的 fence 语言标识，如 "typescript"/"ts"）→ CM6 语言扩展。
/// 含点时按扩展名取段（如 "foo.ts"）；bare 名直接查表（长名别名在 langByToken 内归一）。
export function codeMirrorLanguageFor(name: string): Extension {
  return langByToken(name.includes('.') ? extOf(name) : name.toLowerCase());
}
