package main

import (
	"gorm.io/gen"
	"gorm.io/gen/field"
)

/*
 * GenModelTracker 注册 tracker 模块业务表及其父子关联，生成对应 DO（PO 层）。
 * 结构名取「单数、无 t_ 前缀」；表间无 DB 外键，此处 HasMany 为 gorm/gen 逻辑关联（仅生成结构体字段 + Preload）。
 * HasMany 在父表选项引用子表模板，故按「叶子优先」顺序创建。
 */
func GenModelTracker() {
	// 类型：project 级预定义，任务经 t_project_tasks.type_id 单值引用。
	projectType := G.GenerateModelAs("t_project_types", "ProjectType",
		gen.FieldType("project_id", "string"), // 所属项目 uuid
	)
	// 项目 ↔ 本地仓库 多对多中间表：无关联关系，service 层手动 JOIN 查询（与表间无 DB 外键约定一致）。
	projectLocalRepository := G.GenerateModelAs("t_project_local_repositories", "ProjectLocalRepository",
		gen.FieldType("project_id", "string"),
	)
	// 任务 ↔ 本地仓库+分支 关联表：task_id 同为 TEXT uuid；service 层批量组装。
	taskLocalRepository := G.GenerateModelAs("t_task_local_repositories", "TaskLocalRepository",
		gen.FieldType("task_id", "string"),
	)

	task := G.GenerateModelAs("t_project_tasks", "ProjectTask",
		// 任务主键为 TEXT uuid 字符串（任务目录名）；project_id 同为 uuid 文本。
		gen.FieldType("id", "string"),
		gen.FieldType("project_id", "string"), // 所属项目 uuid
		gen.FieldRelate(field.HasMany, "TaskLocalRepositoryList", taskLocalRepository, &field.RelateConfig{
			RelateSlicePointer: true,
			GORMTag: field.GormTag{
				"foreignKey": []string{"TaskID"}, // 子表 t_task_local_repositories.task_id
				"references": []string{"ID"},     // 父表 t_project_tasks.id
			},
			JSONTag: "taskLocalRepositoryList,omitempty",
		}),
	)

	project := G.GenerateModelAs("t_projects", "Project",
		// 主键为 TEXT uuid 字符串（Create 时生成 uuid v7，时间有序）。
		gen.FieldType("id", "string"),
		gen.FieldRelate(field.HasMany, "ProjectTaskList", task, &field.RelateConfig{
			RelateSlicePointer: true,
			GORMTag: field.GormTag{
				"foreignKey": []string{"ProjectID"}, // 子表 t_project_tasks.project_id
				"references": []string{"ID"},        // 父表 t_projects.id
			},
			JSONTag: "projectTaskList,omitempty",
		}),
		gen.FieldRelate(field.HasMany, "ProjectTypeList", projectType, &field.RelateConfig{
			RelateSlicePointer: true,
			GORMTag: field.GormTag{
				"foreignKey": []string{"ProjectID"}, // 子表 t_project_types.project_id
				"references": []string{"ID"},        // 父表 t_projects.id
			},
			JSONTag: "projectTypeList,omitempty",
		}),
	)

	workspace := G.GenerateModelAs("t_workspaces", "Workspace",
		// 主键为 TEXT uuid 字符串（Create 时生成 uuid v7，时间有序）；工作空间无子表关联（去任务/仓库化）。
		gen.FieldType("id", "string"),
	)

	G.ApplyBasic(workspace, project, task, projectType, projectLocalRepository, taskLocalRepository)
}
