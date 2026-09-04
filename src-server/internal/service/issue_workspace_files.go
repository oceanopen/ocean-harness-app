package service

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"ocean-harness/src-server/internal/dal/types"
)

// 工作空间文件浏览（T5.1 本期：列表 + 预览）。方法挂在 IssueWorkspace 上（同包多文件分工
// 惯例，见 issue_workspace.go 头注释）：只读访问 {baseDir}/{issueId}/，与 DB 无关。
//
// 设计要点：
//   - 一次性全树：忽略构建产物后典型工作空间为数千节点，本机单次遍历毫秒级；懒加载的
//     逐目录增量合并是「先占位后回填」式复杂度，不做。节点上限兜底病态目录。
//   - 忽略名单同时约束两个接口——getFileTree 看不见的文件，getFileContent 点名也拿不到。
//     .ssh 按用户决策移出名单（工作空间内 ssh 配置需在树中可见可查，2026-09-04）。
//   - symlink：树遍历跳过（不进树）；内容读取经 EvalSymlinks 全链解析 + root 前缀断言，
//     逃出 root 一律拒绝（指向 root 内的 symlink 解析后可读——树不展示 symlink，此路
//     仅兜直接点名读取）。

const (
	iwFileMaxEntries = 20000   // 树节点上限（超出置 Truncated，前端提示）
	iwFileMaxText    = 2 << 20 // 文本预览上限 2MB（fileRaw 图片直连不走此限）
)

// 忽略名单：名字精确匹配、任意深度、目录与文件混判。收录工作空间状态文件与常见构建
// 产物目录——否则单仓库即可打爆节点上限。.ssh 已按用户决策移出（树中可见）。
var issueWorkspaceIgnoredNames = map[string]struct{}{
	".git": {}, ".DS_Store": {}, "node_modules": {},
	".workspace-init-state.json": {}, "__pycache__": {}, "target": {},
	"dist": {}, "build": {}, "out": {}, ".next": {}, ".venv": {}, "venv": {},
}

// 图片扩展名 → mime（getFileContent 回元信息；fileRaw 按此白名单直连返回原始字节，
// 前端 <img src> 加载——svg 亦走 <img>，脚本天然不执行）。
var issueWorkspaceImageMimes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

// FileTree POST /api/issueWorkspace/getFileTree：一次性返回工作空间全部文件/目录的
// 扁平节点表（WalkDir 词法序，父目录天然先于其内条目），前端纯函数组树。
func (svc IssueWorkspace) FileTree(req *types.IssueWorkspaceFileTreeRequest) (*types.IssueWorkspaceFileTreeResponseData, error) {
	root, err := issueWorkspaceFileRoot(req.BaseDir, req.IssueID)
	if err != nil {
		return nil, err
	}
	data := &types.IssueWorkspaceFileTreeResponseData{Nodes: []types.IssueWorkspaceFileNode{}}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil // 根自身不出节点（path 语义为相对根的路径）
		}
		if _, ignored := issueWorkspaceIgnoredNames[d.Name()]; ignored {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// symlink/fifo/socket 等非普通文件不进树（WalkDir 不跟随目录软链，此处统一拦截）
		if !d.IsDir() && !d.Type().IsRegular() {
			return nil
		}
		if len(data.Nodes) >= iwFileMaxEntries {
			data.Truncated = true
			return fs.SkipAll
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		var size int64
		if !d.IsDir() {
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			size = info.Size()
		}
		data.Nodes = append(data.Nodes, types.IssueWorkspaceFileNode{
			Path:  filepath.ToSlash(rel),
			Name:  d.Name(),
			IsDir: d.IsDir(),
			Size:  size,
		})
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errors.New("工作空间目录不存在或未初始化")
		}
		return nil, err
	}
	return data, nil
}

// FileContent POST /api/issueWorkspace/getFileContent：读取单个文件内容并定夺传输 kind。
// 判定次序（先 stat 后 read，超大文件不产生读取 IO）：安全链 → Lstat regular → 扩展名
// 图片分派（base64）→ 文本上限 → 整读嗅探（NUL / 非 UTF-8 → binary）→ text 全文。
func (svc IssueWorkspace) FileContent(req *types.IssueWorkspaceFileContentRequest) (*types.IssueWorkspaceFileContentResponseData, error) {
	root, err := issueWorkspaceFileRoot(req.BaseDir, req.IssueID)
	if err != nil {
		return nil, err
	}
	abs, err := issueWorkspaceFilePath(root, req.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("仅支持普通文件（目录/符号链接不可预览）")
	}

	if mime, ok := issueWorkspaceImageMimes[strings.ToLower(filepath.Ext(req.Path))]; ok {
		// 图片字节经 fileRaw URL 直连加载（<img src>，类静态资源），此处仅回元信息
		//（stat-only——不整读、不 base64，大小上限对图片不再适用）。
		return &types.IssueWorkspaceFileContentResponseData{
			Kind:     types.IW_FILE_KIND_IMAGE,
			Size:     info.Size(),
			MimeType: mime,
		}, nil
	}

	if info.Size() > iwFileMaxText {
		return &types.IssueWorkspaceFileContentResponseData{Kind: types.IW_FILE_KIND_TOO_LARGE, Size: info.Size()}, nil
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	if bytes.IndexByte(raw, 0) >= 0 || !utf8.Valid(raw) {
		// 非 UTF-8 文本（如 GBK）会落此档——已知限制，按二进制提示处理。
		return &types.IssueWorkspaceFileContentResponseData{Kind: types.IW_FILE_KIND_BINARY, Size: info.Size()}, nil
	}
	return &types.IssueWorkspaceFileContentResponseData{
		Kind:    types.IW_FILE_KIND_TEXT,
		Size:    info.Size(),
		Content: string(raw),
	}, nil
}

// FileRaw GET /api/issueWorkspace/fileRaw：图片原始字节直连（<img src> 消费，类静态资源
// 服务）。校验链与 getFileContent 完全一致（baseDir/issueId/rel 路径/忽略名单/symlink
// 解析断言）+ 扩展名白名单（仅图片——本端点为 <img> 服务，不做通用文件下载）。
func (svc IssueWorkspace) FileRaw(baseDir, issueID, rel string) ([]byte, string, error) {
	root, err := issueWorkspaceFileRoot(baseDir, issueID)
	if err != nil {
		return nil, "", err
	}
	mime, ok := issueWorkspaceImageMimes[strings.ToLower(filepath.Ext(rel))]
	if !ok {
		return nil, "", errors.New("仅支持图片文件")
	}
	abs, err := issueWorkspaceFilePath(root, rel)
	if err != nil {
		return nil, "", err
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", err
	}
	return raw, mime, nil
}

// issueWorkspaceFileRoot 校验并拼接工作空间根目录 {baseDir}/{issueId}
// （校验复用 issueWorkspace 域既有范式：绝对路径 + issueId 防穿越）。
func issueWorkspaceFileRoot(baseDir, issueID string) (string, error) {
	if !filepath.IsAbs(baseDir) {
		return "", errors.New("baseDir 须为绝对路径")
	}
	if !issueWorkspaceValidIssueID(issueID) {
		return "", errors.New("issueId 非法")
	}
	return filepath.Join(baseDir, issueID), nil
}

// issueWorkspaceFilePath 校验相对路径并解析为 root 下的绝对路径：拒绝空串/绝对路径/
// NUL/反斜杠/Clean 后逃逸 root（".." 与 "../x"）；逐段对照忽略名单（getFileTree 看不见
// 的文件点名也拿不到）。最后经 EvalSymlinks 全链解析并断言仍在 root 内——字符串校验
// 与 Lstat 均拦不住「中间组件是指向 root 外的 symlink 目录」（git checkout 会如实落盘
// 仓库内 symlink，终端 agent 也可 ln -s）；返回 resolved，调用方的 Lstat/ReadFile 落在
// 已验证路径上（顺带消除 Lstat→ReadFile 的替换竞态）。
func issueWorkspaceFilePath(root, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("path 不能为空")
	}
	if strings.ContainsAny(rel, "\x00\\") || filepath.IsAbs(rel) {
		return "", errors.New("path 非法")
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path 非法")
	}
	for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
		if _, ignored := issueWorkspaceIgnoredNames[seg]; ignored {
			return "", errors.New("path 非法")
		}
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(clean)))
	if err != nil {
		return "", err
	}
	if resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(filepath.Separator)) {
		return "", errors.New("path 非法")
	}
	return resolved, nil
}
