-- +goose Up
-- t_workspaces 加 worktree_root：issue 开发流程 worktree 存放根目录（per-workspace）。
-- CreateWorktree 时 Go 经 issue→workspace 查此字段派生 worktree 落盘路径
-- <worktree_root>/<repoName>/workspace_{wid}-project_{pid}-issue_{iid}；为空报错要求配置。
-- per-workspace（跟着工作空间走）取代原全局 appConfig 方案：worktree 绑定工作空间，
-- 不同工作空间可指定不同存放位置；Go 直接查 DB（本就要查 workspace），无需环境变量/不动 Rust。
ALTER TABLE t_workspaces ADD COLUMN worktree_root TEXT NOT NULL DEFAULT '';
