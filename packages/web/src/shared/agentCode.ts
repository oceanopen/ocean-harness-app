/** launch_settings.agentCode 值类型：开放命名 string（对齐后端 enums.AgentCode），取值域 = catalog enabled 条目 code。 */
export type AgentCode = string;

/** 分支判断用映射常量；目录扩展新 agent 不改本表，需分支时才补键。 */
export const AGENT_CODE = {
  claudeAcp: 'claude-acp',
} as const;
