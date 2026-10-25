-- +goose Up
-- 全量基线：工作空间 / 项目 / 任务 / 本地仓库 / IM bot 域 / 会话域 10 张业务表。
-- 约定：t_ 前缀；无 DB 外键（级联清理由 service 层处理）；全表物理删除；
-- 枚举列 TEXT NOT NULL 无默认值（代码显式赋值）；索引命名 idx_/udx_{表名去t_}_{列名}。
-- workspace/project/bot/会话主键与 owner_id/bot_id 为 uuid 文本（service 层 Create 生成 uuid v7，
-- 时间有序；未选哨兵为空串），其余 id 保持自增 INTEGER。

-- t_workspaces：工作空间（个人可建多个）；dir 为绝对路径。launch_settings 为启动设置 JSON
-- 文本（shape 见 dal/types/workspace.go），空串 = 未配置 → 消费端回落全局；emoji 空 = 前端图标兜底。
CREATE TABLE t_workspaces (
    id              TEXT PRIMARY KEY,
    name            TEXT     NOT NULL,
    dir             TEXT     NOT NULL,
    description     TEXT     NOT NULL DEFAULT '',
    emoji           TEXT     NOT NULL DEFAULT '',
    launch_settings TEXT     NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);

-- t_projects：项目（与工作空间相互独立）；dir 为绝对路径，是任务目录（{project.dir}/{taskId}/）的根。
-- 字段族与 t_workspaces 同构；launch_settings 语义同工作空间基线。
CREATE TABLE t_projects (
    id              TEXT PRIMARY KEY,
    name            TEXT     NOT NULL,
    dir             TEXT     NOT NULL,
    description     TEXT     NOT NULL DEFAULT '',
    emoji           TEXT     NOT NULL DEFAULT '',
    launch_settings TEXT     NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);

-- t_project_tasks：项目任务，所属 project。id 为 TEXT uuid（任务目录名）；
-- launch_settings 为启动设置覆盖 JSON 文本，空串 = 未覆盖 → 消费端按字段级合并回落项目级。
CREATE TABLE t_project_tasks (
    id              TEXT PRIMARY KEY,
    project_id      TEXT     NOT NULL,
    name            TEXT     NOT NULL,
    description     TEXT     NOT NULL DEFAULT '',
    type_id         INTEGER  NOT NULL DEFAULT 0,
    launch_settings TEXT     NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);

-- t_project_types：任务类型，所属 project，任务间共享；是否必选为前端语义，后端不校验。
CREATE TABLE t_project_types (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id   TEXT     NOT NULL,
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
    default_branch      TEXT     NOT NULL DEFAULT '',
    last_commit_at      INTEGER  NOT NULL DEFAULT 0,
    last_commit_message TEXT     NOT NULL DEFAULT '',
    created_at          DATETIME NOT NULL,
    updated_at          DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_local_repositories_local_dir ON t_local_repositories (local_dir);

-- t_project_local_repositories：项目 ↔ 本地仓库多对多中间表。
CREATE TABLE t_project_local_repositories (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id           TEXT    NOT NULL,
    local_repository_id  INTEGER NOT NULL,
    created_at           DATETIME NOT NULL,
    updated_at           DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_project_local_repositories_pid_lrid
    ON t_project_local_repositories (project_id, local_repository_id);

-- t_task_local_repositories：任务 ↔ 本地仓库多对多关联，repository_branch 为分支名（不校验存在性）。
CREATE TABLE t_task_local_repositories (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id              TEXT     NOT NULL,
    local_repository_id  INTEGER NOT NULL,
    repository_branch    TEXT     NOT NULL DEFAULT '',
    created_at           DATETIME NOT NULL,
    updated_at           DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_task_local_repositories_tid_lrid
    ON t_task_local_repositories (task_id, local_repository_id);

-- t_im_bots：IM 数字人 bot 定义（渠道凭据 + 归属配置 + 启停）。
-- credential/access_policy/channel_state 为 JSON 文本，shape 见 dal/types/im_bot.go；
-- 归属三哨兵空串 = 未配置：conversation_type ∈ {workspace,project}（两档必填由 T4.1 校验），
-- type=workspace 配 workspace_id、type=project 配 project_id + task_id（可选）；
-- enabled='Y' 的 bot 随 sidecar 启动拉起。
CREATE TABLE t_im_bots (
    id                TEXT PRIMARY KEY,
    name              TEXT     NOT NULL,
    channel           TEXT     NOT NULL,
    credential        TEXT     NOT NULL DEFAULT '{}',
    conversation_type TEXT     NOT NULL DEFAULT '',
    workspace_id      TEXT     NOT NULL DEFAULT '',
    project_id        TEXT     NOT NULL DEFAULT '',
    task_id           TEXT     NOT NULL DEFAULT '',
    access_policy     TEXT     NOT NULL,
    enabled           TEXT     NOT NULL,
    channel_state     TEXT     NOT NULL DEFAULT '{}',
    last_error        TEXT     NOT NULL DEFAULT '',
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL
);

-- t_im_bot_conversations：bot 会话映射（多轮上下文在 claude 会话存储，本表只存锚点）。
-- conversation_key：'single:<userid>' / 'group:<chatid>'；claude_session_id 为 headless
-- --resume 锚（空=未开场，ACP 单驱动后随驱动下线）；conversation_id 为归属会话锚
-- （空=未关联，指向 t_conversations.id，由 T4.3 起读写）。
CREATE TABLE t_im_bot_conversations (
    id                TEXT PRIMARY KEY,
    bot_id            TEXT     NOT NULL,
    conversation_key  TEXT     NOT NULL,
    claude_session_id TEXT     NOT NULL DEFAULT '',
    conversation_id   TEXT     NOT NULL DEFAULT '',
    seen_message_ids  TEXT     NOT NULL DEFAULT '[]',
    last_message_at   DATETIME,
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_im_bot_conversations_bot_conversation
    ON t_im_bot_conversations (bot_id, conversation_key);

-- t_conversations：会话第一性实体（应用只存元数据，消息内容直读 claude 本地 jsonl）。
-- owner_type ∈ {workspace,task} + owner_id 定归属；mode ∈ {terminal,acp} 定打开方式；
-- agent_code 当前恒 claude（为多 agent 预留）；claude_session_id 为终端/ACP 统一锚
-- （空 = 未锚定，终端 fresh spawn 后捕获回写）；parent_id 空串 = 主会话。
CREATE TABLE t_conversations (
    id                TEXT PRIMARY KEY,
    owner_type        TEXT     NOT NULL,
    owner_id          TEXT     NOT NULL,
    title             TEXT     NOT NULL DEFAULT '',
    mode              TEXT     NOT NULL,
    agent_code        TEXT     NOT NULL DEFAULT '',
    claude_session_id TEXT     NOT NULL DEFAULT '',
    parent_id         TEXT     NOT NULL DEFAULT '',
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL
);
CREATE INDEX idx_conversations_owner ON t_conversations (owner_type, owner_id);
