import type { PreviewTab } from '@src/state/workspaceFiles';
import { Close as CloseIcon } from '@mui/icons-material';
import { Box, IconButton, Tab, Tabs, Tooltip, Typography } from '@mui/material';
import { basename } from '@src/shared/repoPath';
import { PANEL_TOOLBAR_HEIGHT } from '../PanelToolbar';

interface PreviewTabsBarProps {
  tabs: PreviewTab[];
  activeTabId: string | null;
  onSelect: (path: string) => void;
  onClose: (path: string) => void;
  /// 一键关闭全部（tab 栏右缘固定按钮——逐 tab 关闭太繁琐，halo 同款入口语义）。
  onCloseAll: () => void;
}

/// 预览 tab 头（36px，PANEL_TOOLBAR_HEIGHT 操作栏带族——浮层在终端内容区（标题栏带之下），
/// 与终端 pane 工具栏同带而非 ToolPanelArea 的 48px 对齐带）。tab id = 文件相对路径，
/// label 取 basename、title 悬浮全路径；长文件名收缩出省略号（Typography flex+minWidth:0，
/// 关闭钮 flexShrink:0 恒可见——flex 默认 min-width:auto 不收缩是省略号失效的根源）。
/// 每 tab 带关闭按钮（stopPropagation 防误切）；右缘固定「关闭全部」。
export default function PreviewTabsBar({ tabs, activeTabId, onSelect, onClose, onCloseAll }: PreviewTabsBarProps) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'stretch', flexShrink: 0, borderBottom: 1, borderColor: 'divider' }}>
      <Tabs
        value={activeTabId ?? ''}
        onChange={(_, path: string) => onSelect(path)}
        variant="scrollable"
        scrollButtons={false}
        sx={{ flex: '1 1 auto', minWidth: 0, height: PANEL_TOOLBAR_HEIGHT, minHeight: PANEL_TOOLBAR_HEIGHT }}
      >
        {tabs.map(tab => (
          <Tab
            key={tab.path}
            value={tab.path}
            title={tab.path}
            label={(
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.25, maxWidth: '100%' }}>
                <Typography variant="caption" noWrap sx={{ flex: '1 1 auto', minWidth: 0 }}>
                  {basename(tab.path)}
                </Typography>
                <IconButton
                  size="small"
                  aria-label={`关闭 ${basename(tab.path)}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    onClose(tab.path);
                  }}
                  sx={{ 'ml': 0.25, 'flexShrink': 0, 'color': 'text.secondary', '&:hover': { bgcolor: 'action.hover' } }}
                >
                  <CloseIcon sx={{ fontSize: 14 }} />
                </IconButton>
              </Box>
            )}
            sx={{ minHeight: PANEL_TOOLBAR_HEIGHT, py: 0.25, maxWidth: 220 }}
          />
        ))}
      </Tabs>
      <Box sx={{ display: 'flex', alignItems: 'center', px: 0.5, borderLeft: 1, borderColor: 'divider' }}>
        <Tooltip title="关闭全部预览">
          <IconButton size="small" aria-label="关闭全部预览" onClick={onCloseAll} sx={{ color: 'text.secondary' }}>
            <CloseIcon sx={{ fontSize: 16 }} />
          </IconButton>
        </Tooltip>
      </Box>
    </Box>
  );
}
