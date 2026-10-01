package enums

// AgentCode ACP 会话 agent 编码映射；取值域 SSOT = catalog enabled 条目 code，取记录走 agentcatalog.GetAgentCatalogInfoByCode。
type AgentCode string

// AGENT_CODE_CLAUDE_ACP Claude Code（一期唯一启用条目）。
const AGENT_CODE_CLAUDE_ACP AgentCode = "claude-acp"
