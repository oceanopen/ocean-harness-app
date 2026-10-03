package main

import (
	"gorm.io/gen"
)

// GenModelAcpSession 注册 ACP 会话域绑定表（t_issue_acp_sessions），生成对应 DO（PO 层）。
//
// 本表无 JSON 列；agent_code 列走 enums.AgentCode（开放命名类型，取值域 SSOT = catalog
// enabled 条目 code）；acp_session_id 为运行时锚（空串 = 无活跃会话），由 acpsession
// 会话域读写，锚点语义对齐 t_im_bot_conversations.claude_session_id。
func GenModelAcpSession() {
	binding := G.GenerateModelAs("t_issue_acp_sessions", "IssueAcpSession",
		gen.FieldType("agent_code", "enums.AgentCode"),
	)
	G.ApplyBasic(binding)
}
