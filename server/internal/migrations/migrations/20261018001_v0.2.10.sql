-- +goose Up
-- t_im_bots 增列 channel_state：渠道适配器自管运行态 JSON（核心不解释内容，{} = 空）。
-- 首个使用方为微信渠道的长轮询游标（get_updates_buf，跨重启续传不重放历史）。
ALTER TABLE t_im_bots ADD COLUMN channel_state TEXT NOT NULL DEFAULT '{}';
