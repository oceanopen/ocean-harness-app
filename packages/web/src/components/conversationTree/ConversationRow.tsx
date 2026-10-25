import type { ConversationModel } from '@src/services';
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Menu,
  MenuItem,
  TextField,
  Typography,
} from '@mui/material';
import { formatRelativeTime } from '@src/shared/time';
import { useToast } from '@src/shared/useToast';
import { useDeleteConversation, useUpdateConversation } from '@src/state/conversation';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import TreeRow from './TreeRow';

interface ConversationRowProps {
  conversation: ConversationModel;
  // 运行态由树级统一判定（会话锚 ∈ claude 活跃快照），行内不重复订阅。
  running: boolean;
  // 左缘缩进（px 字符串）：workspace 页一级 / task 会话二级更深。
  inset?: string;
  // 选中高亮 + 点击进入（T5.3 会话内容区编排；缺省 = 纯展示不响应点击）。
  selected?: boolean;
  onSelect?: () => void;
}

/**
 * 会话行（左树叶子）：标题 + updatedAt 相对时间 + 运行态圆点 + hover「...」菜单（重命名/删除）。
 * 行视觉走共享 TreeRow；时间与「...」hover 互斥显隐（纯 CSS）；点击经 onSelect 上抛（页面写 URL cid 驱动内容区编排）。
 */
function ConversationRow({ conversation, running, inset = '26px', selected = false, onSelect }: ConversationRowProps) {
  const { t } = useTranslation();
  const { show: showToast, snack } = useToast();
  const updateConversation = useUpdateConversation();
  const deleteConversation = useDeleteConversation();
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);
  const [renameOpen, setRenameOpen] = useState(false);
  const [renameDraft, setRenameDraft] = useState('');
  const [renaming, setRenaming] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const openRename = () => {
    setMenuAnchor(null);
    setRenameDraft(conversation.title);
    setRenameOpen(true);
  };

  const submitRename = async () => {
    const title = renameDraft.trim();
    if (title === '' || renaming) {
      return;
    }
    setRenaming(true);
    try {
      await updateConversation.mutateAsync({ id: conversation.id, title });
      setRenameOpen(false);
    } catch (e) {
      showToast(`重命名失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    } finally {
      setRenaming(false);
    }
  };

  const submitDelete = async () => {
    if (deleting) {
      return;
    }
    setDeleting(true);
    try {
      await deleteConversation.mutateAsync({ id: conversation.id });
      showToast('会话已删除', 'success');
      setDeleteOpen(false);
    } catch (e) {
      showToast(`删除失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Box>
      <TreeRow
        variant="leaf"
        title={conversation.title}
        selected={selected}
        onClick={onSelect}
        inset={inset}
        leadingSlot={running
          ? <Box sx={{ width: '8px', height: '8px', borderRadius: '50%', bgcolor: 'success.main' }} aria-label="运行中" />
          : undefined}
        timeLabel={formatRelativeTime(new Date(conversation.updatedAt).getTime(), t, 'tracker')}
        moreLabel="会话操作"
        onMore={setMenuAnchor}
      />
      <Menu anchorEl={menuAnchor} open={menuAnchor != null} onClose={() => setMenuAnchor(null)}>
        <MenuItem onClick={openRename}>重命名…</MenuItem>
        <MenuItem
          onClick={() => {
            setMenuAnchor(null);
            setDeleteOpen(true);
          }}
        >
          删除…
        </MenuItem>
      </Menu>

      <Dialog open={renameOpen} onClose={renaming ? undefined : () => setRenameOpen(false)} maxWidth="xs" fullWidth>
        <DialogTitle>重命名会话</DialogTitle>
        <DialogContent>
          <TextField
            value={renameDraft}
            onChange={e => setRenameDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                void submitRename();
              }
            }}
            fullWidth
            autoFocus
            size="small"
            disabled={renaming}
            slotProps={{ htmlInput: { maxLength: 255 } }}
          />
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setRenameOpen(false)} disabled={renaming}>取消</Button>
          <Button variant="contained" onClick={() => void submitRename()} disabled={renaming || renameDraft.trim() === ''}>保存</Button>
        </DialogActions>
      </Dialog>

      <Dialog open={deleteOpen} onClose={deleting ? undefined : () => setDeleteOpen(false)} maxWidth="xs" fullWidth>
        <DialogTitle>删除会话</DialogTitle>
        <DialogContent>
          <Typography>确认删除会话「{conversation.title}」？仅移除会话记录，不影响 Claude 本地文件。</Typography>
        </DialogContent>
        <DialogActions>
          <Button color="inherit" onClick={() => setDeleteOpen(false)} disabled={deleting}>取消</Button>
          <Button color="error" variant="contained" onClick={() => void submitDelete()} disabled={deleting}>删除</Button>
        </DialogActions>
      </Dialog>

      {snack}
    </Box>
  );
}

export default ConversationRow;
