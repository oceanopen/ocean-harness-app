import type { ImBotAccessPolicy, ImBotModel } from '@src/services';
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
import { useWorkspaces } from '@src/state/tracker';
import { useState } from 'react';
import { IM_BOT_CHANNELS } from './imBotChannels';

// IM bot 编辑抽屉（ResizableDrawer 右滑可拖宽）：secret 掩码回显、保存留空 = 沿用原值；
// 工作空间必选（会话目录取其 dir），渠道为首位禁用下拉。weixin 行无手工凭据（隐藏
// botId/secret，换凭据走「重新扫码授权」换绑链路）。由父组件按需挂载：每次打开全新草稿，关闭即卸载。
interface ImBotDrawerProps {
  bot: ImBotModel | null; // null = 新建（手动接入），非 null = 编辑该 bot
  channelKey: string; // 新建时列表页当前渠道（编辑以 bot.channel 为准）
  onRebind: (bot: ImBotModel) => void; // weixin 重新扫码授权（父组件收本抽屉开扫码换绑）
  onClose: () => void;
}

/** 表单 draft（secret/allowUsers 以文本态编辑，提交时拆分）。workspaceId 空 = 未选。 */
interface ImBotDraft {
  name: string;
  botId: string;
  secret: string;
  workspaceId: string;
  accessMode: ImBotAccessPolicy['mode'];
  allowUsersText: string;
  enabled: boolean;
}

function draftFromBot(bot: ImBotModel | null): ImBotDraft {
  return {
    name: bot?.name ?? '',
    botId: bot?.botId ?? '',
    secret: bot?.secret ?? '',
    workspaceId: bot?.workspaceId ?? '',
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
  // saving 只驱动保存按钮 loading（防重复提交，且「正在保存」状态可见）；X/取消/遮罩在
  // saving 中的禁用是既有交互，保持不动。
  const saving = createMutation.isPending || updateMutation.isPending;

  // 必填校验：名称/工作空间恒必填；wecom 另需 botId 与 secret（创建必填，编辑清空 = 沿用
  // 原值）；weixin 无手工凭据字段，校验面更窄。
  const invalid
    = draft.name.trim() === ''
      || (!isWeixin && draft.botId.trim() === '')
      || (!editing && !isWeixin && draft.secret.trim() === '')
      || draft.workspaceId === '';

  const setField = <K extends keyof ImBotDraft>(key: K, value: ImBotDraft[K]) => {
    setDraft(prev => ({ ...prev, [key]: value }));
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
      workspaceId: draft.workspaceId,
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
            required
            select
            label="工作空间"
            value={draft.workspaceId}
            onChange={e => setField('workspaceId', e.target.value)}
            helperText={helpText(
              draft.workspaceId !== ''
                ? `会话目录取该工作空间的目录：${workspaces.find(ws => ws.id === draft.workspaceId)?.dir ?? ''}`
                : '每个机器人绑定一个工作空间（会话目录取其工作区目录）',
            )}
          >
            {workspaces.map(ws => (
              <MenuItem key={ws.id} value={ws.id}>{ws.name}</MenuItem>
            ))}
          </TextField>

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
