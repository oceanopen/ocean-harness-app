import type { WorkspaceModel, WorkspaceProjectModel } from '@src/services';
import type { KeyboardEvent, ReactNode } from 'react';
import type { CommandConfig, CommandGroup } from './types';
import { SearchOutlined as SearchOutlinedIcon } from '@mui/icons-material';
import { Box, CircularProgress, Dialog, Typography } from '@mui/material';
import { WorkspaceProjectService, WorkspaceService } from '@src/services';
import { Command } from 'cmdk';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useCommandPalette } from './CommandPaletteContext';
import { getCommandsByGroup } from './registry';

// 分组渲染顺序与对应标题 i18n key。
const GROUPS: { group: CommandGroup; headingKey: string }[] = [
  { group: 'navigation', headingKey: 'panel:commandPalette.group.navigation' },
  { group: 'action', headingKey: 'panel:commandPalette.group.action' },
  { group: 'jump', headingKey: 'panel:commandPalette.group.jump' },
];

// cmdk 元素经 data-attribute 暴露（[cmdk-input]/[cmdk-item]/...），
// 用 MUI sx 的后代选择器套主题色，避免引第三方 CSS-in-JS，与全 MUI 栈一致。
// 注：sx 递归解析嵌套对象中的调色板简写（color:'text.primary' 等）。纯样式常量，无运行时依赖。
const commandSx = {
  'overflow': 'hidden',
  '& [cmdk-input]': {
    'flex': 1,
    'minWidth': 0,
    'border': 'none',
    'outline': 'none',
    'background': 'transparent',
    'fontSize': '0.95rem',
    'color': 'text.primary',
    '&::placeholder': { color: 'text.disabled', opacity: 1 },
  },
  '& [cmdk-list]': {
    maxHeight: 360,
    overflowY: 'auto',
    px: 1,
    pb: 1,
    // 滚动条与主题协调（细条）。
    scrollbarWidth: 'thin',
  },
  '& [cmdk-group-heading]': {
    fontSize: '0.7rem',
    fontWeight: 700,
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    color: 'text.disabled',
    px: 1.5,
    pt: 1.5,
    pb: 0.5,
  },
  '& [cmdk-item]': {
    'display': 'flex',
    'alignItems': 'center',
    'gap': 1.5,
    'px': 1.5,
    'py': 1,
    'borderRadius': 1,
    'cursor': 'pointer',
    'color': 'text.primary',
    'fontSize': '0.9rem',
    // cmdk 在激活项上置 data-selected="true"；用 action.selected 标识，避免自调 alpha。
    '&[data-selected="true"]': {
      bgcolor: 'action.selected',
    },
    '&[data-selected="true"] [data-cmdk-icon]': {
      color: 'primary.main',
    },
  },
  '& [cmdk-empty]': {
    px: 2,
    py: 3,
    textAlign: 'center',
    color: 'text.disabled',
    fontSize: '0.85rem',
  },
  // cmdk 内部 active 项自动滚入可视区；滚动条 thumb 用 divider token（深浅色都协调，勿手调 alpha）。
  '& [cmdk-list]::-webkit-scrollbar': { width: 8 },
  '& [cmdk-list]::-webkit-scrollbar-thumb': {
    bgcolor: 'divider',
    borderRadius: 4,
  },
} as const;

// 命令面板 Dialog：MUI Dialog 作浮层（backdrop/Esc/click-away/portal），内嵌 cmdk 负责搜索与键盘。
// 默认页按分组渲染命令；选中"跳转"命令切入二级页（工作空间/项目列表），Backspace(空输入) 返回。
function CommandPaletteDialog() {
  const ctx = useCommandPalette();
  const { t } = useTranslation();
  const inputRef = useRef<HTMLInputElement>(null);
  // 渲染期复位搜索词的 prev 追踪（替代 effect 内同步 setSearch）。
  const prevResetKeyRef = useRef<string | null>(null);

  // 受控搜索词：Backspace 返回逻辑依赖判空，故不交给 cmdk 内部状态。
  const [search, setSearch] = useState('');
  // 二级页实体列表 + 加载态（避免 load 期间误显"无结果"）。
  const [workspaces, setWorkspaces] = useState<WorkspaceModel[]>([]);
  const [projects, setProjects] = useState<WorkspaceProjectModel[]>([]);
  const [loading, setLoading] = useState(false);

  // 打开/切页时重置搜索词：渲染期据 (isOpen, subPage) 变化调整（React 推荐），避免 effect 内同步 setState。
  const resetKey = `${ctx.isOpen}:${ctx.subPage}`;
  if (prevResetKeyRef.current !== resetKey) {
    prevResetKeyRef.current = resetKey;
    setSearch('');
  }

  // 打开时聚焦输入框（MUI Dialog 挂载后 command 输入抢焦）。
  useEffect(() => {
    if (!ctx.isOpen) {
      return;
    }
    // 微延后至 Dialog 过渡挂载完成，确保 input 可聚焦。
    const timer = window.setTimeout(() => inputRef.current?.focus(), 0);
    return () => window.clearTimeout(timer);
  }, [ctx.isOpen]);

  // 二级页拉取对应实体列表（复用 tracker getList，无需新增 server 端点）。
  // 抽成 useCallback（同 tracker 各页 load 范式），effect 仅调用——setState 不在 effect 体内同步执行，
  // 规避 react/set-state-in-effect。loading 显隐与 try/catch/finally 三段式对齐 WorkspacesPage。
  const loadSubPage = useCallback(async () => {
    if (!ctx.isOpen || ctx.subPage == null) {
      return;
    }
    setLoading(true);
    try {
      if (ctx.subPage === 'workspace') {
        setWorkspaces(await WorkspaceService.getList());
      } else if (ctx.subPage === 'project') {
        if (ctx.currentWorkspaceId == null) {
          setProjects([]);
          return;
        }
        setProjects(await WorkspaceProjectService.getList({ workspaceId: ctx.currentWorkspaceId }));
      }
    } catch (e) {
      console.warn(`[command-palette] load ${ctx.subPage} failed:`, e);
      if (ctx.subPage === 'workspace') {
        setWorkspaces([]);
      } else {
        setProjects([]);
      }
    } finally {
      setLoading(false);
    }
  }, [ctx.isOpen, ctx.subPage, ctx.currentWorkspaceId]);

  useEffect(() => {
    void loadSubPage();
  }, [loadSubPage]);

  // 选中命令：执行 action，按 closeOnSelect 决定是否关闭。
  const runCommand = (cmd: CommandConfig) => {
    cmd.action(ctx);
    if (cmd.closeOnSelect) {
      ctx.close();
    }
  };

  // 二级页：选中实体 → 宿主回写 → 关闭。
  const pickWorkspace = (ws: WorkspaceModel) => {
    ctx.selectWorkspace(ws);
    ctx.close();
  };
  const pickProject = (project: WorkspaceProjectModel) => {
    ctx.selectProject(project);
    ctx.close();
  };

  // Backspace 在空输入下返回上一级（仅二级页生效）；Esc 由 MUI Dialog 兜底关闭。
  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Backspace' && search === '' && ctx.subPage != null) {
      e.preventDefault();
      ctx.setSubPage(null);
    }
  };

  const subPageHeading
    = ctx.subPage === 'workspace'
      ? t('panel:commandPalette.jump.workspace')
      : ctx.subPage === 'project'
        ? t('panel:commandPalette.jump.project')
        : '';

  return (
    <Dialog
      open={ctx.isOpen}
      onClose={ctx.close}
      fullWidth
      maxWidth="sm"
      // 取消 Dialog 默认的内边距与 Paper 阴影留白，由内部 Box 接管紧凑布局。
      slotProps={{ paper: { sx: { mt: '18vh', alignSelf: 'flex-start', overflow: 'hidden' } } }}
    >
      <Box sx={commandSx}>
        <Command onKeyDown={handleKeyDown} className="command-palette">
          {/* 搜索行：放大镜 + cmdk 输入。二级页时输入框左侧显示当前页名作面包屑。 */}
          <Box
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 1,
              px: 2,
              py: 1.5,
              borderBottom: 1,
              borderColor: 'divider',
            }}
          >
            <SearchOutlinedIcon sx={{ fontSize: 20, color: 'text.secondary' }} />
            {ctx.subPage != null && (
              <Typography variant="caption" sx={{ fontWeight: 600, color: 'text.disabled', flexShrink: 0 }}>
                {subPageHeading}
              </Typography>
            )}
            <Command.Input
              ref={inputRef}
              value={search}
              onValueChange={setSearch}
              placeholder={ctx.subPage == null
                ? t('panel:commandPalette.placeholder')
                : t('panel:commandPalette.placeholderSubPage')}
            />
          </Box>

          <Command.List>
            {ctx.subPage == null
              ? (
                  GROUPS.map(({ group, headingKey }) => {
                    const items = getCommandsByGroup(group, ctx);
                    if (items.length === 0) {
                      return null;
                    }
                    return (
                      <Command.Group key={group} heading={t(headingKey)}>
                        {items.map(cmd => (
                          <Command.Item
                            key={cmd.id}
                            value={t(cmd.titleI18nKey)}
                            keywords={cmd.keywords}
                            onSelect={() => runCommand(cmd)}
                          >
                            <Box component="span" data-cmdk-icon sx={{ display: 'flex', color: 'text.secondary' }}>
                              {cmd.icon}
                            </Box>
                            <Box component="span" sx={{ flex: 1 }}>
                              {t(cmd.titleI18nKey)}
                            </Box>
                            {cmd.shortcut && (
                              <Typography variant="caption" color="text.disabled">{cmd.shortcut}</Typography>
                            )}
                          </Command.Item>
                        ))}
                      </Command.Group>
                    );
                  })
                )
              : (
                  <SubPageList
                    loading={loading}
                    workspaces={ctx.subPage === 'workspace' ? workspaces : []}
                    projects={ctx.subPage === 'project' ? projects : []}
                    onPickWorkspace={pickWorkspace}
                    onPickProject={pickProject}
                  />
                )}
            {!loading && <Command.Empty>{t('panel:commandPalette.empty')}</Command.Empty>}
          </Command.List>

          {/* 底部提示：Esc 关闭 / 二级页时 ⌫ 返回。 */}
          <Box
            sx={{
              display: 'flex',
              gap: 2,
              px: 2,
              py: 1,
              borderTop: 1,
              borderColor: 'divider',
              bgcolor: 'background.default',
            }}
          >
            <Typography variant="caption" color="text.disabled">
              <Kbd>Esc</Kbd>
              {' '}
              {t('panel:commandPalette.footer.close')}
            </Typography>
            {ctx.subPage != null && (
              <Typography variant="caption" color="text.disabled">
                <Kbd>⌫</Kbd>
                {' '}
                {t('panel:commandPalette.footer.back')}
              </Typography>
            )}
          </Box>
        </Command>
      </Box>
    </Dialog>
  );
}

// 二级页实体列表：loading 显进度环，否则渲染 cmdk Item（实体名既作展示又作过滤 value）。
interface SubPageListProps {
  loading: boolean;
  workspaces: WorkspaceModel[];
  projects: WorkspaceProjectModel[];
  onPickWorkspace: (ws: WorkspaceModel) => void;
  onPickProject: (project: WorkspaceProjectModel) => void;
}

function SubPageList({ loading, workspaces, projects, onPickWorkspace, onPickProject }: SubPageListProps) {
  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
        <CircularProgress size={24} />
      </Box>
    );
  }
  return (
    <>
      {workspaces.map(ws => (
        <Command.Item key={ws.id} value={ws.name} onSelect={() => onPickWorkspace(ws)}>
          <Typography variant="body2">{ws.name}</Typography>
        </Command.Item>
      ))}
      {projects.map(p => (
        <Command.Item key={p.id} value={p.name} onSelect={() => onPickProject(p)}>
          <Typography variant="body2">{p.name}</Typography>
        </Command.Item>
      ))}
    </>
  );
}

// 简易键帽样式（小号等宽 + 描边），用于底部快捷键提示。
function Kbd({ children }: { children: ReactNode }) {
  return (
    <Box
      component="kbd"
      sx={{
        fontFamily: 'monospace',
        fontSize: '0.7rem',
        px: 0.5,
        borderRadius: 0.5,
        border: 1,
        borderColor: 'divider',
        bgcolor: 'action.hover',
      }}
    >
      {children}
    </Box>
  );
}

export default CommandPaletteDialog;
