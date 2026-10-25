import type { ConversationProfile } from '@src/state/browserSession';
import type { ReactNode } from 'react';
import AddOutlinedIcon from '@mui/icons-material/AddOutlined';
import LanguageOutlinedIcon from '@mui/icons-material/LanguageOutlined';
import { Alert, Box, Button, CircularProgress, Tooltip, Typography } from '@mui/material';
import {
  CONVERSATION_SESSION_SLOT_COUNT,
  deriveConversationProfile,
  isSessionLive,
  parseConversationProfile,
  useBrowserSessionEvents,
  useBrowserSessionView,
  useBrowserStageStore,
  useEnsureBrowserSession,
} from '@src/state/browserSession';
import { useConversations } from '@src/state/conversation';
import BrowserSessionCard from './BrowserSessionCard';

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

interface BrowserPanelProps {
  /** 当前锚定 id（会话过滤派生 + 「AI 分析此页」归因；workspace 语境 = 选中会话 id）。 */
  issueId: string;
  /** 面板区展开态（ToolPanelArea 透传）：迷你预览帧流「收起即停」闸门之一。 */
  panelVisible: boolean;
  /** 锚定语境（缺省 = 任务）：实例命名空间在 D11 两族间分派。 */
  conversationScope?: 'task' | 'workspace';
  /** 工作空间容器 id（workspace 语境必传）：profile 命名空间 owner 段。 */
  containerId?: string;
}

/**
 * BrowserPanel：当前锚定的浏览器会话面板（工作台互斥工具 tab）——快照按锚定命名空间族
 * 过滤（任务 issue-<id>_session-… / 工作空间 workspace-<id>_session-…），卡片徽标按归属
 * 会话展示，多实例共享全局登录态；「新建会话」按锚定会话开最低空闲槽位（达 10 槽置灰），
 * 已释放槽位在卡片上原样重开。激活经 useBrowserStageStore 上抛接管工作台浮层，全局 SSE
 * 驱动投影缓存。
 */
export default function BrowserPanel({ issueId, panelVisible, conversationScope = 'task', containerId }: BrowserPanelProps) {
  const { data: snapshot, isLoading, error, refetch } = useBrowserSessionView();
  const ensureMutation = useEnsureBrowserSession();
  const workspaceScope = conversationScope === 'workspace';
  // 命名空间 owner（D11 两族）：任务语境 = 任务 id；工作空间语境 = 容器 id。
  const ownerKind = workspaceScope ? 'workspace' as const : 'issue' as const;
  const ownerId = workspaceScope ? containerId ?? '' : issueId;
  // 任务主会话（parentId 空）=「新建会话」派生锚与归因键；工作空间无主/子概念，锚 = 当前选中会话。
  const { data: conversations } = useConversations('task', workspaceScope ? null : issueId);
  const mainConversation = (conversations ?? []).find(c => c.parentId === '');
  const anchorConversationId = workspaceScope ? issueId : mainConversation?.id ?? null;

  // 全局 SSE 订阅：建连首帧快照 + 增量 sessions 帧驱动投影缓存（含 idle/failed 行回写）。
  useBrowserSessionEvents();

  // 当前命名空间的全部会话实例（任意会话/槽位），按会话与槽位升序。
  const sessions = (snapshot?.sessions ?? [])
    .filter((s) => {
      const parsed = parseConversationProfile(s.profile);
      return parsed != null && parsed.ownerKind === ownerKind && parsed.ownerID === ownerId;
    })
    .sort((a, b) => {
      const pa = parseConversationProfile(a.profile);
      const pb = parseConversationProfile(b.profile);
      return (pa?.conversationId ?? '').localeCompare(pb?.conversationId ?? '') || (pa?.slot ?? 0) - (pb?.slot ?? 0);
    });

  // 锚定会话命名空间的最低无行槽位（行含 idle/failed：新建 = 真正新实例，重开走卡片「打开会话」）。
  const usedSlots = new Set(
    sessions
      .map(s => parseConversationProfile(s.profile))
      .filter((p): p is ConversationProfile => p != null && p.conversationId === anchorConversationId)
      .map(p => p.slot),
  );
  const nextSlot = Array.from({ length: CONVERSATION_SESSION_SLOT_COUNT }, (_, i) => i + 1).find(slot => !usedSlots.has(slot));
  const canCreate = anchorConversationId != null && ownerId !== '' && nextSlot != null;

  const createOrOpen = (profile: string) => {
    ensureMutation.mutate({ profile }, {
      onSuccess: () => useBrowserStageStore.getState().clearWatchPage(profile),
    });
  };

  // 新建会话按钮（primary = 空态主按钮实心态；无锚定会话或达上限置灰）。
  const createButton = (primary: boolean) => (
    <Tooltip title={
      anchorConversationId == null
        ? '任务暂无主会话，无法派生新实例'
        : nextSlot == null
          ? `已达单会话实例上限（${CONVERSATION_SESSION_SLOT_COUNT}）`
          : '再开一个独立会话实例（共享全局登录态）'
    }
    >
      <span>
        <Button
          size="small"
          variant={primary ? 'contained' : 'outlined'}
          startIcon={<AddOutlinedIcon sx={{ fontSize: '16px' }} />}
          onClick={() => canCreate && createOrOpen(deriveConversationProfile(ownerKind, ownerId, anchorConversationId!, nextSlot!))}
          disabled={!canCreate || ensureMutation.isPending}
          sx={{ flexShrink: 0, fontSize: '12px' }}
        >
          {ensureMutation.isPending ? <CircularProgress size={12} color="inherit" /> : '新建会话'}
        </Button>
      </span>
    </Tooltip>
  );

  let body: ReactNode;
  if (error != null) {
    body = (
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', p: '16px' }}>
        <Alert
          severity="error"
          action={(
            <Button color="inherit" size="small" onClick={() => void refetch()}>
              重试
            </Button>
          )}
        >
          {errorMessage(error)}
        </Alert>
      </Box>
    );
  } else if (isLoading) {
    body = (
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <CircularProgress size={20} sx={{ color: 'text.secondary' }} />
      </Box>
    );
  } else if (sessions.length === 0) {
    body = (
      <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: '8px' }}>
        <LanguageOutlinedIcon sx={{ fontSize: '36px', color: 'text.disabled' }} />
        <Typography variant="body2" color="text.secondary">
          {workspaceScope ? '当前工作空间暂无浏览器会话' : '当前任务暂无浏览器会话'}
        </Typography>
        {ensureMutation.error && (
          <Alert severity="error" sx={{ fontSize: '12px' }}>{errorMessage(ensureMutation.error)}</Alert>
        )}
        {createButton(true)}
        <Typography variant="caption" color="text.secondary">
          实例按会话独立，登录态全局共享、跨重启保留
        </Typography>
      </Box>
    );
  } else {
    body = (
      <>
        {ensureMutation.error && (
          <Alert severity="error" onClose={() => ensureMutation.reset()} sx={{ fontSize: '12px' }}>
            {errorMessage(ensureMutation.error)}
          </Alert>
        )}
        {sessions.map(session => (
          <BrowserSessionCard
            key={session.profile}
            session={session}
            conversationId={anchorConversationId ?? ''}
            panelVisible={panelVisible}
            showSlot={sessions.length > 1}
          />
        ))}
      </>
    );
  }

  const hasSessions = sessions.length > 0;
  const liveCount = sessions.filter(s => isSessionLive(s.status)).length;
  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', minHeight: '0px' }}>
      {hasSessions && (
        <Box sx={{ flexShrink: 0, display: 'flex', alignItems: 'center', gap: '8px', px: '8px', pt: '8px' }}>
          {createButton(nextSlot != null)}
          <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>
            {`${liveCount} 个会话进行中`}
          </Typography>
        </Box>
      )}
      <Box sx={{ flex: 1, minHeight: '0px', overflowY: 'auto', p: '8px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
        {body}
      </Box>
    </Box>
  );
}
