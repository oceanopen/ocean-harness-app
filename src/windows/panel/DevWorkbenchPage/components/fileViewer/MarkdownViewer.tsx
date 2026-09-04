import type { SxProps, Theme } from '@mui/material';
import type { CustomRendererProps } from 'streamdown';
import {
  Check as CheckIcon,
  Code as CodeIcon,
  ContentCopy as ContentCopyIcon,
  OpenInFull as OpenInFullScreenIcon,
  VisibilityOutlined as VisibilityOutlinedIcon,
} from '@mui/icons-material';
import { Box, Dialog, IconButton, ToggleButton, ToggleButtonGroup, Tooltip, Typography } from '@mui/material';
import { serverUrl } from '@src/services/http';
import { open as openUrl } from '@tauri-apps/plugin-shell';
import { useEffect, useState } from 'react';
import { Streamdown } from 'streamdown';
import CodeViewer from './CodeViewer';
import { useMathPlugin } from './streamdownPlugins';
import 'streamdown/styles.css';

// Streamdown 的 styles.css 只承载动画/布局（--sd-* 变量），配色与排版由容器提供——
// halo 靠 tailwind prose 提供，本项目用 sx 复刻关键排版参数（标题层级/段落间距/表格
// 边框/行内码底色/引用块边线，颜色全走 MUI 主题 token，明暗自适应）。
const proseSx: SxProps<Theme> = {
  '& h1': { fontSize: '1.6rem', fontWeight: 700, mt: '24px', mb: '14px', lineHeight: 1.3 },
  '& h2': { fontSize: '1.35rem', fontWeight: 700, mt: '22px', mb: '12px', lineHeight: 1.35 },
  '& h3': { fontSize: '1.15rem', fontWeight: 600, mt: '20px', mb: '10px', lineHeight: 1.4 },
  '& h4, & h5, & h6': { fontSize: '1rem', fontWeight: 600, mt: '16px', mb: '8px' },
  '& p': { my: '10px', lineHeight: 1.75 },
  '& ul, & ol': { my: '10px', pl: '26px' },
  '& li': { my: '3px', lineHeight: 1.7 },
  '& a': { color: 'primary.main' },
  '& img': { maxWidth: 'min(100%, 880px)', height: 'auto', borderRadius: '8px', my: '8px' },
  '& blockquote': {
    my: '12px',
    pl: '14px',
    borderLeft: '3px solid',
    borderColor: 'divider',
    color: 'text.secondary',
  },
  // 行内码（块内 code 由 CodeViewer 承载，不受此样式影响——:not(pre) 限定行内）
  '& :not(pre) > code': {
    fontFamily: '\'SF Mono\', \'Fira Code\', \'JetBrains Mono\', Menlo, Monaco, monospace',
    fontSize: '0.86em',
    bgcolor: 'action.hover',
    px: '4px',
    py: '1px',
    borderRadius: '4px',
  },
  // 兜底 pre（未列入 renderers 清表的罕见语言仍走 streamdown 内置块）
  '& pre': {
    my: '12px',
    p: '12px 14px',
    overflowX: 'auto',
    border: 1,
    borderColor: 'divider',
    borderRadius: 1,
    bgcolor: 'background.default',
    fontFamily: '\'SF Mono\', \'Fira Code\', \'JetBrains Mono\', Menlo, Monaco, monospace',
    fontSize: '0.86rem',
  },
  '& table': { my: '12px', borderCollapse: 'collapse', width: '100%', display: 'block', overflowX: 'auto' },
  '& th, & td': { border: '1px solid', borderColor: 'divider', px: '10px', py: '6px', textAlign: 'left' },
  '& th': { bgcolor: 'action.hover', fontWeight: 600 },
  '& hr': { my: '20px', borderColor: 'divider' },
};

interface MarkdownViewerProps {
  content: string;
  /// 图片相对路径解析与 fileRaw URL 构造上下文（issueId + baseDir + 本文件相对路径）。
  issueId: string;
  baseDir: string;
  path: string;
}

/// md 内相对图片解析（移植 halo resolveImageSrc，语义简化为「解析为工作空间相对路径」）：
/// http(s)/data 等 URL 原样保留（null）；'/' 开头相对工作空间根；其余相对本文件目录，
/// '.'/'..' 逐段处理（'..' 在根处钳制）。返回工作空间相对正斜杠路径。
function resolveRelativeSrc(src: string, basePath: string): string | null {
  if (/^(?:https?|data|asset|blob|mailto):/i.test(src)) {
    return null;
  }
  const parts = src.startsWith('/') ? [] : basePath.split('/').filter(Boolean);
  for (const seg of src.split('/')) {
    if (seg === '' || seg === '.') {
      continue;
    }
    if (seg === '..') {
      parts.pop();
      continue;
    }
    parts.push(seg);
  }
  return parts.join('/');
}

/// 自绘 md 代码块头部（语言标签 + 复制 + 全屏）——不含下载按钮（halo 同款按钮为已知
/// bug，刻意不跟）。fullscreen 时头部进 Dialog（全屏钮换关闭钮）。
function CodeBlockHeader({ language, copied, onCopy, onToggleFullscreen }: {
  language: string;
  copied: boolean;
  onCopy: () => void;
  onToggleFullscreen: () => void;
}) {
  return (
    <Box
      sx={{
        height: 28,
        flexShrink: 0,
        display: 'flex',
        alignItems: 'center',
        px: 1,
        gap: 0.5,
        borderBottom: 1,
        borderColor: 'divider',
        bgcolor: 'action.hover',
      }}
    >
      <Typography variant="caption" sx={{ fontFamily: 'monospace', color: 'text.secondary' }}>
        {language === '' ? 'text' : language}
      </Typography>
      <Box sx={{ flex: 1 }} />
      <Tooltip title={copied ? '已复制' : '复制代码'}>
        <IconButton size="small" aria-label="复制代码" onClick={onCopy} sx={{ color: 'text.secondary' }}>
          {copied ? <CheckIcon sx={{ fontSize: 15, color: 'success.main' }} /> : <ContentCopyIcon sx={{ fontSize: 15 }} />}
        </IconButton>
      </Tooltip>
      <Tooltip title="全屏查看">
        <IconButton size="small" aria-label="全屏查看代码块" onClick={onToggleFullscreen} sx={{ color: 'text.secondary' }}>
          <OpenInFullScreenIcon sx={{ fontSize: 15 }} />
        </IconButton>
      </Tooltip>
    </Box>
  );
}

/// md 代码围栏语言清单（Streamdown renderers 为精确匹配、无通配——清单覆盖常见 fence
/// 标识，未列语言回落 streamdown 内置块）。CM 语言表未命中的（sh/toml 等）由 CodeViewer
/// 纯文本兜底，观感仍统一。
const MD_CODE_LANGUAGES = [
  'ts',
  'tsx',
  'js',
  'jsx',
  'javascript',
  'typescript',
  'mjs',
  'cjs',
  'mts',
  'cts',
  'go',
  'golang',
  'py',
  'python',
  'pyi',
  'rs',
  'rust',
  'java',
  'php',
  'json',
  'jsonc',
  'yaml',
  'yml',
  'xml',
  'svg',
  'html',
  'htm',
  'vue',
  'svelte',
  'css',
  'scss',
  'less',
  'sql',
  'markdown',
  'md',
  'c',
  'h',
  'cc',
  'cpp',
  'c++',
  'hpp',
  'sh',
  'bash',
  'zsh',
  'shell',
  'shell-session',
  'console',
  'text',
  'plaintext',
  'txt',
  'diff',
  'patch',
  'log',
  'env',
  'toml',
  'ini',
  'conf',
  'properties',
  'dockerfile',
  'docker',
  'makefile',
  'make',
  'graphql',
  'gql',
  'kotlin',
  'kt',
  'swift',
  'ruby',
  'rb',
  'lua',
  'dart',
  'proto',
  'protobuf',
  'nginx',
];

/// 自绘 md 代码块（经 Streamdown renderers 机制整块替换其内置 CodeBlock）：配色/明暗/
/// 折叠/搜索复用 CodeViewer（CM6，与源码模式同观感），不受 streamdown 内置块对 tailwind
/// 类的依赖（无 tailwind 时其配色与块样式全部失效，且 shikiTheme 缺省时其 highlight
/// 同步崩溃）。块内超高内部滚动；全屏走 Dialog 铺满预览浮层。
function MdCodeBlock({ code, language }: CustomRendererProps) {
  const [copied, setCopied] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);

  const handleCopy = () => {
    void navigator.clipboard.writeText(code).then(() => {
      setCopied(true);
      setTimeout(setCopied, 2000, false);
    });
  };

  return (
    <Box
      sx={{
        my: 1.5,
        border: 1,
        borderColor: 'divider',
        borderRadius: 1,
        overflow: 'hidden',
        bgcolor: 'background.default',
      }}
    >
      <CodeBlockHeader
        language={language}
        copied={copied}
        onCopy={handleCopy}
        onToggleFullscreen={() => setFullscreen(true)}
      />
      <Box sx={{ '& .cm-scroller': { maxHeight: 480 } }}>
        <CodeViewer content={code} path="" language={language} scrollPastEnd={false} />
      </Box>
      {/* 全屏：Dialog 铺满预览浮层（Escape 由 Dialog 自身处理；stopPropagation 防冒泡
          到浮层根误关预览 tab） */}
      <Dialog
        fullScreen
        open={fullscreen}
        onClose={() => setFullscreen(false)}
        onKeyDown={e => e.stopPropagation()}
      >
        <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
          <CodeBlockHeader
            language={language}
            copied={copied}
            onCopy={handleCopy}
            onToggleFullscreen={() => setFullscreen(false)}
          />
          <Box sx={{ flex: 1, minHeight: 0 }}>
            <CodeViewer content={code} path="" language={language} />
          </Box>
        </Box>
      </Dialog>
    </Box>
  );
}

/// Markdown 只读渲染（观感对齐 halo，实现按本项目栈裁剪）：Streamdown static 模式承载
/// 排版/GFM/公式；代码块走自绘渲染器（CM6）；图片经 fileRaw 直连；外链经 plugin-shell
/// 走系统浏览器（Tauri webview 内 target=_blank 不可靠，MarkdownEditor 同款处理）。
/// 头部 36px：预览/源码切换 + 复制；源码视图复用 CodeViewer（与代码文件预览同观感）。
export default function MarkdownViewer({ content, issueId, baseDir, path }: MarkdownViewerProps) {
  const [viewMode, setViewMode] = useState<'rendered' | 'source'>('rendered');
  const [copied, setCopied] = useState(false);
  const mathPlugin = useMathPlugin();
  // fileRaw 端点 base（一次解析，渲染期同步拼 img src；path 逐张 encodeURIComponent）。
  const [rawBase, setRawBase] = useState<string | null>(null);
  useEffect(() => {
    let alive = true;
    serverUrl('/api/issueWorkspace/fileRaw', {})
      .then(url => alive && setRawBase(url))
      .catch(e => console.warn('[MarkdownViewer] resolve fileRaw base failed:', e));
    return () => {
      alive = false;
    };
  }, []);

  const slash = path.lastIndexOf('/');
  const basePath = slash < 0 ? '' : path.slice(0, slash);
  const fileRawHref = (relPath: string): string | null => {
    if (rawBase == null) {
      return null;
    }
    return `${rawBase}?issueId=${encodeURIComponent(issueId)}&baseDir=${encodeURIComponent(baseDir)}&path=${encodeURIComponent(relPath)}`;
  };

  const handleCopy = () => {
    void navigator.clipboard.writeText(content).then(() => {
      setCopied(true);
      setTimeout(setCopied, 2000, false);
    });
  };

  return (
    <Box sx={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      {/* 工具栏：预览/源码切换 + 复制（PanelToolbar 同款 36px 带） */}
      <Box
        sx={{
          height: 36,
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
          px: 1,
          gap: 0.5,
          borderBottom: 1,
          borderColor: 'divider',
        }}
      >
        <ToggleButtonGroup
          size="small"
          exclusive
          value={viewMode}
          onChange={(_, v) => v != null && setViewMode(v)}
        >
          <ToggleButton value="rendered" aria-label="渲染预览" sx={{ py: 0.25, px: 1.25 }}>
            <VisibilityOutlinedIcon sx={{ fontSize: 15, mr: 0.5 }} />
            <Typography variant="caption">预览</Typography>
          </ToggleButton>
          <ToggleButton value="source" aria-label="查看源码" sx={{ py: 0.25, px: 1.25 }}>
            <CodeIcon sx={{ fontSize: 15, mr: 0.5 }} />
            <Typography variant="caption">源码</Typography>
          </ToggleButton>
        </ToggleButtonGroup>
        <Box sx={{ flex: 1 }} />
        <Tooltip title={copied ? '已复制' : '复制全文'}>
          <IconButton size="small" onClick={handleCopy} aria-label="复制 markdown 全文" sx={{ color: 'text.secondary' }}>
            {copied ? <CheckIcon sx={{ fontSize: 16, color: 'success.main' }} /> : <ContentCopyIcon sx={{ fontSize: 16 }} />}
          </IconButton>
        </Tooltip>
      </Box>

      {viewMode === 'rendered'
        ? (
            <Box sx={{ flex: 1, minHeight: 0, overflow: 'auto', ...proseSx }}>
              <Box sx={{ maxWidth: 860, mx: 'auto', px: 3, py: 2.5, color: 'text.primary' }}>
                <Streamdown
                  mode="static"
                  // controls 全关：streamdown 的块控件（表格下载等）样式依赖 tailwind，
                  // 无 tailwind 下呈裸乱观感；代码块控件由自绘渲染器提供。
                  controls={false}
                  // shikiTheme 兜底：未列语言仍走内置块时，缺省 themes 会让其 highlight
                  // 同步崩溃（读 undefined[0]）——给合法元组保底（顺序 [light, dark]）。
                  shikiTheme={['github-light', 'github-dark']}
                  plugins={{
                    ...(mathPlugin != null ? { math: mathPlugin } : {}),
                    // md 代码块自绘渲染器（renderers 是 plugins 配置项，精确语言匹配）
                    renderers: [{ language: MD_CODE_LANGUAGES, component: MdCodeBlock }],
                  }}
                  components={{
                    a: ({ href, children }) => (
                      <a
                        href={href}
                        onClick={(e) => {
                          if (typeof href === 'string') {
                            e.preventDefault();
                            void openUrl(href);
                          }
                        }}
                      >
                        {children}
                      </a>
                    ),
                    img: ({ src, alt }) => {
                      const rel = typeof src === 'string' ? resolveRelativeSrc(src, basePath) : null;
                      const href = rel != null ? fileRawHref(rel) : (typeof src === 'string' ? src : null);
                      return href != null ? <img src={href} alt={alt} loading="lazy" /> : null;
                    },
                  }}
                >
                  {content}
                </Streamdown>
              </Box>
            </Box>
          )
        : <CodeViewer content={content} path={path} />}
    </Box>
  );
}
