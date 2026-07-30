package main

import (
	"gorm.io/gen"
	"gorm.io/gen/field"
)

// GenModelTracker 注册 tracker 模块 6 张业务表及其父子关联，生成对应 DO（PO 层）。
// 结构名取「单数、无 t_ 前缀」；表间无 DB 外键，此处 HasMany 为 gorm/gen 逻辑关联（仅生成结构体字段 + Preload）。
// HasMany 在父表选项引用子表模板，故按「叶子优先」顺序创建。
func GenModelTracker() {
	state := G.GenerateModelAs("t_project_states", "ProjectState")
	issueLabel := G.GenerateModelAs("t_issue_labels", "IssueLabel")
	label := G.GenerateModelAs("t_workspace_labels", "WorkspaceLabel")

	issue := G.GenerateModelAs("t_project_issues", "ProjectIssue",
		gen.FieldRelate(field.HasMany, "IssueLabelList", issueLabel, &field.RelateConfig{
			RelateSlicePointer: true,
			GORMTag: field.GormTag{
				"foreignKey": []string{"IssueID"}, // 子表 t_issue_labels.issue_id
				"references": []string{"ID"},      // 父表 t_project_issues.id
			},
			JSONTag: "issueLabelList,omitempty",
		}),
	)

	project := G.GenerateModelAs("t_workspace_projects", "WorkspaceProject",
		gen.FieldRelate(field.HasMany, "ProjectStateList", state, &field.RelateConfig{
			RelateSlicePointer: true,
			GORMTag: field.GormTag{
				"foreignKey": []string{"ProjectID"}, // 子表 t_project_states.project_id
				"references": []string{"ID"},        // 父表 t_workspace_projects.id
			},
			JSONTag: "projectStateList,omitempty",
		}),
		gen.FieldRelate(field.HasMany, "ProjectIssueList", issue, &field.RelateConfig{
			RelateSlicePointer: true,
			GORMTag: field.GormTag{
				"foreignKey": []string{"ProjectID"}, // 子表 t_project_issues.project_id
				"references": []string{"ID"},        // 父表 t_workspace_projects.id
			},
			JSONTag: "projectIssueList,omitempty",
		}),
	)

	workspace := G.GenerateModelAs("t_workspaces", "Workspace",
		gen.FieldRelate(field.HasMany, "WorkspaceProjectList", project, &field.RelateConfig{
			RelateSlicePointer: true,
			GORMTag: field.GormTag{
				"foreignKey": []string{"WorkspaceID"}, // 子表 t_workspace_projects.workspace_id
				"references": []string{"ID"},          // 父表 t_workspaces.id
			},
			JSONTag: "workspaceProjectList,omitempty",
		}),
		gen.FieldRelate(field.HasMany, "WorkspaceLabelList", label, &field.RelateConfig{
			RelateSlicePointer: true,
			GORMTag: field.GormTag{
				"foreignKey": []string{"WorkspaceID"}, // 子表 t_workspace_labels.workspace_id
				"references": []string{"ID"},          // 父表 t_workspaces.id
			},
			JSONTag: "workspaceLabelList,omitempty",
		}),
	)

	G.ApplyBasic(workspace, project, state, issue, label, issueLabel)
}
