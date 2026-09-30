import type { WorkspaceLaunchSettings } from '@src/services';

/** 一期终端启动 Agent 选项（launch_settings.autoCommand，terminal-auto 档消费；新 CLI 直启在此扩展）。 */
export const TERMINAL_AGENT_OPTIONS = [
  { value: 'claude', label: 'Claude Code' },
] as const;

/** 一期 ACP 会话 Agent 选项（launch_settings.agentCode；T1.2 catalog 落地后切换数据源）。 */
export const ACP_AGENT_OPTIONS = [
  { value: 'claude-acp', label: 'Claude Code' },
  { value: 'codex', label: 'Codex' },
  { value: 'opencode', label: 'OpenCode' },
  { value: 'pi', label: 'Pi' },
] as const;

/** 终端启动 Agent 展示名（autoCommand → 产品名；未知值原样展示）。 */
export function terminalAgentLabel(autoCommand: string | undefined): string {
  if (autoCommand === 'claude' || autoCommand == null) {
    return 'Claude Code';
  }
  return autoCommand;
}

/** ACP 会话 Agent 展示名（agentCode 枚举码 → 产品名）。 */
export function acpAgentLabel(code: NonNullable<WorkspaceLaunchSettings['agentCode']> | undefined): string {
  switch (code) {
    case 'codex':
      return 'Codex';
    case 'opencode':
      return 'OpenCode';
    case 'pi':
      return 'Pi';
    default:
      return 'Claude Code';
  }
}

/**
 * 启动设置字段级合并（T0.4）：override（issue 级）显式写过的键覆盖 base（workspace 级），
 * 未覆盖键回落 base。两侧均可 undefined（未配置）。mode 显式 'none' 是有效覆盖值（≠未覆盖）。
 */
export function mergeLaunchSettings(
  base: WorkspaceLaunchSettings | undefined,
  override: WorkspaceLaunchSettings | undefined,
): WorkspaceLaunchSettings | undefined {
  if (override == null) {
    return base;
  }
  if (base == null) {
    return override;
  }
  return {
    mode: override.mode ?? base.mode,
    agentCode: override.agentCode ?? base.agentCode,
    autoCommand: override.autoCommand ?? base.autoCommand,
    permissionMode: override.permissionMode ?? base.permissionMode,
  };
}
