import type { ReactNode } from 'react';
import AddOutlinedIcon from '@mui/icons-material/AddOutlined';
import LanguageOutlinedIcon from '@mui/icons-material/LanguageOutlined';
import { Alert, Box, Button, CircularProgress, Tooltip, Typography } from '@mui/material';
import {
  deriveIssueProfileSlot,
  isSessionLive,
  isTaskSessionProfile,
  parseTaskSessionProfile,
  TASK_SESSION_SLOT_COUNT,
  useBrowserSessionEvents,
  useBrowserSessionView,
  useBrowserStageStore,
  useEnsureBrowserSession,
} from '@src/state/browserSession';
import BrowserSessionCard from './BrowserSessionCard';

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

interface BrowserPanelProps {
  /** 当前选中 issue id（会话过滤派生 + 「AI 分析此页」归因）。 */
  issueId: string;
  /** 当前任务归属工作空间（会话共享登录态键，ensure 直传后端持久化）。 */
  workspaceId: string;
  /** 面板区展开态（ToolPanelArea 透传）：迷你预览帧流「收起即停」闸门之一。 */
  panelVisible: boolean;
}

/**
 * BrowserPanel：当前任务的浏览器会话面板（工作台互斥工具 tab）——快照按任务命名空间过滤
 * （issue-<id> 基名 + -2..-5 多开槽位，槽位升序），多会话共享任务工作空间登录态；头部
 * 「新建会话」开最低空闲槽位（达 5 槽置灰），已释放槽位在卡片上原样重开。激活经
 * useBrowserStageStore 上抛接管工作台浮层，全局 SSE 驱动投影缓存。
 */
export default function BrowserPanel({ issueId, workspaceId, panelVisible }: BrowserPanelProps) {
  const { data: snapshot, isLoading, error, refetch } = useBrowserSessionView();
  const ensureMutation = useEnsureBrowserSession();

  // 全局 SSE 订阅：建连首帧快照 + 增量 sessions 帧驱动投影缓存（含 idle/failed 行回写）。
  useBrowserSessionEvents();

  // 当前任务命名空间的全部会话（任意槽位），按槽位升序。
  const sessions = (snapshot?.sessions ?? [])
    .filter(s => isTaskSessionProfile(issueId, s.profile))
    .sort((a, b) => (parseTaskSessionProfile(a.profile)?.slot ?? 1) - (parseTaskSessionProfile(b.profile)?.slot ?? 1));

  // 最低无行槽位（行含 idle/failed：新建 = 真正新实例，重开走卡片「打开会话」）。
  const usedSlots = new Set(sessions.map(s => parseTaskSessionProfile(s.profile)?.slot ?? 1));
  const nextSlot = Array.from({ length: TASK_SESSION_SLOT_COUNT }, (_, i) => i + 1).find(slot => !usedSlots.has(slot));

  const createOrOpen = (profile: string) => {
    // workspaceId 随 ensure 直传：新建会话即刻共享任务工作空间登录态（后端落盘续传）。
    ensureMutation.mutate({ profile, workspaceId }, {
      onSuccess: () => useBrowserStageStore.getState().clearWatchPage(profile),
    });
  };

  // 新建会话按钮（primary = 空态主按钮实心态；nextSlot 为空即达上限置灰）。
  const createButton = (primary: boolean) => (
    <Tooltip title={nextSlot == null ? `已达单任务会话上限（${TASK_SESSION_SLOT_COUNT}）` : '再开一个独立会话（共享本任务工作空间登录态）'}>
      <span>
        <Button
          size="small"
          variant={primary ? 'contained' : 'outlined'}
          startIcon={<AddOutlinedIcon sx={{ fontSize: '16px' }} />}
          onClick={() => nextSlot != null && createOrOpen(deriveIssueProfileSlot(issueId, nextSlot))}
          disabled={nextSlot == null || ensureMutation.isPending}
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
          会话按任务独立，登录态跨重启保留
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
            conversationId="" /* 分析键随 T7 接 conversation 上下文，当前显式降级 */
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
            {`${liveCount}/${TASK_SESSION_SLOT_COUNT} 个会话进行中`}
          </Typography>
        </Box>
      )}
      <Box sx={{ flex: 1, minHeight: '0px', overflowY: 'auto', p: '8px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
        {body}
      </Box>
    </Box>
  );
}
