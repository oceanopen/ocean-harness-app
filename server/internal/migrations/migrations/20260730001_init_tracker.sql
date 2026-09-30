-- +goose Up
-- 全量基线：工作空间 / 项目 / Issue / 本地仓库 / IM bot 域 9 张业务表。
-- 约定：t_ 前缀；无 DB 外键（级联清理由 service 层处理）；全表物理删除；
-- 枚举列 TEXT NOT NULL 无默认值（代码显式赋值）；索引命名 idx_/udx_{表名去t_}_{列名}。

-- t_workspaces：工作空间（个人可建多个）；dir 为绝对路径，是 issue 任务目录与 bot 会话目录的根。
-- launch_settings 为启动设置 JSON 文本（shape 见 dal/types/workspace.go），空串 = 未配置 → 消费端回落全局。
CREATE TABLE t_workspaces (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT     NOT NULL,
    dir             TEXT     NOT NULL,
    description     TEXT     NOT NULL DEFAULT '',
    launch_settings TEXT     NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);

-- t_workspace_projects：项目，所属 workspace；允许重名，靠 id 区分。
CREATE TABLE t_workspace_projects (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id     INTEGER  NOT NULL,
    name             TEXT     NOT NULL,
    description      TEXT     NOT NULL DEFAULT '',
    emoji            TEXT     NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL
);

-- t_project_issues：issue 工作项，所属 project。id 为 TEXT uuid（任务目录名）；
-- state_code/priority 为代码赋值枚举，DONE 时写 completed_at；type_id 引用 t_workspace_types（0=未分类）。
-- launch_settings 为启动设置覆盖 JSON 文本（shape 同 t_workspaces.launch_settings），
-- 空串 = 未覆盖 → 消费端按字段级合并回落 workspace 级。
CREATE TABLE t_project_issues (
    id                  TEXT PRIMARY KEY,
    project_id          INTEGER  NOT NULL,
    workspace_id        INTEGER  NOT NULL,
    name                TEXT     NOT NULL,
    description         TEXT     NOT NULL DEFAULT '',
    launch_settings     TEXT     NOT NULL DEFAULT '',
    state_code          TEXT     NOT NULL,
    priority            TEXT     NOT NULL,
    sort_order          REAL     NOT NULL DEFAULT 0,
    parent_id           TEXT,
    type_id             INTEGER  NOT NULL DEFAULT 0,
    start_date          TEXT,
    target_date         TEXT,
    completed_at        DATETIME,
    created_at          DATETIME NOT NULL,
    updated_at          DATETIME NOT NULL
);

-- t_workspace_types：issue 类型，所属 workspace，项目间共享；是否必选为前端语义，后端不校验。
CREATE TABLE t_workspace_types (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER  NOT NULL,
    name         TEXT     NOT NULL,
    color        TEXT     NOT NULL DEFAULT '',
    description  TEXT     NOT NULL DEFAULT '',
    sort_order   REAL     NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL
);

-- t_local_repositories：本地仓库（顶层资源）；sub_dir_list 为 JSON 文本，由 service 层序列化。
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

-- t_project_local_repositories：项目 ↔ 本地仓库多对多中间表。
CREATE TABLE t_project_local_repositories (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_project_id INTEGER NOT NULL,
    local_repository_id  INTEGER NOT NULL,
    created_at           DATETIME NOT NULL,
    updated_at           DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_project_local_repositories_pid_lrid
    ON t_project_local_repositories (workspace_project_id, local_repository_id);

-- t_issue_local_repositories：issue ↔ 本地仓库多对多关联，repository_branch 为分支名（不校验存在性）。
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

-- t_im_bots：IM 数字人 bot 定义（渠道凭据 + claude 编排配置 + 启停）。
-- credential/access_policy 为 JSON 文本，shape 见 dal/types/im_bot.go；
-- workspace_id 关联 t_workspaces（0=未选择）；enabled='Y' 的 bot 随 sidecar 启动拉起。
CREATE TABLE t_im_bots (
    id            INTEGER  PRIMARY KEY AUTOINCREMENT,
    name          TEXT     NOT NULL,
    channel       TEXT     NOT NULL,
    credential    TEXT     NOT NULL DEFAULT '{}',
    workspace_id INTEGER  NOT NULL DEFAULT 0,
    model         TEXT     NOT NULL DEFAULT '',
    system_prompt TEXT     NOT NULL DEFAULT '',
    access_policy TEXT     NOT NULL,
    enabled       TEXT     NOT NULL,
    last_error    TEXT     NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);

-- t_im_bot_conversations：bot 会话映射（多轮上下文在 claude 会话存储，本表只存锚点）。
-- conversation_key：'single:<userid>' / 'group:<chatid>'；claude_session_id 为 --resume 锚（空=未开场）。
CREATE TABLE t_im_bot_conversations (
    id                INTEGER  PRIMARY KEY AUTOINCREMENT,
    bot_id            INTEGER  NOT NULL,
    conversation_key  TEXT     NOT NULL,
    claude_session_id TEXT     NOT NULL DEFAULT '',
    seen_message_ids  TEXT     NOT NULL DEFAULT '[]',
    last_message_at   DATETIME,
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_im_bot_conversations_bot_conversation
    ON t_im_bot_conversations (bot_id, conversation_key);
