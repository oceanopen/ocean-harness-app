package service

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/query"
	"ocean-harness/server/internal/global"
)

/*
 * 浏览器工作空间解析（browser 域共享登录态的 DB 查询供给，main 装配注入）：issue 不存在
 * 或未挂工作空间按「无共享」空串处理——对会话编排属正常分支而非错误。
 */

// ResolveIssueWorkspaceID 解析 issue 归属工作空间 id（不存在/未挂 = 空串）。
func ResolveIssueWorkspaceID(ctx context.Context, issueID string) (string, error) {
	q := query.Use(global.SqliteDB)
	issue, err := q.ProjectIssue.WithContext(ctx).Where(q.ProjectIssue.ID.Eq(issueID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return issue.WorkspaceID, nil
}
