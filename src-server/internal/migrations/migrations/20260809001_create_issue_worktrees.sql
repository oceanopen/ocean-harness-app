-- +goose Up
-- t_issue_worktrees：issue 开发流程的 worktree 元数据（docs/worktree_term.md §5.1）。
-- worktree_id = `${localRepositoryId}::${absWorktreePath}`，Go 与 Rust 共享键（前端穿针引线）。
-- 与 issue 既有 repository_branch 正交：本表是「为开发创建的隔离工作区」的物理生命周期（status）；
-- 开发阶段（init/developing/pull_request/cleanup）由 issue.stateId 在 started 组开发步骤子 state 上推进表达，不存本表。
-- P1 桩（Module G）：createWorktree 写假路径记录（不真调 git worktree add）、getList 真查作 worktreePath/worktreeId SSOT；
--   真实现（worktree_term.md §6 阶段 1）补 gitutil.WorktreeAdd/Remove + 路径真派生（§5.3），本表结构不变。
-- 普通索引按项目约定「数据量较小、本期暂不建」不加（worktree_id UNIQUE 已隐式索引）；后续按查询热点 idx_ 追加。
CREATE TABLE t_issue_worktrees (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    worktree_id         TEXT     NOT NULL UNIQUE,        -- ${repoId}::${absPath}，跨端共享键
    issue_id            INTEGER  NOT NULL,               -- t_project_issues.id
    local_repository_id INTEGER  NOT NULL,               -- t_local_repositories.id
    worktree_path       TEXT     NOT NULL,               -- 绝对路径（P1 桩为派生占位）
    worktree_branch     TEXT     NOT NULL,               -- worktree 所在分支
    base_branch         TEXT     NOT NULL DEFAULT '',    -- 创建时的基准分支（如 origin/main）
    status              TEXT     NOT NULL,               -- active|stale|removed（typed enum，无默认值）
    created_at          DATETIME NOT NULL,
    deleted_at          DATETIME                         -- 软删除，与 tracker 域一致
);
