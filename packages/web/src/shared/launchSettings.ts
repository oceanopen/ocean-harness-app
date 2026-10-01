import type { AgentCatalogEntry, WorkspaceLaunchSettings } from '@src/services';
import type { AgentCode } from '@src/shared/agentCode';

/** 一期终端启动 Agent 选项（launch_settings.autoCommand，terminal-auto 档消费；新 CLI 直启在此扩展）。 */
export const TERMINAL_AGENT_OPTIONS = [
  { value: 'claude', label: 'Claude Code' },
] as const;

/** 终端启动 Agent 展示名（autoCommand → 产品名；未知值原样展示）。 */
export function terminalAgentLabel(autoCommand: string | undefined): string {
  if (autoCommand === 'claude' || autoCommand == null) {
    return 'Claude Code';
  }
  return autoCommand;
}

/** ACP 会话 Agent 展示名（agentCode → 条目 label；未配置取首个 enabled，遗留值原样展示）。 */
export function acpAgentLabel(entries: AgentCatalogEntry[] | undefined, code: AgentCode | undefined): string {
  if (code != null && code !== '') {
    return entries?.find(entry => entry.code === code)?.label ?? code;
  }
  return entries?.find(entry => entry.enabled)?.label ?? 'Claude Code';
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
