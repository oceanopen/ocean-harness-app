import type { SxProps, Theme } from '@mui/material';
import rehypeSlug from 'rehype-slug';
import remarkBreaks from 'remark-breaks';
import { defaultRehypePlugins, defaultRemarkPlugins } from 'streamdown';

// Streamdown 渲染的共享常量（MarkdownViewer 与 ACP 会话视图等同构消费方共用；
// 独立成文件满足 react-refresh 的「组件文件只导出组件」约束）。

// 等宽字体栈（行内码/md 代码块/源码 pre 共用）。
export const MONO_FONT = '\'SF Mono\', \'Fira Code\', \'JetBrains Mono\', Menlo, Monaco, monospace';

// Streamdown 的 styles.css 只承载动画/布局（--sd-* 变量），配色与排版由容器提供——
// halo 靠 tailwind prose 提供，本项目用 sx 复刻关键排版参数（标题层级与 h1/h2 下划线/
// 段落间距/表格边框/行内码底色/引用块边线，颜色全走 MUI 主题 token，明暗自适应）。
export const proseSx: SxProps<Theme> = {
  // h1/h2 带下分割线（halo 主题同款），h3 以下保持纯字重层级
  '& h1': { fontSize: 26, fontWeight: 700, mt: '24px', mb: '14px', lineHeight: 1.3, pb: '10px', borderBottom: '1px solid', borderColor: 'divider' },
  '& h2': { fontSize: 22, fontWeight: 700, mt: '22px', mb: '12px', lineHeight: 1.35, pb: '8px', borderBottom: '1px solid', borderColor: 'divider' },
  '& h3': { fontSize: 18, fontWeight: 600, mt: '20px', mb: '10px', lineHeight: 1.4 },
  '& h4, & h5, & h6': { fontSize: 16, fontWeight: 600, mt: '16px', mb: '8px' },
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
    fontSize: 12,
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
    fontSize: 14,
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
export const REMARK_PLUGINS = [defaultRemarkPlugins.gfm, defaultRemarkPlugins.codeMeta, remarkBreaks];
export const REHYPE_PLUGINS = [
  defaultRehypePlugins.raw,
  rehypeSlug,
  defaultRehypePlugins.sanitize,
  defaultRehypePlugins.harden,
];
