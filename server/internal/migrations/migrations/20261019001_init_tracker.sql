-- +goose Up
-- 全量基线：工作空间 / Issue / 本地仓库 / IM bot 域 / ACP 会话域 9 张业务表。
-- 约定：t_ 前缀；无 DB 外键（级联清理由 service 层处理）；全表物理删除；
-- 枚举列 TEXT NOT NULL 无默认值（代码显式赋值）；索引命名 idx_/udx_{表名去t_}_{列名}。
-- workspace/bot/会话主键与 workspace_id/bot_id 为 uuid 文本（service 层 Create 生成 uuid v7，
-- 时间有序；未选哨兵为空串），其余 id 保持自增 INTEGER。

-- t_workspaces：工作空间（个人可建多个）；dir 为绝对路径，是 issue 任务目录与 bot 会话目录的根。
-- launch_settings 为启动设置 JSON 文本（shape 见 dal/types/workspace.go），空串 = 未配置 → 消费端回落全局。
-- emoji 为展示图标（列表/树节点用，空 = 前端图标兜底）。
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

-- t_workspace_issues：issue 工作项，所属 workspace。id 为 TEXT uuid（任务目录名）；
-- state_code 为代码赋值枚举，DONE 时写 completed_at；type_id 引用 t_workspace_types（0=未分类）。
-- launch_settings 为启动设置覆盖 JSON 文本（shape 同 t_workspaces.launch_settings），
-- 空串 = 未覆盖 → 消费端按字段级合并回落 workspace 级。
CREATE TABLE t_workspace_issues (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT    NOT NULL,
    name                TEXT     NOT NULL,
    description         TEXT     NOT NULL DEFAULT '',
    launch_settings     TEXT     NOT NULL DEFAULT '',
    state_code          TEXT     NOT NULL,
    sort_order          REAL     NOT NULL DEFAULT 0,
    type_id             INTEGER  NOT NULL DEFAULT 0,
    start_date          TEXT,
    target_date         TEXT,
    completed_at        DATETIME,
    created_at          DATETIME NOT NULL,
    updated_at          DATETIME NOT NULL
);

-- t_workspace_types：issue 类型，所属 workspace，任务间共享；是否必选为前端语义，后端不校验。
CREATE TABLE t_workspace_types (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT     NOT NULL,
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

-- t_workspace_local_repositories：工作空间 ↔ 本地仓库多对多中间表。
CREATE TABLE t_workspace_local_repositories (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id         TEXT    NOT NULL,
    local_repository_id  INTEGER NOT NULL,
    created_at           DATETIME NOT NULL,
    updated_at           DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_workspace_local_repositories_wid_lrid
    ON t_workspace_local_repositories (workspace_id, local_repository_id);

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
-- credential/access_policy/channel_state 为 JSON 文本，shape 见 dal/types/im_bot.go；
-- id 与 workspace_id 均为 uuid 文本（workspace_id ''=未选择）；
-- channel_state 为渠道适配器自管运行态（首个使用方为微信长轮询游标，跨重启续传）；
-- enabled='Y' 的 bot 随 sidecar 启动拉起。
CREATE TABLE t_im_bots (
    id            TEXT PRIMARY KEY,
    name          TEXT     NOT NULL,
    channel       TEXT     NOT NULL,
    credential    TEXT     NOT NULL DEFAULT '{}',
    workspace_id  TEXT     NOT NULL DEFAULT '',
    access_policy TEXT     NOT NULL,
    enabled       TEXT     NOT NULL,
    channel_state TEXT     NOT NULL DEFAULT '{}',
    last_error    TEXT     NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);

-- t_im_bot_conversations：bot 会话映射（多轮上下文在 claude 会话存储，本表只存锚点）。
-- conversation_key：'single:<userid>' / 'group:<chatid>'；claude_session_id 为 --resume 锚（空=未开场）。
-- bound_issue_id 为任务关联锚（空=未关联；与 claude_session_id 正交，issue 删除时由 service 层级联清列，
-- 行保留）；acp_session_id 为 ACP 模式会话运行时锚（空=无活跃会话；无 resume 语义，sidecar 重启后
-- 由会话域启动清扫归零）。未关联会话的 ACP 回合以本行为锚（cwd=工作空间根目录），关联会话走任务锚。
CREATE TABLE t_im_bot_conversations (
    id                TEXT PRIMARY KEY,
    bot_id            TEXT     NOT NULL,
    conversation_key  TEXT     NOT NULL,
    claude_session_id TEXT     NOT NULL DEFAULT '',
    bound_issue_id    TEXT     NOT NULL DEFAULT '',
    acp_session_id    TEXT     NOT NULL DEFAULT '',
    seen_message_ids  TEXT     NOT NULL DEFAULT '[]',
    last_message_at   DATETIME,
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_im_bot_conversations_bot_conversation
    ON t_im_bot_conversations (bot_id, conversation_key);

-- t_issue_acp_sessions：issue ↔ ACP 会话绑定锚点（工作台任务域；一 issue 至多一个活跃会话）。
-- acp_session_id 为运行时锚（空 = 无活跃会话；无 resume 语义，sidecar 重启后由会话域启动清扫归零）；
-- agent_code 记创建会话时的实际取值；last_error 跨重启可见（对齐 t_im_bots.last_error）。
CREATE TABLE t_issue_acp_sessions (
    id             INTEGER  PRIMARY KEY AUTOINCREMENT,
    issue_id       TEXT     NOT NULL,
    agent_code     TEXT     NOT NULL DEFAULT '',
    acp_session_id TEXT     NOT NULL DEFAULT '',
    last_error     TEXT     NOT NULL DEFAULT '',
    created_at     DATETIME NOT NULL,
    updated_at     DATETIME NOT NULL
);
CREATE UNIQUE INDEX udx_issue_acp_sessions_issue_id ON t_issue_acp_sessions (issue_id);
