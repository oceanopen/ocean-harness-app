// CM6 代码查看主题（移植自 hello-halo 的 renderer/lib/codemirror-theme.ts，观感对齐）。
// 差异：halo 用 shadcn CSS 变量（hsl(var(--background)) 等），本项目无 tailwind/CSS 变量
// 体系——改为函数式构建：调用方传入 MUI palette + 明暗标志，明暗切换经 Compartment
// 热切重建（见 CodeViewer）。语法高亮色板照搬 halo 的 One Dark Pro 硬编码色（暗色
// 最佳、亮色可接受，halo 同款取舍）。
import type { Extension } from '@codemirror/state';
import type { Palette } from '@mui/material/styles';
import { defaultHighlightStyle, HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { EditorView } from '@codemirror/view';
import { tags } from '@lezer/highlight';
import { alpha } from '@mui/material/styles';

/// 编辑器结构主题：字体栈（SF Mono 系）/行高/内容内边距/gutter 样式/活动行/折叠槽/
/// 搜索面板全套（输入框/按钮/匹配高亮/关闭钮）——halo codemirror-theme 的观感主体，
/// 颜色逐项映射到 MUI palette token（background/text/divider/action/primary/error）。
function editorTheme(p: Palette, dark: boolean): Extension {
  return EditorView.theme(
    {
      '&': {
        height: '100%',
        fontSize: '13px',
        fontFamily: '\'SF Mono\', \'Fira Code\', \'JetBrains Mono\', Menlo, Monaco, \'Courier New\', monospace',
        backgroundColor: p.background.default,
        color: p.text.primary,
      },

      '.cm-content': {
        padding: '16px 0',
        caretColor: p.primary.main,
        fontFamily: 'inherit',
        lineHeight: '1.6',
      },

      '.cm-scroller': {
        overflow: 'auto',
        fontFamily: 'inherit',
      },

      // 压掉 webkit 横竖滚动条交角处的白块（halo 同款处理）。
      '.cm-scroller::-webkit-scrollbar-corner': {
        backgroundColor: p.background.default,
      },

      '&.cm-focused .cm-cursor': {
        borderLeftColor: p.primary.main,
        borderLeftWidth: '2px',
      },

      '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': {
        backgroundColor: alpha(p.primary.main, 0.2),
      },

      '.cm-gutters': {
        backgroundColor: p.background.default,
        borderRight: `1px solid ${alpha(p.divider, 0.5)}`,
        color: alpha(p.text.secondary, 0.5),
        fontFamily: 'inherit',
      },

      '.cm-lineNumbers .cm-gutterElement': {
        padding: '0 12px 0 16px',
        minWidth: '40px',
        textAlign: 'right',
      },

      '.cm-foldGutter .cm-gutterElement': {
        padding: '0 4px',
        cursor: 'pointer',
        color: alpha(p.text.secondary, 0.4),
        transition: 'color 0.15s ease',
      },

      '.cm-foldGutter .cm-gutterElement:hover': {
        color: p.text.primary,
      },

      // 活动行直接用 action.hover（MUI 自带 4-8% 透明度的微灰覆盖）——不可再包 alpha()：
      // alpha() 会整体覆盖其透明度（如 0.6 → 深色下 60% 白，刺眼）。
      '.cm-activeLine': {
        backgroundColor: p.action.hover,
      },

      '.cm-activeLineGutter': {
        backgroundColor: p.action.hover,
        color: alpha(p.text.primary, 0.7),
      },

      '&.cm-focused .cm-matchingBracket': {
        backgroundColor: alpha(p.primary.main, 0.2),
        outline: `1px solid ${alpha(p.primary.main, 0.5)}`,
      },

      // 搜索面板（Cmd+F）
      '.cm-panels': {
        backgroundColor: p.background.paper,
        borderBottom: `1px solid ${p.divider}`,
      },

      '.cm-panels.cm-panels-top': {
        borderBottom: `1px solid ${p.divider}`,
      },

      '.cm-panel.cm-search': {
        display: 'flex',
        alignItems: 'center',
        flexWrap: 'wrap',
        gap: '4px',
        padding: '6px 8px',
        fontSize: '13px',
      },

      '.cm-panel.cm-search label': {
        display: 'inline-flex',
        alignItems: 'center',
        gap: '3px',
        fontSize: '12px',
        color: p.text.secondary,
        cursor: 'pointer',
        userSelect: 'none',
        whiteSpace: 'nowrap',
      },

      '.cm-panel.cm-search input[type=checkbox]': {
        margin: '0',
        cursor: 'pointer',
      },

      '.cm-panel.cm-search button[name=close]': {
        position: 'absolute',
        top: '6px',
        right: '8px',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: '20px',
        height: '20px',
        padding: '0',
        margin: '0',
        border: 'none',
        borderRadius: '4px',
        backgroundColor: 'transparent',
        color: p.text.secondary,
        cursor: 'pointer',
        fontSize: '16px',
        lineHeight: '1',
      },

      '.cm-panel.cm-search button[name=close]:hover': {
        backgroundColor: p.action.hover,
        color: p.text.primary,
      },

      // replace 行换行到新 flex 行（CM 基础样式用 <br> 断行）。
      '.cm-panel.cm-search br': {
        width: '100%',
        height: '0',
        flexBasis: '100%',
      },

      '.cm-searchMatch': {
        backgroundColor: 'rgba(250, 204, 21, 0.3)',
        outline: '1px solid rgba(250, 204, 21, 0.5)',
      },

      '.cm-searchMatch.cm-searchMatch-selected': {
        backgroundColor: alpha(p.primary.main, 0.3),
        outline: `1px solid ${p.primary.main}`,
      },

      '.cm-textfield': {
        backgroundColor: p.background.paper,
        border: `1px solid ${p.divider}`,
        borderRadius: '4px',
        padding: '4px 8px',
        color: p.text.primary,
        fontSize: '13px',
        lineHeight: '1.4',
        outline: 'none',
      },

      '.cm-textfield:focus': {
        borderColor: p.primary.main,
        boxShadow: `0 0 0 2px ${alpha(p.primary.main, 0.2)}`,
      },

      '.cm-button': {
        WebkitAppearance: 'none',
        appearance: 'none',
        backgroundImage: 'none',
        backgroundColor: p.action.selected,
        border: `1px solid ${p.divider}`,
        borderRadius: '4px',
        padding: '4px 8px',
        color: p.text.primary,
        fontSize: '12px',
        lineHeight: '1.4',
        cursor: 'pointer',
        whiteSpace: 'nowrap',
        transition: 'background-color 0.15s ease',
      },

      '.cm-button:hover': {
        backgroundColor: p.action.hover,
      },

      '.cm-foldPlaceholder': {
        backgroundColor: p.action.hover,
        border: `1px solid ${p.divider}`,
        borderRadius: '3px',
        padding: '0 6px',
        margin: '0 4px',
        color: p.text.secondary,
        cursor: 'pointer',
      },

      '.cm-tooltip': {
        backgroundColor: p.background.paper,
        border: `1px solid ${p.divider}`,
        borderRadius: '6px',
        boxShadow: '0 4px 12px rgba(0, 0, 0, 0.15)',
      },

      '.cm-selectionMatch': {
        backgroundColor: alpha(p.primary.main, 0.15),
      },

      '.cm-specialChar': {
        color: p.error.main,
      },

      '.cm-trailingSpace': {
        backgroundColor: alpha(p.error.main, 0.2),
      },
    },
    // dark 标志让 CM 采用内置深色基调（选区/面板等兜底样式随之适配）。
    { dark },
  );
}

/// 语法高亮色板：One Dark Pro 硬编码色照搬（halo 同款——暗色最佳、亮色可接受）；
/// 注释/标点等语义色接 MUI palette 随明暗自适应。
function highlightStyle(p: Palette): HighlightStyle {
  return HighlightStyle.define([
    { tag: tags.comment, color: alpha(p.text.secondary, 0.6), fontStyle: 'italic' },
    { tag: tags.lineComment, color: alpha(p.text.secondary, 0.6), fontStyle: 'italic' },
    { tag: tags.blockComment, color: alpha(p.text.secondary, 0.6), fontStyle: 'italic' },
    { tag: tags.docComment, color: alpha(p.text.secondary, 0.7), fontStyle: 'italic' },

    { tag: tags.keyword, color: '#c678dd' },
    { tag: tags.controlKeyword, color: '#c678dd' },
    { tag: tags.operatorKeyword, color: '#c678dd' },
    { tag: tags.definitionKeyword, color: '#c678dd' },
    { tag: tags.moduleKeyword, color: '#c678dd' },

    { tag: tags.variableName, color: '#e06c75' },
    { tag: tags.definition(tags.variableName), color: '#e06c75' },
    { tag: tags.local(tags.variableName), color: '#e06c75' },

    { tag: tags.propertyName, color: '#e06c75' },
    { tag: tags.attributeName, color: '#d19a66' },
    { tag: tags.attributeValue, color: '#98c379' },

    { tag: tags.function(tags.variableName), color: '#61afef' },
    { tag: tags.function(tags.propertyName), color: '#61afef' },

    { tag: tags.typeName, color: '#e5c07b' },
    { tag: tags.className, color: '#e5c07b' },
    { tag: tags.namespace, color: '#e5c07b' },

    { tag: tags.string, color: '#98c379' },
    { tag: tags.special(tags.string), color: '#56b6c2' },
    { tag: tags.character, color: '#98c379' },
    { tag: tags.escape, color: '#56b6c2' },

    { tag: tags.number, color: '#d19a66' },
    { tag: tags.integer, color: '#d19a66' },
    { tag: tags.float, color: '#d19a66' },
    { tag: tags.bool, color: '#d19a66' },
    { tag: tags.null, color: '#d19a66' },

    { tag: tags.operator, color: '#56b6c2' },
    { tag: tags.compareOperator, color: '#56b6c2' },
    { tag: tags.arithmeticOperator, color: '#56b6c2' },
    { tag: tags.logicOperator, color: '#56b6c2' },
    { tag: tags.bitwiseOperator, color: '#56b6c2' },
    { tag: tags.derefOperator, color: '#56b6c2' },
    { tag: tags.punctuation, color: alpha(p.text.primary, 0.7) },
    { tag: tags.bracket, color: alpha(p.text.primary, 0.7) },
    { tag: tags.brace, color: alpha(p.text.primary, 0.7) },
    { tag: tags.paren, color: alpha(p.text.primary, 0.7) },
    { tag: tags.squareBracket, color: alpha(p.text.primary, 0.7) },
    { tag: tags.angleBracket, color: alpha(p.text.primary, 0.7) },

    { tag: tags.tagName, color: '#e06c75' },
    { tag: tags.documentMeta, color: '#abb2bf' },

    { tag: tags.regexp, color: '#56b6c2' },
    { tag: tags.special(tags.regexp), color: '#c678dd' },

    { tag: tags.heading, color: '#e06c75', fontWeight: 'bold' },
    { tag: tags.heading1, color: '#e06c75', fontWeight: 'bold', fontSize: '1.2em' },
    { tag: tags.heading2, color: '#e06c75', fontWeight: 'bold', fontSize: '1.1em' },
    { tag: tags.heading3, color: '#e06c75', fontWeight: 'bold' },
    { tag: tags.quote, color: p.text.secondary, fontStyle: 'italic' },
    { tag: tags.emphasis, fontStyle: 'italic' },
    { tag: tags.strong, fontWeight: 'bold' },
    { tag: tags.strikethrough, textDecoration: 'line-through' },
    { tag: tags.link, color: '#61afef', textDecoration: 'underline' },
    { tag: tags.url, color: '#61afef' },
    { tag: tags.monospace, fontFamily: 'inherit', backgroundColor: p.action.hover },

    { tag: tags.meta, color: '#abb2bf' },
    { tag: tags.annotation, color: '#d19a66' },
    { tag: tags.processingInstruction, color: '#abb2bf' },

    { tag: tags.labelName, color: '#61afef' },

    { tag: tags.invalid, color: p.error.main, textDecoration: 'underline wavy' },
  ]);
}

/// 完整主题扩展：结构主题 + 语法高亮（含默认高亮兜底——无语言匹配时仍有基础着色）。
export function buildCodeTheme(palette: Palette, dark: boolean): Extension {
  return [
    editorTheme(palette, dark),
    syntaxHighlighting(highlightStyle(palette)),
    syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
  ];
}
