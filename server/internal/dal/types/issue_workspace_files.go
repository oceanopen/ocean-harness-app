package types

// issueWorkspace 文件浏览（T5.1 本期：列表 + 内容预览）的 HTTP 入参/出参。
// 文件系统即 SSOT——service 只读遍历 {baseDir}/{issueId}/，不查库、无 watcher（前端手动刷新）。
//
// 内容传输 kind 由后端定夺（前后端单一真相，下期 fileSave 编辑判定复用同一套）；
// 文本内部细分（markdown/代码/纯文本）是前端呈现 concern，不经由本层。

// IssueWorkspaceFileContentKind 文件内容的传输类型。
type IssueWorkspaceFileContentKind string

const (
	IW_FILE_KIND_TEXT      IssueWorkspaceFileContentKind = "text"     // UTF-8 文本全文（markdown/代码/纯文本，呈现分派在前端按扩展名）
	IW_FILE_KIND_IMAGE     IssueWorkspaceFileContentKind = "image"    // 图片元信息（mimeType/size；字节经 fileRaw URL 直连加载，不 base64）
	IW_FILE_KIND_BINARY    IssueWorkspaceFileContentKind = "binary"   // 检出非文本（合法响应态，前端给信息提示）
	IW_FILE_KIND_TOO_LARGE IssueWorkspaceFileContentKind = "tooLarge" // 超文本预览上限（合法响应态，非错误；size 供提示文案）
)

// IssueWorkspaceFileTreeRequest 是 POST /api/issueWorkspace/getFileTree 的入参。
type IssueWorkspaceFileTreeRequest struct {
	IssueID string `json:"issueId" binding:"required"`
	BaseDir string `json:"baseDir" binding:"required"` // 须为绝对路径（service 层校验）
}

// IssueWorkspaceFileNode 是文件树节点（扁平表成员，WalkDir 词法序）。Path 为相对
// {baseDir}/{issueId}/ 的正斜杠路径（自生成，前端直接用作树 key 与 getFileContent 的
// path 入参）；目录 Size 恒 0。
type IssueWorkspaceFileNode struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// IssueWorkspaceFileTreeResponseData 是 getFileTree 的响应：一次性全树扁平节点表
// （前端纯函数组树）；Truncated = 节点数超上限被截断（前端提示）。
type IssueWorkspaceFileTreeResponseData struct {
	Nodes     []IssueWorkspaceFileNode `json:"nodes"`
	Truncated bool                     `json:"truncated"`
}

// IssueWorkspaceFileContentRequest 是 POST /api/issueWorkspace/getFileContent 的入参。
// Path 为相对 {baseDir}/{issueId}/ 的路径（getFileTree 返回的 node.path，service 层防穿越）。
type IssueWorkspaceFileContentRequest struct {
	IssueID string `json:"issueId" binding:"required"`
	BaseDir string `json:"baseDir" binding:"required"` // 须为绝对路径（service 层校验）
	Path    string `json:"path" binding:"required"`
}

// IssueWorkspaceFileContentResponseData 是 getFileContent 的响应。各 kind 字段占用：
// text → Content 全文；image → MimeType（字节走 fileRaw）；binary/tooLarge → 仅 Size。
type IssueWorkspaceFileContentResponseData struct {
	Kind     IssueWorkspaceFileContentKind `json:"kind"`
	Size     int64                         `json:"size"`
	MimeType string                        `json:"mimeType,omitempty"`
	Content  string                        `json:"content,omitempty"`
}

// —— Git 变更（T5.1：文件工具「Git 变更」模式）——
// 口径：只看未 commit 的变更（暂存区 + 工作区 vs HEAD）+ untracked 新文件，不对比基准分支、
// 不探测 upstream（用户分步 commit，每次只关注当前未提交的部分）。

// IssueWorkspaceGitChangesRequest 是 POST /api/issueWorkspace/getGitChanges 的入参。
type IssueWorkspaceGitChangesRequest struct {
	IssueID string `json:"issueId" binding:"required"`
	BaseDir string `json:"baseDir" binding:"required"` // 须为绝对路径（service 层校验）
}

// IssueWorkspaceGitChangeFile 单个文件的未提交变更（多仓库扁平，Path 带 repo/{name}/ 前缀——
// 与 getFileTree 的 node.path 同构，前端复用同一套组树与展示）。
// Status：A=新增（HEAD 无此文件）/ M=修改 / D=删除（工作区已无）/ U=untracked（git 未跟踪的新文件）；
// Ins/Del 为变更行数（binary 文件为 -1，前端不展示数字）。
type IssueWorkspaceGitChangeFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Ins    int    `json:"ins"`
	Del    int    `json:"del"`
}

// IssueWorkspaceGitChangesResponseData 是 getGitChanges 的响应：全部仓库未提交变更的扁平表
// （无变更为空数组）。
type IssueWorkspaceGitChangesResponseData struct {
	Files []IssueWorkspaceGitChangeFile `json:"files"`
}

// IssueWorkspaceFileDiffRequest 是 POST /api/issueWorkspace/getFileDiff 的入参。
// Path 为 getGitChanges 返回的 file.path（repo/{name}/ 前缀的工作空间相对路径）。
type IssueWorkspaceFileDiffRequest struct {
	IssueID string `json:"issueId" binding:"required"`
	BaseDir string `json:"baseDir" binding:"required"` // 须为绝对路径（service 层校验）
	Path    string `json:"path" binding:"required"`
}

// IssueWorkspaceFileDiffResponseData 是 getFileDiff 的响应：前后内容对（前端
// react-diff-viewer-continued 消费）。OldContent = HEAD 版本（新增/untracked 为空串），
// NewContent = 工作区当前内容（删除为空串）；Ins/Del 取自 numstat（binary 为 -1）。
// Kind 仅 text/binary/tooLarge（diff 无图片态——图片变更直接给 binary 档信息提示）。
type IssueWorkspaceFileDiffResponseData struct {
	Kind       IssueWorkspaceFileContentKind `json:"kind"`
	Status     string                        `json:"status"`
	Ins        int                           `json:"ins"`
	Del        int                           `json:"del"`
	OldContent string                        `json:"oldContent,omitempty"`
	NewContent string                        `json:"newContent,omitempty"`
}
