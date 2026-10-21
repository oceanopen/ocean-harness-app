-- +goose Up
-- 会话闲置过期锚点备份列：2h 闲置判定触发时 claude_session_id 备份至此，/继续 接回、/clear 一并清除。
ALTER TABLE t_im_bot_conversations ADD COLUMN prev_session_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE t_im_bot_conversations DROP COLUMN prev_session_id;
