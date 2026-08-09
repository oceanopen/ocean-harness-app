package types

import (
	"we-claude-terminal/go-server/internal/dal/model"
)

// IssueWorktree 对应 /api/tracker/issueWorktree 命名空间下各 action 的入参/响应（issue 开发流程 worktree 元数据）。
// P1 桩（Module G）：createWorktree 派生假 worktree 路径写记录（不真调 git worktree add）；真实现见 worktree_term.md §6。

// IssueWorktreeCreateWorktreeRequest POST /api/tracker/issueWorktree/createWorktree 入参。
// worktreeBranch 为开发分支名；baseBranch 为基准分支（如 origin/main，P1 桩仅存档，真实现用作 git worktree add 的基准）。
type IssueWorktreeCreateWorktreeRequest struct {
	IssueID           int    `json:"issueId" binding:"required"`
	LocalRepositoryID int    `json:"localRepositoryId" binding:"required"`
	BaseBranch        string `json:"baseBranch"`
	WorktreeBranch    string `json:"worktreeBranch" binding:"required"`
}

// IssueWorktreeRemoveWorktreeRequest POST /api/tracker/issueWorktree/removeWorktree 入参。
// 前置：前端已调 pty_stop_for_worktree 停 PTY（worktree_term.md §9.3 两阶段编排）。
type IssueWorktreeRemoveWorktreeRequest struct {
	WorktreeID string `json:"worktreeId" binding:"required"`
}

// IssueWorktreeGetListRequest POST /api/tracker/issueWorktree/getList 入参。
type IssueWorktreeGetListRequest struct {
	IssueID int `json:"issueId" binding:"required"`
}

// IssueWorktreeResponseData worktree 响应：嵌入 DO（JSON 平铺 worktree 字段）。
// status 为 typed 枚举（active/stale/removed）；worktreeId/worktreePath 为前端展示与 removeWorktree 调用所需的共享键。
type IssueWorktreeResponseData struct {
	*model.IssueWorktree
}
