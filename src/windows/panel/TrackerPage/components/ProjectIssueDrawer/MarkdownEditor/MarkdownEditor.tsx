import { useTheme } from '@mui/material';
import { open as openUrl } from '@tauri-apps/plugin-shell';
import MDEditor from '@uiw/react-md-editor';
import '@uiw/react-md-editor/markdown-editor.css';
import './markdownEditor.css';

interface MarkdownEditorProps {
  value: string; // markdown 字符串
  onChange: (markdown: string) => void;
  placeholder?: string;
  disabled?: boolean;
}

// Markdown 编辑器（@uiw/react-md-editor）：左源码 + 右预览分屏，自带顶部工具栏。
// 受控 value/onChange（纯 markdown 字符串）；data-color-mode 跟随 palette.mode 切暗色（system/light/dark 三档）。
// disabled → textarea readOnly；textareaProps 关闭 webview 自动大写/纠正/拼写检查（与全局策略一致）。
function MarkdownEditor({ value, onChange, placeholder, disabled = false }: MarkdownEditorProps) {
  const theme = useTheme();

  return (
    <MDEditor
      value={value}
      onChange={val => onChange(val ?? '')}
      preview="live"
      height={300}
      visibleDragbar={false}
      // help 按钮原 execute 用 window.open 开外链，Tauri webview 会拦截；
      // 改用 plugin-shell 的 open 调系统浏览器打开 markdown 语法文档。
      commandsFilter={cmd => (cmd.name === 'help'
        ? { ...cmd, execute: () => void openUrl('https://www.markdownguide.org/basic-syntax/') }
        : cmd)}
      textareaProps={{
        placeholder,
        readOnly: disabled,
        spellCheck: false,
        autoCapitalize: 'off',
        autoCorrect: 'off',
      }}
      data-color-mode={theme.palette.mode}
    />
  );
}

export default MarkdownEditor;
