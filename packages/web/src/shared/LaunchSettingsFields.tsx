import type { WorkspaceLaunchSettings } from '@src/services';
import type { AgentCode } from '@src/shared/agentCode';
import { Divider, MenuItem, TextField } from '@mui/material';
import { TERMINAL_AGENT_OPTIONS } from '@src/shared/launchSettings';
import { useAcpAgentOptions, useEffectiveAcpAgentCode } from '@src/state/agentCatalog';

type LaunchMode = '' | NonNullable<WorkspaceLaunchSettings['mode']>;

interface LaunchSettingsFieldsProps {
  mode: LaunchMode;
  onModeChange: (mode: LaunchMode) => void;
  autoCommand: NonNullable<WorkspaceLaunchSettings['autoCommand']>;
  onAutoCommandChange: (v: NonNullable<WorkspaceLaunchSettings['autoCommand']>) => void;
  /** 未加工选中态：undefined = 未显式选择（组件内派生首个 enabled 条目）。 */
  agentCode?: AgentCode;
  onAgentCodeChange: (code: AgentCode | undefined) => void;
  permissionMode: NonNullable<WorkspaceLaunchSettings['permissionMode']>;
  onPermissionModeChange: (v: NonNullable<WorkspaceLaunchSettings['permissionMode']>) => void;
  disabled?: boolean;
  /** issue 级覆盖的「跟随工作空间」（'' 档）：模式下拉首项，未覆盖键字段级回落。 */
  showFollowOption?: boolean;
}

/**
 * LaunchSettingsFields：启动设置字段组——启动模式四档 + terminal-auto 档「终端 Agent」+
 * acp 档「ACP Agent / 执行模式」。WorkspaceDrawer（workspace 单源）与 ProjectIssueDrawer
 * （issue 级覆盖）共用，差异仅 showFollowOption；提交拼装由各抽屉自理。
 */
export default function LaunchSettingsFields({
  mode,
  onModeChange,
  autoCommand,
  onAutoCommandChange,
  agentCode,
  onAgentCodeChange,
  permissionMode,
  onPermissionModeChange,
  disabled = false,
  showFollowOption = false,
}: LaunchSettingsFieldsProps) {
  const acpAgentOptions = useAcpAgentOptions();
  const effectiveAgentCode = useEffectiveAcpAgentCode(agentCode);
  return (
    <>
      <Divider />
      <TextField
        select
        label="默认任务启动模式"
        value={mode}
        onChange={e => onModeChange(e.target.value as LaunchMode)}
        fullWidth
        disabled={disabled}
        // 空串（跟随工作空间）是有效选项：displayEmpty 渲染选中项文本；InputBase 的
        // filled 判定不含空串，label 需显式常驻收缩，否则与内容重影。
        slotProps={showFollowOption
          ? { select: { displayEmpty: true }, inputLabel: { shrink: true } }
          : undefined}
        helperText={showFollowOption
          ? '跟随工作空间 = 未覆盖，沿用工作空间启动设置；覆盖仅对本 issue 生效'
          : '不执行任何操作 = issue 就绪后不进入任何会话，出启动方式选择面板；自动打开终端 = 直接进入裸 shell 终端（Agent 经终端工具条自行选择）；终端 - 自动启动 = 直接进入终端并拉起所选 Agent；ACP 会话在 issue 主窗口以对话视图运行'}
      >
        {showFollowOption && <MenuItem value="">跟随工作空间</MenuItem>}
        <MenuItem value="none">不执行任何操作</MenuItem>
        <MenuItem value="terminal-manual">自动打开终端</MenuItem>
        <MenuItem value="terminal-auto">终端 - 自动启动 Agent</MenuItem>
        <MenuItem value="acp">ACP 方式启动 Agent</MenuItem>
      </TextField>
      {mode === 'terminal-auto' && (
        <TextField
          select
          label="终端 Agent"
          value={autoCommand}
          onChange={e => onAutoCommandChange(e.target.value)}
          fullWidth
          disabled={disabled}
          helperText="选中 issue 打开主终端时直接运行的 CLI Agent（无 shell 中转）"
        >
          {TERMINAL_AGENT_OPTIONS.map(opt => (
            <MenuItem key={opt.value} value={opt.value}>{opt.label}</MenuItem>
          ))}
        </TextField>
      )}
      {mode === 'acp' && (
        <>
          <TextField
            select
            label="ACP Agent"
            value={effectiveAgentCode}
            onChange={e => onAgentCodeChange(e.target.value)}
            fullWidth
            disabled={disabled || acpAgentOptions.length === 0}
            helperText="ACP 会话驱动的编码 agent（可选面来自内嵌 agent 目录，随目录升级自动扩展）"
          >
            {acpAgentOptions.map(opt => (
              <MenuItem key={opt.value} value={opt.value}>{opt.label}</MenuItem>
            ))}
            {/* 目录已不含的遗留值：禁用项显式呈现，保存不静默改写。 */}
            {effectiveAgentCode !== '' && !acpAgentOptions.some(opt => opt.value === effectiveAgentCode) && (
              <MenuItem value={effectiveAgentCode} disabled>
                {`${effectiveAgentCode}（目录未启用）`}
              </MenuItem>
            )}
          </TextField>
          <TextField
            select
            label="执行模式"
            value={permissionMode}
            onChange={e => onPermissionModeChange(e.target.value as NonNullable<WorkspaceLaunchSettings['permissionMode']>)}
            fullWidth
            disabled={disabled}
            helperText="需要审批 = 敏感操作推送审批卡人工确认；自动放行 = 免审批直接执行"
          >
            <MenuItem value="acceptEdits">需要审批</MenuItem>
            <MenuItem value="bypassPermissions">自动放行</MenuItem>
          </TextField>
        </>
      )}
    </>
  );
}
