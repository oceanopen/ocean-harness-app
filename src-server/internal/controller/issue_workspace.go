package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ocean-harness/src-server/internal/apis"
	"ocean-harness/src-server/internal/dal/types"
	"ocean-harness/src-server/internal/service"
)

// IssueWorkspace 对应 /api/issueWorkspace 命名空间下的接口（issue 运行工作空间初始化）。
// 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
type IssueWorkspace struct {
	apis.Api
}

// Init POST /api/issueWorkspace/init：受理初始化（后台异步执行），返回受理后状态
// （RUNNING；幂等命中时 SUCCESS；执行中重复触发返回当前进度）。
func (api IssueWorkspace) Init(ctx *gin.Context) {
	req := &types.IssueWorkspaceInitRequest{}
	svc := service.IssueWorkspace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Init(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Status POST /api/issueWorkspace/status：查询初始化进度（读状态文件派生，供前端轮询）。
func (api IssueWorkspace) Status(ctx *gin.Context) {
	req := &types.IssueWorkspaceStatusRequest{}
	svc := service.IssueWorkspace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Status(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// Archive POST /api/issueWorkspace/archive：归档/取消工作空间（T3.2，两段式——force=false
// 仅安全检查返回警告不执行；force=true 删 {issueId}/ 目录 + 流转 issue 状态）。
func (api IssueWorkspace) Archive(ctx *gin.Context) {
	req := &types.IssueWorkspaceArchiveRequest{}
	svc := service.IssueWorkspace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.Archive(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// FileTree POST /api/issueWorkspace/getFileTree：一次性返回工作空间全部文件/目录的
// 扁平节点表（T5.1 本期，前端组树渲染文件面板）。
func (api IssueWorkspace) FileTree(ctx *gin.Context) {
	req := &types.IssueWorkspaceFileTreeRequest{}
	svc := service.IssueWorkspace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.FileTree(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// FileContent POST /api/issueWorkspace/getFileContent：读取单个文件内容并定夺传输 kind
// （text/image/binary/tooLarge，T5.1 本期，前端预览浮层消费）。
func (api IssueWorkspace) FileContent(ctx *gin.Context) {
	req := &types.IssueWorkspaceFileContentRequest{}
	svc := service.IssueWorkspace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.FileContent(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

// FileRaw GET /api/issueWorkspace/fileRaw：图片原始字节直连（<img src> 消费，类静态资源；
// 校验链与 getFileContent 一致 + 图片扩展名白名单）。参数走 query（GET 无法 JSON body）。
func (api IssueWorkspace) FileRaw(ctx *gin.Context) {
	svc := service.IssueWorkspace{}
	api.MakeContext(ctx).MakeService(&svc.Service)
	issueID := ctx.Query("issueId")
	baseDir := ctx.Query("baseDir")
	path := ctx.Query("path")
	if issueID == "" || baseDir == "" || path == "" {
		api.JsonFail(errors.New("参数缺失"))
		return
	}
	raw, mime, err := svc.FileRaw(baseDir, issueID, path)
	if err != nil {
		api.JsonFail(err)
		return
	}
	// agent 在终端持续改文件：禁缓存，新鲜度由前端 ?v= cache-buster 与重验控制。
	ctx.Header("Cache-Control", "no-store")
	ctx.Data(http.StatusOK, mime, raw)
}
