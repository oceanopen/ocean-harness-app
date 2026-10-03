package enums

import "database/sql/driver"

// AgentCode ACP 会话 agent 编码映射；取值域 SSOT = catalog enabled 条目 code，取记录走 agentcatalog.GetAgentCatalogInfoByCode。
type AgentCode string

// AGENT_CODE_CLAUDE_ACP Claude Code（一期唯一启用条目）。
const AGENT_CODE_CLAUDE_ACP AgentCode = "claude-acp"

// Value 实现 driver.Valuer（gen 的 field.Field 泛型约束要求）。与 StateCode/Priority 的
// 静态值域校验不同：agent_code 取值域是 catalog enabled 条目（运行时动态），数据层不
// 校验——空串合法（= 无活跃会话的占位），非空透传（写入方 resolveSessionConfig 已对
// catalog 校验）。
func (a AgentCode) Value() (driver.Value, error) {
	return string(a), nil
}
