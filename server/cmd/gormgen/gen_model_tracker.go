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
	// 类型：workspace 级预定义，issue 经 t_workspace_issues.type_id 单值引用。
	workspaceType := G.GenerateModelAs("t_workspace_types", "WorkspaceType",
		gen.FieldType("workspace_id", "string"), // 所属工作空间 uuid
	)
	// 工作空间 ↔ 本地仓库 多对多中间表：无关联关系，service 层手动 JOIN 查询（与表间无 DB 外键约定一致）。
	// workspace_id 为工作空间主键 uuid 文本。
	workspaceLocalRepository := G.GenerateModelAs("t_workspace_local_repositories", "WorkspaceLocalRepository",
		gen.FieldType("workspace_id", "string"),
	)
	// issue ↔ 本地仓库+分支 关联表：issue_id 同为 TEXT uuid；service 层批量组装。
	issueLocalRepository := G.GenerateModelAs("t_issue_local_repositories", "IssueLocalRepository",
		gen.FieldType("issue_id", "string"),
	)

	issue := G.GenerateModelAs("t_workspace_issues", "WorkspaceIssue",
		// issue 主键为 TEXT uuid 字符串（与 claude session_id 同格式）；workspace_id 同为 uuid 文本。
		gen.FieldType("id", "string"),
		gen.FieldType("workspace_id", "string"), // 所属工作空间 uuid
		gen.FieldType("state_code", "enums.StateCode"),
		// completed_at 用 *time.Time 指针：未完成=nil（写 NULL）/ 完成=&time，便于 Save 统一处理可空语义。
		gen.FieldType("completed_at", "*time.Time"),
		gen.FieldRelate(field.HasMany, "IssueLocalRepositoryList", issueLocalRepository, &field.RelateConfig{
			RelateSlicePointer: true,
			GORMTag: field.GormTag{
				"foreignKey": []string{"IssueID"}, // 子表 t_issue_local_repositories.issue_id
				"references": []string{"ID"},      // 父表 t_workspace_issues.id
			},
			JSONTag: "issueLocalRepositoryList,omitempty",
		}),
	)

	workspace := G.GenerateModelAs("t_workspaces", "Workspace",
		// 主键为 TEXT uuid 字符串（Create 时生成 uuid v7，时间有序）。
		gen.FieldType("id", "string"),
		gen.FieldRelate(field.HasMany, "WorkspaceIssueList", issue, &field.RelateConfig{
			RelateSlicePointer: true,
			GORMTag: field.GormTag{
				"foreignKey": []string{"WorkspaceID"}, // 子表 t_workspace_issues.workspace_id
				"references": []string{"ID"},          // 父表 t_workspaces.id
			},
			JSONTag: "workspaceIssueList,omitempty",
		}),
		gen.FieldRelate(field.HasMany, "WorkspaceTypeList", workspaceType, &field.RelateConfig{
			RelateSlicePointer: true,
			GORMTag: field.GormTag{
				"foreignKey": []string{"WorkspaceID"}, // 子表 t_workspace_types.workspace_id
				"references": []string{"ID"},          // 父表 t_workspaces.id
			},
			JSONTag: "workspaceTypeList,omitempty",
		}),
	)

	G.ApplyBasic(workspace, issue, workspaceType, workspaceLocalRepository, issueLocalRepository)
}
