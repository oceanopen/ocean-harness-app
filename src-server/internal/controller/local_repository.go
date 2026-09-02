package controller

import (
	"github.com/gin-gonic/gin"

	"ocean-harness/src-server/internal/apis"
	"ocean-harness/src-server/internal/dal/types"
	"ocean-harness/src-server/internal/service"
)

// LocalRepository 对应 /api/localRepository 命名空间下的接口（本地仓库 CRUD + git 刷新）。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type LocalRepository struct {
	apis.Api
}

// GetList POST /api/localRepository/getList：返回全部本地仓库。
func (api LocalRepository) GetList(ctx *gin.Context) {
	req := &types.LocalRepositoryGetListRequest{}
	svc := service.LocalRepository{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.GetList()
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// GetInfo POST /api/localRepository/getInfo：返回单个本地仓库。
func (api LocalRepository) GetInfo(ctx *gin.Context) {
	req := &types.LocalRepositoryGetInfoRequest{}
	svc := service.LocalRepository{}
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

// Create POST /api/localRepository/create：新增本地仓库（校验 + 解析 git 信息）。
func (api LocalRepository) Create(ctx *gin.Context) {
	req := &types.LocalRepositoryCreateRequest{}
	svc := service.LocalRepository{}
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

// Update POST /api/localRepository/update：更新本地仓库（校验 + 重解析 git 信息）。
func (api LocalRepository) Update(ctx *gin.Context) {
	req := &types.LocalRepositoryUpdateRequest{}
	svc := service.LocalRepository{}
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

// Delete POST /api/localRepository/delete：物理删除本地仓库。
func (api LocalRepository) Delete(ctx *gin.Context) {
	req := &types.LocalRepositoryDeleteRequest{}
	svc := service.LocalRepository{}
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

// Refresh POST /api/localRepository/refresh：重解析单个仓库的 git 信息。
func (api LocalRepository) Refresh(ctx *gin.Context) {
	req := &types.LocalRepositoryRefreshRequest{}
	svc := service.LocalRepository{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Refresh(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// RefreshAll POST /api/localRepository/refreshAll：重解析全部仓库的 git 信息，返回最新列表。
func (api LocalRepository) RefreshAll(ctx *gin.Context) {
	req := &types.LocalRepositoryGetListRequest{}
	svc := service.LocalRepository{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.RefreshAll()
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// GetLocalBranches POST /api/localRepository/getLocalBranches：列出仓库的本地分支名（供 issue 分支选择器）。
func (api LocalRepository) GetLocalBranches(ctx *gin.Context) {
	req := &types.LocalRepositoryGetLocalBranchesRequest{}
	svc := service.LocalRepository{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.GetLocalBranches(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}
