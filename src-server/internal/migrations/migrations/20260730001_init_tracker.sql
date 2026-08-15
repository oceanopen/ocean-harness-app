-- +goose Up
-- 工作空间 / 项目 / Issue / 本地仓库 管理：基线 8 张业务表（最终态，已合并历次增量迁移：
-- 20260804001 local_repositories、20260804002 项目↔仓库中间表、20260811001 local_repository.default_branch、
-- issue 仓库+分支多选（曾为 issue 表两列/JSON 列，现独立关联表 t_issue_local_repositories））。
-- 命名格式：YYYYMMDD + 三位序号 + _name.sql；启动时 goose 自动向前迁移（仅 Up，见 initialize/migrate.go）。
--
-- 约定：
--   - 业务表统一 t_ 前缀，且表名带「所属关系」前缀：顶级 t_workspaces / t_local_repositories 保持，
--     子表以直接父单数作前缀（t_workspace_projects / t_project_issues /
--     t_workspace_labels / t_issue_labels / t_project_local_repositories）；
--   - 主键统一自增 INTEGER（例外：t_project_issues.id 为 TEXT uuid）；
--   - 公共字段 created_at/updated_at（DATETIME，gorm 自动维护）+ deleted_at（DATETIME，软删除 = gorm.DeletedAt）；
--     t_local_repositories / t_project_local_repositories 无 deleted_at（物理删除）；
--   - 唯一索引 udx_ 前缀且【全局唯一】（不带 WHERE deleted_at IS NULL）——已删除记录仍占用唯一键，
--     配合 service 层「恢复式 upsert」；命名 udx_{表名去t_}_{列名...}（SQLite 索引名 schema 级全局唯一，带表名段避免跨表重名）；
--   - 普通索引：数据量较小，本期暂不建，后续按查询热点按 idx_{表名去t_}_{列名} 追加；
--   - 【无 DB 外键约束】表间不建 FOREIGN KEY，跨表关联一律通过 SQL JOIN 或应用层组装查询；
--     数据级联清理（如删 workspace 连带清其下 project/issue/label）由 service 层手动处理；
--   - typed 枚举列（state_code / is_draft / priority）用 TEXT NOT NULL 无默认值，
--     由代码显式赋值（避免 DEFAULT '' 触发 gorm 零值省略、静默存空串，见记忆 tracker-enum-pattern）。

-- t_workspaces：顶层容器（个人可建多个，如「个人 / 工作 / 开源」）。
CREATE TABLE t_workspaces (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT     NOT NULL,
    slug          TEXT     NOT NULL,
    description   TEXT     NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL,
    deleted_at    DATETIME
);
CREATE UNIQUE INDEX udx_workspaces_slug ON t_workspaces (slug);

-- t_workspace_projects：项目，所属 workspace。允许重名（个人场景靠 id 区分），无短码。
CREATE TABLE t_workspace_projects (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id     INTEGER  NOT NULL,
    name             TEXT     NOT NULL,
    description      TEXT     NOT NULL DEFAULT '',
    emoji            TEXT     NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    deleted_at       DATETIME
);

-- t_project_issues：核心工作项，所属 project。
-- issue 主键 id 为 TEXT uuid 字符串（与 claude session_id 同格式，Create 时由 service 生成 uuid v7；
-- 后续将作为工作空间运行任务目录的唯一标识）；sort_order 列表排序权重；priority 五级枚举。
-- state_code 为固定 5 值 typed 枚举（BACKLOG/TODO/IN_PROGRESS/DONE/CANCELLED，元数据见 enums.StateCatalog，
-- 无 state_id/项目级状态行）；DONE 触发 issue.completed_at。
-- parent_id 逻辑指向 t_project_issues.id，不建 DB 外键。
-- issue 关联的多仓库+分支在独立关联表 t_issue_local_repositories（同 label 关联 t_issue_labels 模式）。
CREATE TABLE t_project_issues (
    id                  TEXT PRIMARY KEY,
    project_id          INTEGER  NOT NULL,
    workspace_id        INTEGER  NOT NULL,
    name                TEXT     NOT NULL,
    description         TEXT     NOT NULL DEFAULT '',
    state_code          TEXT     NOT NULL,
    priority            TEXT     NOT NULL,
    sort_order          REAL     NOT NULL DEFAULT 0,
    parent_id           TEXT,
    start_date          TEXT,
    target_date         TEXT,
    completed_at        DATETIME,
    is_draft            TEXT     NOT NULL,
    created_at          DATETIME NOT NULL,
    updated_at          DATETIME NOT NULL,
    deleted_at          DATETIME
);

-- t_workspace_labels：标签，所属 workspace；所有项目共享一套通用标签（无 project 级归属）。
CREATE TABLE t_workspace_labels (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER  NOT NULL,
    name         TEXT     NOT NULL,
    color        TEXT     NOT NULL DEFAULT '',
    description  TEXT     NOT NULL DEFAULT '',
    sort_order   REAL     NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME
);

-- t_issue_labels：issue ↔ label 多对多关联，所属 issue。
CREATE TABLE t_issue_labels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id   TEXT     NOT NULL,
    label_id   INTEGER  NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    deleted_at DATETIME
);
CREATE UNIQUE INDEX udx_issue_labels_issue_id_label_id ON t_issue_labels (issue_id, label_id);

-- t_local_repositories：本地仓库（顶层资源，无 workspace 归属）。
-- sub_dir_list 存 JSON 文本（monorepo 子目录列表），由 service 层负责 []RepoSubDir ↔ JSON 序列化。
-- 无 deleted_at：随移除物理删除（见 local_repository service.Delete）。
CREATE TABLE t_local_repositories (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    name                TEXT     NOT NULL,
    local_dir           TEXT     NOT NULL,
    description         TEXT     NOT NULL DEFAULT '',
    sub_dir_list        TEXT     NOT NULL DEFAULT '[]',
    remote_url          TEXT     NOT NULL DEFAULT '',
    current_branch      TEXT     NOT NULL DEFAULT '',
    default_branch      TEXT     NOT NULL DEFAULT '',  -- 仓库默认分支（origin/HEAD）
    last_commit_at      INTEGER  NOT NULL DEFAULT 0,
    last_commit_message TEXT     NOT NULL DEFAULT '',
    created_at          DATETIME NOT NULL,
    updated_at          DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_local_repositories_local_dir ON t_local_repositories (local_dir);

-- t_project_local_repositories：项目 ↔ 本地仓库 多对多中间表。
-- 无 deleted_at：随项目/仓库物理删除而硬删关联记录（见 service 层事务级联清理）。
CREATE TABLE t_project_local_repositories (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_project_id INTEGER NOT NULL,
    local_repository_id  INTEGER NOT NULL,
    created_at           DATETIME NOT NULL,
    updated_at           DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_project_local_repositories_pid_lrid
    ON t_project_local_repositories (workspace_project_id, local_repository_id);

-- t_issue_local_repositories：issue ↔ 本地仓库+分支 多对多关联表（issue 可关联多个仓库，每仓库至多一条并带分支名）。
-- repository_branch 为分支名文本引用（freeSolo 可手输，不校验存在性）；无 DB 外键。
-- 无 deleted_at：随 issue 软删/项目解绑仓库/仓库删除，由 service 层事务级联硬删（见各 service 注释）。
CREATE TABLE t_issue_local_repositories (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id             TEXT     NOT NULL,
    local_repository_id  INTEGER NOT NULL,
    repository_branch    TEXT     NOT NULL DEFAULT '',
    created_at           DATETIME NOT NULL,
    updated_at           DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_issue_local_repositories_iid_lrid
    ON t_issue_local_repositories (issue_id, local_repository_id);
