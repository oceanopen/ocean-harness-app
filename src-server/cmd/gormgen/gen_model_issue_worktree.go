package main

import (
	"gorm.io/gen"
)

// GenModelIssueWorktree 注册 issue 开发流程 worktree 元数据表（docs/worktree_term.md §5.1），生成对应 DO（PO 层）。
// status 为 typed 枚举（active/stale/removed）；
// 无 DB 外键，issue/仓库合法性由 service 层校验（P1 桩暂不校验）。
// 结构名取「单数、无 t_ 前缀」，与其他表一致。
func GenModelIssueWorktree() {
	worktree := G.GenerateModelAs("t_issue_worktrees", "IssueWorktree",
		gen.FieldType("status", "enums.IssueWorktreeStatus"),
	)
	G.ApplyBasic(worktree)
}
