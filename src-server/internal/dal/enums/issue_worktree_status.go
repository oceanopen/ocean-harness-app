package enums

import (
	"database/sql/driver"
	"fmt"
)

// IssueWorktreeStatus worktree 物理生命周期状态（t_issue_worktrees.status）。取值 active/stale/removed。
// 与 issue 的开发阶段（wt_init/developing/pr_open/cleanup，由 stateId 表达）正交：本枚举只描述 worktree 自身物理状态。
type IssueWorktreeStatus string

const (
	ISSUE_WORKTREE_STATUS_ACTIVE  IssueWorktreeStatus = "active"  // 在用
	ISSUE_WORKTREE_STATUS_STALE   IssueWorktreeStatus = "stale"   // 路径消失（reconcile 标记）
	ISSUE_WORKTREE_STATUS_REMOVED IssueWorktreeStatus = "removed" // 已软删（清理完成/取消）
)

// Value 实现 driver.Valuer：写库时校验合法值并返回底层 string；非法值返回错误，由 gorm 在 INSERT/UPDATE 时触发。
func (s IssueWorktreeStatus) Value() (driver.Value, error) {
	switch s {
	case
		ISSUE_WORKTREE_STATUS_ACTIVE,
		ISSUE_WORKTREE_STATUS_STALE,
		ISSUE_WORKTREE_STATUS_REMOVED:
		return string(s), nil
	default:
		return nil, fmt.Errorf("invalid IssueWorktreeStatus: %v", s)
	}
}
