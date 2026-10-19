package main

import (
	"gorm.io/gen"
)

/*
 * GenModelImBot 注册 IM 渠道数字人 bot 域两张表，生成对应 DO（PO 层）。
 * JSON 列（credential/access_policy/seen_message_ids）DO 字段类型保持 string（gen 默认 TEXT → string），由 service/core 层负责与结构化类型的序列化（同 local_repository 的 sub_dir_list 约定，不在 DO 层引入自定义 Scanner/Valuer，避免 model ↔ types 循环依赖）。
 * 无 HasMany 关联：conversations 由核心层按 bot_id 直接查询（runtime 高频路径，不需要 Preload）。
 */
func GenModelImBot() {
	imBot := G.GenerateModelAs("t_im_bots", "ImBot",
		// 主键为 TEXT uuid 字符串（Create 时生成 uuid v7）；workspace_id 同为 uuid（''=未选择）。
		gen.FieldType("id", "string"),
		gen.FieldType("workspace_id", "string"),
		gen.FieldType("channel", "enums.Channel"),
		// 启停开关走公共是否型映射 enums.YesNo（"Y"/"N"，对齐 Rust 侧 app_config 词汇）。
		gen.FieldType("enabled", "enums.YesNo"),
	)
	conversation := G.GenerateModelAs("t_im_bot_conversations", "ImBotConversation",
		// 主键与所属 bot 均为 TEXT uuid 字符串（bot 域整域 uuid 化）。
		gen.FieldType("id", "string"),
		gen.FieldType("bot_id", "string"),
		// 可空时间指针：未开场/未完成回合 = nil（写 NULL），语义同 t_project_issues.completed_at。
		gen.FieldType("last_message_at", "*time.Time"),
	)
	G.ApplyBasic(imBot, conversation)
}
