import { CloseOutlined as CloseOutlinedIcon, FolderOpen as FolderOpenIcon } from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  IconButton,
  InputAdornment,
  TextField,
  Typography,
} from '@mui/material';
import ResizableDrawer from '@src/shared/ResizableDrawer';
import { useAddPluginMarketplace } from '@src/state/pluginMarketplace';
import { open as openDialog } from '@tauri-apps/plugin-dialog';
import { useState } from 'react';

// 添加插件市场抽屉：手输市场地址（本地绝对路径 / GitHub owner/repo / git URL）或经目录
// 选择对话框回填本地路径；提交即调 claude plugin marketplace add（后端透传），成功后
// 返回最新列表投影并自动刷新缓存。本地路径的清单预检由后端完成（webview 无 fs 能力）。
interface AddMarketplaceDrawerProps {
  onClose: () => void;
}

// 从未知错误对象提取 message（Go 经 http.ts 抛 new Error(msg)）。
function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

function AddMarketplaceDrawer({ onClose }: AddMarketplaceDrawerProps) {
  const addMu = useAddPluginMarketplace();
  const submitting = addMu.isPending;

  const [source, setSource] = useState('');
  const [error, setError] = useState<string | null>(null);

  const canSubmit = source.trim().length > 0 && !submitting;

  const handleBrowse = async () => {
    // directory: true 多选关闭，返回 string | null。
    const selected = await openDialog({ directory: true, multiple: false });
    if (typeof selected === 'string') {
      setSource(selected);
      setError(null);
    }
  };

  const handleConfirm = async () => {
    setError(null);
    try {
      await addMu.mutateAsync({ source: source.trim() });
      onClose();
    } catch (e) {
      // 统一展示后台返回的错误信息（后端返回可读中文：目录不存在/清单缺失/CLI 报错）。
      setError(errMsg(e));
    }
  };

  return (
    <ResizableDrawer
      open
      // 提交中禁止背景点击/Esc 关闭，避免半成品状态丢失。
      onClose={submitting ? undefined : onClose}
      defaultWidthPct={50}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* 头部 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="subtitle1" sx={{ flex: 1, fontWeight: 600 }} noWrap>
            添加插件市场
          </Typography>
          <IconButton size="small" onClick={onClose} disabled={submitting} aria-label="关闭">
            <CloseOutlinedIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 内容 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: 2, display: 'flex', flexDirection: 'column', gap: 2 }}>
          <TextField
            label="插件市场地址"
            placeholder="/path/to/plugin-repo、owner/repo 或 https://git.example.com/repo.git"
            value={source}
            onChange={(e) => {
              setSource(e.target.value);
              setError(null);
            }}
            fullWidth
            autoFocus
            disabled={submitting}
            slotProps={{
              input: {
                endAdornment: (
                  <InputAdornment position="end">
                    <IconButton edge="end" onClick={handleBrowse} disabled={submitting} aria-label="选择本地目录">
                      <FolderOpenIcon />
                    </IconButton>
                  </InputAdornment>
                ),
              },
            }}
          />
          <Typography variant="caption" sx={{ color: 'text.secondary' }}>
            支持三种地址形式：本地插件仓库绝对路径（含 .claude-plugin/marketplace.json）、GitHub 仓库
            （owner/repo）、git 地址（https/ssh）。提交后通过 Claude Code CLI 注册市场，与命令行行为一致。
          </Typography>
          {error && <Alert severity="error">{error}</Alert>}
        </Box>

        {/* 底部操作栏：一律左对齐 */}
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, p: 2, borderTop: 1, borderColor: 'divider' }}>
          <Button color="inherit" onClick={onClose} disabled={submitting}>
            取消
          </Button>
          <Button variant="contained" onClick={handleConfirm} disabled={!canSubmit}>
            {submitting ? '正在注册…' : '注册插件市场'}
          </Button>
        </Box>
      </Box>
    </ResizableDrawer>
  );
}

export default AddMarketplaceDrawer;
