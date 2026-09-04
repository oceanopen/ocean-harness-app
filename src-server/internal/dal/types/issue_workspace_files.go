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
