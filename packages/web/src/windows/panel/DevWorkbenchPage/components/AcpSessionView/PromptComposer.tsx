import type { KeyboardEvent } from 'react';
import { Box, Button, TextField } from '@mui/material';
import { useToast } from '@src/shared/useToast';
import { useCancelAcpSession, usePromptAcpSession } from '@src/state/acpSession';
import { useState } from 'react';

// 底部任务描述输入框：Enter 发送 / Shift+Enter 换行（TerminalSearch 键盘范式）。
// 回合态交互（拍板）：turnActive 禁用输入框，发送钮切换为「停止」（软取消，受理期间
// 按钮 loading 可见——等待态优先 loading 而非仅禁用）；受理成功清空文本，失败 toast
// 保留输入。
export default function PromptComposer({ issueId, turnActive }: { issueId: string; turnActive: boolean }) {
  const [text, setText] = useState('');
  const prompt = usePromptAcpSession();
  const cancel = useCancelAcpSession();
  const toast = useToast();

  const send = () => {
    const value = text.trim();
    if (value === '' || turnActive || prompt.isPending) {
      return;
    }
    prompt.mutate(
      { issueId, text: value },
      {
        onSuccess: () => setText(''),
        onError: err => toast.show(`发送失败：${err.message}`, 'error'),
      },
    );
  };

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  };

  return (
    <Box sx={{ borderTop: 1, borderColor: 'divider', px: 2, py: 1.5 }}>
      <Box sx={{ maxWidth: 860, mx: 'auto', display: 'flex', gap: 1, alignItems: 'flex-end' }}>
        <TextField
          fullWidth
          multiline
          minRows={1}
          maxRows={8}
          size="small"
          placeholder={turnActive ? '回合进行中，可点击停止中断…' : '描述任务，Enter 发送，Shift+Enter 换行'}
          disabled={turnActive}
          value={text}
          onChange={e => setText(e.target.value)}
          onKeyDown={handleKeyDown}
        />
        {turnActive
          ? (
              <Button
                variant="outlined"
                color="error"
                onClick={() => cancel.mutate({ issueId }, { onError: err => toast.show(`停止失败：${err.message}`, 'error') })}
                loading={cancel.isPending}
              >
                停止
              </Button>
            )
          : (
              <Button variant="contained" onClick={send} disabled={text.trim() === ''} loading={prompt.isPending}>
                发送
              </Button>
            )}
      </Box>
      {toast.snack}
    </Box>
  );
}
