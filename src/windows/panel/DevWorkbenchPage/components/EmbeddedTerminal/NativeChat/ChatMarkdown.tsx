// 只读 markdown 渲染（terminal_chat T2.2）：react-markdown + remark-gfm + rehype-highlight。
// 链接点击分流（同 TerminalView activateLink 范式）：http(s) 走 plugin-shell（window.open
// 被 Tauri webview 拦截），绝对文件路径走 Rust open_path。chat 内容是静态快照，无选区
// 误触顾虑，纯单击即打开（与终端 Cmd/Ctrl+Click 守卫不同）。
//
// 代码高亮配色在 chatMarkdown.css（rehype-highlight 产出 hljs-* class）：不引 highlight.js
// 主题包——pnpm 不提升传递依赖，引不到 highlight.js 的 css，且自定义配色可随终暗色统一。

import { Box } from '@mui/material';
import { commands } from '@src/shared/bindings';
import { unwrap } from '@src/shared/commands';
import { open as openExternalUrl } from '@tauri-apps/plugin-shell';
import Markdown from 'react-markdown';
import rehypeHighlight from 'rehype-highlight';
import remarkGfm from 'remark-gfm';
import './chatMarkdown.css';

function openLink(href: string): void {
  if (/^https?:\/\//.test(href)) {
    void openExternalUrl(href).catch((e: unknown) => {
      console.warn('[ChatMarkdown] open url failed:', e);
    });
  } else if (href.startsWith('/')) {
    void unwrap(commands.openPath(href)).catch((e: unknown) => {
      console.warn('[ChatMarkdown] open path failed:', e);
    });
  }
  // 其他（锚点/相对路径/mailto）静默忽略，避免误导航。
}

interface ChatMarkdownProps {
  content: string;
}

export default function ChatMarkdown({ content }: ChatMarkdownProps) {
  return (
    <Box
      className="chat-markdown"
      sx={{ fontSize: '13px', lineHeight: 1.6, overflowWrap: 'break-word' }}
    >
      <Markdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          a: ({ href, children }) => (
            <Box
              component="a"
              href={href}
              onClick={(event) => {
                event.preventDefault();
                if (href != null) {
                  openLink(href);
                }
              }}
              sx={{
                color: 'primary.main',
                cursor: 'pointer',
                textDecoration: 'underline',
              }}
            >
              {children}
            </Box>
          ),
        }}
      >
        {content}
      </Markdown>
    </Box>
  );
}
