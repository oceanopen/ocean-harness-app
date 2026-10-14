import type { ReactNode } from 'react';
import LanguageOutlinedIcon from '@mui/icons-material/LanguageOutlined';
import { Alert, Box, Button, CircularProgress, Typography } from '@mui/material';
import { deriveIssueProfile, useBrowserSessionEvents, useBrowserSessionView, useEnsureBrowserSession } from '@src/state/browserSession';
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
 * BrowserPanel：当前任务的浏览器会话面板（工作台互斥工具 tab）——快照按任务派生 profile
 * 过滤（任务↔会话 1:1，登录态跨重启保留）。空态内一键新建/打开（headless 读 profile 偏好），
 * 有会话直接展示卡片；激活经 useBrowserStageStore 上抛接管工作台浮层，全局 SSE 驱动投影缓存。
 */
export default function BrowserPanel({ issueId, panelVisible }: BrowserPanelProps) {
  const { data: snapshot, isLoading, error, refetch } = useBrowserSessionView();
  const ensureMutation = useEnsureBrowserSession();

  // 全局 SSE 订阅：建连首帧快照 + 增量 sessions 帧驱动投影缓存（含 idle/failed 行回写）。
  useBrowserSessionEvents();

  // 仅当前任务关联的会话（profile 由任务派生，1:1）。
  const sessions = (snapshot?.sessions ?? []).filter(s => s.profile === deriveIssueProfile(issueId));

  const createOrOpen = () => {
    // profile 由任务派生：新建即关联当前任务，同任务复用同 profile；headless 键省略读后端 D10 偏好。
    ensureMutation.mutate({ profile: deriveIssueProfile(issueId) });
  };

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
        <Button variant="contained" size="small" onClick={createOrOpen} disabled={ensureMutation.isPending}>
          {ensureMutation.isPending ? <CircularProgress size={14} color="inherit" /> : '新建 / 打开会话'}
        </Button>
        <Typography variant="caption" color="text.secondary">
          会话按任务独立，登录态跨重启保留
        </Typography>
      </Box>
    );
  } else {
    body = sessions.map(session => (
      <BrowserSessionCard key={session.profile} session={session} issueId={issueId} panelVisible={panelVisible} />
    ));
  }

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', minHeight: '0px' }}>
      <Box sx={{ flex: 1, minHeight: '0px', overflowY: 'auto', p: '8px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
        {body}
      </Box>
    </Box>
  );
}
