package types

/*
 * taskWorkspace 文件浏览（T5.1 本期：列表 + 内容预览）的 HTTP 入参/出参，查看归属 taskId / workspaceId 二选一（service 白名单解析目录），文件系统即内容 SSOT——只读遍历，无 watcher（前端手动刷新）；
 * 内容传输 kind 由后端定夺（前后端单一真相，下期 fileSave 编辑判定复用同一套），文本内部细分（markdown/代码/纯文本）是前端呈现 concern，不经由本层。
 */

// TaskWorkspaceFileContentKind 文件内容的传输类型。
type TaskWorkspaceFileContentKind string

const (
	IW_FILE_KIND_TEXT      TaskWorkspaceFileContentKind = "text"     // UTF-8 文本全文（markdown/代码/纯文本，呈现分派在前端按扩展名）
	IW_FILE_KIND_IMAGE     TaskWorkspaceFileContentKind = "image"    // 图片元信息（mimeType/size；字节经 fileRaw URL 直连加载，不 base64）
	IW_FILE_KIND_BINARY    TaskWorkspaceFileContentKind = "binary"   // 检出非文本（合法响应态，前端给信息提示）
	IW_FILE_KIND_TOO_LARGE TaskWorkspaceFileContentKind = "tooLarge" // 超文本预览上限（合法响应态，非错误；size 供提示文案）
)

/*
 * TaskWorkspaceFileTreeRequest 是 POST /api/taskWorkspace/getFileTree 的入参。
 * 查看归属（其余四个文件/Git 端点同款）：TaskID / WorkspaceID 互斥且必传其一（XOR
 * 无法用 gin binding 表达，校验收敛在 service 层，故均不设 required）。
 */
type TaskWorkspaceFileTreeRequest struct {
	TaskID      string `json:"taskId"`
	WorkspaceID string `json:"workspaceId"`
}

/*
 * TaskWorkspaceFileNode 是文件树节点（扁平表成员，WalkDir 词法序）：Path 为相对查看根目录（taskId 归属 {baseDir}/{taskId}/、workspaceId 归属工作空间目录本身）的正斜杠路径（自生成，前端直接用作树 key 与 getFileContent 的 path 入参）；
 * 目录 Size 恒 0。
 */
type TaskWorkspaceFileNode struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

/*
 * TaskWorkspaceFileTreeResponseData 是 getFileTree 的响应：一次性全树扁平节点表（前端纯函数组树）；
 * Truncated = 节点数超上限被截断（前端提示）。
 */
type TaskWorkspaceFileTreeResponseData struct {
	Nodes     []TaskWorkspaceFileNode `json:"nodes"`
	Truncated bool                    `json:"truncated"`
}

/*
 * TaskWorkspaceFileContentRequest 是 POST /api/taskWorkspace/getFileContent 的入参：Path 为相对查看根目录的路径（getFileTree 返回的 node.path，service 层防穿越）。
 */
type TaskWorkspaceFileContentRequest struct {
	TaskID      string `json:"taskId"`
	WorkspaceID string `json:"workspaceId"`
	Path        string `json:"path" binding:"required"`
}

/*
 * TaskWorkspaceFileContentResponseData 是 getFileContent 的响应，各 kind 字段占用：text → Content 全文；
 * image → MimeType（字节走 fileRaw）；binary/tooLarge → 仅 Size。
 */
type TaskWorkspaceFileContentResponseData struct {
	Kind     TaskWorkspaceFileContentKind `json:"kind"`
	Size     int64                        `json:"size"`
	MimeType string                       `json:"mimeType,omitempty"`
	Content  string                       `json:"content,omitempty"`
}

/*
 * Git 变更（T5.1：文件工具「Git 变更」模式）：口径只看未 commit 的变更（暂存区 + 工作区 vs HEAD）+ untracked 新文件，不对比基准分支、不探测 upstream（用户分步 commit，每次只关注当前未提交的部分）。
 */

// TaskWorkspaceGitChangesRequest 是 POST /api/taskWorkspace/getGitChanges 的入参。
type TaskWorkspaceGitChangesRequest struct {
	TaskID      string `json:"taskId"`
	WorkspaceID string `json:"workspaceId"`
}

/*
 * TaskWorkspaceGitChangeFile 单个文件的未提交变更（多仓库扁平，Path 带 repo/{name}/ 前缀——与 getFileTree 的 node.path 同构，前端复用同一套组树与展示）：Status：A=新增（HEAD 无此文件）/ M=修改 / D=删除（工作区已无）/ U=untracked；
 * Ins/Del 为变更行数（binary 文件为 -1，前端不展示数字）。
 */
type TaskWorkspaceGitChangeFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Ins    int    `json:"ins"`
	Del    int    `json:"del"`
}

/*
 * TaskWorkspaceGitChangesResponseData 是 getGitChanges 的响应：全部仓库未提交变更的扁平表（无变更为空数组）；
 * IsGitRepo = 查看根目录是否 git 仓库（taskId 归属恒 false——任务目录本身不是仓库，多仓库在其 repo/ 下；
 * workspaceId 归属经 gitutil.IsRepo 探测，git 命令失败按非 git 计）。
 */
type TaskWorkspaceGitChangesResponseData struct {
	Files     []TaskWorkspaceGitChangeFile `json:"files"`
	IsGitRepo bool                         `json:"isGitRepo"`
}

/*
 * TaskWorkspaceFileDiffRequest 是 POST /api/taskWorkspace/getFileDiff 的入参：Path 为 getGitChanges 返回的 file.path（taskId 归属带 repo/{name}/ 前缀，workspaceId 归属为仓库内相对路径）。
 */
type TaskWorkspaceFileDiffRequest struct {
	TaskID      string `json:"taskId"`
	WorkspaceID string `json:"workspaceId"`
	Path        string `json:"path" binding:"required"`
}

/*
 * TaskWorkspaceFileDiffResponseData 是 getFileDiff 的响应：前后内容对（前端 react-diff-viewer-continued 消费），OldContent = HEAD 版本（新增/untracked 为空串），NewContent = 工作区当前内容（删除为空串），Ins/Del 取自 numstat（binary 为 -1）；
 * Kind 仅 text/binary/tooLarge（diff 无图片态——图片变更直接给 binary 档信息提示）。
 */
type TaskWorkspaceFileDiffResponseData struct {
	Kind       TaskWorkspaceFileContentKind `json:"kind"`
	Status     string                       `json:"status"`
	Ins        int                          `json:"ins"`
	Del        int                          `json:"del"`
	OldContent string                       `json:"oldContent,omitempty"`
	NewContent string                       `json:"newContent,omitempty"`
}
