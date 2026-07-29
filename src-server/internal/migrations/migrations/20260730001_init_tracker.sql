-- +goose Up
-- 工作空间 / 项目 / Issue 管理：MVP 6 张业务表。
-- 命名格式：YYYYMMDD + 三位序号 + _name.sql；启动时 goose 自动向前迁移（仅 Up，见 initialize/migrate.go）。
--
-- 约定：
--   - 主键统一自增 INTEGER；
--   - 公共字段 created_at/updated_at（DATETIME，gorm 自动维护）+ deleted_at（DATETIME，软删除 = gorm.DeletedAt）；
--   - 软删除唯一性用部分唯一索引「WHERE deleted_at IS NULL」保证「未删除记录间唯一」；
--   - 外键声明级联意图；软删除级联（删 workspace 时连带软删 project/state/issue）由 service 层手动处理，
--     硬级联（ON DELETE CASCADE）仅用于真删场景，需连接初始化时开启 PRAGMA foreign_keys=ON（见 sqlite 初始化 DSN）。

-- workspaces：顶层容器（个人可建多个，如「个人 / 工作 / 开源」）。
CREATE TABLE workspaces (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT     NOT NULL,
    slug        TEXT     NOT NULL,
    description TEXT     NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    deleted_at  DATETIME
);
CREATE UNIQUE INDEX idx_workspaces_slug ON workspaces (slug) WHERE deleted_at IS NULL;

-- projects：identifier 为项目短码（大写，如 PLN），同 workspace 内唯一，用于 issue key「PLN-1」。
CREATE TABLE projects (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id     INTEGER  NOT NULL,
    name             TEXT     NOT NULL,
    identifier       TEXT     NOT NULL,
    description      TEXT     NOT NULL DEFAULT '',
    emoji            TEXT     NOT NULL DEFAULT '',
    default_state_id INTEGER,
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    deleted_at       DATETIME,
    FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_projects_workspace_identifier ON projects (workspace_id, identifier) WHERE deleted_at IS NULL;
CREATE INDEX idx_projects_workspace ON projects (workspace_id);

-- states：issue 状态。新建项目自动种 5 个默认状态（见 service.DefaultStates）。
-- state_group ∈ backlog/unstarted/started/completed/cancelled（completed 组触发 issue.completed_at）。
CREATE TABLE states (
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
    deleted_at   DATETIME,
    FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE
);
CREATE INDEX idx_states_project ON states (project_id);

-- issues：核心工作项。
-- sequence_id 项目内自增（组成 key）；sort_order 列表排序权重；priority 五级枚举。
CREATE TABLE issues (
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
    deleted_at   DATETIME,
    FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE,
    FOREIGN KEY (state_id) REFERENCES states (id) ON DELETE SET NULL,
    FOREIGN KEY (parent_id) REFERENCES issues (id) ON DELETE SET NULL
);
CREATE INDEX idx_issues_project ON issues (project_id);
CREATE INDEX idx_issues_state ON issues (state_id);

-- labels：标签，可挂 workspace 级（project_id 为空）或 project 级。
CREATE TABLE labels (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER  NOT NULL,
    project_id   INTEGER,
    name         TEXT     NOT NULL,
    color        TEXT     NOT NULL DEFAULT '',
    description  TEXT     NOT NULL DEFAULT '',
    sort_order   REAL     NOT NULL DEFAULT 65535,
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME,
    FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE
);

-- issue_labels：issue ↔ label 多对多关联。
CREATE TABLE issue_labels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id   INTEGER  NOT NULL,
    label_id   INTEGER  NOT NULL,
    created_at DATETIME NOT NULL,
    FOREIGN KEY (issue_id) REFERENCES issues (id) ON DELETE CASCADE,
    FOREIGN KEY (label_id) REFERENCES labels (id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_issue_labels_unique ON issue_labels (issue_id, label_id);
