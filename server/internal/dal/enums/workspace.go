package enums

// AgentCode ACP 模式 agent 枚举码（t_workspaces.launch_settings JSON 内部字段）。
// 与表列枚举的差异：launch_settings 整列为 TEXT（service 层序列化），本类型无独立写库
// 路径，故不实现 driver.Valuer，值域校验经 DTO binding oneof（types.WorkspaceLaunchSettings）。
// 一期取值域固定四值（P4）；T1.2 catalog 落地后取值域 SSOT 移交 catalog，校验同步放宽。
type AgentCode string

const (
	AgentClaudeCode AgentCode = "claude-acp"
	AgentCodex      AgentCode = "codex"
	AgentOpencode   AgentCode = "opencode"
	AgentPi         AgentCode = "pi"
)
