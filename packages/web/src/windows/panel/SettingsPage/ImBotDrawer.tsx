import type { ImBotAccessPolicy, ImBotModel } from '@src/services';
import {
  Alert,
  Box,
  Button,
  Chip,
  Collapse,
  Divider,
  FormControlLabel,
  MenuItem,
  Switch,
  TextField,
  Typography,
} from '@mui/material';
import { useCreateImBot, useUpdateImBot } from '@src/state/imBots';
import { useMemo, useState } from 'react';

// IM bot 编辑抽屉（对齐 UserProfilePage 敏感值不回显范式：secret 独立 draft 态，
// 留空 = 沿用原值、非空 = 覆盖；创建时必填）。
// 文案约定：中文直出（仅菜单标题走 i18n，见 ImBotsPage 头注）。

/** 抽屉打开状态：bot 为 null = 新建，非 null = 编辑该 bot。 */
export interface ImBotDrawerState {
  open: boolean;
  bot: ImBotModel | null;
}

/** 表单 draft（secret/白名单/工具以文本态编辑，提交时拆分）。 */
interface ImBotDraft {
  name: string;
  botId: string;
  secret: string;
  workspaceDir: string;
  model: string;
  systemPrompt: string;
  allowedToolsText: string;
  accessMode: ImBotAccessPolicy['mode'];
  allowUsersText: string;
  enabled: boolean;
}

function draftFromBot(bot: ImBotModel | null): ImBotDraft {
  return {
    name: bot?.name ?? '',
    botId: bot?.botId ?? '',
    secret: '',
    workspaceDir: bot?.workspaceDir ?? '',
    model: bot?.model ?? '',
    systemPrompt: bot?.systemPrompt ?? '',
    allowedToolsText: bot?.allowedTools.join(', ') ?? '',
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

function ImBotDrawer(props: { state: ImBotDrawerState; onClose: () => void }) {
  const { state, onClose } = props;
  const editing = state.bot != null;
  // draft 以初始值构造；切换编辑对象由父组件以 key 重挂载本组件（首帧即终值，无副作用重置）。
  const [draft, setDraft] = useState<ImBotDraft>(() => draftFromBot(state.bot));

  const createMutation = useCreateImBot();
  const updateMutation = useUpdateImBot();
  const saving = createMutation.isPending || updateMutation.isPending;

  // 必填校验：名称/botId/工作目录恒必填；secret 仅创建必填（编辑留空 = 沿用）。
  const invalid = useMemo(
    () =>
      draft.name.trim() === ''
      || draft.botId.trim() === ''
      || draft.workspaceDir.trim() === ''
      || (!editing && draft.secret.trim() === ''),
    [draft, editing],
  );

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
      workspaceDir: draft.workspaceDir.trim(),
      model: draft.model.trim(),
      systemPrompt: draft.systemPrompt,
      allowedTools: splitList(draft.allowedToolsText),
      accessPolicy,
      enabled: draft.enabled,
    };
    if (editing && state.bot) {
      updateMutation.mutate(
        { id: state.bot.id, ...shared, secret: draft.secret.trim() },
        { onSuccess: onClose },
      );
    } else {
      createMutation.mutate(
        { ...shared, secret: draft.secret.trim() },
        { onSuccess: onClose },
      );
    }
  };

  const helpText = (text: string) => (
    <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>{text}</Typography>
  );

  return (
    <Collapse in={state.open}>
      <Box sx={{ p: 3, display: 'flex', flexDirection: 'column', gap: 2, overflow: 'auto' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography sx={{ fontSize: 16, fontWeight: 600 }}>
            {editing ? '编辑 IM 机器人' : '新建 IM 机器人'}
          </Typography>
          {editing && state.bot && (
            <Chip size="small" label={state.bot.channel} variant="outlined" sx={{ ml: 'auto', fontSize: 12 }} />
          )}
        </Box>

        <Box sx={{ display: 'flex', gap: 2 }}>
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
        </Box>

        <TextField
          size="small"
          fullWidth
          type="password"
          autoComplete="off"
          required={!editing}
          label="Secret"
          placeholder={editing ? '留空 = 沿用原值' : '企业微信后台颁发的 Secret'}
          value={draft.secret}
          onChange={e => setField('secret', e.target.value)}
          helperText={helpText('保存后不回显；同一凭据全局仅允许一条活跃连接')}
        />

        <TextField
          size="small"
          fullWidth
          required
          label="工作目录"
          placeholder="/absolute/path/to/workspace"
          value={draft.workspaceDir}
          onChange={e => setField('workspaceDir', e.target.value)}
          helperText={helpText('claude 会话的工作目录（绝对路径，自动创建）')}
        />

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

        <TextField
          size="small"
          fullWidth
          multiline
          minRows={2}
          label="人设提示词（可选）"
          value={draft.systemPrompt}
          onChange={e => setField('systemPrompt', e.target.value)}
          helperText={helpText('注入为系统提示词，塑造机器人的角色与语气')}
        />

        <Divider />

        <TextField
          size="small"
          fullWidth
          label="工具白名单"
          placeholder="逗号分隔，如 Read, Edit, Bash"
          value={draft.allowedToolsText}
          onChange={e => setField('allowedToolsText', e.target.value)}
          helperText={helpText('留空 = 默认白名单（Read/Glob/Grep/Edit/Write/Bash/WebFetch/WebSearch/TodoWrite）')}
        />

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

        {(createMutation.error || updateMutation.error) && (
          <Alert severity="error" sx={{ fontSize: 12 }}>
            {(createMutation.error ?? updateMutation.error)!.message}
          </Alert>
        )}

        <Box sx={{ display: 'flex', justifyContent: 'flex-end', gap: 1 }}>
          <Button onClick={onClose} color="inherit" disabled={saving}>取消</Button>
          <Button onClick={handleSave} variant="contained" disabled={invalid || saving}>保存</Button>
        </Box>
      </Box>
    </Collapse>
  );
}

export default ImBotDrawer;
