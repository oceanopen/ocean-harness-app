import type { ImBotConversationModel, ImBotModel } from '@src/services';
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
  // 在场 = 工作空间语境：IM 会话投影命中该会话 id 时头部标注「已关联当前会话」。
  linkedConversationId?: string;
}

/** 会话明细只读行：身份行（类型 · 最近活跃 · key 短码）；路由归属由消息侧自动维护，不在此操作。 */
function ConversationRow({ conversation }: { conversation: ImBotConversationModel }) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', px: '12px', py: '6px' }}>
      <Typography sx={{ fontSize: '12px', color: 'text.secondary', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        {`${chatTypeLabel(conversation.chatType)} · ${conversationActiveText(conversation.lastMessageAt)} · ${conversationShortCode(conversation.conversationKey)}`}
      </Typography>
    </Box>
  );
}

/**
 * ImBotBotCard：bot 聚合卡（只读）——名称 + 启用态 + 连接状态徽标 + 会话计数（工作空间
 * 语境标注「已关联当前会话」），展开列会话明细。会话数据经 useImBotConversations(bot.id)
 * 按 bot 独立缓存；归属/启用变更统一走「IM 机器人」页编辑抽屉。
 */
export default function ImBotBotCard({ bot, linkedConversationId }: ImBotBotCardProps) {
  const [expanded, setExpanded] = useState(true);
  const { data: conversations = [], isLoading, error, refetch } = useImBotConversations(bot.id);
  const chip = imBotStateChip(bot.connState);
  const linkedToCurrent = linkedConversationId != null
    && conversations.some(row => row.conversationId === linkedConversationId);

  return (
    <Box sx={{ borderRadius: '8px', border: '1px solid', borderColor: 'divider', overflow: 'hidden', bgcolor: 'background.paper' }}>
      {/* 聚合头：点按整行展开/收起；连接徽标 tooltip 显 lastError 或状态说明（同 ImBotsPage 卡片） */}
      <Box
        onClick={() => setExpanded(v => !v)}
        sx={{ display: 'flex', alignItems: 'center', gap: '8px', px: '12px', py: '10px', cursor: 'pointer' }}
      >
        <Typography sx={{ fontSize: '13px', fontWeight: 600 }}>{bot.name}</Typography>
        <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>{bot.enabled ? '已启用' : '已停用'}</Typography>
        <Tooltip title={bot.lastError || chip.tooltip}>
          <Chip size="small" label={chip.label} color={chip.color} variant="outlined" />
        </Tooltip>
        {isLoading
          ? <CircularProgress size={14} sx={{ color: 'text.secondary' }} />
          : (
              <Chip
                size="small"
                label={conversations.length > 0 ? `${conversations.length} 个会话` : '暂无会话'}
                variant="outlined"
                sx={{ fontSize: '12px' }}
              />
            )}
        {linkedToCurrent && (
          <Tooltip title="该机器人的 IM 会话已绑定当前选中的工作空间会话">
            <Chip size="small" label="已关联当前会话" color="primary" variant="outlined" sx={{ fontSize: '12px' }} />
          </Tooltip>
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
        <Box sx={{ borderTop: '1px solid', borderColor: 'divider', py: '4px', display: 'flex', flexDirection: 'column' }}>
          {error
            ? (
                <Box sx={{ px: '12px', py: '8px' }}>
                  <Alert severity="error" action={<Button color="inherit" size="small" onClick={() => refetch()}>重试</Button>}>
                    会话列表加载失败：{error.message}
                  </Alert>
                </Box>
              )
            : isLoading
              ? (
                  <Box sx={{ display: 'flex', justifyContent: 'center', py: '12px' }}>
                    <CircularProgress size={18} />
                  </Box>
                )
              : conversations.length === 0
                ? (
                    <Typography sx={{ fontSize: '12px', color: 'text.secondary', px: '12px', py: '8px' }}>
                      暂无会话——在 IM 中向机器人发消息后出现
                    </Typography>
                  )
                : conversations.map(conversation => (
                    <ConversationRow key={conversation.conversationKey} conversation={conversation} />
                  ))}
        </Box>
      )}
    </Box>
  );
}
