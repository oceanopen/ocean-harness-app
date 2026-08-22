// chat composer（terminal_chat T3.1）：多行输入 + 发送/停止。发送与停止按钮同时显示
// （用户确认），发送仅 idle 可点、停止仅 busy 可点。Enter 提交、Shift+Enter 换行。

import { Box, Button, TextField } from '@mui/material';
import { useState } from 'react';

interface NativeChatComposerProps {
  // 发送正文（父层走 session.write 回写 PTY 的字节编排）。
  onSend: (text: string) => void;
  // 停止（ESC 中断 claude 正在生成的回复）。
  onStop: () => void;
  // 发送门槛：claude idle 才可发送（响应中/交互态禁止写，避免输入乱序）。
  canSend: boolean;
  // 停止门槛：claude busy（响应中）才可中断。
  isBusy: boolean;
}

export default function NativeChatComposer({ onSend, onStop, canSend, isBusy }: NativeChatComposerProps) {
  const [draft, setDraft] = useState('');

  const submit = () => {
    const text = draft.trim();
    if (text === '' || !canSend) {
      return;
    }
    onSend(text);
    setDraft('');
  };

  return (
    <Box
      sx={{
        flexShrink: 0,
        display: 'flex',
        alignItems: 'flex-end',
        gap: 0.5,
        px: 1,
        py: 1,
        borderTop: 1,
        borderColor: 'divider',
      }}
    >
      <TextField
        size="small"
        multiline
        minRows={1}
        maxRows={6}
        fullWidth
        value={draft}
        placeholder="给 claude 发送消息（Shift+Enter 换行）"
        onChange={e => setDraft(e.target.value)}
        onKeyDown={(e) => {
          // IME 组合输入（中文选字回车）会触发一次 Enter keydown，isComposing=true
          // 时不提交，避免候选确认被误当发送。
          if (e.nativeEvent.isComposing) {
            return;
          }
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            submit();
          }
        }}
      />
      <Button size="small" variant="contained" onClick={submit} disabled={!canSend || draft.trim() === ''}>
        发送
      </Button>
      <Button size="small" color="error" onClick={onStop} disabled={!isBusy}>
        停止
      </Button>
    </Box>
  );
}
