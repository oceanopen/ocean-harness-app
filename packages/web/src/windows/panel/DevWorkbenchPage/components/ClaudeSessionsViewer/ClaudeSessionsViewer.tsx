import type { ConversationModel } from '@src/services';
import { ForumOutlined as ForumOutlinedIcon } from '@mui/icons-material';
import { Alert, Box, Button, CircularProgress, Tooltip, Typography } from '@mui/material';
import { useClaudeActiveSessionIds } from '@src/components/conversationTree/useClaudeActiveSessionIds';
import { useToast } from '@src/shared/useToast';
import { useConversations } from '@src/state/conversation';
import { useMemo, useState } from 'react';
import AcpSessionView from '../AcpSessionView';
import TerminalErrorBoundary from '../EmbeddedTerminal/TerminalErrorBoundary';
import PanelToolbar from '../PanelToolbar';
import RefreshIconButton from '../RefreshIconButton';
import ConversationCard from './ConversationCard';

interface ClaudeSessionsViewerProps {
  // 当前锚定 id（会话归属 owner 的 ownerId；workspace 语境 = 当前选中会话 id）。
  issueId: string;
  // 任务归属项目 id（task 语境关联 bot 候选过滤域）。
  projectId: string;
  // 锚定语境（缺省 = 任务）：workspace = 当前选中会话单卡（工作空间会话无子会话概念）。
  conversationScope?: 'task' | 'workspace';
  // 工作空间容器 id（workspace 语境必传）：会话列表归属域 + 关联 bot 候选域 + 历史 cwd。
  containerId?: string;
}

/**
 * Claude 会话查看工具（O16/T8.1）双场景：任务语境 = 当前任务全部会话（含子会话，D12 仅
 * 此处可见）卡片列表；工作空间语境 = 当前选中会话单卡。实时查看分流：acp 内嵌完整
 * AcpSessionView；terminal 锚为锚定维度由工作台终端承载，仅提示。「新增子会话」恒拦截；
 * 运行态徽标走 claude 活跃快照订阅。
 */
export default function ClaudeSessionsViewer({ issueId, projectId, conversationScope = 'task', containerId }: ClaudeSessionsViewerProps) {
  const { show: showToast, snack } = useToast();
  const workspaceScope = conversationScope === 'workspace';
  const { data: conversations = [], isPending, isFetching, error, refetch } = useConversations(
    workspaceScope ? 'workspace' : 'task',
    workspaceScope ? containerId ?? null : issueId,
  );
  const activeSessionIds = useClaudeActiveSessionIds();
  // 实时查看中的 acp 会话（null = 列表态）；terminal 会话不进入该态（handleLiveView 分流）。
  const [viewing, setViewing] = useState<ConversationModel | null>(null);

  // 展示集：工作空间 = 仅当前选中会话；任务 = 全量按 updatedAt 倒序（最近活跃在前，
  // 客户端重排，不动共享缓存）。
  const sorted = useMemo(
    () => workspaceScope
      ? conversations.filter(c => c.id === issueId)
      : [...conversations].sort((a, b) => (a.updatedAt < b.updatedAt ? 1 : a.updatedAt > b.updatedAt ? -1 : 0)),
    [conversations, workspaceScope, issueId],
  );

  const refresh = () => {
    void refetch();
  };

  const handleLiveView = (conversation: ConversationModel) => {
    if (conversation.mode === 'acp') {
      setViewing(conversation);
      return;
    }
    showToast(workspaceScope ? '请在工作台终端查看（终端锚为工作空间维度）' : '请在工作台终端查看（终端锚为任务维度）', 'info');
  };

  if (viewing != null) {
    return (
      <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <PanelToolbar
          left={<Typography variant="caption" noWrap sx={{ minWidth: '0px' }}>{viewing.title}</Typography>}
          right={<Button size="small" onClick={() => setViewing(null)}>返回列表</Button>}
        />
        <Box sx={{ flex: 1, minHeight: '0px' }}>
          <TerminalErrorBoundary title="会话视图异常退出">
            <AcpSessionView conversationId={viewing.id} />
          </TerminalErrorBoundary>
        </Box>
        {snack}
      </Box>
    );
  }

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <PanelToolbar
        left={<Typography variant="caption" color="text.secondary">{`会话 ${sorted.length}`}</Typography>}
        right={(
          <>
            {/* 子会话由 bot 自动创建（D12）：手动入口恒禁用；工作空间会话无子会话概念。 */}
            <Tooltip title={workspaceScope ? '工作空间会话无子会话概念' : '子会话由机器人收消息自动创建，不支持手动新增'}>
              <span>
                <Button size="small" disabled>新增子会话</Button>
              </span>
            </Tooltip>
            <RefreshIconButton onClick={refresh} fetching={isFetching} ariaLabel="刷新会话列表" />
          </>
        )}
      />
      <Box sx={{ flex: 1, overflow: 'auto', px: '12px', pt: '12px', pb: '4px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
        {error != null
          ? (
              <Alert severity="error" action={<Button color="inherit" size="small" onClick={refresh}>重试</Button>}>
                {`会话列表加载失败：${error.message}`}
              </Alert>
            )
          : isPending
            ? (
                <Box sx={{ display: 'flex', justifyContent: 'center', py: '24px' }}>
                  <CircularProgress size={20} />
                </Box>
              )
            : sorted.length === 0
              ? <EmptyState workspace={workspaceScope} />
              : sorted.map(conversation => (
                  <ConversationCard
                    key={conversation.id}
                    conversation={conversation}
                    running={conversation.claudeSessionId !== '' && activeSessionIds.has(conversation.claudeSessionId)}
                    projectId={projectId}
                    issueId={issueId}
                    containerId={workspaceScope ? containerId : undefined}
                    onLiveView={() => handleLiveView(conversation)}
                  />
                ))}
      </Box>
      {snack}
    </Box>
  );
}

/** 空态：任务 = 无任何会话（主会话由任务页编排创建后出现）；工作空间 = 选中会话未查到。 */
function EmptyState({ workspace }: { workspace: boolean }) {
  return (
    <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: '8px', p: '16px', textAlign: 'center' }}>
      <ForumOutlinedIcon sx={{ fontSize: '48px', color: 'text.secondary' }} />
      <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>{workspace ? '会话不存在或已删除' : '当前任务暂无会话'}</Typography>
      <Typography variant="body2" color="text.secondary">
        {workspace ? '请在左侧重新选择会话' : '在工作台启动终端或 ACP 会话后出现；子会话由 bot 收消息自动创建'}
      </Typography>
    </Box>
  );
}
