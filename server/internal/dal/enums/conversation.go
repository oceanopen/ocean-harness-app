package enums

import (
	"database/sql/driver"
	"fmt"
)

/*
 * 本文件定义会话域枚举：bot 归属档位（ConversationType，'' = 未配置哨兵）、会话归属实体
 * （ConversationOwnerType）、会话执行模式（ConversationMode）。
 */

// ConversationType bot 会话类型（t_im_bots.conversation_type）：workspace = 关联工作空间，project = 关联项目+任务。
type ConversationType string

const (
	CONVERSATION_TYPE_WORKSPACE ConversationType = "workspace"
	CONVERSATION_TYPE_PROJECT   ConversationType = "project"
)

/*
 * Value 实现 driver.Valuer（gen 的 field.Field 泛型约束要求）；空串 = 未配置哨兵合法，
 * 非空透传（仿 AgentCode 开放校验，两档必填值域校验归 service 层）。
 */
func (c ConversationType) Value() (driver.Value, error) {
	return string(c), nil
}

// ConversationOwnerType 会话归属实体（t_conversations.owner_type）：workspace 会话 cwd = 工作空间目录，task 会话 cwd = 任务目录。
type ConversationOwnerType string

const (
	OWNER_TYPE_WORKSPACE ConversationOwnerType = "workspace"
	OWNER_TYPE_TASK      ConversationOwnerType = "task"
)

// Value 实现 driver.Valuer：写库时校验合法值；非法值返回错误，由 gorm 在 INSERT/UPDATE 时触发。
func (c ConversationOwnerType) Value() (driver.Value, error) {
	switch c {
	case OWNER_TYPE_WORKSPACE, OWNER_TYPE_TASK:
		return string(c), nil
	default:
		return nil, fmt.Errorf("invalid ConversationOwnerType: %v", c)
	}
}

// ConversationMode 会话执行模式（t_conversations.mode）：terminal = 终端直启（--resume 锚定），acp = ACP 受控会话。
type ConversationMode string

const (
	CONVERSATION_MODE_TERMINAL ConversationMode = "terminal"
	CONVERSATION_MODE_ACP      ConversationMode = "acp"
)

// Value 实现 driver.Valuer：写库时校验合法值；非法值返回错误，由 gorm 在 INSERT/UPDATE 时触发。
func (c ConversationMode) Value() (driver.Value, error) {
	switch c {
	case CONVERSATION_MODE_TERMINAL, CONVERSATION_MODE_ACP:
		return string(c), nil
	default:
		return nil, fmt.Errorf("invalid ConversationMode: %v", c)
	}
}

/*
 * ClaudeSessionStatus claude 会话状态（t_conversations.claude_session_status）：pending = 预置未
 * 驱动（claude_session_id 为建行预生成值，驱动时以 --session-id 开新会话），active = 已驱动
 * （锚与实际 claude 会话 id 一致，复用走 --resume / session/load）。空串 = 存量哨兵，语义同 active。
 */
type ClaudeSessionStatus string

const (
	CLAUDE_SESSION_STATUS_PENDING ClaudeSessionStatus = "pending"
	CLAUDE_SESSION_STATUS_ACTIVE  ClaudeSessionStatus = "active"
)

// Value 实现 driver.Valuer：空串哨兵合法，非空透传（值域校验归调用方）。
func (c ClaudeSessionStatus) Value() (driver.Value, error) {
	return string(c), nil
}
