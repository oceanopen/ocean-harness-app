import type { ImBotModel } from '@src/services';
import type { ImBotDrawerState } from './ImBotDrawer';
import AddOutlinedIcon from '@mui/icons-material/AddOutlined';
import ForumOutlinedIcon from '@mui/icons-material/ForumOutlined';
import QrCode2OutlinedIcon from '@mui/icons-material/QrCode2Outlined';
import RefreshOutlinedIcon from '@mui/icons-material/RefreshOutlined';
import {
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  IconButton,
  Tooltip,
  Typography,
} from '@mui/material';
import { useDeleteImBot, useImBots, useRestartImBot } from '@src/state/imBots';
import { useState } from 'react';
import ImBotDrawer from './ImBotDrawer';
import ImBotProvisionDrawer from './ImBotProvisionDrawer';

// IM 机器人设置分区：左栏渠道小卡片列表（本期仅企微，结构留飞书扩展）+ 右栏当前渠道机器人
// 满行卡片列表，头部「扫码接入 / 手动接入」双入口（dsh-im 同款交互）。编辑/新增均右侧 Drawer。
// 手动接入路径当前会话列表无 bot 的提示：凭据从企微后台复制。文案约定：仅菜单标题走 i18n。

/** 渠道元数据（本期仅企微；飞书接入时追加一项即可）。 */
interface ChannelMeta {
  key: string;
  label: string;
}

const CHANNELS: readonly ChannelMeta[] = [
  { key: 'wecom', label: '企业微信' },
];

/** 连接状态 → 徽标文案与色调。 */
function stateChip(state: string): { label: string; color: 'success' | 'warning' | 'error' | 'default' } {
  switch (state) {
    case 'connected':
      return { label: '已连接', color: 'success' };
    case 'connecting':
      return { label: '连接中', color: 'warning' };
    case 'disconnected':
      return { label: '已断开', color: 'error' };
    default:
      return { label: '未运行', color: 'default' };
  }
}

/** 左栏渠道小卡片：图标 + 名称 + 在线 bot 数徽标，选中态高亮。 */
function ChannelCard(props: { channel: ChannelMeta; total: number; online: number; selected: boolean; onSelect: () => void }) {
  const { channel, total, online, selected, onSelect } = props;
  return (
    <Box
      onClick={onSelect}
      sx={{
        'borderRadius': 2,
        'border': 1,
        'borderColor': selected ? 'primary.main' : 'divider',
        'bgcolor': selected ? 'action.selected' : 'background.paper',
        'px': 1.5,
        'py': 1.25,
        'display': 'flex',
        'alignItems': 'center',
        'gap': 1.25,
        'cursor': 'pointer',
        '&:hover': { bgcolor: 'action.hover' },
      }}
    >
      <ForumOutlinedIcon sx={{ '& svg': { fontSize: 20 }, 'fontSize': 20, 'color': selected ? 'primary.main' : 'text.secondary' }} />
      <Typography sx={{ fontSize: 13, fontWeight: selected ? 600 : 400 }}>{channel.label}</Typography>
      <Chip
        size="small"
        label={`${online}/${total}`}
        variant="outlined"
        sx={{ ml: 'auto', fontSize: 12, minWidth: 44 }}
      />
    </Box>
  );
}

/** 右栏机器人满行卡片：名称 + 状态徽标 + 操作（重启/编辑/删除），下两行工作目录/访问策略。 */
function ImBotCard(props: {
  bot: ImBotModel;
  onEdit: () => void;
  onDeleteAsk: () => void;
}) {
  const { bot, onEdit, onDeleteAsk } = props;
  const restartMutation = useRestartImBot();
  const chip = stateChip(bot.connState);

  return (
    <Box sx={{ borderRadius: 2, border: 1, borderColor: 'divider', overflow: 'hidden', bgcolor: 'background.paper' }}>
      <Box sx={{ px: 2, py: 1.5, display: 'flex', alignItems: 'center', gap: 1.5 }}>
        <Typography sx={{ fontSize: 14, fontWeight: 600 }}>{bot.name}</Typography>
        <Tooltip title={bot.lastError || undefined}>
          <Chip size="small" label={chip.label} color={chip.color} variant="outlined" />
        </Tooltip>
        {!bot.workspaceDir && (
          <Tooltip title="尚未配置工作目录，对话时将收到补配提醒">
            <Chip size="small" label="待配置" color="warning" variant="outlined" sx={{ fontSize: 12 }} />
          </Tooltip>
        )}
        <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: 1 }}>
          <Tooltip title="重启连接">
            {/* 停用不提供重启（重启 = 以落库配置拉起，停用态应先打开开关）；pending 禁用防双击并发重启 */}
            <IconButton
              size="small"
              disabled={!bot.enabled || restartMutation.isPending}
              onClick={() => restartMutation.mutate({ id: bot.id })}
              sx={{ '& svg': { fontSize: 18 } }}
            >
              <RefreshOutlinedIcon />
            </IconButton>
          </Tooltip>
          <Button size="small" onClick={onEdit}>编辑</Button>
          <Button size="small" color="error" onClick={onDeleteAsk}>删除</Button>
        </Box>
      </Box>
      <Box sx={{ px: 2, pb: 1.5, display: 'flex', gap: 3, alignItems: 'center' }}>
        <Typography sx={{ fontSize: 12, color: 'text.secondary', flex: 2, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {bot.workspaceDir ? `工作目录：${bot.workspaceDir}` : '工作目录：未配置'}
        </Typography>
        <Typography sx={{ fontSize: 12, color: 'text.secondary', flex: 1 }}>
          {bot.accessPolicy.mode === 'open'
            ? '开放模式（所有人可用）'
            : `白名单 ${bot.accessPolicy.allowUsers.length} 人`}
        </Typography>
        <Typography sx={{ fontSize: 12, color: 'text.secondary', flexShrink: 0 }}>
          {bot.enabled ? '已启用' : '已停用'}
        </Typography>
      </Box>
    </Box>
  );
}

/** IM 机器人设置分区页（两栏：渠道卡片 + 机器人卡片列表）。 */
function ImBotsPage() {
  const { data: bots = [], isLoading, refetch, isFetching } = useImBots();
  const deleteMutation = useDeleteImBot();
  const [channelKey, setChannelKey] = useState(CHANNELS[0].key);
  const [drawer, setDrawer] = useState<ImBotDrawerState>({ open: false, bot: null });
  const [provisionOpen, setProvisionOpen] = useState(false);
  const [provisionKey, setProvisionKey] = useState(0);
  const [deleteTarget, setDeleteTarget] = useState<ImBotModel | null>(null);

  const channel = CHANNELS.find(c => c.key === channelKey) ?? CHANNELS[0];
  // 后端本期只有 wecom 渠道数据；按渠道过滤的结构位（飞书加入后自然生效）。
  const channelBots = bots.filter(bot => bot.channel === channel.key);

  const handleDeleteConfirm = () => {
    if (!deleteTarget) {
      return;
    }
    deleteMutation.mutate({ id: deleteTarget.id }, { onSettled: () => setDeleteTarget(null) });
  };

  return (
    <Box sx={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
      {/* 左栏：渠道列表（小卡片） */}
      <Box
        sx={{
          width: 220,
          flexShrink: 0,
          borderRight: 1,
          borderColor: 'divider',
          bgcolor: 'background.paper',
          p: 1.5,
          display: 'flex',
          flexDirection: 'column',
          gap: 1,
          overflow: 'auto',
        }}
      >
        <Typography sx={{ fontSize: 12, color: 'text.secondary', px: 1 }}>渠道</Typography>
        {CHANNELS.map(ch => (
          <ChannelCard
            key={ch.key}
            channel={ch}
            total={bots.filter(bot => bot.channel === ch.key).length}
            online={bots.filter(bot => bot.channel === ch.key && bot.connState === 'connected').length}
            selected={ch.key === channelKey}
            onSelect={() => setChannelKey(ch.key)}
          />
        ))}
      </Box>

      {/* 右栏：当前渠道机器人列表（满行卡片）+ 双接入入口 */}
      <Box sx={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <Box sx={{ px: 3, py: 2, display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography sx={{ fontSize: 15, fontWeight: 600 }}>{channel.label}</Typography>
          <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: 1 }}>
            <Tooltip title="刷新">
              <IconButton
                size="small"
                onClick={() => refetch()}
                disabled={isFetching}
                sx={{ '& svg': { fontSize: 20 } }}
              >
                <RefreshOutlinedIcon />
              </IconButton>
            </Tooltip>
            <Button
              variant="outlined"
              startIcon={<QrCode2OutlinedIcon sx={{ '& svg': { fontSize: 18 } }} />}
              onClick={() => setProvisionOpen(true)}
            >
              扫码接入
            </Button>
            <Button
              variant="contained"
              startIcon={<AddOutlinedIcon sx={{ '& svg': { fontSize: 18 } }} />}
              onClick={() => setDrawer({ open: true, bot: null })}
            >
              手动接入
            </Button>
          </Box>
        </Box>

        <Box sx={{ px: 3, pb: 3, flex: 1, overflow: 'auto', display: 'flex', flexDirection: 'column', gap: 1.5 }}>
          <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>
            接入企业微信智能机器人：消息驱动本机 Claude Code 会话，流式回复，支持引用与附件。
          </Typography>

          {isLoading && <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>…</Typography>}
          {!isLoading && channelBots.length === 0 && (
            <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>
              还没有机器人：推荐「扫码接入」（企业微信 App 授权自动创建），或「手动接入」填写后台凭据。
            </Typography>
          )}
          {channelBots.map(bot => (
            <ImBotCard
              key={bot.id}
              bot={bot}
              onEdit={() => setDrawer({ open: true, bot })}
              onDeleteAsk={() => setDeleteTarget(bot)}
            />
          ))}
        </Box>
      </Box>

      {/* 编辑/手动接入抽屉：key 随编辑对象变化重挂载（draft 首帧即终值） */}
      <ImBotDrawer
        key={drawer.bot?.id ?? 'create'}
        state={drawer}
        onClose={() => setDrawer(prev => ({ ...prev, open: false }))}
      />

      {/* 扫码接入抽屉：key 重挂载即「重新生成二维码」（卸载清定时器，新 begin 自动取消旧会话） */}
      <ImBotProvisionDrawer
        key={provisionKey}
        open={provisionOpen}
        onClose={() => setProvisionOpen(false)}
        onRegenerate={() => setProvisionKey(k => k + 1)}
      />

      <Dialog open={deleteTarget != null} onClose={() => setDeleteTarget(null)}>
        <DialogTitle>删除机器人</DialogTitle>
        <DialogContent>
          <DialogContentText sx={{ fontSize: 13 }}>
            {`将删除「${deleteTarget?.name ?? ''}」及其全部会话映射，且不可恢复。确认删除？`}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteTarget(null)} color="inherit">取消</Button>
          <Button onClick={handleDeleteConfirm} color="error" variant="contained">删除</Button>
        </DialogActions>
      </Dialog>

    </Box>
  );
}

export default ImBotsPage;
