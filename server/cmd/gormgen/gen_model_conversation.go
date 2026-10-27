package main

import (
	"gorm.io/gen"
)

/*
 * GenModelConversation 注册 agent 会话第一性实体表（t_agent_conversations，唯一会话源），
 * 生成对应 DO（PO 层）。owner_type/mode/claude_session_status 为纯枚举强类型，agent_code 走
 * catalog 开放枚举；bot_* 三列 = bot 渠道归属。无关联注册。
 */
func GenModelConversation() {
	conversation := G.GenerateModelAs("t_agent_conversations", "Conversation",
		gen.FieldType("id", "string"),
		gen.FieldType("owner_id", "string"),
		gen.FieldType("owner_type", "enums.ConversationOwnerType"),
		gen.FieldType("mode", "enums.ConversationMode"),
		gen.FieldType("agent_code", "enums.AgentCode"),
		gen.FieldType("claude_session_status", "enums.ClaudeSessionStatus"),
		gen.FieldType("bot_id", "string"),
		gen.FieldType("bot_channel", "enums.Channel"),
		gen.FieldType("bot_conversation_key", "string"),
		// 可空时间指针：未开场/未完成回合 = nil（写 NULL），对齐可空时间列约定。
		gen.FieldType("last_message_at", "*time.Time"),
	)
	G.ApplyBasic(conversation)
}
