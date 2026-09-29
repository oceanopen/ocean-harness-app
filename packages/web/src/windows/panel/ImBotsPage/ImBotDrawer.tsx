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

// IM bot 编辑抽屉（ResizableDrawer 右滑、左缘可拖宽，dsh-im 同款交互）。secret 明文回显（数据
// 全本地）：默认 password 掩码、右侧小眼睛切换明文；保存留空 = 沿用原值、非空 = 覆盖，创建必填。
// 工作空间必选下拉（会话目录取其 dir）；工具白名单收敛为「执行权限」下拉——接口仍收
// allowedTools，前端按选项转译（后续新场景在此追加选项即可）。渠道为表单首位的禁用下拉
// （新建 = 列表页当前渠道，编辑 = bot.channel）。文案约定：中文直出（仅菜单标题走 i18n）。
//
// 由父组件按需挂载（{drawer && <ImBotDrawer/>}）：每次打开都是全新 useState 初值（首帧即终值，
// 草稿含 secret 不残留），关闭即卸载。
interface ImBotDrawerProps {
  bot: ImBotModel | null; // null = 新建（手动接入），非 null = 编辑该 bot
  channelKey: string; // 新建时列表页当前选中渠道（编辑以 bot.channel 为准）
  onClose: () => void;
}

/** 执行权限选项：普通下拉选项值（非 boolean），新场景在此追加并配 allowedTools 转译。 */
interface ExecPermissionMeta {
  value: string;
  label: string;
}

const EXEC_PERMISSIONS: readonly ExecPermissionMeta[] = [
  { value: 'execute', label: '可执行命令' },
  { value: 'readonly', label: '不可执行命令' },
];

/** 不可执行命令时的工具白名单 = 代码默认白名单剔除 Bash（对齐 server bot.DefaultAllowedTools）。 */
const TOOLS_WITHOUT_BASH = ['Read', 'Glob', 'Grep', 'Edit', 'Write', 'WebFetch', 'WebSearch', 'TodoWrite'];

/** 执行权限 → allowedTools：可执行命令 = 空数组（后端默认白名单，含 Bash）。 */
function allowedToolsForExecPermission(value: string): string[] {
  return value === 'execute' ? [] : TOOLS_WITHOUT_BASH;
}

/** allowedTools → 执行权限初值：空数组（= 默认白名单含 Bash）或显式含 Bash = 可执行命令。 */
function execPermissionFromAllowedTools(tools: string[]): string {
  return tools.length === 0 || tools.includes('Bash') ? 'execute' : 'readonly';
}

/** 表单 draft（secret/allowUsers 以文本态编辑，提交时拆分）。workspaceId 0 = 未选。 */
interface ImBotDraft {
  name: string;
  botId: string;
  secret: string;
  workspaceId: number;
  model: string;
  systemPrompt: string;
  execPermission: string;
  accessMode: ImBotAccessPolicy['mode'];
  allowUsersText: string;
  enabled: boolean;
}

function draftFromBot(bot: ImBotModel | null): ImBotDraft {
  return {
    name: bot?.name ?? '',
    botId: bot?.botId ?? '',
    secret: bot?.secret ?? '',
    workspaceId: bot?.workspaceId ?? 0,
    model: bot?.model ?? '',
    systemPrompt: bot?.systemPrompt ?? '',
    execPermission: execPermissionFromAllowedTools(bot?.allowedTools ?? []),
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
  const { bot, channelKey, onClose } = props;
  const editing = bot != null;
  // 渠道展示值：编辑取 bot.channel，新建取列表页传入的当前渠道；禁用不可改。
  const displayChannelKey = bot ? bot.channel : channelKey;
  const [draft, setDraft] = useState<ImBotDraft>(() => draftFromBot(bot));
  // secret 显隐：默认掩码（password），点小眼睛切换明文（数据全本地，明文展示无风险）。
  const [showSecret, setShowSecret] = useState(false);

  const createMutation = useCreateImBot();
  const updateMutation = useUpdateImBot();
  const { data: workspaces = [] } = useWorkspaces();
  // saving 只驱动保存按钮 loading（防重复提交，且「正在保存」状态可见）；X/取消/遮罩在
  // saving 中的禁用是既有交互，保持不动。
  const saving = createMutation.isPending || updateMutation.isPending;

  // 必填校验：名称/botId 恒必填；secret 仅创建必填（编辑清空 = 沿用原值）；工作空间必选。
  const invalid
    = draft.name.trim() === ''
      || draft.botId.trim() === ''
      || (!editing && draft.secret.trim() === '')
      || draft.workspaceId <= 0;

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
      model: draft.model.trim(),
      systemPrompt: draft.systemPrompt,
      allowedTools: allowedToolsForExecPermission(draft.execPermission),
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
        { ...shared },
        { onSuccess: onClose },
      );
    }
  };

  const helpText = (text: string) => (
    <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>{text}</Typography>
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
        <Box sx={{ px: 3, py: 2, borderBottom: 1, borderColor: 'divider', display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography sx={{ fontSize: 16, fontWeight: 600 }}>
            {editing ? '编辑机器人' : '手动接入机器人'}
          </Typography>
          <IconButton
            size="small"
            onClick={onClose}
            disabled={saving}
            sx={{ 'ml': 'auto', '& svg': { fontSize: 18 } }}
          >
            <CloseOutlinedIcon />
          </IconButton>
        </Box>

        {/* 表单区 */}
        <Box sx={{ flex: 1, overflow: 'auto', p: 3, display: 'flex', flexDirection: 'column', gap: 2 }}>
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
                      sx={{ '& svg': { fontSize: 18 } }}
                    >
                      {showSecret ? <VisibilityOffOutlinedIcon /> : <VisibilityOutlinedIcon />}
                    </IconButton>
                  </InputAdornment>
                ),
              },
            }}
            helperText={helpText('默认掩码展示，点右侧图标切换明文；同一凭据全局仅允许一条活跃连接')}
          />

          <TextField
            size="small"
            fullWidth
            required
            select
            label="工作空间"
            value={draft.workspaceId}
            onChange={e => setField('workspaceId', Number(e.target.value))}
            helperText={helpText(
              draft.workspaceId > 0
                ? `会话目录取该工作空间的目录：${workspaces.find(ws => ws.id === draft.workspaceId)?.dir ?? ''}`
                : '每个机器人绑定一个工作空间（会话目录取其工作区目录）',
            )}
          >
            {workspaces.map(ws => (
              <MenuItem key={ws.id} value={ws.id}>{ws.name}</MenuItem>
            ))}
          </TextField>

          <Box sx={{ display: 'flex', gap: 2 }}>
            <TextField
              size="small"
              fullWidth
              label="模型（可选）"
              placeholder="留空 = CLI 默认"
              value={draft.model}
              onChange={e => setField('model', e.target.value)}
            />
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
          </Box>

          <Divider />

          <TextField
            size="small"
            fullWidth
            select
            label="执行权限"
            value={draft.execPermission}
            onChange={e => setField('execPermission', e.target.value)}
            helperText={helpText('不可执行命令 = 工具白名单剔除 Bash（仅保留读取/编辑/搜索类工具）')}
          >
            {EXEC_PERMISSIONS.map(p => (
              <MenuItem key={p.value} value={p.value}>{p.label}</MenuItem>
            ))}
          </TextField>

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
              helperText={helpText('白名单模式下仅名单内用户可使用（单聊与群聊 @ 均按发送者判定）')}
            />
          )}

          <TextField
            size="small"
            fullWidth
            multiline
            minRows={5}
            maxRows={10}
            label="人设提示词（可选）"
            value={draft.systemPrompt}
            onChange={e => setField('systemPrompt', e.target.value)}
            helperText={helpText('注入为系统提示词，塑造机器人的角色与语气')}
          />

          {(createMutation.error || updateMutation.error) && (
            <Alert severity="error" sx={{ fontSize: 12 }}>
              {(createMutation.error ?? updateMutation.error)!.message}
            </Alert>
          )}
        </Box>

        {/* 底部操作栏 */}
        <Box sx={{ px: 3, py: 2, borderTop: 1, borderColor: 'divider', display: 'flex', justifyContent: 'flex-end', gap: 1 }}>
          <Button onClick={onClose} color="inherit" disabled={saving}>取消</Button>
          <Button onClick={handleSave} variant="contained" loading={saving} disabled={invalid}>保存</Button>
        </Box>
      </Box>
    </ResizableDrawer>
  );
}

export default ImBotDrawer;
