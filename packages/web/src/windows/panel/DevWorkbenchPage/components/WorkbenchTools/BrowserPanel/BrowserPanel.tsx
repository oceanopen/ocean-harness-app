import type { ConversationProfile } from '@src/state/browserSession';
import type { ReactNode } from 'react';
import AddOutlinedIcon from '@mui/icons-material/AddOutlined';
import LanguageOutlinedIcon from '@mui/icons-material/LanguageOutlined';
import { Alert, Box, Button, CircularProgress, Tooltip, Typography } from '@mui/material';
import {
  CONVERSATION_SESSION_SLOT_COUNT,
  deriveConversationProfile,
  isSessionLive,
  isTaskSessionProfile,
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
  /** 当前选中 issue id（会话过滤派生 + 「AI 分析此页」归因）。 */
  issueId: string;
  /** 面板区展开态（ToolPanelArea 透传）：迷你预览帧流「收起即停」闸门之一。 */
  panelVisible: boolean;
}

/**
 * BrowserPanel：当前任务的浏览器会话面板（工作台互斥工具 tab）——快照按任务命名空间过滤
 * （issue-<id>_session-<convId>-NN），卡片徽标按归属会话展示，多实例共享全局登录态；
 * 头部「新建会话」按任务主会话开最低空闲槽位（达 10 槽置灰），已释放槽位在卡片上原样
 * 重开。激活经 useBrowserStageStore 上抛接管工作台浮层，全局 SSE 驱动投影缓存。
 */
export default function BrowserPanel({ issueId, panelVisible }: BrowserPanelProps) {
  const { data: snapshot, isLoading, error, refetch } = useBrowserSessionView();
  const ensureMutation = useEnsureBrowserSession();
  const { data: conversations } = useConversations('task', issueId);
  // 任务主会话（parentId 空）：「新建会话」的派生锚与「AI 分析此页」的归因键。
  const mainConversation = (conversations ?? []).find(c => c.parentId === '');

  // 全局 SSE 订阅：建连首帧快照 + 增量 sessions 帧驱动投影缓存（含 idle/failed 行回写）。
  useBrowserSessionEvents();

  // 当前任务命名空间的全部会话实例（任意会话/槽位），按会话与槽位升序。
  const sessions = (snapshot?.sessions ?? [])
    .filter(s => isTaskSessionProfile(issueId, s.profile))
    .sort((a, b) => {
      const pa = parseConversationProfile(a.profile);
      const pb = parseConversationProfile(b.profile);
      return (pa?.conversationId ?? '').localeCompare(pb?.conversationId ?? '') || (pa?.slot ?? 0) - (pb?.slot ?? 0);
    });

  // 主会话命名空间的最低无行槽位（行含 idle/failed：新建 = 真正新实例，重开走卡片「打开会话」）。
  const usedSlots = new Set(
    sessions
      .map(s => parseConversationProfile(s.profile))
      .filter((p): p is ConversationProfile => p != null && p.conversationId === mainConversation?.id)
      .map(p => p.slot),
  );
  const nextSlot = Array.from({ length: CONVERSATION_SESSION_SLOT_COUNT }, (_, i) => i + 1).find(slot => !usedSlots.has(slot));

  const createOrOpen = (profile: string) => {
    ensureMutation.mutate({ profile }, {
      onSuccess: () => useBrowserStageStore.getState().clearWatchPage(profile),
    });
  };

  // 新建会话按钮（primary = 空态主按钮实心态；无主会话或达上限置灰）。
  const createButton = (primary: boolean) => (
    <Tooltip title={
      mainConversation == null
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
          onClick={() => mainConversation != null && nextSlot != null && createOrOpen(deriveConversationProfile('issue', issueId, mainConversation.id, nextSlot))}
          disabled={mainConversation == null || nextSlot == null || ensureMutation.isPending}
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
        <Typography variant="body2" color="text.secondary">当前任务暂无浏览器会话</Typography>
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
            conversationId={mainConversation?.id ?? ''}
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
