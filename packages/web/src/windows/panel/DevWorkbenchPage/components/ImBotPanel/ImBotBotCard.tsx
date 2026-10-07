import type { ImBotBindIssueRequest, ImBotConversationModel, ImBotModel, ProjectIssueResponseData } from '@src/services';
import ExpandLessOutlinedIcon from '@mui/icons-material/ExpandLessOutlined';
import ExpandMoreOutlinedIcon from '@mui/icons-material/ExpandMoreOutlined';
import {
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  IconButton,
  Tooltip,
  Typography,
} from '@mui/material';
import { useImBotConversations } from '@src/state/imBots';
import { imBotStateChip } from '@src/windows/panel/ImBotsPage/imBotStateChip';
import { useState } from 'react';
import { chatTypeLabel, conversationActiveText, conversationShortCode } from './format';

interface ImBotBotCardProps {
  bot: ImBotModel;
  /// 当前工作台选中任务（绑定目标 + 聚合 chip 判定基准）。
  issue: ProjectIssueResponseData;
  /// 面板级绑定 mutation 在飞请求（isPending 时的 variables；null = 空闲）。写操作统一在
  /// 面板层确认弹窗后执行（单飞行中操作），本卡按 botId 自筛后驱动行级按钮 loading/disabled。
  bindPending: ImBotBindIssueRequest | null;
  /// 会话行操作上抛（关联/切换/解绑统一由面板层确认弹窗承接）。
  onConversationAction: (bot: ImBotModel, conversation: ImBotConversationModel) => void;
}

/** 会话明细行：身份行（类型 · 最近活跃 · key 短码）+ 绑定态行 + 操作按钮。 */
function ConversationRow(props: {
  conversation: ImBotConversationModel;
  issue: ProjectIssueResponseData;
  bindPending: ImBotBindIssueRequest | null;
  onAction: (conversation: ImBotConversationModel) => void;
}) {
  const { conversation, issue, bindPending, onAction } = props;
  const boundCurrent = conversation.boundIssueId === issue.id;
  const boundOther = conversation.boundIssueId !== '' && !boundCurrent;
  // 本 bot 有在飞操作时禁用其余行（单飞行中操作），命中行以 loading 呈现。
  const rowLoading = bindPending != null && bindPending.conversationKey === conversation.conversationKey;
  const rowDisabled = bindPending != null && !rowLoading;

  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 1.5, py: 0.75 }}>
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <Typography sx={{ fontSize: 12, color: 'text.secondary', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {`${chatTypeLabel(conversation.chatType)} · ${conversationActiveText(conversation.lastMessageAt)} · ${conversationShortCode(conversation.conversationKey)}`}
        </Typography>
        {boundCurrent && (
          <Typography sx={{ fontSize: 12, color: 'success.main' }}>已关联当前任务</Typography>
        )}
        {boundOther && (
          <Typography sx={{ fontSize: 12, color: 'text.secondary', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {`已关联：${conversation.boundIssueName || '（任务已不存在）'}`}
          </Typography>
        )}
      </Box>
      <Button
        size="small"
        color={boundCurrent ? 'inherit' : 'primary'}
        disabled={rowDisabled}
        loading={rowLoading}
        onClick={() => onAction(conversation)}
        sx={{ flexShrink: 0 }}
      >
        {boundCurrent ? '解除关联' : boundOther ? '切换为当前任务' : '关联当前任务'}
      </Button>
    </Box>
  );
}

/**
 * ImBotBotCard：bot 聚合卡——名称 + 连接状态徽标 + 「N 个会话已关联」聚合 chip，展开列
 * 会话明细（私聊/群聊 · 最近活跃 · key 短码 + 所绑任务 + 行级绑定操作）。会话数据经
 * useImBotConversations(bot.id) 按 bot 独立缓存；绑定/解绑/切换均上抛面板层确认弹窗执行。
 */
export default function ImBotBotCard({ bot, issue, bindPending, onConversationAction }: ImBotBotCardProps) {
  const [expanded, setExpanded] = useState(true);
  const { data: conversations = [], isLoading, error, refetch } = useImBotConversations(bot.id);
  const boundCount = conversations.filter(c => c.boundIssueId === issue.id).length;
  const chip = imBotStateChip(bot.connState);
  // 本卡只消费属于本 bot 的在飞请求（面板级单飞行中操作的全局下传值）。
  const cardBindPending = bindPending != null && bindPending.botId === bot.id ? bindPending : null;

  return (
    <Box sx={{ borderRadius: 2, border: 1, borderColor: 'divider', overflow: 'hidden', bgcolor: 'background.paper' }}>
      {/* 聚合头：点按整行展开/收起；连接徽标 tooltip 显 lastError（同 ImBotsPage 卡片） */}
      <Box
        onClick={() => setExpanded(v => !v)}
        sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 1.5, py: 1.25, cursor: 'pointer' }}
      >
        <Typography sx={{ fontSize: 13, fontWeight: 600 }}>{bot.name}</Typography>
        <Tooltip title={bot.lastError || undefined}>
          <Chip size="small" label={chip.label} color={chip.color} variant="outlined" />
        </Tooltip>
        {isLoading
          ? <CircularProgress size={14} sx={{ color: 'text.secondary' }} />
          : (
              <Chip
                size="small"
                label={boundCount > 0 ? `${boundCount} 个会话已关联` : '未关联'}
                color={boundCount > 0 ? 'success' : 'default'}
                variant="outlined"
                sx={{ fontSize: 12 }}
              />
            )}
        {/* 展开箭头带自身 onClick（键盘可激活；stopPropagation 防与父行点击冒泡双 toggle 抵消） */}
        <IconButton
          size="small"
          aria-label={expanded ? '收起会话明细' : '展开会话明细'}
          aria-expanded={expanded}
          onClick={(e) => {
            e.stopPropagation();
            setExpanded(v => !v);
          }}
          sx={{ ml: 'auto', color: 'text.secondary' }}
        >
          {expanded ? <ExpandLessOutlinedIcon fontSize="small" /> : <ExpandMoreOutlinedIcon fontSize="small" />}
        </IconButton>
      </Box>

      {expanded && (
        <Box sx={{ borderTop: 1, borderColor: 'divider', py: 0.5, display: 'flex', flexDirection: 'column' }}>
          {error
            ? (
                <Box sx={{ px: 1.5, py: 1 }}>
                  <Alert severity="error" action={<Button color="inherit" size="small" onClick={() => refetch()}>重试</Button>}>
                    会话列表加载失败：{error.message}
                  </Alert>
                </Box>
              )
            : isLoading
              ? (
                  <Box sx={{ display: 'flex', justifyContent: 'center', py: 1.5 }}>
                    <CircularProgress size={18} />
                  </Box>
                )
              : conversations.length === 0
                ? (
                    <Typography sx={{ fontSize: 12, color: 'text.secondary', px: 1.5, py: 1 }}>
                      暂无会话——在 IM 中向机器人发消息后出现
                    </Typography>
                  )
                : conversations.map(conversation => (
                    <ConversationRow
                      key={conversation.conversationKey}
                      conversation={conversation}
                      issue={issue}
                      bindPending={cardBindPending}
                      onAction={conv => onConversationAction(bot, conv)}
                    />
                  ))}
        </Box>
      )}
    </Box>
  );
}
