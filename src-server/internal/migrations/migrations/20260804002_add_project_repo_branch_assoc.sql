-- +goose Up
-- 迭代2：项目 ↔ 本地仓库 多对多关联 + issue ↔ 指定分支 关联。

-- 项目 ↔ 本地仓库 中间表（多对多）。
-- 命名沿用 t_issue_labels 范式：t_<父实体单数>_<子实体复数>（project 即 workspace_project，与 t_project_states / t_project_issues 一致）。
-- 无 deleted_at：随本地仓库物理删除而硬删关联记录（见 local_repository service.Delete 事务级联清理）。
-- 无 DB 外键：workspace_project_id / local_repository_id 均为逻辑外键，关联合法性由 service 层校验。
CREATE TABLE t_project_local_repositories (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_project_id INTEGER NOT NULL,
    local_repository_id  INTEGER NOT NULL,
    created_at           DATETIME NOT NULL,
    updated_at           DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_project_local_repositories_pid_lrid
    ON t_project_local_repositories (workspace_project_id, local_repository_id);

-- issue 关联指定分支：
ALTER TABLE t_project_issues ADD COLUMN local_repository_id INTEGER;
ALTER TABLE t_project_issues ADD COLUMN repository_branch TEXT NOT NULL DEFAULT '';
