-- +goose Up
-- 会话备份锚列下线：/继续 已移除，列无消费方。
ALTER TABLE t_im_bot_conversations DROP COLUMN prev_session_id;
-- +goose Down
ALTER TABLE t_im_bot_conversations ADD COLUMN prev_session_id TEXT NOT NULL DEFAULT '';
