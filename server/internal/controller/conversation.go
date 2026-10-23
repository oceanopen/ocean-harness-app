package controller

import (
	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/service"
)

/*
 * Conversation 对应 /api/conversation 命名空间下的接口。
 * 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
 */
type Conversation struct {
	apis.Api
}

// GetList POST /api/conversation/getList：返回某归属实体下会话列表（created_at 升序）。
func (api Conversation) GetList(ctx *gin.Context) {
	req := &types.ConversationGetListRequest{}
	svc := service.Conversation{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.GetList(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// GetInfo POST /api/conversation/getInfo：返回单个会话。
func (api Conversation) GetInfo(ctx *gin.Context) {
	req := &types.ConversationGetInfoRequest{}
	svc := service.Conversation{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.GetInfo(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Create POST /api/conversation/create：新建会话（title 空默认「新会话」）。
func (api Conversation) Create(ctx *gin.Context) {
	req := &types.ConversationCreateRequest{}
	svc := service.Conversation{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Create(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Update POST /api/conversation/update：更新会话标题。
func (api Conversation) Update(ctx *gin.Context) {
	req := &types.ConversationUpdateRequest{}
	svc := service.Conversation{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Update(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// SetClaudeSessionID POST /api/conversation/setClaudeSessionId：终端捕获回写统一锚（首捕获胜出）。
func (api Conversation) SetClaudeSessionID(ctx *gin.Context) {
	req := &types.ConversationSetClaudeSessionIdRequest{}
	svc := service.Conversation{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.SetClaudeSessionID(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Delete POST /api/conversation/delete：删除会话行（不动 claude 本地文件）。
func (api Conversation) Delete(ctx *gin.Context) {
	req := &types.ConversationDeleteRequest{}
	svc := service.Conversation{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	if err := svc.Delete(req); err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(nil)
}
