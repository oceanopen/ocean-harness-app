import type { ImBotModel } from '@src/services';
import type { ImBotDrawerState } from './ImBotDrawer';
import AddOutlinedIcon from '@mui/icons-material/AddOutlined';
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
  Switch,
  Tooltip,
  Typography,
} from '@mui/material';
import { useDeleteImBot, useImBots, useRestartImBot, useUpdateImBot } from '@src/state/imBots';
import { useState } from 'react';
import ImBotDrawer from './ImBotDrawer';

// IM 机器人设置分区：bot 卡片列表（连接状态徽标/启停开关/重启连接/编辑/删除确认）+ 编辑抽屉。
// 文案约定：仅页面标题（菜单 label，见 routes.ts SECTION_MENUS）走 i18n，内容为中文直出、
// 保留多语言口子（后续需要时再收口到 settings:imBots.*）。手动刷新模型（项目既定决策）：
// 操作后由 mutation invalidate 自动刷新，另有手动刷新按钮；页面切走即卸载。

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

/** 单张 bot 卡片：名称 + 状态徽标 + 启停开关 + 操作（重启/编辑/删除）。 */
function ImBotCard(props: {
  bot: ImBotModel;
  onEdit: () => void;
  onDeleteAsk: () => void;
}) {
  const { bot, onEdit, onDeleteAsk } = props;
  const updateMutation = useUpdateImBot();
  const restartMutation = useRestartImBot();
  const chip = stateChip(bot.connState);

  // 启停开关直接以卡片现值全量回写（secret 留空沿用原值语义，仅 enabled 变化）。
  const toggleEnabled = (enabled: boolean) => {
    updateMutation.mutate({
      id: bot.id,
      name: bot.name,
      botId: bot.botId,
      workspaceDir: bot.workspaceDir,
      model: bot.model,
      systemPrompt: bot.systemPrompt,
      allowedTools: bot.allowedTools,
      accessPolicy: bot.accessPolicy,
      enabled,
    });
  };

  return (
    <Box sx={{ borderRadius: 2, border: 1, borderColor: 'divider', overflow: 'hidden' }}>
      <Box sx={{ px: 2, py: 1.5, display: 'flex', alignItems: 'center', gap: 1.5 }}>
        <Typography sx={{ fontSize: 14, fontWeight: 600 }}>{bot.name}</Typography>
        <Tooltip title={bot.lastError || undefined}>
          <Chip size="small" label={chip.label} color={chip.color} variant="outlined" />
        </Tooltip>
        <Chip size="small" label={bot.channel} variant="outlined" sx={{ fontSize: 12 }} />
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
          <Switch
            size="small"
            checked={bot.enabled}
            disabled={updateMutation.isPending}
            onChange={e => toggleEnabled(e.target.checked)}
          />
        </Box>
      </Box>
      <Box sx={{ px: 2, pb: 1.5, display: 'flex', flexDirection: 'column', gap: 0.5 }}>
        <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>
          工作目录：
          {bot.workspaceDir}
        </Typography>
        {bot.model && (
          <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>
            模型：
            {bot.model}
          </Typography>
        )}
        <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>
          {bot.accessPolicy.mode === 'open'
            ? '开放模式（所有人可用）'
            : `白名单 ${bot.accessPolicy.allowUsers.length} 人`}
        </Typography>
      </Box>
    </Box>
  );
}

/** IM 机器人设置分区页。 */
function ImBotsPage() {
  const { data: bots = [], isLoading, refetch, isFetching } = useImBots();
  const deleteMutation = useDeleteImBot();
  const [drawer, setDrawer] = useState<ImBotDrawerState>({ open: false, bot: null });
  const [deleteTarget, setDeleteTarget] = useState<ImBotModel | null>(null);

  const handleDeleteConfirm = () => {
    if (!deleteTarget) {
      return;
    }
    deleteMutation.mutate({ id: deleteTarget.id }, { onSettled: () => setDeleteTarget(null) });
  };

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Box sx={{ p: 3, flex: 1, overflow: 'auto', display: 'flex', flexDirection: 'column', gap: 2 }}>
        <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>
          接入企业微信智能机器人：消息驱动本机 Claude Code 会话，流式回复，支持引用与附件。
        </Typography>

        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
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
          <Box sx={{ ml: 'auto' }}>
            <Button
              variant="contained"
              onClick={() => setDrawer({ open: true, bot: null })}
              sx={{ '& svg': { fontSize: 18 } }}
              startIcon={<AddOutlinedIcon />}
            >
              新建机器人
            </Button>
          </Box>
        </Box>

        {isLoading && <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>…</Typography>}
        {!isLoading && bots.length === 0 && (
          <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>
            还没有机器人，点击右上角「新建机器人」创建。
          </Typography>
        )}
        {bots.map(bot => (
          <ImBotCard
            key={bot.id}
            bot={bot}
            onEdit={() => setDrawer({ open: true, bot })}
            onDeleteAsk={() => setDeleteTarget(bot)}
          />
        ))}
      </Box>

      {/* 编辑抽屉：key 随编辑对象变化重挂载（draft 首帧即终值，无副作用重置） */}
      <ImBotDrawer
        key={drawer.bot?.id ?? 'create'}
        state={drawer}
        onClose={() => setDrawer(prev => ({ ...prev, open: false }))}
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
