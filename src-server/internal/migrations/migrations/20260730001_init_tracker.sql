-- +goose Up
-- 工作空间 / 项目 / Issue 管理：MVP 6 张业务表。
-- 命名格式：YYYYMMDD + 三位序号 + _name.sql；启动时 goose 自动向前迁移（仅 Up，见 initialize/migrate.go）。
--
-- 约定：
--   - 业务表统一 t_ 前缀，且表名带「所属关系」前缀：顶级 t_workspaces 保持，子表以直接父单数作前缀
--     （t_workspace_projects / t_project_states / t_project_issues / t_workspace_labels / t_issue_labels）；
--   - 主键统一自增 INTEGER；
--   - 公共字段 created_at/updated_at（DATETIME，gorm 自动维护）+ deleted_at（DATETIME，软删除 = gorm.DeletedAt）；
--   - 唯一索引 udx_ 前缀且【全局唯一】（不带 WHERE deleted_at IS NULL）——已删除记录仍占用唯一键，
--     配合 service 层「恢复式 upsert」（同唯一键已删除记录 → 恢复并重置业务字段，保留 id + created_at）；
--     命名 udx_{表名去t_}_{列名...}（SQLite 索引名 schema 级全局唯一，带表名段避免跨表重名）；
--   - 普通索引：数据量较小，本期暂不建，后续按查询热点按 idx_{表名去t_}_{列名} 追加；
--   - 【无 DB 外键约束】表间不建 FOREIGN KEY，跨表关联一律通过 SQL JOIN 或应用层组装查询；
--     数据级联清理（如删 workspace 连带清其下 project/state/issue/label）由 service 层手动处理。

-- t_workspaces：顶层容器（个人可建多个，如「个人 / 工作 / 开源」）。
CREATE TABLE t_workspaces (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT     NOT NULL,
    slug        TEXT     NOT NULL,
    description TEXT     NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    deleted_at  DATETIME
);
CREATE UNIQUE INDEX udx_workspaces_slug ON t_workspaces (slug);

-- t_workspace_projects：项目，所属 workspace。identifier 为项目短码（大写，如 PLN），同 workspace 内唯一，用于 issue key「PLN-1」。
-- default_state_id 逻辑指向 t_project_states.id，但不建 DB 外键。
CREATE TABLE t_workspace_projects (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id     INTEGER  NOT NULL,
    name             TEXT     NOT NULL,
    identifier       TEXT     NOT NULL,
    description      TEXT     NOT NULL DEFAULT '',
    emoji            TEXT     NOT NULL DEFAULT '',
    default_state_id INTEGER,
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    deleted_at       DATETIME
);
CREATE UNIQUE INDEX udx_workspace_projects_workspace_id_identifier ON t_workspace_projects (workspace_id, identifier);

-- t_project_states：issue 状态，所属 project。新建项目自动种 5 个默认状态（见 service.DefaultStates）。
-- state_group ∈ backlog/unstarted/started/completed/cancelled（completed 组触发 issue.completed_at）。
CREATE TABLE t_project_states (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id   INTEGER  NOT NULL,
    workspace_id INTEGER  NOT NULL,
    name         TEXT     NOT NULL,
    color        TEXT     NOT NULL,
    slug         TEXT     NOT NULL DEFAULT '',
    state_group  TEXT     NOT NULL DEFAULT 'backlog',
    sort_order   REAL     NOT NULL DEFAULT 65535,
    is_default   INTEGER  NOT NULL DEFAULT 0,
    is_triage    INTEGER  NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME
);

-- t_project_issues：核心工作项，所属 project。
-- sequence_id 项目内自增（组成 key）；sort_order 列表排序权重；priority 五级枚举。
-- state_id / parent_id 逻辑指向他表，但不建 DB 外键。
CREATE TABLE t_project_issues (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id   INTEGER  NOT NULL,
    workspace_id INTEGER  NOT NULL,
    name         TEXT     NOT NULL,
    description  TEXT     NOT NULL DEFAULT '',
    state_id     INTEGER,
    priority     TEXT     NOT NULL DEFAULT 'none',
    sequence_id  INTEGER  NOT NULL DEFAULT 1,
    sort_order   REAL     NOT NULL DEFAULT 65535,
    parent_id    INTEGER,
    start_date   TEXT,
    target_date  TEXT,
    completed_at DATETIME,
    is_draft     INTEGER  NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME
);

-- t_workspace_labels：标签，所属 workspace（project_id 可空表 workspace 级，非空表 project 级）。
CREATE TABLE t_workspace_labels (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER  NOT NULL,
    project_id   INTEGER,
    name         TEXT     NOT NULL,
    color        TEXT     NOT NULL DEFAULT '',
    description  TEXT     NOT NULL DEFAULT '',
    sort_order   REAL     NOT NULL DEFAULT 65535,
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME
);

-- t_issue_labels：issue ↔ label 多对多关联，所属 issue。
CREATE TABLE t_issue_labels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id   INTEGER  NOT NULL,
    label_id   INTEGER  NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    deleted_at DATETIME
);
CREATE UNIQUE INDEX udx_issue_labels_issue_id_label_id ON t_issue_labels (issue_id, label_id);
