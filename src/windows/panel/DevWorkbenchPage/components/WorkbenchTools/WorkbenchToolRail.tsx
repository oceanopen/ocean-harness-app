import { ChevronLeft as ChevronLeftIcon, ChevronRight as ChevronRightIcon } from '@mui/icons-material';
import { Box, IconButton, Tooltip } from '@mui/material';
import { useToolTabs, useWorkbenchToolsStore } from '@src/state/workbenchTools';
import { WORKBENCH_TOOLS } from './toolRegistry';

interface WorkbenchToolRailProps {
  /// 选中 issue id；null = 未选中（方格与工具图标全部禁用）。
  issueId: string | null;
  /// 面板区折叠态（config SSOT 订阅值，由页面传入；rail 不直接订阅 config）。
  panelCollapsed: boolean;
  /// 方格点击：切面板区折叠（页面写 config）。
  onTogglePanel: () => void;
  /// 工具图标点击后面板区仍收起时展开（页面写 config）。
  onExpandPanel: () => void;
}

/// WorkbenchToolRail：开发工作台最右侧常驻工具条（48px 竖条）。
/// 顶部方格（48×48，与中栏标题栏同高对齐）= 面板区总开关（切 config 折叠态，不动 tabs）；
/// 下方为工具图标列（注册表驱动，点击 = openTool + 面板区未展开则展开）。激活 tab 所属
/// 工具高亮（并存工具任一实例激活即高亮；有 tab 非激活不作次级态，保持两态简单）。
/// tabs 读取走域级 useToolTabs（hydration + 响应式订阅成对封装，见 store.ts 注释）。
export default function WorkbenchToolRail({ issueId, panelCollapsed, onTogglePanel, onExpandPanel }: WorkbenchToolRailProps) {
  const openTool = useWorkbenchToolsStore(s => s.openTool);
  const { tabs, activeTabId } = useToolTabs(issueId);
  const activeTab = tabs.find(t => t.id === activeTabId) ?? null;

  // 工具图标点击时序（固定两步，顺序不可换）：先 store 开/激活 tab，后 config 展开面板区。
  const handleToolClick = (toolId: string, exclusive: boolean) => {
    if (issueId == null) {
      return;
    }
    openTool(issueId, toolId, exclusive);
    if (panelCollapsed) {
      onExpandPanel();
    }
  };

  return (
    <Box
      sx={{
        width: 48,
        flexShrink: 0,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'stretch',
        borderLeft: 1,
        borderColor: 'divider',
        bgcolor: 'background.paper',
      }}
    >
      {/* 面板区总开关（正方形 48×48，与标题栏同高；Chevron 指示运动方向：收起态向左展开） */}
      <Tooltip title={panelCollapsed ? '展开工具面板区' : '收起工具面板区'}>
        <span>
          <IconButton
            onClick={onTogglePanel}
            disabled={issueId == null}
            aria-label={panelCollapsed ? '展开工具面板区' : '收起工具面板区'}
            sx={{ width: 48, height: 48, borderRadius: 0, color: 'text.secondary' }}
          >
            {panelCollapsed ? <ChevronLeftIcon /> : <ChevronRightIcon />}
          </IconButton>
        </span>
      </Tooltip>
      <Box sx={{ borderBottom: 1, borderColor: 'divider' }} />

      {/* 工具图标列：注册表驱动；激活 tab 所属工具高亮 */}
      {WORKBENCH_TOOLS.map((tool) => {
        const active = activeTab?.toolId === tool.id;
        return (
          <Tooltip key={tool.id} title={tool.title}>
            <span>
              <IconButton
                onClick={() => handleToolClick(tool.id, tool.exclusive)}
                disabled={issueId == null}
                aria-label={`打开${tool.title}`}
                sx={{
                  'width': 48,
                  'height': 48,
                  'borderRadius': 0,
                  'color': active ? 'primary.main' : 'text.secondary',
                  '&:hover': { bgcolor: 'action.hover' },
                }}
              >
                <tool.icon />
              </IconButton>
            </span>
          </Tooltip>
        );
      })}
    </Box>
  );
}
