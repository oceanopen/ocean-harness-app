package types

import "time"

// issueWorkspace 相关类型分两部分：
//  1. HTTP 入参/出参（Request/ResponseData，与其他模块 DTO 风格一致）；
//  2. 状态文件 schema（.workspace-init-state.json 的持久化结构）——该文件是工作空间初始化的
//     唯一持久真相（记录步骤/仓库级进度与幂等 manifest），Go 侧不落库，status 接口直接读它。
//
// 注意：issueWorkspace（issue 运行工作空间）与 tracker 的 workspace（任务管理容器）是两个概念。

// IssueWorkspaceStatus 工作空间初始化状态（同一套取值用于两个层面）：
// 服务态（status 接口派生的顶层结论，含状态文件之外的 NOT_INITIALIZED/CORRUPTED/INTERRUPTED）
// 与步骤/仓库级状态（steps[].status、repos[].status，多一个 SKIPPED 表示本期占位未实现）。
type IssueWorkspaceStatus string

const (
	IW_STATUS_NOT_INITIALIZED IssueWorkspaceStatus = "NOT_INITIALIZED" // 状态文件不存在（从未初始化）
	IW_STATUS_PENDING         IssueWorkspaceStatus = "PENDING"         // 待执行（步骤初始态）
	IW_STATUS_RUNNING         IssueWorkspaceStatus = "RUNNING"         // 后台任务执行中
	IW_STATUS_SUCCESS         IssueWorkspaceStatus = "SUCCESS"         // 全部步骤成功（= 工作空间就绪）
	IW_STATUS_FAILED          IssueWorkspaceStatus = "FAILED"          // 任一步骤失败（可重新触发重试）
	IW_STATUS_CORRUPTED       IssueWorkspaceStatus = "CORRUPTED"       // 状态文件存在但解析失败（重新 init 覆盖修复）
	IW_STATUS_INTERRUPTED     IssueWorkspaceStatus = "INTERRUPTED"     // 文件 RUNNING 但无活跃任务（进程中断遗留，可重试）
	IW_STATUS_SKIPPED         IssueWorkspaceStatus = "SKIPPED"         // 步骤/仓库级：本期占位未实现（后续任务接入）
)

// 初始化步骤 key（固定顺序执行：createDirs → sshConfig → mcpConfig → cloneRepos）。
const (
	IW_STEP_KEY_CREATE_DIRS = "createDirs" // 创建 {issueId}/.ssh、{issueId}/repo 目录结构
	IW_STEP_KEY_SSH_CONFIG  = "sshConfig"  // 生成 .ssh/config（T1.2，本期占位）
	IW_STEP_KEY_MCP_CONFIG  = "mcpConfig"  // 生成 .mcp.json（T1.3，本期占位）
	IW_STEP_KEY_CLONE_REPOS = "cloneRepos" // clone 各仓库 + agent_{issueId} 分支（T1.4，本期占位）
)

// IssueWorkspaceInitRequest 是 POST /api/issueWorkspace/init 的入参。
// baseDir 即设置页的 workspace_base_dir（前端逐请求传入，Go 不持久化）；issueId 为 t_project_issues.id（uuid）。
type IssueWorkspaceInitRequest struct {
	IssueID string `json:"issueId" binding:"required"`
	BaseDir string `json:"baseDir" binding:"required"` // 须为绝对路径（service 层校验）
}

// IssueWorkspaceStatusRequest 是 POST /api/issueWorkspace/status 的入参（读状态文件派生，不查库）。
type IssueWorkspaceStatusRequest struct {
	IssueID string `json:"issueId" binding:"required"`
	BaseDir string `json:"baseDir" binding:"required"` // 须为绝对路径（service 层校验）
}

// IssueWorkspaceStatusResponseData 是 init/status 共用的响应：serverStatus 为顶层结论，
// state 为状态文件全文（未初始化时为 null）。
type IssueWorkspaceStatusResponseData struct {
	ServerStatus IssueWorkspaceStatus `json:"serverStatus"`
	State        *IssueWorkspaceState `json:"state"`
}

// IssueWorkspaceState 是 .workspace-init-state.json 的 schema（位于 {baseDir}/{issueId}/ 下）。
// status 为顶层状态；steps 为固定顺序步骤及各自进度；manifest 为幂等键（已初始化的仓库+基准分支
// 集合，与 issue 当前关联一致且 status=SUCCESS 时跳过重复初始化）；error 为顶层失败原因。
type IssueWorkspaceState struct {
	Version   int                     `json:"version"`
	IssueID   string                  `json:"issueId"`
	BaseDir   string                  `json:"baseDir"`
	Status    IssueWorkspaceStatus    `json:"status"`
	Steps     []*IssueWorkspaceStep   `json:"steps"`
	Manifest  []IssueWorkspaceRepoRef `json:"manifest"`
	Error     string                  `json:"error"`
	CreatedAt time.Time               `json:"createdAt"`
	UpdatedAt time.Time               `json:"updatedAt"`
}

// IssueWorkspaceStep 是单个全局步骤的进度。repos 仅 cloneRepos 步骤使用（仓库级子状态，
// clone 进度载体；本期占位步骤整体置 SKIPPED）。
type IssueWorkspaceStep struct {
	Key    string                     `json:"key"`
	Title  string                     `json:"title"`
	Status IssueWorkspaceStatus       `json:"status"`
	Repos  []*IssueWorkspaceRepoState `json:"repos,omitempty"`
}

// IssueWorkspaceRepoState 是仓库级子状态（cloneRepos 步骤内逐仓库进度）。
type IssueWorkspaceRepoState struct {
	LocalRepositoryID int                  `json:"localRepositoryId"`
	Name              string               `json:"name"`
	RemoteURL         string               `json:"remoteUrl"`
	BaseBranch        string               `json:"baseBranch"`   // 基准分支（issue 关联时选定的 repository_branch，可为空）
	TargetBranch      string               `json:"targetBranch"` // 目标分支，统一 agent_{issueId}
	Status            IssueWorkspaceStatus `json:"status"`
	Message           string               `json:"message"` // 失败原因/说明
}

// IssueWorkspaceRepoRef 是幂等 manifest 的一项（对比键 localRepositoryId+baseBranch）。
type IssueWorkspaceRepoRef struct {
	LocalRepositoryID int    `json:"localRepositoryId"`
	RemoteURL         string `json:"remoteUrl"`
	BaseBranch        string `json:"baseBranch"`
}
