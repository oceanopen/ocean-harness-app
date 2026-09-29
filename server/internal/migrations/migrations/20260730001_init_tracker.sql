-- +goose Up
-- 工作空间 / 项目 / Issue / 本地仓库 / IM bot 管理：基线 9 张业务表（最终态，已合并历次增量迁移：
-- 20260804001 local_repositories、20260804002 项目↔仓库中间表、20260811001 local_repository.default_branch、
-- issue 仓库+分支多选（曾为 issue 表两列/JSON 列，现独立关联表 t_issue_local_repositories）、
-- 20260928001 init_im_bot（t_im_bots / t_im_bot_conversations）。
-- 命名格式：YYYYMMDD + 三位序号 + _name.sql；启动时 goose 自动向前迁移（仅 Up，见 initialize/migrate.go）。
--
-- 约定：
--   - 业务表统一 t_ 前缀，且表名带「所属关系」前缀：顶级 t_workspaces / t_local_repositories 保持，
--     子表以直接父单数作前缀（t_workspace_projects / t_project_issues /
--     t_workspace_types / t_project_local_repositories）；
--   - 主键统一自增 INTEGER（例外：t_project_issues.id 为 TEXT uuid）；
--   - 公共字段 created_at/updated_at（DATETIME，gorm 自动维护）；【全部表物理删除】（单用户本地个人数据，
--     无恢复需求，Delete 即 DELETE）；唯一索引随行删除自然释放；
--   - 唯一索引 udx_ 前缀；命名 udx_{表名去t_}_{列名...}（SQLite 索引名 schema 级全局唯一，带表名段避免跨表重名）；
--   - 普通索引：数据量较小，本期暂不建，后续按查询热点按 idx_{表名去t_}_{列名} 追加；
--   - 【无 DB 外键约束】表间不建 FOREIGN KEY，跨表关联一律通过 SQL JOIN 或应用层组装查询；
--     数据级联清理（如删 workspace 连带清其下 project/issue/type）由 service 层手动处理；
--   - typed 枚举列（state_code / priority）用 TEXT NOT NULL 无默认值，
--     由代码显式赋值（避免 DEFAULT '' 触发 gorm 零值省略、静默存空串，见记忆 tracker-enum-pattern）。

-- t_workspaces：顶层容器（个人可建多个，如「个人 / 工作 / 开源」）。
-- dir：工作区目录（绝对路径，service 层校验可写；issue 运行目录与 IM bot 会话目录的根）。
CREATE TABLE t_workspaces (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT     NOT NULL,
    dir           TEXT     NOT NULL,
    description   TEXT     NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);

-- t_workspace_projects：项目，所属 workspace。允许重名（个人场景靠 id 区分），无短码。
CREATE TABLE t_workspace_projects (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id     INTEGER  NOT NULL,
    name             TEXT     NOT NULL,
    description      TEXT     NOT NULL DEFAULT '',
    emoji            TEXT     NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL
);

-- t_project_issues：核心工作项，所属 project。
-- issue 主键 id 为 TEXT uuid 字符串（与 claude session_id 同格式，Create 时由 service 生成 uuid v7；
-- 后续将作为工作空间运行任务目录的唯一标识）；sort_order 列表排序权重；priority 五级枚举。
-- state_code 为固定 5 值 typed 枚举（BACKLOG/TODO/IN_PROGRESS/DONE/CANCELLED，元数据见 enums.StateCatalog，
-- 无 state_id/项目级状态行）；DONE 触发 issue.completed_at。
-- parent_id 逻辑指向 t_project_issues.id，不建 DB 外键。
-- type_id 单值引用 t_workspace_types（每 issue 至多一个类型，0=未分类兜底；
-- 删类型时由 service 层将引用的 issue.type_id 置 0）。
-- issue 关联的多仓库+分支在独立关联表 t_issue_local_repositories。
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
    type_id             INTEGER  NOT NULL DEFAULT 0,
    start_date          TEXT,
    target_date         TEXT,
    completed_at        DATETIME,
    created_at          DATETIME NOT NULL,
    updated_at          DATETIME NOT NULL
);

-- t_workspace_types：类型，所属 workspace；所有项目共享一套通用类型（无 project 级归属）。
-- 「必选、新建默认需求」为前端语义（类型列表含「需求」时默认选中），后端不强制校验 type_id>0。
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

-- t_local_repositories：本地仓库（顶层资源，无 workspace 归属）。
-- sub_dir_list 存 JSON 文本（monorepo 子目录列表），由 service 层负责 []RepoSubDir ↔ JSON 序列化。
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

-- t_project_local_repositories：项目 ↔ 本地仓库 多对多中间表（随项目/仓库删除由 service 层事务级联清理）。
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
-- repository_branch 为分支名文本引用（freeSolo 可手输，不校验存在性）；无 DB 外键；
-- 随 issue 删除/项目解绑仓库/仓库删除，由 service 层事务级联清理（见各 service 注释）。
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

-- ===== IM 渠道数字人 bot 域（原 20260928001_init_im_bot.sql 并入） =====
-- 域架构：internal/bot 为渠道无关核心（编排/幂等/白名单/prompt/claude 驱动/回复泵/生命周期），
-- internal/bot/wecom 等渠道适配器实现核心的 ChannelRuntime 接口；本域表结构渠道无关——
-- 渠道差异只体现在 channel 枚举与 credential JSON 的 shape（由各渠道适配器自行解释）。

-- t_im_bots：IM 渠道数字人 bot 定义（每 bot 独立渠道凭据 + claude 编排配置 + 启停）。
-- channel：渠道枚举（enums.Channel，本期 wecom）；credential：渠道特定凭据 JSON
--（wecom: {"botId":"...","secret":"..."}）——botId 在 JSON 内无法建 DB 唯一索引，查重由 service
-- 层在 create/update 时扫描校验；workspace_id：关联 t_workspaces（0 = 未选择，扫码接入先建
-- bot 后补选；会话目录取工作空间 dir）；allowed_tools：JSON 字符串数组（claude --allowedTools 透传，
-- 空数组 = 采用代码默认白名单）；access_policy：访问白名单 JSON
-- {"mode":"open|allowlist","allowUsers":["userid",...]}，解析失败按 fail closed 拒绝（核心 access.go）；
-- enabled：启停开关（公共映射 enums.YesNo："Y"/"N"，与 Rust 侧 app_config 同词汇），
-- sidecar 启动时拉起全部 enabled='Y' 的 bot；
-- last_error：最近一次连接终态错误（认证失败/被新连接踢下线），重启后仍可见，供前端状态灯展示。
CREATE TABLE t_im_bots (
    id            INTEGER  PRIMARY KEY AUTOINCREMENT,
    name          TEXT     NOT NULL,
    channel       TEXT     NOT NULL,
    credential    TEXT     NOT NULL DEFAULT '{}',
    workspace_id INTEGER  NOT NULL DEFAULT 0,
    model         TEXT     NOT NULL DEFAULT '',
    system_prompt TEXT     NOT NULL DEFAULT '',
    allowed_tools TEXT     NOT NULL DEFAULT '[]',
    access_policy TEXT     NOT NULL,
    enabled       TEXT     NOT NULL,
    last_error    TEXT     NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);

-- t_im_bot_conversations：会话映射（多轮上下文的 SSOT 是 claude 自身会话存储，本表只存指针）。
-- conversation_key：'single:<userid>' / 'group:<chatid>'（前缀自描述免 chat_type 列；
-- 单聊一人一会话，群聊全群共享一个 claude 会话）；claude_session_id：最近一回合 system/init
-- 捕获的 session id（--resume 锚，空 = 未开场）；seen_message_ids：近 100 条 msgid JSON 环形数组
--（企微 WS 无补投，幂等仅防瞬时重推，跨重启仍有效）；last_message_at：最近一回合完成时间。
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
