package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/service"
)

/*
 * TaskWorkspace 对应 /api/taskWorkspace 命名空间下的接口（issue 运行工作空间初始化）。
 * 嵌入 apis.Api 获得链式装配（MakeContext/Bind/Validate/MakeService）与 JsonOK/JsonFail。
 */
type TaskWorkspace struct {
	apis.Api
}

/*
 * Init POST /api/taskWorkspace/init：
 * 受理初始化（后台异步执行），返回受理后状态（RUNNING；幂等命中时 SUCCESS；执行中重复触发返回当前进度）。
 */
func (api TaskWorkspace) Init(ctx *gin.Context) {
	req := &types.TaskWorkspaceInitRequest{}
	svc := service.TaskWorkspace{}
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

// Status POST /api/taskWorkspace/status：查询初始化进度（读状态文件派生，供前端轮询）。
func (api TaskWorkspace) Status(ctx *gin.Context) {
	req := &types.TaskWorkspaceStatusRequest{}
	svc := service.TaskWorkspace{}
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

/*
 * Archive POST /api/taskWorkspace/archive：归档/取消工作空间（T3.2，
 * 两段式——force=false 仅安全检查返回警告不执行；force=true 删 {taskId}/ 目录 + 流转 issue 状态）。
 */
func (api TaskWorkspace) Archive(ctx *gin.Context) {
	req := &types.TaskWorkspaceArchiveRequest{}
	svc := service.TaskWorkspace{}
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

/*
 * FileTree POST /api/taskWorkspace/getFileTree：
 * 一次性返回工作空间全部文件/目录的扁平节点表（T5.1 本期，前端组树渲染文件面板）。
 */
func (api TaskWorkspace) FileTree(ctx *gin.Context) {
	req := &types.TaskWorkspaceFileTreeRequest{}
	svc := service.TaskWorkspace{}
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

/*
 * FileContent POST /api/taskWorkspace/getFileContent：
 * 读取单个文件内容并定夺传输 kind（text/image/binary/tooLarge，T5.1 本期，前端预览浮层消费）。
 */
func (api TaskWorkspace) FileContent(ctx *gin.Context) {
	req := &types.TaskWorkspaceFileContentRequest{}
	svc := service.TaskWorkspace{}
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

/*
 * GitChanges POST /api/taskWorkspace/getGitChanges：
 * 列出全部仓库的未提交变更（暂存+工作区 vs HEAD + untracked，T5.1「Git 变更」模式）。
 */
func (api TaskWorkspace) GitChanges(ctx *gin.Context) {
	req := &types.TaskWorkspaceGitChangesRequest{}
	svc := service.TaskWorkspace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.GitChanges(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

/*
 * FileDiff POST /api/taskWorkspace/getFileDiff：
 * 返回单文件未提交变更的前后内容对（old=HEAD 版本 / new=工作区当前，前端 diff 视图消费）。
 */
func (api TaskWorkspace) FileDiff(ctx *gin.Context) {
	req := &types.TaskWorkspaceFileDiffRequest{}
	svc := service.TaskWorkspace{}
	if err := api.MakeContext(ctx).Bind(req).Validate(req).MakeService(&svc.Service).Errors; err != nil {
		api.JsonFail(err)
		return
	}
	data, err := svc.FileDiff(req)
	if err != nil {
		api.JsonFail(err)
		return
	}
	api.JsonOK(data)
}

/*
 * FileRaw GET /api/taskWorkspace/fileRaw：图片原始字节直连（<img src> 消费，
 * 类静态资源；校验链与 getFileContent 一致 + 图片扩展名白名单）。参数走 query（GET 无法 JSON body）。
 */
func (api TaskWorkspace) FileRaw(ctx *gin.Context) {
	svc := service.TaskWorkspace{}
	api.MakeContext(ctx).MakeService(&svc.Service)
	taskID := ctx.Query("taskId")
	path := ctx.Query("path")
	if taskID == "" || path == "" {
		api.JsonFail(errors.New("参数缺失"))
		return
	}
	raw, mime, err := svc.FileRaw(taskID, path)
	if err != nil {
		api.JsonFail(err)
		return
	}
	// agent 在终端持续改文件：禁缓存，新鲜度由前端 ?v= cache-buster 与重验控制。
	ctx.Header("Cache-Control", "no-store")
	ctx.Data(http.StatusOK, mime, raw)
}
