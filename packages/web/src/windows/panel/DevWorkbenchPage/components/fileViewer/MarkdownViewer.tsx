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
import { Streamdown } from 'streamdown';
import CodeViewer from './CodeViewer';
import { highlightCodeBlock } from './shikiHighlighter';
import { useMathPlugin, useMermaidPlugin } from './streamdownPlugins';
import { MONO_FONT, proseSx, REHYPE_PLUGINS, REMARK_PLUGINS } from './streamdownShared';
import { useCopyFeedback } from './useCopyFeedback';
import { MD_CODE_LANGUAGES } from './viewerKind';
import ViewerToolbar from './ViewerToolbar';
import 'streamdown/styles.css';

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
  fontSize: 14,
  lineHeight: 1.6,
  tabSize: 4,
};

// Streamdown 渲染共享常量（proseSx/插件链/MONO_FONT）在本目录 streamdownShared.ts，
// 与 ACP 会话视图等同构消费方共用。

interface MarkdownViewerProps {
  content: string;
  /// 图片相对路径解析与 fileRaw URL 构造上下文（issueId + 本文件相对路径）。
  issueId: string;
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

// 插件链常量见 streamdownShared.ts（模块级防击穿块级 memo，注释同处）。

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
export default function MarkdownViewer({ content, issueId, path }: MarkdownViewerProps) {
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
    return `${rawBase}?issueId=${encodeURIComponent(issueId)}&path=${encodeURIComponent(relPath)}`;
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
