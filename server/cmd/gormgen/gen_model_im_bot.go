package main

import (
	"gorm.io/gen"
)

/*
 * GenModelImBot 注册 IM 渠道数字人 bot 表（t_im_bots），生成对应 DO（PO 层）。JSON 列
 * （credential/access_policy）DO 字段类型保持 string，序列化归 service/core 层（不在 DO 层
 * 引入自定义 Scanner/Valuer，避免 model ↔ types 循环依赖）。
 */
func GenModelImBot() {
	imBot := G.GenerateModelAs("t_im_bots", "ImBot",
		// 主键与归属列均为 TEXT uuid 字符串；归属三列空串 = 未配置哨兵（conversation_type ∈ {workspace,project}，值域校验归 service）。
		gen.FieldType("id", "string"),
		gen.FieldType("workspace_id", "string"),
		gen.FieldType("project_id", "string"),
		gen.FieldType("task_id", "string"),
		gen.FieldType("conversation_type", "enums.ConversationType"),
		gen.FieldType("channel", "enums.Channel"),
		// 启停开关走公共是否型映射 enums.YesNo（"Y"/"N"，对齐 Rust 侧 app_config 词汇）。
		gen.FieldType("enabled", "enums.YesNo"),
	)
	G.ApplyBasic(imBot)
}
