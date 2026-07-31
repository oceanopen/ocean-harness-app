import { LinkOutlined as LinkOutlinedIcon } from '@mui/icons-material';
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  TextField,
  ToggleButton,
  Tooltip,
} from '@mui/material';
import Placeholder from '@tiptap/extension-placeholder';
import TaskItem from '@tiptap/extension-task-item';
import TaskList from '@tiptap/extension-task-list';
import { EditorContent, useEditor } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';
import DOMPurify from 'dompurify';
import { Fragment, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { proseSx, purifyConfig, TOOLBAR_GROUPS } from './richTextEditorShared';

interface RichTextEditorProps {
  value: string;
  onChange: (html: string) => void;
  placeholder?: string;
  editable?: boolean;
  disabled?: boolean;
}

// 富文本编辑器（TipTap）：工具栏 + 编辑区 + 链接弹窗。
// onUpdate 内 DOMPurify sanitize 为单一安全出口；受控同步用 lastEmittedRef 避免回环/光标跳。
function RichTextEditor({ value, onChange, placeholder, editable = true, disabled = false }: RichTextEditorProps) {
  const { t } = useTranslation();
  const [linkOpen, setLinkOpen] = useState(false);
  const [linkUrl, setLinkUrl] = useState('');
  // 记录编辑器最近输出的值，避免 onChange→父 setState→value 变化→setContent 回环导致光标跳。
  const lastEmittedRef = useRef(value);

  const editor = useEditor({
    immediatelyRender: false, // React 19 StrictMode 双挂载防护
    extensions: [
      StarterKit.configure({
        // 配置内置 link（编辑态不跳转、粘贴 URL 自动链接、新窗口打开）。
        link: { openOnClick: false, autolink: true, HTMLAttributes: { rel: 'noopener noreferrer', target: '_blank' } },
        heading: { levels: [1, 2, 3] },
      }),
      TaskList,
      TaskItem.configure({ nested: true }),
      Placeholder.configure({ placeholder: placeholder || '' }),
    ],
    content: value,
    editable: editable && !disabled,
    onUpdate: ({ editor }) => {
      const clean = DOMPurify.sanitize(editor.getHTML(), purifyConfig);
      lastEmittedRef.current = clean;
      onChange(clean);
    },
    editorProps: {
      attributes: {
        class: 'rte-content ProseMirror',
      },
    },
  });

  // 外部 value 变化（非编辑器自身触发，如保存回灌）时同步进编辑器。
  useEffect(() => {
    if (!editor) {
      return;
    }
    if (value !== lastEmittedRef.current) {
      lastEmittedRef.current = value;
      editor.commands.setContent(value || '', { emitUpdate: false });
    }
  }, [editor, value]);

  // editable / disabled 同步。
  useEffect(() => {
    editor?.setEditable(editable && !disabled);
  }, [editor, editable, disabled]);

  // 卸载销毁。
  useEffect(() => () => {
    editor?.destroy();
  }, [editor]);

  if (!editor) {
    return null;
  }

  const openLinkDialog = () => {
    setLinkUrl(editor.getAttributes('link').href || '');
    setLinkOpen(true);
  };

  const applyLink = () => {
    const url = linkUrl.trim();
    if (!url) {
      editor.chain().focus().extendMarkRange('link').unsetLink().run();
    } else {
      editor.chain().focus().extendMarkRange('link').setLink({ href: url }).run();
    }
    setLinkOpen(false);
  };

  return (
    <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 1, overflow: 'hidden', opacity: disabled ? 0.6 : 1 }}>
      {/* 工具栏 */}
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.25, p: 0.5, borderBottom: 1, borderColor: 'divider', bgcolor: 'action.hover' }}>
        {TOOLBAR_GROUPS.map((group, gi) => (
          <Fragment key={group.id}>
            {gi > 0 && <Divider orientation="vertical" flexItem sx={{ mx: 0.25, my: 0.5 }} />}
            {group.items.map(item => (
              <Tooltip key={item.value} title={t(`tracker:issue.rte.${item.i18nKey}`)} arrow>
                <ToggleButton
                  size="small"
                  value={item.value}
                  selected={item.active ? item.active(editor) : false}
                  disabled={disabled || (item.can ? !item.can(editor) : false)}
                  onClick={() => item.run(editor)}
                  sx={{ px: 1, py: 0.25, border: 0, borderRadius: 0.5 }}
                >
                  <item.Icon fontSize="small" />
                </ToggleButton>
              </Tooltip>
            ))}
          </Fragment>
        ))}
        {/* 链接按钮（单独，带弹窗编辑 URL） */}
        <Divider orientation="vertical" flexItem sx={{ mx: 0.25, my: 0.5 }} />
        <Tooltip title={t('tracker:issue.rte.link')} arrow>
          <ToggleButton
            size="small"
            value="link"
            selected={editor.isActive('link')}
            disabled={disabled}
            onClick={openLinkDialog}
            sx={{ px: 1, py: 0.25, border: 0, borderRadius: 0.5 }}
          >
            <LinkOutlinedIcon fontSize="small" />
          </ToggleButton>
        </Tooltip>
      </Box>

      {/* 编辑区 */}
      <Box sx={proseSx}>
        <EditorContent editor={editor} />
      </Box>

      {/* 链接编辑弹窗 */}
      <Dialog open={linkOpen} onClose={() => setLinkOpen(false)} fullWidth maxWidth="xs">
        <DialogTitle>{t('tracker:issue.rte.linkTitle')}</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            fullWidth
            size="small"
            label={t('tracker:issue.rte.linkUrl')}
            value={linkUrl}
            onChange={e => setLinkUrl(e.target.value)}
            placeholder="https://"
          />
        </DialogContent>
        <DialogActions>
          {editor.isActive('link') && (
            <Button
              color="error"
              onClick={() => {
                editor.chain().focus().extendMarkRange('link').unsetLink().run();
                setLinkOpen(false);
              }}
            >
              {t('tracker:issue.rte.removeLink')}
            </Button>
          )}
          <Box sx={{ flex: 1 }} />
          <Button color="inherit" onClick={() => setLinkOpen(false)}>{t('tracker:issue.create.cancel')}</Button>
          <Button variant="contained" onClick={applyLink}>{t('tracker:issue.rte.linkTitle')}</Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

export default RichTextEditor;
