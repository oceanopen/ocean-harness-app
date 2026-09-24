import type { WorkspaceOpenToolId } from './openTools';
import { Check as CheckIcon, KeyboardArrowDown as KeyboardArrowDownIcon } from '@mui/icons-material';
import { Box, IconButton, ListItemIcon, MenuItem, MenuList, Paper, Popper, Typography } from '@mui/material';
import {
  decodeWorkspaceBaseDir,
  DEFAULT_WORKSPACE_BASE_DIR,
  setAppConfig,
  WORKSPACE_BASE_DIR_KEY,
  WORKSPACE_OPEN_TOOL_KEY,
} from '@src/shared/appConfig';
import { currentPlatform } from '@src/shared/platform';
import { useConfigReady } from '@src/shared/useConfigReady';
import { useConfigValue } from '@src/shared/useConfigValue';
import { useToast } from '@src/shared/useToast';
import { useEffect, useRef, useState } from 'react';
import {
  decodeWorkspaceOpenTool,
  DEFAULT_WORKSPACE_OPEN_TOOL,
  openWorkspaceDir,
  resolveWorkspaceOpenTool,
  workspaceOpenToolsFor,
} from './openTools';

// 从未知错误对象提取 message（unwrap throw 裸字符串；Go/兜底场景 Error，两者兼容）。
function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

// 挂载前置配置集：默认工具 + 工作空间根目录。前者决定胶囊首帧图标（不闸门会先闪
// 默认 vscode 图标再被真实值纠正——编码规则 1），后者虽仅在点击时消费，一并闸门
// 可杜绝「配置未回填期点击误判 baseDir 未设置」。模块级常量保证引用稳定。
const OPEN_TOOL_CONFIG_KEYS: readonly string[] = [WORKSPACE_OPEN_TOOL_KEY, WORKSPACE_BASE_DIR_KEY];

// 浮层关闭宽限（ms）：鼠标从胶囊移向浮层途中短暂离开放行，防闪烁。
const FLYOUT_CLOSE_GRACE_MS = 150;

interface OpenWorkspaceDirButtonProps {
  /// 当前选中 issue id（打开目录 = {baseDir}/{issueId}，与 EmbeddedTerminal 的 cwd 派生同源）。
  issueId: string;
}

/// OpenWorkspaceDirButton：开发工作台标题栏「打开工作区目录」胶囊分体按钮。
/// 左段 = 当前默认工具图标（仅点击 = 用默认工具打开，无 hover 行为）；右段 = ▼
/// （hover 展开浮层，下方、右缘对齐；点击 = 显式开关兜底入口）。浮层行点击 =
/// 切默认 + 立即打开，两者串行：await 打开命令 settle 且成功后才写 config（deepseek-harness
/// 同款语义）——图标随 useConfigValue 订阅切换、滞后于打开动作肉眼可感知；失败 toast
/// 不污染默认值。工具目录/平台过滤/命令分发见 openTools.tsx。
export default function OpenWorkspaceDirButton({ issueId }: OpenWorkspaceDirButtonProps) {
  const { show: showToast, snack: toastSnack } = useToast();
  const configReady = useConfigReady(OPEN_TOOL_CONFIG_KEYS);
  const baseDir = useConfigValue(WORKSPACE_BASE_DIR_KEY, decodeWorkspaceBaseDir, DEFAULT_WORKSPACE_BASE_DIR);
  const configuredTool = useConfigValue(WORKSPACE_OPEN_TOOL_KEY, decodeWorkspaceOpenTool, DEFAULT_WORKSPACE_OPEN_TOOL);

  const tools = workspaceOpenToolsFor(currentPlatform);
  const preferred = resolveWorkspaceOpenTool(configuredTool, currentPlatform);
  const preferredTool = tools.find(t => t.id === preferred) ?? tools[0];
  const PreferredIcon = preferredTool.Icon;

  // 浮层锚点（null = 关闭）+ 打开中标志（in-flight 防重入：终端冷启动 AppleScript 可达秒级）。
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  const [opening, setOpening] = useState(false);
  const pillRef = useRef<HTMLDivElement | null>(null);
  const closeTimerRef = useRef<number | null>(null);

  // 浮层开合状态机：closeFlyout 是唯一关闭出口（清 timer + 清锚点成对）；scheduleClose
  // 挂宽限；openFlyout 以胶囊为锚（pillRef，hover 与 ▼ 两入口同锚）。
  const clearCloseTimer = () => {
    if (closeTimerRef.current != null) {
      window.clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
  };
  const closeFlyout = () => {
    clearCloseTimer();
    setAnchorEl(null);
  };
  const scheduleFlyoutClose = () => {
    clearCloseTimer();
    closeTimerRef.current = window.setTimeout(closeFlyout, FLYOUT_CLOSE_GRACE_MS);
  };
  const openFlyout = () => {
    clearCloseTimer();
    if (pillRef.current != null) {
      setAnchorEl(pillRef.current);
    }
  };
  const toggleFlyout = () => {
    if (anchorEl != null) {
      closeFlyout();
    } else {
      openFlyout();
    }
  };
  // 卸载清 timer（页面切走即卸载的页面级生命周期内，定时器不得越界触发 setState）。
  useEffect(() => clearCloseTimer, []);

  // 串行打开：先 await 打开命令，settle 且成功才写默认工具配置（值变化才写，避免
  // 同值写入触发一次多余的 app-config-changed 广播）；失败 toast 且默认值不动。
  // baseDir 未设置时引导去设置页（EmbeddedTerminal/WorkspaceFilePanel 同款文案口径）。
  const runOpen = async (toolId: WorkspaceOpenToolId) => {
    closeFlyout();
    if (baseDir === '') {
      showToast('请先在设置 → 项目配置中设置工作空间根目录', 'error');
      return;
    }
    setOpening(true);
    try {
      await openWorkspaceDir(toolId, `${baseDir}/${issueId}`);
      if (toolId !== preferred) {
        void setAppConfig(WORKSPACE_OPEN_TOOL_KEY, toolId);
      }
    } catch (e) {
      showToast(`打开失败：${errMsg(e)}`, 'error');
    } finally {
      setOpening(false);
    }
  };

  const disabled = !configReady || opening;

  return (
    <>
      <Box
        ref={pillRef}
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          height: 28,
          borderRadius: 999,
          border: 1,
          borderColor: 'divider',
          overflow: 'hidden',
          flexShrink: 0,
        }}
      >
        {/* 左段：默认工具图标，仅点击 = 用当前默认工具打开（不改默认值；无 hover 行为） */}
        <IconButton
          onClick={() => void runOpen(preferred)}
          disabled={disabled}
          aria-label={`打开工作区目录（${preferredTool.label}）`}
          sx={{ height: 28, borderRadius: 0, px: 1, color: 'text.secondary' }}
        >
          {configReady ? <PreferredIcon /> : null}
        </IconButton>
        {/* 右段：▼ 浮层唯一 hover 热区（移入展开、移出挂宽限关闭）；点击 = 显式开关
            兜底入口。分隔线强化分体语义。 */}
        <IconButton
          onClick={toggleFlyout}
          disabled={disabled}
          onMouseEnter={() => {
            if (!disabled) {
              openFlyout();
            }
          }}
          onMouseLeave={scheduleFlyoutClose}
          aria-label="切换默认打开工具"
          aria-haspopup="menu"
          aria-expanded={anchorEl != null}
          sx={{ height: 28, borderRadius: 0, px: 0.5, borderLeft: 1, borderColor: 'divider', color: 'text.secondary' }}
        >
          <KeyboardArrowDownIcon sx={{ fontSize: 14 }} />
        </IconButton>
      </Box>

      {/* 工具选择浮层：非模态 Popper + Paper（hover 类浮层禁用模态 Popover——其全视口
          透明 Backdrop 会盖住胶囊本体：悬停配对断裂陷入开关闪烁循环、TrapFocus 抢
          嵌入式终端焦点）。bottom-end = 下方展开、右缘与胶囊右缘对齐；热区仅 ▼ 段与
          Paper 本体（左段非热区，移入即走宽限关闭），进出双端挂宽限；Esc 兜底。
          configReady 前不展开（首帧即终值）。 */}
      <Popper
        open={configReady && anchorEl != null}
        anchorEl={anchorEl}
        placement="bottom-end"
        modifiers={[{ name: 'offset', options: { offset: [0, 4] } }]}
        sx={{ zIndex: theme => theme.zIndex.tooltip }}
      >
        <Paper
          elevation={4}
          onMouseEnter={clearCloseTimer}
          onMouseLeave={scheduleFlyoutClose}
          sx={{ py: 0.5, minWidth: 160 }}
        >
          <MenuList
            dense
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                closeFlyout();
              }
            }}
          >
            {tools.map((tool) => {
              const Icon = tool.Icon;
              const isPreferred = tool.id === preferred;
              return (
                <MenuItem
                  key={tool.id}
                  onClick={() => void runOpen(tool.id)}
                  disabled={opening}
                  sx={{ gap: 1, px: 1.5, py: 0.75 }}
                >
                  <ListItemIcon sx={{ minWidth: 24, color: 'text.secondary' }}>
                    <Icon />
                  </ListItemIcon>
                  <Typography variant="body2" sx={{ flex: 1 }}>{tool.label}</Typography>
                  {isPreferred && <CheckIcon sx={{ fontSize: 14, color: 'primary.main' }} />}
                </MenuItem>
              );
            })}
          </MenuList>
        </Paper>
      </Popper>
      {toastSnack}
    </>
  );
}
