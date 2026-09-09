import type { SxProps, Theme } from '@mui/material';
import type { CustomRendererProps } from 'streamdown';
import {
  Check as CheckIcon,
  Code as CodeIcon,
  ContentCopy as ContentCopyIcon,
  OpenInFull as OpenInFullScreenIcon,
  VisibilityOutlined as VisibilityOutlinedIcon,
} from '@mui/icons-material';
import { Box, Dialog, IconButton, ToggleButton, ToggleButtonGroup, Tooltip, Typography, useTheme } from '@mui/material';
import { IssueWorkspaceService } from '@src/services';
import { open as openUrl } from '@tauri-apps/plugin-shell';
import { useEffect, useMemo, useRef, useState } from 'react';
import rehypeSlug from 'rehype-slug';
import remarkBreaks from 'remark-breaks';
import { defaultRehypePlugins, defaultRemarkPlugins, Streamdown } from 'streamdown';
import CodeViewer from './CodeViewer';
import { highlightCodeBlock } from './shikiHighlighter';
import { useMathPlugin, useMermaidPlugin } from './streamdownPlugins';
import { useCopyFeedback } from './useCopyFeedback';
import { MD_CODE_LANGUAGES } from './viewerKind';
import ViewerToolbar from './ViewerToolbar';
import 'streamdown/styles.css';

// 等宽字体栈（行内码/md 代码块/源码 pre 共用）。
const MONO_FONT = '\'SF Mono\', \'Fira Code\', \'JetBrains Mono\', Menlo, Monaco, monospace';

// 自绘块体 pre（shiki 输出与纯文本兜底共用）：显式 border/borderRadius/bgcolor 归零阻断
// proseSx '& pre' 兜底规则（面向 streamdown 内置块）泄漏——否则自绘块外框内再套一圈边框
// 成框中框；调用点以 '&&' 提升特异性，属性覆盖确定性生效（不依赖 emotion 插入顺序）。
const codePreSx = {
  m: 0,
  p: '10px 12px',
  maxHeight: 480,
  overflow: 'auto',
  border: 0,
  borderRadius: 0,
  bgcolor: 'transparent',
  fontFamily: MONO_FONT,
  fontSize: '0.86rem',
  lineHeight: 1.6,
  tabSize: 4,
};

// Streamdown 的 styles.css 只承载动画/布局（--sd-* 变量），配色与排版由容器提供——
// halo 靠 tailwind prose 提供，本项目用 sx 复刻关键排版参数（标题层级与 h1/h2 下划线/
// 段落间距/表格边框/行内码底色/引用块边线，颜色全走 MUI 主题 token，明暗自适应）。
const proseSx: SxProps<Theme> = {
  // h1/h2 带下分割线（halo 主题同款），h3 以下保持纯字重层级
  '& h1': { fontSize: '1.6rem', fontWeight: 700, mt: '24px', mb: '14px', lineHeight: 1.3, pb: '10px', borderBottom: '1px solid', borderColor: 'divider' },
  '& h2': { fontSize: '1.35rem', fontWeight: 700, mt: '22px', mb: '12px', lineHeight: 1.35, pb: '8px', borderBottom: '1px solid', borderColor: 'divider' },
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
  // 行内码（块内 code 由 shiki 自绘块承载，不受此样式影响——:not(pre) 限定行内）
  '& :not(pre) > code': {
    fontFamily: MONO_FONT,
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
    fontFamily: MONO_FONT,
    fontSize: '0.86rem',
  },
  // 内置块行分隔兜底（根治代码块换行丢失）：streamdown 内置 CodeBlock 把每行渲染为
  // <span class="block">，行分隔完全依赖 tailwind 的 display:block（本项目无 tailwind，
  // 行 span 全 inline → 多行挤成一行，DOM 中亦无换行符可保留）。显式补 display:block
  // 恢复逐行展示；'>' 仅命中 code 直接子级的行 span，不波及其内 token 着色 span。
  '& [data-streamdown="code-block-body"] pre > code > span': {
    display: 'block',
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

/// 锚点 fragment 解码：URL 编码形式（浏览器自动编码的中文锚点）解码为原文；含裸 '%'
/// 的手写锚点（如「覆盖率 50%」）解码会抛 URIError，原样返回。
function safeDecodeFragment(fragment: string): string {
  try {
    return decodeURIComponent(fragment);
  } catch {
    return fragment;
  }
}

// 插件常量提为模块级：数组字面量入参会因每次渲染的身份变化击穿 Streamdown 的块级
// memoization。注意 streamdown 的 remarkPlugins/rehypePlugins 是替换语义（传入即丢弃
// 默认链——gfm 表格/删除线/内嵌 HTML 的 raw+sanitize 全丢），必须显式并入默认链再
// 追加本项目插件：
// - remark 链：gfm/codeMeta 为默认链成员；remark-breaks 追加其后——单换行渲染为换行
//   （贴合中文「每行一句」写法，区别于 GitHub 文件渲染的标准 soft break 语义；breaks
//   只作用于 text 节点，fence 内换行不受影响）
// - rehype 链：raw/sanitize/harden 为默认链成员；rehype-slug 插在 raw 之后（元素树已
//   含内嵌 HTML 解析结果）、sanitize 之前（其默认 schema 放行 id 且 clobberPrefix 为
//   空串）——给标题生成 github 同款 slug id，供 #fragment 锚点定位
const REMARK_PLUGINS = [defaultRemarkPlugins.gfm, defaultRemarkPlugins.codeMeta, remarkBreaks];
const REHYPE_PLUGINS = [
  defaultRehypePlugins.raw,
  rehypeSlug,
  defaultRehypePlugins.sanitize,
  defaultRehypePlugins.harden,
];

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

/// 自绘 md 代码块（经 Streamdown renderers 机制整块替换其内置 CodeBlock）：块体走 shiki
/// 静态高亮（VS Code 同源语法/主题，halo 同款内核；异步回填——挂载即纯文本底，与 katex
/// 插件懒加载回填同模式：静态底 = 终态排版，高亮 = 渐进增强；未知语言/失败恒纯文本）。
/// 替换原「每块一个 CM6 实例」方案：切 tab 整层重挂载时 N 个编辑器同步重建卡主线程数秒。
/// 头部（语言标签 + 复制 + 全屏）不变；全屏 Dialog 内按需挂 CM6（折叠/Cmd+F 保留，单实例
/// 打开才建）；Escape 由 Dialog 自身处理，stopPropagation 防冒泡浮层根误关预览 tab。
function MdCodeBlock({ code, language }: CustomRendererProps) {
  const theme = useTheme();
  const dark = theme.palette.mode === 'dark';
  const { copied, copy } = useCopyFeedback();
  const [fullscreen, setFullscreen] = useState(false);
  // shiki 双主题 HTML（null = 未就绪/不支持，渲染纯文本底）。
  const [html, setHtml] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    void highlightCodeBlock(code, language).then(h => alive && setHtml(h));
    return () => {
      alive = false;
    };
  }, [code, language]);

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
        onCopy={() => copy(code)}
        onToggleFullscreen={() => setFullscreen(true)}
      />
      {html != null
        ? (
            <Box
              // shiki 输出为 <pre class="shiki">（defaultColor:false 仅携带 --shiki-* 变量，
              // 布局/字体/底色全由下方 sx 提供）；token 颜色 span 级选 var，明暗切换零重高亮。
              sx={{
                '&& pre': codePreSx,
                '& span': {
                  color: `var(--shiki-${dark ? 'dark' : 'light'})`,
                  fontStyle: `var(--shiki-${dark ? 'dark' : 'light'}-font-style)`,
                  fontWeight: `var(--shiki-${dark ? 'dark' : 'light'}-font-weight)`,
                  textDecoration: `var(--shiki-${dark ? 'dark' : 'light'}-text-decoration)`,
                },
              }}
              // eslint-disable-next-line react/dom-no-dangerously-set-innerhtml -- shiki 生成的高亮 HTML：token 文本经其内部转义，来源为本地工作空间文件，无 XSS 注入面
              dangerouslySetInnerHTML={{ __html: html }}
            />
          )
        : <Box component="pre" sx={{ '&&': codePreSx }}>{code}</Box>}
      {/* 全屏：Dialog 铺满预览浮层 */}
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
            onCopy={() => copy(code)}
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
/// 排版/GFM/公式；代码块自绘渲染器（shiki 静态高亮，全屏内按需 CM6）；图片经 fileRaw
/// 直连；外链经 plugin-shell 走系统浏览器（Tauri webview 内 target=_blank 不可靠，
/// MarkdownEditor 同款处理）。头部 36px（ViewerToolbar 共享载体）：meta + 预览/源码切换
/// + 复制；源码视图复用 CodeViewer（与代码文件预览同观感）。
export default function MarkdownViewer({ content, issueId, baseDir, path }: MarkdownViewerProps) {
  const theme = useTheme();
  const dark = theme.palette.mode === 'dark';
  const [viewMode, setViewMode] = useState<'rendered' | 'source'>('rendered');
  const mathPlugin = useMathPlugin();
  const mermaidPlugin = useMermaidPlugin();
  // 预览滚动容器锚点：#fragment 点击定位目标标题后在此容器内平滑滚动。
  const scrollRef = useRef<HTMLElement | null>(null);
  // mermaid 主题随明暗（config 对象按 dark 记忆——身份变化触发图表按新主题重渲染）。
  const mermaidOptions = useMemo(() => ({ config: { theme: dark ? 'dark' : 'default' } }), [dark]);
  // fileRaw 端点 base（一次解析，渲染期同步拼 img src；path 逐张 encodeURIComponent）。
  // 端点 SSOT 在 IssueWorkspaceService.fileRawBase（与 fileRawUrl 同源）。
  const [rawBase, setRawBase] = useState<string | null>(null);
  useEffect(() => {
    let alive = true;
    IssueWorkspaceService.fileRawBase()
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

  return (
    <Box sx={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      {/* 操作栏（ViewerToolbar 共享载体）：左「Markdown · N 行」meta + 预览/源码切换，
          右复制全文——与代码文件预览的操作栏同源同观感。 */}
      <ViewerToolbar path={path} content={content}>
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
      </ViewerToolbar>

      {viewMode === 'rendered'
        ? (
            <Box ref={scrollRef} sx={{ flex: 1, minHeight: 0, overflow: 'auto', ...proseSx }}>
              <Box sx={{ maxWidth: 860, mx: 'auto', px: 3, py: 2.5, color: 'text.primary' }}>
                <Streamdown
                  mode="static"
                  // controls 全关：streamdown 的块控件（表格下载等）样式依赖 tailwind，
                  // 无 tailwind 下呈裸乱观感；代码块控件由自绘渲染器提供。
                  controls={false}
                  // shikiTheme 兜底：未列语言仍走内置块时，缺省 themes 会让其 highlight
                  // 同步崩溃（读 undefined[0]）——给合法元组保底（顺序 [light, dark]）。
                  shikiTheme={['github-light', 'github-dark']}
                  // mermaid 图表配置（主题随明暗）；插件本体经 plugins.mermaid 懒加载接入
                  mermaid={mermaidOptions}
                  remarkPlugins={REMARK_PLUGINS}
                  rehypePlugins={REHYPE_PLUGINS}
                  plugins={{
                    ...(mathPlugin != null ? { math: mathPlugin } : {}),
                    ...(mermaidPlugin != null ? { mermaid: mermaidPlugin } : {}),
                    // md 代码块自绘渲染器（renderers 是 plugins 配置项，精确语言匹配）
                    renderers: [{ language: MD_CODE_LANGUAGES, component: MdCodeBlock }],
                  }}
                  components={{
                    a: ({ href, children }) => (
                      <a
                        href={href}
                        onClick={(e) => {
                          if (typeof href !== 'string') {
                            return;
                          }
                          e.preventDefault();
                          // 锚点（#fragment）：预览容器内定位标题（rehype-slug 生成 id）
                          // 平滑滚动；外链经 plugin-shell 走系统浏览器
                          if (href.startsWith('#')) {
                            const id = safeDecodeFragment(href.slice(1));
                            scrollRef.current
                              ?.querySelector(`[id="${CSS.escape(id)}"]`)
                              ?.scrollIntoView({ behavior: 'smooth', block: 'start' });
                            return;
                          }
                          void openUrl(href);
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
