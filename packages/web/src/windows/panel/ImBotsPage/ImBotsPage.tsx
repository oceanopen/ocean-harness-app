import type { DoctorEntryState, ImBotModel, WorkspaceModel } from '@src/services';
import type { AcpAgentOption } from '@src/state/agentCatalog';
import type { ImBotChannelMeta } from './imBotChannels';
import AddOutlinedIcon from '@mui/icons-material/AddOutlined';
import ForumOutlinedIcon from '@mui/icons-material/ForumOutlined';
import MonitorHeartOutlinedIcon from '@mui/icons-material/MonitorHeartOutlined';
import QrCode2OutlinedIcon from '@mui/icons-material/QrCode2Outlined';
import RefreshOutlinedIcon from '@mui/icons-material/RefreshOutlined';
import RestartAltOutlinedIcon from '@mui/icons-material/RestartAltOutlined';
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
import { effectiveAcpAgentCode, useAcpAgentOptions } from '@src/state/agentCatalog';
import { useDoctorCheck, useDoctorReports } from '@src/state/doctor';
import { useDeleteImBot, useImBots, useRestartImBot } from '@src/state/imBots';
import { useWorkspaces } from '@src/state/tracker';
import { useState } from 'react';
import { IM_BOT_CHANNELS } from './imBotChannels';
import ImBotDrawer from './ImBotDrawer';
import ImBotProvisionDrawer from './ImBotProvisionDrawer';
import { imBotStateChip } from './imBotStateChip';

// IM 机器人顶层菜单页（/imBots）：左栏渠道卡片 + 右栏机器人卡片列表，接入入口与引导文案由
// imBotChannels 渠道元数据驱动（weixin 仅扫码授权，无手工凭据路径）；weixin 换绑 = 编辑抽屉
// 「重新扫码授权」切扫码抽屉。文案约定：中文直出（仅菜单标题走 i18n）。

/** 健康徽标形态（T3.5 doctor 四态派生）；tooltip 为引导文案（unhealthy 时直显探测原因）。 */
interface HealthChip {
  label: string;
  color: 'success' | 'error' | 'info' | 'default';
  tooltip?: string;
}

/**
 * ACP bot 的 agent doctor 健康徽标（T3.5）：仅 workspace 启动模式 = acp 的 bot 依赖 doctor
 * 探测链（node→vendored→adapter→握手）；终端模式 headless 直接 spawn vendored claude 二进制
 * 不经该链，doctor 结论对其语义不精确——不显示。agentCode 回落规则走域内 SSOT
 * （effectiveAcpAgentCode，与后端 resolveSessionConfig 同语义）。返回 null = 不显示（非 ACP / workspace 不在缓存 /
 * doctor 快照未就绪 / catalog 遗留 agentCode 无 doctor 条目）。
 */
function acpHealthChip(
  bot: ImBotModel,
  workspaces: WorkspaceModel[] | undefined,
  agentOptions: AcpAgentOption[],
  doctorEntries: DoctorEntryState[] | undefined,
): HealthChip | null {
  if (bot.workspaceId === '' || !workspaces || !doctorEntries) {
    return null;
  }
  const ws = workspaces.find(w => w.id === bot.workspaceId);
  if (!ws || ws.launchSettings?.mode !== 'acp') {
    return null;
  }
  const agentCode = effectiveAcpAgentCode(ws.launchSettings.agentCode, agentOptions);
  const entry = doctorEntries.find(e => e.agentCode === agentCode);
  if (!entry) {
    return null;
  }
  switch (entry.status) {
    case 'healthy':
      return { label: 'Agent 正常', color: 'success' };
    case 'unhealthy':
      return { label: 'Agent 不可用', color: 'error', tooltip: entry.reason ?? 'ACP 运行环境不可用，请检查后重新检测' };
    case 'checking':
      return { label: 'Agent 检测中', color: 'info', tooltip: '正在探测 ACP 运行环境' };
    default: // unknown：从未探测（sidecar 重启后由启动探测重填）
      return { label: 'Agent 待检测', color: 'default', tooltip: '尚未完成探测，稍候自动检测' };
  }
}

/** 左栏渠道小卡片：图标 + 名称 + 在线 bot 数徽标，选中态高亮。 */
function ChannelCard(props: { channel: ImBotChannelMeta; total: number; online: number; selected: boolean; onSelect: () => void }) {
  const { channel, total, online, selected, onSelect } = props;
  return (
    <Box
      onClick={onSelect}
      sx={{
        'borderRadius': '8px',
        'border': '1px solid',
        'borderColor': selected ? 'primary.main' : 'divider',
        'bgcolor': selected ? 'action.selected' : 'background.paper',
        'px': '12px',
        'py': '10px',
        'display': 'flex',
        'alignItems': 'center',
        'gap': '10px',
        'cursor': 'pointer',
        '&:hover': { bgcolor: 'action.hover' },
      }}
    >
      <ForumOutlinedIcon sx={{ '& svg': { fontSize: '20px' }, 'fontSize': '20px', 'color': selected ? 'primary.main' : 'text.secondary' }} />
      <Typography sx={{ fontSize: '13px', fontWeight: selected ? 600 : 400 }}>{channel.label}</Typography>
      <Chip
        size="small"
        label={`${online}/${total}`}
        variant="outlined"
        sx={{ ml: 'auto', fontSize: '12px', minWidth: '44px' }}
      />
    </Box>
  );
}

/** 右栏机器人满行卡片：名称 + 状态徽标（连接 + ACP bot 的 agent 健康）+ 操作（重启/编辑/删除），下两行工作空间/访问策略。 */
function ImBotCard(props: {
  bot: ImBotModel;
  health: HealthChip | null;
  onEdit: () => void;
  onDeleteAsk: () => void;
}) {
  const { bot, health, onEdit, onDeleteAsk } = props;
  const restartMutation = useRestartImBot();
  const chip = imBotStateChip(bot.connState);

  return (
    <Box sx={{ borderRadius: '8px', border: '1px solid', borderColor: 'divider', overflow: 'hidden', bgcolor: 'background.paper' }}>
      <Box sx={{ px: '16px', py: '12px', display: 'flex', alignItems: 'center', gap: '12px' }}>
        <Typography sx={{ fontSize: '14px', fontWeight: 600 }}>{bot.name}</Typography>
        <Tooltip title={bot.lastError || undefined}>
          <Chip size="small" label={chip.label} color={chip.color} variant="outlined" />
        </Tooltip>
        {health && (
          <Tooltip title={health.tooltip || undefined}>
            <Chip size="small" label={health.label} color={health.color} variant="outlined" />
          </Tooltip>
        )}
        {bot.workspaceId === '' && (
          <Tooltip title="尚未选择工作空间，对话时将收到补选提醒">
            <Chip size="small" label="待配置" color="warning" variant="outlined" sx={{ fontSize: '12px' }} />
          </Tooltip>
        )}
        <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <Tooltip title="重启连接">
            {/* 停用不提供重启（重启 = 以落库配置拉起，停用态应先打开开关）；pending 禁用防双击并发重启。
                图标用 RestartAlt 与右上角整体刷新（Refresh）区分——本按钮是销毁性重启，非读取刷新。 */}
            <IconButton
              size="small"
              disabled={!bot.enabled || restartMutation.isPending}
              onClick={() => restartMutation.mutate({ id: bot.id })}
              sx={{ '& svg': { fontSize: '18px' } }}
            >
              <RestartAltOutlinedIcon />
            </IconButton>
          </Tooltip>
          <Button size="small" onClick={onEdit}>编辑</Button>
          <Button size="small" color="error" onClick={onDeleteAsk}>删除</Button>
        </Box>
      </Box>
      <Box sx={{ px: '16px', pb: '12px', display: 'flex', gap: '24px', alignItems: 'center' }}>
        <Typography sx={{ fontSize: '12px', color: 'text.secondary', flex: 2, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {bot.workspaceId !== '' ? `工作空间：${bot.workspaceName || bot.workspaceId}` : '工作空间：未选择'}
        </Typography>
        <Typography sx={{ fontSize: '12px', color: 'text.secondary', flex: 1 }}>
          {bot.accessPolicy.mode === 'open'
            ? '开放模式（所有人可用）'
            : `白名单 ${bot.accessPolicy.allowUsers.length} 人`}
        </Typography>
        <Typography sx={{ fontSize: '12px', color: 'text.secondary', flexShrink: 0 }}>
          {bot.enabled ? '已启用' : '已停用'}
        </Typography>
      </Box>
    </Box>
  );
}

/** IM 机器人设置分区页（两栏：渠道卡片 + 机器人卡片列表）。 */
function ImBotsPage() {
  const { data: bots = [], isLoading, refetch, isFetching, error } = useImBots();
  const deleteMutation = useDeleteImBot();
  // T3.5 健康徽标 join 数据面：workspaces（mode/agentCode）+ agentCatalog（回落序）+ doctor
  // 四态快照，三缓存各自失效/轮询独立；doctor busy（checking/unknown）1s 条件轮询自动驱动
  // 徽标收敛，全落定自动停。
  const { data: workspaces } = useWorkspaces();
  const agentOptions = useAcpAgentOptions();
  const { data: doctorEntries } = useDoctorReports();
  const checkDoctor = useDoctorCheck();
  const [channelKey, setChannelKey] = useState(IM_BOT_CHANNELS[0].key);
  const [drawer, setDrawer] = useState<{ bot: ImBotModel | null; channelKey: string } | null>(null);
  // 扫码抽屉会话：rebindBotId 空 = 扫码新建，非空为既有 weixin bot 换绑凭据（编辑抽屉跳转而来）。
  const [provision, setProvision] = useState<{ rebindBotId: string } | null>(null);
  const [provisionKey, setProvisionKey] = useState(0);
  const [deleteTarget, setDeleteTarget] = useState<ImBotModel | null>(null);

  const channel = IM_BOT_CHANNELS.find(c => c.key === channelKey) ?? IM_BOT_CHANNELS[0];
  // 按当前渠道过滤机器人列表。
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
          width: '220px',
          flexShrink: 0,
          borderRight: '1px solid',
          borderColor: 'divider',
          bgcolor: 'background.paper',
          p: '12px',
          display: 'flex',
          flexDirection: 'column',
          gap: '8px',
          overflow: 'auto',
        }}
      >
        <Typography sx={{ fontSize: '12px', color: 'text.secondary', px: '8px' }}>渠道</Typography>
        {IM_BOT_CHANNELS.map(ch => (
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
        <Box sx={{ px: '24px', py: '16px', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <Typography sx={{ fontSize: '15px', fontWeight: 600 }}>{channel.label}</Typography>
          <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: '8px' }}>
            {/* 检测 Agent（T3.5）：受理全局 doctor 探测（doctor 结论 per-agentCode 全局共享，同
                agent 的 bot 共用一份），受理与探测异步解耦，收敛经四态轮询自动呈现于徽标。 */}
            <Tooltip title="检测 Agent 运行环境（node / claude / 适配器握手）">
              <IconButton
                size="small"
                onClick={() => checkDoctor.mutate(undefined)}
                loading={checkDoctor.isPending}
                sx={{ '& svg': { fontSize: '20px' } }}
              >
                <MonitorHeartOutlinedIcon />
              </IconButton>
            </Tooltip>
            <Tooltip title="刷新">
              <IconButton
                size="small"
                onClick={() => refetch()}
                disabled={isFetching}
                sx={{ '& svg': { fontSize: '20px' } }}
              >
                <RefreshOutlinedIcon />
              </IconButton>
            </Tooltip>
            <Button
              variant="outlined"
              startIcon={<QrCode2OutlinedIcon sx={{ '& svg': { fontSize: '18px' } }} />}
              onClick={() => setProvision({ rebindBotId: '' })}
            >
              {channel.provisionLabel}
            </Button>
            {channel.manualProvision && (
              <Button
                variant="contained"
                startIcon={<AddOutlinedIcon sx={{ '& svg': { fontSize: '18px' } }} />}
                onClick={() => setDrawer({ bot: null, channelKey })}
              >
                手动接入
              </Button>
            )}
          </Box>
        </Box>

        <Box sx={{ px: '24px', pb: '24px', flex: 1, overflow: 'auto', display: 'flex', flexDirection: 'column', gap: '12px' }}>
          <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>
            {channel.intro}
          </Typography>

          {isLoading && <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>…</Typography>}
          {!isLoading && error && (
            <Typography sx={{ fontSize: '12px', color: 'error.main' }}>
              {`机器人列表加载失败：${error.message}（点右上角刷新重试）`}
            </Typography>
          )}
          {!isLoading && !error && channelBots.length === 0 && (
            <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>
              {channel.emptyHint}
            </Typography>
          )}
          {channelBots.map(bot => (
            <ImBotCard
              key={bot.id}
              bot={bot}
              health={acpHealthChip(bot, workspaces, agentOptions, doctorEntries)}
              onEdit={() => setDrawer({ bot, channelKey })}
              onDeleteAsk={() => setDeleteTarget(bot)}
            />
          ))}
        </Box>
      </Box>

      {/* 编辑/手动接入抽屉：按需挂载（每次打开全新草稿、首帧即终值，关闭即卸载不残留） */}
      {drawer && (
        <ImBotDrawer
          bot={drawer.bot}
          channelKey={drawer.channelKey}
          onRebind={(bot) => {
            // weixin 换凭据：收编辑抽屉，切扫码抽屉换绑语境（connected 后列表缓存自动刷新）。
            setDrawer(null);
            setProvision({ rebindBotId: bot.id });
          }}
          onClose={() => setDrawer(null)}
        />
      )}

      {/* 扫码接入抽屉：key 重挂载即「重新生成二维码」（卸载清定时器，新 begin 自动取消旧会话） */}
      <ImBotProvisionDrawer
        key={provisionKey}
        open={provision != null}
        channelKey={channelKey}
        rebindBotId={provision?.rebindBotId ?? ''}
        onClose={() => setProvision(null)}
        onRegenerate={() => setProvisionKey(k => k + 1)}
      />

      <Dialog open={deleteTarget != null} onClose={() => setDeleteTarget(null)}>
        <DialogTitle>删除机器人</DialogTitle>
        <DialogContent>
          <DialogContentText sx={{ fontSize: '13px' }}>
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
