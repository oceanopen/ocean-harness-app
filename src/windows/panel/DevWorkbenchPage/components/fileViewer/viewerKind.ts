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

/// 路径（含点时取末段）→ 小写扩展名；无扩展名返回空串。预览分派与文件树图标分派共用。
export function extOf(path: string): string {
  const dot = path.lastIndexOf('.');
  return dot < 0 ? '' : path.slice(dot + 1).toLowerCase();
}

// 扩展名 → 语言显示名（文件预览操作栏左侧 meta 用）：覆盖 langByToken 语言表全部条目
// 及 md 代码围栏常见标识；键集同时派生 MD_CODE_LANGUAGES（见下），新增语言时与
// langByToken 两处同步。未命中回退原始扩展名（如 env/dockerfile 自身即语义），不猜近似语言。
const LANGUAGE_LABELS: Record<string, string> = {
  'js': 'JavaScript',
  'mjs': 'JavaScript',
  'cjs': 'JavaScript',
  'javascript': 'JavaScript',
  'jsx': 'JSX',
  'ts': 'TypeScript',
  'mts': 'TypeScript',
  'cts': 'TypeScript',
  'typescript': 'TypeScript',
  'tsx': 'TSX',
  'go': 'Go',
  'golang': 'Go',
  'py': 'Python',
  'pyi': 'Python',
  'python': 'Python',
  'rs': 'Rust',
  'rust': 'Rust',
  'java': 'Java',
  'php': 'PHP',
  'json': 'JSON',
  'jsonc': 'JSON',
  'yaml': 'YAML',
  'yml': 'YAML',
  'sql': 'SQL',
  'c': 'C',
  'h': 'C',
  'cc': 'C++',
  'cpp': 'C++',
  'hpp': 'C++',
  'c++': 'C++',
  'css': 'CSS',
  'scss': 'SCSS',
  'less': 'Less',
  'html': 'HTML',
  'htm': 'HTML',
  'svelte': 'Svelte',
  'vue': 'Vue',
  'xml': 'XML',
  'svg': 'SVG',
  'md': 'Markdown',
  'markdown': 'Markdown',
  'sh': 'Shell',
  'bash': 'Shell',
  'zsh': 'Shell',
  'shell': 'Shell',
  'shell-session': 'Shell',
  'console': 'Shell',
  'powershell': 'PowerShell',
  'ps1': 'PowerShell',
  'pwsh': 'PowerShell',
  'cmd': 'Batch',
  'bat': 'Batch',
  'batch': 'Batch',
  'toml': 'TOML',
  'ini': 'INI',
  'conf': 'Conf',
  'properties': 'Properties',
  'graphql': 'GraphQL',
  'gql': 'GraphQL',
  'kotlin': 'Kotlin',
  'kt': 'Kotlin',
  'swift': 'Swift',
  'ruby': 'Ruby',
  'rb': 'Ruby',
  'lua': 'Lua',
  'dart': 'Dart',
  'proto': 'Protobuf',
  'protobuf': 'Protobuf',
  'nginx': 'Nginx',
  'diff': 'Diff',
  'patch': 'Diff',
  'dockerfile': 'Dockerfile',
  'docker': 'Dockerfile',
  'makefile': 'Makefile',
  'make': 'Makefile',
  'cmake': 'CMake',
  'txt': 'Text',
  'text': 'Text',
  'plaintext': 'Text',
  'log': 'Log',
  'env': 'Env',
};

/// 文件路径 → 语言显示名（预览操作栏 meta，如 "TypeScript · 128 行"）。未识别回退
/// 原始扩展名（空扩展名回退 'File'），不猜近似语言（与 langByToken 立场一致）。
export function languageLabel(path: string): string {
  const ext = extOf(path);
  return LANGUAGE_LABELS[ext] ?? (ext !== '' ? ext : 'File');
}

/// md 代码围栏语言清单（MarkdownViewer 的 Streamdown renderers 用，精确匹配、无通配——
/// 未列语言回落 streamdown 内置块；清单内语言由 shiki 静态高亮或纯文本底，统一带
/// 自绘复制/全屏头）。由语言名表派生 + 'tree'（无着色语言，仅为统一自绘块观感入列）。
/// 原为 MarkdownViewer 内手写 84 项平行清单、与语言名表键集完全重合——派生归一后
/// 新增语言只改 LANGUAGE_LABELS 与 langByToken 两处。裸 ``` 围栏（语言标识为空）被
/// streamdown 空值守卫拦下，必落内置块（MarkdownViewer proseSx 兜底），与本清单无关。
export const MD_CODE_LANGUAGES: string[] = [...Object.keys(LANGUAGE_LABELS), 'tree'];

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
