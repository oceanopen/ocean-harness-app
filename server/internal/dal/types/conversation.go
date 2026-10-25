package types

import "ocean-harness/server/internal/dal/enums"

/*
 * 每 action 一个独立 Request 类型（tracker 范式）：会话主键 id 为 uuid 文本（Create 时由 service 生成 uuid v7）。
 * create 的 mode 为必填入参，由前端编排按归属实体 mergeLaunchSettings 算好传入（后端不感知 launch_settings）；
 * title 空 = 默认「新会话」；parentId 空 = 主会话（子会话预留）。
 */

// ConversationGetListRequest 是 POST /api/conversation/getList 的入参（按归属实体过滤，扁平全量）。
type ConversationGetListRequest struct {
	OwnerType enums.ConversationOwnerType `json:"ownerType" binding:"required,oneof=workspace task"`
	OwnerID   string                      `json:"ownerId" binding:"required"`
}

// ConversationGetInfoRequest 是 POST /api/conversation/getInfo 的入参。
type ConversationGetInfoRequest struct {
	ID string `json:"id" binding:"required"`
}

// ConversationCreateRequest 是 POST /api/conversation/create 的入参。
type ConversationCreateRequest struct {
	OwnerType enums.ConversationOwnerType `json:"ownerType" binding:"required,oneof=workspace task"`
	OwnerID   string                      `json:"ownerId" binding:"required"`
	Title     string                      `json:"title"` // 空 = 默认「新会话」
	Mode      enums.ConversationMode      `json:"mode" binding:"required,oneof=terminal acp"`
	ParentID  string                      `json:"parentId"` // 空 = 主会话
}

// ConversationUpdateRequest 是 POST /api/conversation/update 的入参（仅改标题；mode/锚各有专属链路）。
type ConversationUpdateRequest struct {
	ID    string `json:"id" binding:"required"`
	Title string `json:"title" binding:"required,max=255"`
}

// ConversationSetClaudeSessionIdRequest 是 POST /api/conversation/setClaudeSessionId 的入参
// （终端捕获回写统一锚；首捕获胜出，锚非空不覆盖，返回现行行）。
type ConversationSetClaudeSessionIdRequest struct {
	ID              string `json:"id" binding:"required"`
	ClaudeSessionID string `json:"claudeSessionId" binding:"required"`
}

// ConversationDeleteRequest 是 POST /api/conversation/delete 的入参。
type ConversationDeleteRequest struct {
	ID string `json:"id" binding:"required"`
}
