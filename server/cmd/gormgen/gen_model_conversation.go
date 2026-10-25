package main

import (
	"gorm.io/gen"
)

/*
 * GenModelConversation 注册会话第一性实体表（t_conversations），生成对应 DO（PO 层）。
 * owner_type/mode 为纯枚举强类型；agent_code 走 catalog 开放枚举；claude_session_id 为终端/ACP
 * 统一锚（'' = 未锚定）；parent_id '' = 主会话。无 JSON 列、无关联注册。
 */
func GenModelConversation() {
	conversation := G.GenerateModelAs("t_conversations", "Conversation",
		gen.FieldType("id", "string"),
		gen.FieldType("owner_id", "string"),
		gen.FieldType("owner_type", "enums.ConversationOwnerType"),
		gen.FieldType("mode", "enums.ConversationMode"),
		gen.FieldType("agent_code", "enums.AgentCode"),
	)
	G.ApplyBasic(conversation)
}
