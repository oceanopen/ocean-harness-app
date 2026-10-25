import type { WorkspaceModel } from '@src/services';
import {
  AddOutlined as AddOutlinedIcon,
  AppsOutlined as AppsOutlinedIcon,
  FolderOutlined as FolderOutlinedIcon,
  WorkspacesOutlined as WorkspacesOutlinedIcon,
} from '@mui/icons-material';
import {
  Box,
  Button,
  CircularProgress,
  IconButton,
  Menu,
  MenuItem,
  Typography,
} from '@mui/material';
import ConversationRow from '@src/components/conversationTree/ConversationRow';
import TreeRow from '@src/components/conversationTree/TreeRow';
import { useClaudeActiveSessionIds } from '@src/components/conversationTree/useClaudeActiveSessionIds';
import { mergeLaunchSettings } from '@src/shared/launchSettings';
import { useToast } from '@src/shared/useToast';
import { useConversations, useCreateConversation } from '@src/state/conversation';
import { useWorkspaces } from '@src/state/tracker';
import { useMemo, useState } from 'react';
import WorkspaceDrawer from './WorkspaceDrawer';

interface WorkspaceTreeProps {
  // 选中工作空间 id（选中态 SSOT 在页面 URL，树上只做高亮与上抛）。
  selectedId: string | null;
  // 选中会话 id（SSOT 同在 URL cid；树上只做高亮与上抛）。
  selectedConversationId: string | null;
  // 「列表」开关（SSOT 在页面：驱动内容区卡片网格，树上只做高亮与上抛）。
  listOpen: boolean;
  onToggleList: () => void;
  onSelectWorkspace: (ws: WorkspaceModel) => void;
  // 会话行点击/新建后自动进入（携带归属工作空间，页面一次写 wid+cid）。
  onSelectConversation: (wsId: string, conversationId: string) => void;
}

/**
 * 工作空间页左树：顶部「新建/列表」工具栏（列表开关上抛页面驱动内容区卡片网格）+
 * 平铺树（工作空间节点 → 会话行）。节点 hover「...」= 编辑工作空间/新建会话；
 * 会话行运行态 = 会话锚 ∈ claude 活跃快照。工作空间 CRUD 走 tracker 域 mutation。
 */
function WorkspaceTree({ selectedId, selectedConversationId, listOpen, onToggleList, onSelectWorkspace, onSelectConversation }: WorkspaceTreeProps) {
  const { data: workspaces = [], isLoading } = useWorkspaces();
  const activeSessionIds = useClaudeActiveSessionIds();
  const { show: showToast, snack } = useToast();
  const createConversation = useCreateConversation();
  // 折叠集合（默认全展开）：仅记录用户显式收起的节点，新建空间自动展开。
  const [collapsedIds, setCollapsedIds] = useState<Set<string>>(() => new Set());
  const [menu, setMenu] = useState<{ ws: WorkspaceModel; anchor: HTMLElement } | null>(null);
  // 抽屉态：create 无载荷；edit 携带反显实体（条件挂载，每次打开新挂载）。
  const [drawer, setDrawer] = useState<{ mode: 'create' } | { mode: 'edit'; ws: WorkspaceModel } | null>(null);

  // 兜底排序（uuid v7 字典序 DESC，新建在前；与旧列表视图同口径）。
  const sorted = useMemo(
    () => [...workspaces].sort((a, b) => (a.id < b.id ? 1 : a.id > b.id ? -1 : 0)),
    [workspaces],
  );

  const toggleCollapsed = (id: string) => {
    setCollapsedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  // 新建会话：mode 取工作空间生效启动档（merge 单源，issue 级覆盖缺省）；acp 档建 acp 会话，
  // 其余建终端会话；成功即选中进入（页面写 wid+cid，内容区按 §2.3 时序编排）。
  const handleCreateConversation = async (ws: WorkspaceModel) => {
    const mode = mergeLaunchSettings(ws.launchSettings, undefined)?.mode === 'acp' ? 'acp' : 'terminal';
    try {
      const created = await createConversation.mutateAsync({ ownerType: 'workspace', ownerId: ws.id, mode });
      onSelectConversation(ws.id, created.id);
      setCollapsedIds((prev) => {
        const next = new Set(prev);
        next.delete(ws.id);
        return next;
      });
      showToast('会话已创建', 'success');
    } catch (e) {
      showToast(`创建会话失败：${e instanceof Error ? e.message : String(e)}`, 'error');
    }
  };

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      {/* 工具栏：计数 + 新建 + 列表浮层开关（浮层管理入口见 WorkspacesView 范式） */}
      <Box
        sx={{
          height: '40px',
          flexShrink: 0,
          px: '8px',
          borderBottom: '1px solid',
          borderColor: 'divider',
          display: 'flex',
          alignItems: 'center',
          gap: '4px',
        }}
      >
        <Typography variant="caption" color="text.secondary" sx={{ flex: 1 }}>
          {`共 ${workspaces.length} 个工作空间`}
        </Typography>
        <IconButton size="small" onClick={() => setDrawer({ mode: 'create' })} aria-label="新建工作空间">
          <AddOutlinedIcon sx={{ fontSize: '18px' }} />
        </IconButton>
        <IconButton
          size="small"
          color={listOpen ? 'primary' : 'default'}
          onClick={onToggleList}
          aria-label={listOpen ? '收起列表' : '工作空间列表'}
        >
          <AppsOutlinedIcon sx={{ fontSize: '18px' }} />
        </IconButton>
      </Box>

      {/* 树体 */}
      <Box sx={{ flex: 1, minHeight: '0px' }}>
        <Box sx={{ height: '100%', overflow: 'auto', py: '4px' }}>
          {isLoading && workspaces.length === 0 && (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: '24px' }}>
              <CircularProgress size={20} />
            </Box>
          )}
          {!isLoading && workspaces.length === 0 && (
            <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '8px', px: '16px', py: '32px' }}>
              <WorkspacesOutlinedIcon sx={{ fontSize: '36px', color: 'text.secondary' }} />
              <Typography variant="body2" color="text.secondary" align="center">暂无工作空间</Typography>
              <Button variant="contained" size="small" startIcon={<AddOutlinedIcon />} onClick={() => setDrawer({ mode: 'create' })}>
                新建工作空间
              </Button>
            </Box>
          )}
          {sorted.map(ws => (
            <WorkspaceNode
              key={ws.id}
              ws={ws}
              selected={ws.id === selectedId}
              selectedConversationId={selectedConversationId}
              collapsed={collapsedIds.has(ws.id)}
              activeSessionIds={activeSessionIds}
              onSelect={() => onSelectWorkspace(ws)}
              onSelectConversation={onSelectConversation}
              onToggleCollapsed={() => toggleCollapsed(ws.id)}
              onMenu={anchor => setMenu({ ws, anchor })}
            />
          ))}
        </Box>
      </Box>

      {/* 工作空间节点「...」菜单：编辑 / 新建会话 */}
      <Menu anchorEl={menu?.anchor ?? null} open={menu != null} onClose={() => setMenu(null)}>
        <MenuItem
          onClick={() => {
            const ws = menu?.ws;
            setMenu(null);
            if (ws) {
              setDrawer({ mode: 'edit', ws });
            }
          }}
        >
          编辑工作空间…
        </MenuItem>
        <MenuItem
          onClick={() => {
            const ws = menu?.ws;
            setMenu(null);
            if (ws) {
              void handleCreateConversation(ws);
            }
          }}
        >
          新建会话
        </MenuItem>
      </Menu>

      {snack}

      {drawer != null && (
        <WorkspaceDrawer
          workspace={drawer.mode === 'edit' ? drawer.ws : undefined}
          onClose={() => setDrawer(null)}
          onCreated={ws => showToast(`工作空间「${ws.name}」创建成功`, 'success')}
          onUpdated={ws => showToast(`工作空间「${ws.name}」已保存`, 'success')}
        />
      )}
    </Box>
  );
}

// 工作空间节点：标题行（折叠箭头 + 图标 + 名称 + hover「...」）+ 展开时的会话行列表。
interface WorkspaceNodeProps {
  ws: WorkspaceModel;
  selected: boolean;
  selectedConversationId: string | null;
  collapsed: boolean;
  activeSessionIds: Set<string>;
  onSelect: () => void;
  onSelectConversation: (wsId: string, conversationId: string) => void;
  onToggleCollapsed: () => void;
  onMenu: (anchor: HTMLElement) => void;
}

function WorkspaceNode({ ws, selected, selectedConversationId, collapsed, activeSessionIds, onSelect, onSelectConversation, onToggleCollapsed, onMenu }: WorkspaceNodeProps) {
  const { data: conversations = [] } = useConversations('workspace', ws.id);
  const emoji = ws.emoji.trim();
  // 选中互斥：会话行选中即容器节点让位高亮（cid 与 wid 同源成对，cid 非空即本容器有会话选中）。
  const nodeSelected = selected && selectedConversationId == null;

  return (
    <Box>
      <TreeRow
        variant="node"
        title={ws.name}
        selected={nodeSelected}
        onClick={onSelect}
        collapsed={collapsed}
        icon={emoji
          ? <Typography component="span" sx={{ fontSize: '14px', lineHeight: 1 }}>{emoji}</Typography>
          : <FolderOutlinedIcon sx={{ fontSize: '16px', color: 'text.disabled' }} />}
        onToggleCollapsed={onToggleCollapsed}
        moreLabel="工作空间操作"
        onMore={onMenu}
      />
      {!collapsed && conversations.map(conv => (
        <ConversationRow
          key={conv.id}
          conversation={conv}
          selected={conv.id === selectedConversationId}
          onSelect={() => onSelectConversation(ws.id, conv.id)}
          running={conv.claudeSessionId !== '' && activeSessionIds.has(conv.claudeSessionId)}
        />
      ))}
    </Box>
  );
}

export default WorkspaceTree;
