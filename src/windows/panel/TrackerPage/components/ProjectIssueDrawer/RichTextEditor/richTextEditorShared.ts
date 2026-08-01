import type { SxProps, Theme } from '@mui/material/styles';
import type { Editor } from '@tiptap/react';
import type { Config } from 'dompurify';
import type { ComponentType } from 'react';
import {
  Checklist as ChecklistIcon,
  Code as CodeIcon,
  DataObject as DataObjectIcon,
  FormatBold as FormatBoldIcon,
  FormatItalic as FormatItalicIcon,
  FormatListBulleted as FormatListBulletedIcon,
  FormatListNumbered as FormatListNumberedIcon,
  FormatQuote as FormatQuoteIcon,
  FormatStrikethrough as FormatStrikethroughIcon,
  HorizontalRule as HorizontalRuleIcon,
  Looks3 as Looks3Icon,
  LooksOne as LooksOneIcon,
  LooksTwo as LooksTwoIcon,
  Redo as RedoIcon,
  Undo as UndoIcon,
} from '@mui/icons-material';

type IconType = ComponentType<{ fontSize?: 'small' | 'medium' | 'large' }>;

export interface ToolbarItem {
  value: string;
  i18nKey: string;
  Icon: IconType;
  run: (editor: Editor) => void;
  active?: (editor: Editor) => boolean;
  can?: (editor: Editor) => boolean;
}

export interface ToolbarGroup {
  id: string;
  items: ToolbarItem[];
}

// 工具栏分组：文本格式 / 标题 / 列表 / 块级 / 历史。链接按钮单独处理（需弹窗编辑 URL）。
export const TOOLBAR_GROUPS: ToolbarGroup[] = [
  {
    id: 'text',
    items: [
      { value: 'bold', i18nKey: 'bold', Icon: FormatBoldIcon, run: e => e.chain().focus().toggleBold().run(), active: e => e.isActive('bold') },
      { value: 'italic', i18nKey: 'italic', Icon: FormatItalicIcon, run: e => e.chain().focus().toggleItalic().run(), active: e => e.isActive('italic') },
      { value: 'strike', i18nKey: 'strike', Icon: FormatStrikethroughIcon, run: e => e.chain().focus().toggleStrike().run(), active: e => e.isActive('strike') },
      { value: 'code', i18nKey: 'code', Icon: CodeIcon, run: e => e.chain().focus().toggleCode().run(), active: e => e.isActive('code') },
    ],
  },
  {
    id: 'heading',
    items: [
      { value: 'h1', i18nKey: 'h1', Icon: LooksOneIcon, run: e => e.chain().focus().toggleHeading({ level: 1 }).run(), active: e => e.isActive('heading', { level: 1 }) },
      { value: 'h2', i18nKey: 'h2', Icon: LooksTwoIcon, run: e => e.chain().focus().toggleHeading({ level: 2 }).run(), active: e => e.isActive('heading', { level: 2 }) },
      { value: 'h3', i18nKey: 'h3', Icon: Looks3Icon, run: e => e.chain().focus().toggleHeading({ level: 3 }).run(), active: e => e.isActive('heading', { level: 3 }) },
    ],
  },
  {
    id: 'list',
    items: [
      { value: 'bullet', i18nKey: 'bulletList', Icon: FormatListBulletedIcon, run: e => e.chain().focus().toggleBulletList().run(), active: e => e.isActive('bulletList') },
      { value: 'ordered', i18nKey: 'orderedList', Icon: FormatListNumberedIcon, run: e => e.chain().focus().toggleOrderedList().run(), active: e => e.isActive('orderedList') },
      { value: 'task', i18nKey: 'taskList', Icon: ChecklistIcon, run: e => e.chain().focus().toggleTaskList().run(), active: e => e.isActive('taskList') },
    ],
  },
  {
    id: 'block',
    items: [
      { value: 'quote', i18nKey: 'quote', Icon: FormatQuoteIcon, run: e => e.chain().focus().toggleBlockquote().run(), active: e => e.isActive('blockquote') },
      { value: 'codeBlock', i18nKey: 'codeBlock', Icon: DataObjectIcon, run: e => e.chain().focus().toggleCodeBlock().run(), active: e => e.isActive('codeBlock') },
      { value: 'divider', i18nKey: 'divider', Icon: HorizontalRuleIcon, run: e => e.chain().focus().setHorizontalRule().run() },
    ],
  },
  {
    id: 'history',
    items: [
      { value: 'undo', i18nKey: 'undo', Icon: UndoIcon, run: e => e.chain().focus().undo().run(), can: e => e.can().undo() },
      { value: 'redo', i18nKey: 'redo', Icon: RedoIcon, run: e => e.chain().focus().redo().run(), can: e => e.can().redo() },
    ],
  },
];

// DOMPurify 配置：白名单覆盖 TipTap 实际产出（含 taskList 的 input/label/data-checked）。
// 链接协议白名单防 javascript:/data:；script/on* 由 DOMPurify 默认剥离。
export const purifyConfig: Config = {
  ALLOWED_TAGS: ['p', 'br', 'hr', 'h1', 'h2', 'h3', 'strong', 'em', 's', 'code', 'pre', 'blockquote', 'ul', 'ol', 'li', 'a', 'span', 'div', 'input', 'label'],
  ALLOWED_ATTR: ['href', 'target', 'rel', 'data-type', 'data-checked', 'type', 'checked', 'disabled'],
  ALLOWED_URI_REGEXP: /^(?:(?:https?|mailto):|[^:/?#]+(?:[/?#]|$))/i,
};

// ProseMirror 编辑区样式（emotion sx 后代选择器，注入到 EditorContent 外层 Box）。
// 选择 .rte-content.ProseMirror 精确命中编辑区根节点，不污染其他实例。
export const proseSx: SxProps<Theme> = {
  '& .rte-content.ProseMirror': {
    'minHeight': 160,
    'p': 1.5,
    'outline': 'none',
    'fontSize': 14,
    'lineHeight': 1.6,
    '&:focus': { outline: 'none' },
    '&.is-editor-empty:first-child::before': {
      content: 'attr(data-placeholder)',
      color: 'text.disabled',
      float: 'left',
      height: 0,
      pointerEvents: 'none',
    },
    '& h1': { fontSize: '1.6rem', fontWeight: 600, my: 0.5 },
    '& h2': { fontSize: '1.35rem', fontWeight: 600, my: 0.5 },
    '& h3': { fontSize: '1.15rem', fontWeight: 600, my: 0.5 },
    '& p': { my: 0.5 },
    '& code': { px: 0.4, py: 0.1, borderRadius: 0.5, fontFamily: 'monospace', fontSize: '0.85em', bgcolor: 'action.selected' },
    '& pre': { 'p': 1.5, 'my': 1, 'borderRadius': 1, 'overflow': 'auto', 'bgcolor': 'action.selected', 'fontFamily': 'monospace', 'fontSize': '0.85em', '& code': { px: 0, py: 0, bgcolor: 'transparent' } },
    '& blockquote': { ml: 0, pl: 2, my: 1, color: 'text.secondary', borderLeft: 3, borderColor: 'divider' },
    '& ul': { pl: 3 },
    '& ol': { pl: 3 },
    '& ul[data-type="taskList"]': { 'listStyle': 'none', 'pl': 1, '& li': { display: 'flex', alignItems: 'flex-start', gap: 1 }, '& li > label': { flexShrink: 0, mt: 0.25 } },
    '& a': { 'color': 'primary.main', 'textDecoration': 'none', '&:hover': { textDecoration: 'underline' } },
    '& hr': { border: 0, borderTop: 1, borderColor: 'divider', my: 2 },
  },
};
