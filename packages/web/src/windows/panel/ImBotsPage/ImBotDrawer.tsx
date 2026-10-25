import type { ImBotAccessPolicy, ImBotConversationType, ImBotModel } from '@src/services';
import ClearOutlinedIcon from '@mui/icons-material/ClearOutlined';
import CloseOutlinedIcon from '@mui/icons-material/CloseOutlined';
import VisibilityOffOutlinedIcon from '@mui/icons-material/VisibilityOffOutlined';
import VisibilityOutlinedIcon from '@mui/icons-material/VisibilityOutlined';
import {
  Alert,
  Box,
  Button,
  Divider,
  FormControlLabel,
  IconButton,
  InputAdornment,
  MenuItem,
  Switch,
  TextField,
  Typography,
} from '@mui/material';
import ResizableDrawer from '@src/shared/ResizableDrawer';
import { useCreateImBot, useUpdateImBot } from '@src/state/imBots';
import { useProjects } from '@src/state/projects';
import { useProjectTasks, useWorkspaces } from '@src/state/tracker';
import { useState } from 'react';
import { IM_BOT_CHANNELS } from './imBotChannels';

// IM bot 编辑抽屉（ResizableDrawer 右滑可拖宽）：secret 掩码回显、保存留空 = 沿用原值；
// 会话类型两档（D8）可选可清空：工作空间档选工作空间、项目档选项目 + 可选关联任务，未配置合法
// （收消息由服务端提示拦截）；切档/清档/清项目级联清空下游。weixin 行换凭据走「重新扫码授权」。
interface ImBotDrawerProps {
  bot: ImBotModel | null; // null = 新建（手动接入），非 null = 编辑该 bot
  channelKey: string; // 新建时列表页当前渠道（编辑以 bot.channel 为准）
  onRebind: (bot: ImBotModel) => void; // weixin 重新扫码授权（父组件收本抽屉开扫码换绑）
  onClose: () => void;
}

/** 表单 draft（secret/allowUsers 以文本态编辑，提交时拆分）。各 id 与会话类型空串 = 未选。 */
interface ImBotDraft {
  name: string;
  botId: string;
  secret: string;
  conversationType: ImBotConversationType;
  workspaceId: string;
  projectId: string;
  taskId: string;
  accessMode: ImBotAccessPolicy['mode'];
  allowUsersText: string;
  enabled: boolean;
}

function draftFromBot(bot: ImBotModel | null): ImBotDraft {
  return {
    name: bot?.name ?? '',
    botId: bot?.botId ?? '',
    secret: bot?.secret ?? '',
    conversationType: bot?.conversationType ?? '',
    workspaceId: bot?.workspaceId ?? '',
    projectId: bot?.projectId ?? '',
    taskId: bot?.taskId ?? '',
    accessMode: bot?.accessPolicy.mode ?? 'allowlist',
    allowUsersText: bot?.accessPolicy.allowUsers.join(', ') ?? '',
    enabled: bot?.enabled ?? true,
  };
}

/** 拆分逗号分隔文本为字符串数组（去空白、去空项）。 */
function splitList(text: string): string[] {
  return text
    .split(/[,，]/)
    .map(item => item.trim())
    .filter(item => item !== '');
}

// 选择框清空按钮（endAdornment 注入）：mousedown 阻断避免触发下拉展开。
function clearAdornment(onClear: () => void) {
  return (
    <InputAdornment position="end">
      <IconButton
        size="small"
        aria-label="清空"
        onMouseDown={e => e.stopPropagation()}
        onClick={onClear}
        sx={{ '& svg': { fontSize: '16px' } }}
      >
        <ClearOutlinedIcon />
      </IconButton>
    </InputAdornment>
  );
}

function ImBotDrawer(props: ImBotDrawerProps) {
  const { bot, channelKey, onRebind, onClose } = props;
  const editing = bot != null;
  // 渠道展示值：编辑取 bot.channel，新建取列表页传入的当前渠道；禁用不可改。
  const displayChannelKey = bot ? bot.channel : channelKey;
  const isWeixin = displayChannelKey === 'weixin';
  const [draft, setDraft] = useState<ImBotDraft>(() => draftFromBot(bot));
  // secret 显隐：默认掩码（password），点小眼睛切换明文（数据全本地，明文展示无风险）。
  const [showSecret, setShowSecret] = useState(false);

  const createMutation = useCreateImBot();
  const updateMutation = useUpdateImBot();
  const { data: workspaces = [] } = useWorkspaces();
  const { data: projects = [] } = useProjects();
  // 项目档任务列表（可选关联任务；projectId 空串时 hooks 守卫不查询）。
  const { data: projectTasks = [] } = useProjectTasks(draft.conversationType === 'project' ? draft.projectId : null);
  // saving 只驱动保存按钮 loading（防重复提交，且「正在保存」状态可见）；X/取消/遮罩在
  // saving 中的禁用是既有交互，保持不动。
  const saving = createMutation.isPending || updateMutation.isPending;

  // 必填校验：仅名称与凭据（归属四字段全部可选，未配置由收消息链路提示拦截）；wecom 需
  // botId 与 secret（创建必填，编辑清空 = 沿用原值）；weixin 无手工凭据字段，校验面更窄。
  const invalid
    = draft.name.trim() === ''
      || (!isWeixin && draft.botId.trim() === '')
      || (!editing && !isWeixin && draft.secret.trim() === '');

  const setField = <K extends keyof ImBotDraft>(key: K, value: ImBotDraft[K]) => {
    setDraft(prev => ({ ...prev, [key]: value }));
  };

  // 切换/清空会话类型：下游容器选择一并清空（档位语义变化后旧值不可复用）。
  const changeConversationType = (value: ImBotConversationType) => {
    setDraft(prev => ({ ...prev, conversationType: value, workspaceId: '', projectId: '', taskId: '' }));
  };

  // 清空项目：关联任务随项目清空（任务选项由项目派生）。
  const clearProject = () => {
    setField('projectId', '');
    setField('taskId', '');
  };

  const handleSave = () => {
    const accessPolicy: ImBotAccessPolicy = {
      mode: draft.accessMode,
      allowUsers: draft.accessMode === 'allowlist' ? splitList(draft.allowUsersText) : [],
    };
    const shared = {
      name: draft.name.trim(),
      botId: draft.botId.trim(),
      secret: draft.secret.trim(),
      conversationType: draft.conversationType,
      workspaceId: draft.conversationType === 'workspace' ? draft.workspaceId : undefined,
      projectId: draft.conversationType === 'project' ? draft.projectId : undefined,
      taskId: draft.conversationType === 'project' && draft.taskId !== '' ? draft.taskId : undefined,
      accessPolicy,
      enabled: draft.enabled,
    };
    if (bot) {
      updateMutation.mutate(
        { id: bot.id, ...shared },
        { onSuccess: onClose },
      );
    } else {
      createMutation.mutate(
        { channel: displayChannelKey, ...shared },
        { onSuccess: onClose },
      );
    }
  };

  const helpText = (text: string) => (
    <Typography sx={{ fontSize: '12px', color: 'text.secondary' }}>{text}</Typography>
  );

  return (
    <ResizableDrawer
      open
      // saving 中禁止背景点击/Esc 关闭，避免半成品状态丢失。
      onClose={saving ? undefined : onClose}
      defaultWidthPct={40}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* 标题栏 */}
        <Box sx={{ px: '24px', py: '16px', borderBottom: '1px solid', borderColor: 'divider', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <Typography sx={{ fontSize: '16px', fontWeight: 600 }}>
            {editing ? '编辑机器人' : '手动接入机器人'}
          </Typography>
          <IconButton
            size="small"
            onClick={onClose}
            disabled={saving}
            sx={{ 'ml': 'auto', '& svg': { fontSize: '18px' } }}
          >
            <CloseOutlinedIcon />
          </IconButton>
        </Box>

        {/* 表单区 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: '24px', display: 'flex', flexDirection: 'column', gap: '16px' }}>
          <FormControlLabel
            sx={{ flexShrink: 0 }}
            control={(
              <Switch
                checked={draft.enabled}
                onChange={e => setField('enabled', e.target.checked)}
              />
            )}
            label="启用"
          />

          <TextField
            size="small"
            fullWidth
            select
            disabled
            label="渠道"
            value={displayChannelKey}
            helperText={helpText('渠道在创建时确定，不可更改')}
          >
            {IM_BOT_CHANNELS.map(ch => (
              <MenuItem key={ch.key} value={ch.key}>{ch.label}</MenuItem>
            ))}
          </TextField>

          <TextField
            size="small"
            fullWidth
            required
            label="名称"
            value={draft.name}
            onChange={e => setField('name', e.target.value)}
          />

          {/* weixin 无手工凭据：botId/secret 仅 wecom 渲染；weixin 编辑态展示扫码登记的账号 id + 换绑入口 */}
          {!isWeixin && (
            <>
              <TextField
                size="small"
                fullWidth
                required
                label="Bot ID"
                value={draft.botId}
                onChange={e => setField('botId', e.target.value)}
                helperText={helpText('企业微信后台「智能机器人」详情页的 botId')}
              />

              <TextField
                size="small"
                fullWidth
                type={showSecret ? 'text' : 'password'}
                autoComplete="off"
                required={!editing}
                label="Secret"
                placeholder={editing ? '清空 = 沿用原值' : '企业微信后台颁发的 Secret'}
                value={draft.secret}
                onChange={e => setField('secret', e.target.value)}
                slotProps={{
                  input: {
                    endAdornment: (
                      <InputAdornment position="end">
                        <IconButton
                          size="small"
                          aria-label={showSecret ? '隐藏 Secret' : '显示 Secret'}
                          onClick={() => setShowSecret(v => !v)}
                          sx={{ '& svg': { fontSize: '18px' } }}
                        >
                          {showSecret ? <VisibilityOffOutlinedIcon /> : <VisibilityOutlinedIcon />}
                        </IconButton>
                      </InputAdornment>
                    ),
                  },
                }}
                helperText={helpText('默认掩码展示，点右侧图标切换明文；同一凭据全局仅允许一条活跃连接')}
              />
            </>
          )}

          {isWeixin && bot && (
            <>
              <TextField
                size="small"
                fullWidth
                disabled
                label="微信账号"
                value={bot.accountId}
                helperText={helpText('扫码授权时自动登记的 iLink 账号 id（不可手改）')}
              />
              <Alert severity="info" sx={{ fontSize: '12px' }}>
                微信连接凭据由扫码授权管理：token 失效、被踢下线或更换微信账号时，请重新扫码授权。
              </Alert>
              <Button variant="outlined" onClick={() => onRebind(bot)}>重新扫码授权</Button>
            </>
          )}

          <TextField
            size="small"
            fullWidth
            select
            label="会话类型"
            value={draft.conversationType}
            onChange={e => changeConversationType(e.target.value as ImBotConversationType)}
            slotProps={{ input: { endAdornment: draft.conversationType !== '' ? clearAdornment(() => changeConversationType('')) : undefined } }}
            helperText={helpText(draft.conversationType === ''
              ? '可不选：暂不绑定会话目标，收消息时提示先配置'
              : '工作空间档：工作目录不变的会话；项目档：每任务独立目录（任务可选关联）')}
          >
            <MenuItem value="workspace">工作空间</MenuItem>
            <MenuItem value="project">项目</MenuItem>
          </TextField>

          {draft.conversationType === 'workspace' && (
            <TextField
              size="small"
              fullWidth
              select
              label="工作空间"
              value={draft.workspaceId}
              onChange={e => setField('workspaceId', e.target.value)}
              slotProps={{ input: { endAdornment: draft.workspaceId !== '' ? clearAdornment(() => setField('workspaceId', '')) : undefined } }}
              helperText={helpText(
                draft.workspaceId !== ''
                  ? `会话目录取该工作空间的目录：${workspaces.find(ws => ws.id === draft.workspaceId)?.dir ?? ''}`
                  : '可选；不选则收消息时提示先配置',
              )}
            >
              {workspaces.map(ws => (
                <MenuItem key={ws.id} value={ws.id}>{ws.name}</MenuItem>
              ))}
            </TextField>
          )}

          {draft.conversationType === 'project' && (
            <>
              <TextField
                size="small"
                fullWidth
                select
                label="项目"
                value={draft.projectId}
                onChange={(e) => {
                  setField('projectId', e.target.value);
                  setField('taskId', '');
                }}
                slotProps={{ input: { endAdornment: draft.projectId !== '' ? clearAdornment(clearProject) : undefined } }}
                helperText={helpText(
                  draft.projectId !== ''
                    ? `项目根目录：${projects.find(pr => pr.id === draft.projectId)?.dir ?? ''}（各任务会话在其任务子目录运行）`
                    : '可选；不选则收消息时提示先配置',
                )}
              >
                {projects.map(pr => (
                  <MenuItem key={pr.id} value={pr.id}>{pr.name}</MenuItem>
                ))}
              </TextField>
              <TextField
                size="small"
                fullWidth
                select
                label="关联任务（可选）"
                value={draft.taskId}
                onChange={e => setField('taskId', e.target.value)}
                slotProps={{ input: { endAdornment: draft.taskId !== '' ? clearAdornment(() => setField('taskId', '')) : undefined } }}
                helperText={helpText(
                  draft.projectId === ''
                    ? '先选择项目'
                    : draft.taskId !== ''
                      ? `收到的消息将路由到该任务的主会话：${projectTasks.find(t => t.id === draft.taskId)?.name ?? ''}`
                      : '可不关联：收到消息时机器人会提示先指定任务',
                )}
              >
                {projectTasks.map(t => (
                  <MenuItem key={t.id} value={t.id}>{t.name}</MenuItem>
                ))}
              </TextField>
            </>
          )}

          <Divider />

          <TextField
            size="small"
            fullWidth
            select
            label="访问模式"
            value={draft.accessMode}
            onChange={e => setField('accessMode', e.target.value as ImBotAccessPolicy['mode'])}
          >
            <MenuItem value="open">开放（所有人可用）</MenuItem>
            <MenuItem value="allowlist">白名单（仅名单内用户）</MenuItem>
          </TextField>

          {draft.accessMode === 'allowlist' && (
            <TextField
              size="small"
              fullWidth
              label="白名单用户"
              placeholder="逗号分隔的 userid"
              value={draft.allowUsersText}
              onChange={e => setField('allowUsersText', e.target.value)}
              helperText={helpText(
                isWeixin
                  ? '仅名单内用户可使用；扫码创建的机器人默认白名单仅机主本人（按微信号判定），可按需调整'
                  : '白名单模式下仅名单内用户可使用（单聊与群聊 @ 均按发送者判定）',
              )}
            />
          )}

          {(createMutation.error || updateMutation.error) && (
            <Alert severity="error" sx={{ fontSize: '12px' }}>
              {(createMutation.error ?? updateMutation.error)!.message}
            </Alert>
          )}
        </Box>

        {/* 底部操作栏 */}
        <Box sx={{ px: '24px', py: '16px', borderTop: '1px solid', borderColor: 'divider', display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
          <Button onClick={onClose} color="inherit" disabled={saving}>取消</Button>
          <Button onClick={handleSave} variant="contained" loading={saving} disabled={invalid}>保存</Button>
        </Box>
      </Box>
    </ResizableDrawer>
  );
}

export default ImBotDrawer;
