package model

import (
	"github.com/google/uuid"

	"ocean-harness/server/internal/dal/enums"
)

/*
 * NewPresetClaudeSession 生成建行预置 claude 会话锚（uuid v7）与未驱动状态。
 * 唯一 SSOT：service.Create 与 bot 会话行创建共用——建行即预生成、驱动注入 --session-id。
 */
func NewPresetClaudeSession() (string, enums.ClaudeSessionStatus) {
	return uuid.Must(uuid.NewV7()).String(), enums.CLAUDE_SESSION_STATUS_PENDING
}
