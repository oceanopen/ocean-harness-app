import type { ImBotConversationModel, ImBotModel, ProjectIssueResponseData } from '@src/services';
import AddIcon from '@mui/icons-material/Add';
import SmartToyOutlinedIcon from '@mui/icons-material/SmartToyOutlined';
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  IconButton,
  ListItemText,
  MenuItem,
  MenuList,
  Popover,
  Tooltip,
  Typography,
} from '@mui/material';
import { imBotsKeys, useBindImBotIssue, useImBots, useUpdateImBot } from '@src/state/imBots';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import PanelToolbar from '../PanelToolbar';
import RefreshIconButton from '../RefreshIconButton';
import { chatTypeLabel, conversationShortCode } from './format';
import ImBotBotCard from './ImBotBotCard';

/** bot 关联工作空间短名（名缺失回落 #id，与 ImBotsPage 卡片同规则）。 */
function workspaceShortLabel(bot: ImBotModel): string {
  return bot.workspaceName || `#${bot.workspaceId}`;
}

/** 确认弹窗待确认动作：面板内一切写操作（会话绑定三态 + 补绑迁移）统一经此弹窗二次确认。 */
type PendingAction
  = | { kind: 'conversation'; bot: ImBotModel; conversation: ImBotConversationModel }
    | { kind: 'addBot'; bot: ImBotModel };

/** 确认弹窗文案派生：会话操作按当前绑定分关联/切换/解绑三态；补绑区分迁移与首次加入。 */
function confirmSpec(action: PendingAction, issue: ProjectIssueResponseData): { title: string; text: string; confirm: string } {
  if (action.kind === 'addBot') {
    const { bot } = action;
    if (bot.workspaceId > 0) {
      return {
        title: '迁移机器人',
        text: `将机器人「${bot.name}」从工作空间「${workspaceShortLabel(bot)}」迁移到当前工作空间？迁移后其全部会话跟随到新工作空间，连接将自动重载。`,
        confirm: '迁移',
      };
    }
    return {
      title: '加入工作空间',
      text: `将机器人「${bot.name}」加入当前工作空间？加入后即可在本工作空间的任务上使用。`,
      confirm: '加入',
    };
  }
  const { conversation } = action;
  const desc = `${chatTypeLabel(conversation.chatType)} ${conversationShortCode(conversation.conversationKey)}`;
  if (conversation.boundIssueId === issue.id) {
    return {
      title: '解除任务关联',
      text: `解除会话「${desc}」与任务「${issue.name}」的关联？解除后该会话的 IM 消息将回到未绑定引导。`,
      confirm: '解除关联',
    };
  }
  if (conversation.boundIssueId !== '') {
    return {
      title: '切换任务关联',
      text: `会话「${desc}」当前关联任务「${conversation.boundIssueName || '（任务已不存在）'}」，切换为「${issue.name}」？切换后该会话的 IM 消息将驱动新任务。`,
      confirm: '切换',
    };
  }
  return {
    title: '关联任务',
    text: `将会话「${desc}」关联到当前任务「${issue.name}」？关联后该会话的 IM 消息将驱动本任务。`,
    confirm: '关联',
  };
}

interface ImBotPanelProps {
  /// 当前选中任务（挂载方经 toolRegistry ctx 保证非空；workspaceId 决定 bot 过滤域）。
  issue: ProjectIssueResponseData;
}

/**
 * ImBotPanel：开发工作台工具面板区的「IM 机器人」tab 内容（T3.2，经 toolRegistry 挂载）。
 * 展示当前任务所属工作空间的 bot 聚合卡（连接徽标 + 「N 个会话已关联」chip + 会话明细），
 * 桌面侧闭环入口：会话级「关联当前任务 / 切换 / 解除关联」与「+」补绑（把其他工作空间/
 * 未配置的 bot 迁入本工作空间）。写操作统一面板级确认弹窗 + 单飞行中操作（在飞请求经
 * bindPending 下传 bot 卡做行级 loading）。文案中文直出（对齐 ImBotsPage 约定）。
 *
 * 数据：useImBots 共享列表缓存按 issue.workspaceId 过滤；会话明细 per-bot query
 * （useImBotConversations）。IM 侧卡片绑定（T2.2）与 MCP 写库后端均不推事件，实时性靠
 * 头部刷新按钮手动 invalidate 整域（子任务面板同款口径，T3.1 定稿）。
 */
export default function ImBotPanel({ issue }: ImBotPanelProps) {
  const qc = useQueryClient();
  const { data: bots = [], isLoading, error, isFetching } = useImBots();
  const bindMutation = useBindImBotIssue();
  const updateMutation = useUpdateImBot();
  const [addAnchor, setAddAnchor] = useState<HTMLElement | null>(null);
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null);

  const workspaceBots = bots.filter(bot => bot.workspaceId === issue.workspaceId);
  // 补绑候选：不属于本工作空间的 bot（含未配置 workspaceId = 0）。
  const candidateBots = bots.filter(bot => bot.workspaceId !== issue.workspaceId);

  // 在飞绑定请求（面板级单飞行中操作）：下传 bot 卡做行级 loading/disabled。
  const bindPending = bindMutation.isPending ? bindMutation.variables ?? null : null;
  const actionPending = bindMutation.isPending || updateMutation.isPending;
  const spec = pendingAction != null ? confirmSpec(pendingAction, issue) : null;
  let actionError: Error | null = null;
  if (pendingAction?.kind === 'conversation') {
    actionError = bindMutation.error;
  } else if (pendingAction != null) {
    actionError = updateMutation.error;
  }

  const refresh = () => {
    // 整域失效：列表 + 全部会话 key 一并重取（跨 bot 的 IM 侧绑定变更经此手动回填）。
    void qc.invalidateQueries({ queryKey: imBotsKeys.root });
  };

  const openAction = (action: PendingAction) => {
    // 打开即清上一轮错误（失败保留弹窗可见，取消/重开后不残留旧错误）。
    bindMutation.reset();
    updateMutation.reset();
    setPendingAction(action);
  };

  const handleConversationAction = (bot: ImBotModel, conversation: ImBotConversationModel) => {
    openAction({ kind: 'conversation', bot, conversation });
  };

  const handleAddBotAsk = (bot: ImBotModel) => {
    setAddAnchor(null);
    openAction({ kind: 'addBot', bot });
  };

  const handleConfirm = () => {
    if (!pendingAction) {
      return;
    }
    if (pendingAction.kind === 'conversation') {
      // 已绑当前任务 → 解绑（issueId 空）；否则绑定/切换到当前任务。失败保留弹窗（错误可见）。
      const issueId = pendingAction.conversation.boundIssueId === issue.id ? '' : issue.id;
      bindMutation.mutate(
        {
          botId: pendingAction.bot.id,
          conversationKey: pendingAction.conversation.conversationKey,
          issueId,
        },
        { onSuccess: () => setPendingAction(null) },
      );
    } else {
      // 补绑 = 全表单 update（workspaceId 换为当前；secret 空串 = 沿用原值），配置变更后端热重载。
      const bot = pendingAction.bot;
      updateMutation.mutate(
        {
          id: bot.id,
          name: bot.name,
          botId: bot.botId,
          secret: '',
          workspaceId: issue.workspaceId,
          model: bot.model,
          systemPrompt: bot.systemPrompt,
          accessPolicy: bot.accessPolicy,
          enabled: bot.enabled,
        },
        { onSuccess: () => setPendingAction(null) },
      );
    }
  };

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* 头部：机器人计数 + 补绑（+）+ 刷新（RefreshIconButton，isFetching 旋转） */}
      <PanelToolbar
        left={!isLoading && workspaceBots.length > 0
          ? <Typography variant="caption" color="text.secondary">{`机器人 ${workspaceBots.length}`}</Typography>
          : null}
        right={(
          <>
            <Tooltip title="把其他工作空间的机器人加入本工作空间">
              <IconButton
                size="small"
                onClick={e => setAddAnchor(e.currentTarget)}
                disabled={isLoading}
                aria-label="补绑机器人"
                sx={{ color: 'text.secondary' }}
              >
                <AddIcon fontSize="small" />
              </IconButton>
            </Tooltip>
            <RefreshIconButton onClick={refresh} fetching={isFetching} ariaLabel="刷新" />
          </>
        )}
      />

      {/* 内容区：错误 / 加载中 / 空态引导 / bot 聚合卡列表 */}
      {error
        ? (
            <Box sx={{ p: 1.5 }}>
              <Alert
                severity="error"
                action={<Button color="inherit" size="small" onClick={refresh}>重试</Button>}
              >
                机器人列表加载失败：{error.message}
              </Alert>
            </Box>
          )
        : isLoading
          ? (
              <Box sx={{ display: 'flex', justifyContent: 'center', p: 2 }}>
                <CircularProgress size={20} />
              </Box>
            )
          : workspaceBots.length === 0
            ? (
                <Box
                  sx={{
                    flex: 1,
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    justifyContent: 'center',
                    gap: 1,
                    p: 2,
                    textAlign: 'center',
                  }}
                >
                  <SmartToyOutlinedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />
                  <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>本工作空间暂无 IM 机器人</Typography>
                  <Typography variant="body2" color="text.secondary">
                    在左侧菜单「IM 机器人」页接入机器人，或点右上角「+」把已有机器人加入本工作空间
                  </Typography>
                </Box>
              )
            : (
                <Box sx={{ flex: 1, overflow: 'auto', pt: 1.5, pb: 0.5, px: 1.5, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
                  {workspaceBots.map(bot => (
                    <ImBotBotCard
                      key={bot.id}
                      bot={bot}
                      issue={issue}
                      bindPending={bindPending}
                      onConversationAction={handleConversationAction}
                    />
                  ))}
                </Box>
              )}

      {/* 「+」补绑候选浮层：不属于本工作空间的 bot（含未配置）；无候选给禁用提示项 */}
      <Popover
        open={addAnchor != null}
        anchorEl={addAnchor}
        onClose={() => setAddAnchor(null)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
        transformOrigin={{ vertical: 'top', horizontal: 'right' }}
      >
        <MenuList dense sx={{ minWidth: 240, maxHeight: 320, overflow: 'auto' }}>
          {candidateBots.length === 0
            ? <MenuItem disabled>暂无其他机器人可加入</MenuItem>
            : candidateBots.map(bot => (
                <MenuItem key={bot.id} onClick={() => handleAddBotAsk(bot)}>
                  <ListItemText
                    primary={bot.name}
                    secondary={bot.workspaceId > 0 ? `工作空间：${workspaceShortLabel(bot)}` : '未配置工作空间'}
                  />
                </MenuItem>
              ))}
        </MenuList>
      </Popover>

      {/* 统一确认弹窗：pending 中禁关闭防误触/防并发（归档弹窗同款口径）；失败保留弹窗 + 错误可见 */}
      <Dialog open={pendingAction != null} onClose={actionPending ? undefined : () => setPendingAction(null)}>
        <DialogTitle>{spec?.title}</DialogTitle>
        <DialogContent>
          <DialogContentText sx={{ fontSize: 13 }}>{spec?.text}</DialogContentText>
          {actionError && (
            <Alert severity="error" sx={{ mt: 1.5, fontSize: 12 }}>{actionError.message}</Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setPendingAction(null)} color="inherit" disabled={actionPending}>取消</Button>
          <Button onClick={handleConfirm} variant="contained" autoFocus loading={actionPending}>
            {spec?.confirm}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
