import { Autorenew as AutorenewIcon, Close as CloseIcon } from '@mui/icons-material';
import { Box, IconButton, Tab, Tabs, Tooltip, Typography } from '@mui/material';
import { basename } from '@src/shared/repoPath';
import { PANEL_TOOLBAR_HEIGHT } from '../PanelToolbar';

interface PreviewTabsBarProps {
  /// 打开的 tab 路径数组（tab id = 文件相对路径）。
  tabs: string[];
  activeTabId: string | null;
  /// Git 变更模式下的变更文件路径集（tab 文件名前加主色小点标记；空集 = 全部文件模式）。
  changedPaths: ReadonlySet<string>;
  onSelect: (path: string) => void;
  onClose: (path: string) => void;
  /// 一键关闭全部（tab 栏右缘固定按钮——逐 tab 关闭太繁琐，halo 同款入口语义）。
  onCloseAll: () => void;
  /// 刷新激活 tab 的文件内容（halo 同款右缘入口；作用于激活 tab 而非全部）。
  onRefresh: () => void;
}

/// 预览 tab 头（36px，PANEL_TOOLBAR_HEIGHT 操作栏带族——浮层在终端内容区（标题栏带之下），
/// 与终端 pane 工具栏同带而非 ToolPanelArea 的 40px 对齐带）。tab id = 文件相对路径，
/// label 取 basename、title 悬浮全路径；长文件名收缩出省略号（Typography flex+minWidth:0，
/// 关闭钮 flexShrink:0 恒可见——flex 默认 min-width:auto 不收缩是省略号失效的根源）。
/// 每 tab 带关闭按钮（stopPropagation 防误切）；右缘固定「关闭全部」。
export default function PreviewTabsBar({ tabs, activeTabId, changedPaths, onSelect, onClose, onCloseAll, onRefresh }: PreviewTabsBarProps) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'stretch', flexShrink: 0, borderBottom: 1, borderColor: 'divider' }}>
      <Tabs
        value={activeTabId ?? ''}
        onChange={(_, path: string) => onSelect(path)}
        variant="scrollable"
        scrollButtons={false}
        sx={{ flex: '1 1 auto', minWidth: 0, height: PANEL_TOOLBAR_HEIGHT, minHeight: PANEL_TOOLBAR_HEIGHT }}
      >
        {tabs.map(path => (
          <Tab
            key={path}
            value={path}
            title={path}
            label={(
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.25, maxWidth: '100%' }}>
                {/* 变更小圆点常驻占位（visibility 而非条件渲染）：切模式时不出现/消失引起
                    文件名宽度跳动；有变更才可见，与文件名间隔 4px（mr: 0.5）。 */}
                <Box
                  sx={{
                    width: 6,
                    height: 6,
                    borderRadius: '50%',
                    bgcolor: 'primary.main',
                    visibility: changedPaths.has(path) ? 'visible' : 'hidden',
                    flexShrink: 0,
                    mr: 0.5,
                  }}
                />
                <Typography variant="caption" noWrap sx={{ flex: '1 1 auto', minWidth: 0 }}>
                  {basename(path)}
                </Typography>
                <IconButton
                  size="small"
                  aria-label={`关闭 ${basename(path)}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    onClose(path);
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
        <Tooltip title="刷新当前文件">
          <span>
            <IconButton
              size="small"
              aria-label="刷新当前文件"
              disabled={activeTabId == null}
              onClick={onRefresh}
              sx={{ color: 'text.secondary' }}
            >
              {/* Autorenew 与子任务面板刷新按钮同款图标 */}
              <AutorenewIcon sx={{ fontSize: 16 }} />
            </IconButton>
          </span>
        </Tooltip>
        <Tooltip title="关闭全部预览">
          <IconButton size="small" aria-label="关闭全部预览" onClick={onCloseAll} sx={{ color: 'text.secondary' }}>
            <CloseIcon sx={{ fontSize: 16 }} />
          </IconButton>
        </Tooltip>
      </Box>
    </Box>
  );
}
