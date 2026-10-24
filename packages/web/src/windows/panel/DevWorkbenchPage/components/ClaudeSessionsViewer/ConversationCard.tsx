import type { ConversationModel, ImBotModel } from '@src/services';
import { ExpandLessOutlined as ExpandLessOutlinedIcon, ExpandMoreOutlined as ExpandMoreOutlinedIcon } from '@mui/icons-material';
import { Box, Button, Chip, IconButton, Tooltip, Typography } from '@mui/material';
import { formatRelativeTime } from '@src/shared/time';
import { useImBotConversations, useImBots } from '@src/state/imBots';
import { useProjects } from '@src/state/projects';
import { issueWorkspacePath } from '@src/windows/panel/DevWorkbenchPage/issueWorkspacePath';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import ConversationHistory from './ConversationHistory';

interface ConversationCardProps {
  conversation: ConversationModel;
  // 运行态（会话锚 ∈ claude 活跃快照），由列表级统一判定传入。
  running: boolean;
  projectId: string;
  issueId: string;
  onLiveView: () => void;
}

/**
 * 会话卡片（O16）：标题 + 子会话/mode/锚状态徽标 + 实时查看 + 关联 bot 行 + 可展开本地历史。
 * 纯展示组件：live 点击上抛（viewer 分流 acp 内嵌 / terminal 提示），历史展开态卡片自持。
 */
function ConversationCard({ conversation, running, projectId, issueId, onLiveView }: ConversationCardProps) {
  const { t } = useTranslation();
  const [historyOpen, setHistoryOpen] = useState(false);
  const anchored = conversation.claudeSessionId !== '';
  // 历史直读 cwd = 任务工作区目录（{项目目录}/{taskId}）；项目缺失（旧数据）降级「暂无本地历史」。
  const { data: projects = [] } = useProjects();
  const projectDir = projects.find(p => p.id === projectId)?.dir ?? null;
  const cwd = projectDir != null ? issueWorkspacePath(projectDir, issueId) : null;

  return (
    <Box sx={{ borderRadius: '8px', border: '1px solid', borderColor: 'divider', bgcolor: 'background.paper', overflow: 'hidden', flexShrink: 0 }}>
      <Box sx={{ px: '12px', py: '10px', display: 'flex', alignItems: 'center', gap: '8px' }}>
        {conversation.parentId !== '' && (
          <Tooltip title="子会话由 bot 收消息自动创建，仅本工具可见">
            <Chip size="small" label="子会话" variant="outlined" sx={{ fontSize: '12px', flexShrink: 0 }} />
          </Tooltip>
        )}
        <Typography noWrap title={conversation.title} sx={{ fontSize: '13px', fontWeight: 600, flex: 1, minWidth: '0px' }}>
          {conversation.title}
        </Typography>
        <Chip size="small" label={conversation.mode === 'acp' ? 'ACP' : '终端'} variant="outlined" sx={{ fontSize: '12px', flexShrink: 0 }} />
        {running
          ? <Chip size="small" label="运行中" color="success" variant="outlined" sx={{ fontSize: '12px', flexShrink: 0 }} />
          : !anchored && <Chip size="small" label="未锚定" variant="outlined" sx={{ fontSize: '12px', color: 'text.secondary', flexShrink: 0 }} />}
        <Button size="small" onClick={onLiveView}>实时查看</Button>
        <Tooltip title={anchored ? '本地历史消息' : '未锚定（无本地会话记录）'}>
          {/* 禁用钮不吞 Tooltip，外层 span 承载 */}
          <span>
            <IconButton
              size="small"
              aria-label={historyOpen ? '收起历史消息' : '展开历史消息'}
              aria-expanded={historyOpen}
              disabled={!anchored}
              onClick={() => setHistoryOpen(v => !v)}
              sx={{ color: 'text.secondary' }}
            >
              {historyOpen ? <ExpandLessOutlinedIcon sx={{ fontSize: '18px' }} /> : <ExpandMoreOutlinedIcon sx={{ fontSize: '18px' }} />}
            </IconButton>
          </span>
        </Tooltip>
      </Box>
      <Box sx={{ px: '12px', pb: '10px', display: 'flex', alignItems: 'center', gap: '16px' }}>
        <ConversationBotNames conversationId={conversation.id} projectId={projectId} />
        <Typography sx={{ fontSize: '12px', color: 'text.disabled', flexShrink: 0 }}>
          {formatRelativeTime(new Date(conversation.updatedAt).getTime(), t, 'tracker')}
        </Typography>
      </Box>
      {historyOpen && anchored && <ConversationHistory claudeSessionId={conversation.claudeSessionId} cwd={cwd} />}
    </Box>
  );
}

/** 关联 bot 行：候选 = 项目档且归属本项目；无候选直接「无关联 bot」。 */
function ConversationBotNames({ conversationId, projectId }: { conversationId: string; projectId: string }) {
  const { data: bots = [] } = useImBots();
  const candidates = bots.filter(bot => bot.conversationType === 'project' && bot.projectId === projectId);
  if (candidates.length === 0) {
    return (
      <Typography noWrap sx={{ fontSize: '12px', color: 'text.secondary', flex: 1, minWidth: '0px' }}>
        机器人：无关联 bot
      </Typography>
    );
  }
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: '4px', flex: 1, minWidth: '0px', overflow: 'hidden' }}>
      <Typography sx={{ fontSize: '12px', color: 'text.secondary', flexShrink: 0 }}>机器人：</Typography>
      {candidates.map(bot => <BotLinkChip key={bot.id} bot={bot} conversationId={conversationId} />)}
    </Box>
  );
}

/** 单 bot 连线徽标：该 bot 的 IM 会话投影含本会话 id 才渲染（per-bot 查询共享缓存）。 */
function BotLinkChip({ bot, conversationId }: { bot: ImBotModel; conversationId: string }) {
  const { data: conversations = [] } = useImBotConversations(bot.id);
  if (!conversations.some(row => row.conversationId === conversationId)) {
    return null;
  }
  return <Chip size="small" label={bot.name} variant="outlined" sx={{ fontSize: '12px', flexShrink: 0 }} />;
}

export default ConversationCard;
