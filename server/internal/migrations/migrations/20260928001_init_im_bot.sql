-- +goose Up
-- IM 渠道数字人 bot 域基线 2 张表：t_im_bots（bot 定义）+ t_im_bot_conversations（会话映射）。
-- 域架构：internal/bot 为渠道无关核心（编排/幂等/白名单/prompt/claude 驱动/回复泵/生命周期），
-- internal/bot/wecom 等渠道适配器实现核心的 ChannelRuntime 接口；本域表结构渠道无关——
-- 渠道差异只体现在 channel 枚举与 credential JSON 的 shape（由各渠道适配器自行解释）。
--
-- 约定同 20260730001_init_tracker.sql：t_ 前缀、无 DB 外键（级联由 service 层手动处理）、
-- 全物理删除、typed 枚举 TEXT NOT NULL 无默认（代码显式赋值）、JSON 列由 service 层序列化、
-- 唯一索引 udx_{表名去t_}_{列名...}。

-- t_im_bots：IM 渠道数字人 bot 定义（每 bot 独立渠道凭据 + claude 编排配置 + 启停）。
-- channel：渠道枚举（enums.Channel，本期 wecom）；credential：渠道特定凭据 JSON
--（wecom: {"botId":"...","secret":"..."}）——botId 在 JSON 内无法建 DB 唯一索引，查重由 service
-- 层在 create/update 时扫描校验；allowed_tools：JSON 字符串数组（claude --allowedTools 透传，
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
    workspace_dir TEXT     NOT NULL,
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
