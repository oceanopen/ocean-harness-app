package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/gitutil"
)

// 工作空间 Git 变更（T5.1：文件工具「Git 变更」模式）。方法挂在 IssueWorkspace 上（同包
// 多文件分工惯例，见 issue_workspace.go 头注释）。
//
// 口径定稿：只看未 commit 的变更（暂存区 + 工作区 vs HEAD）+ untracked 新文件——用户分步
// commit（子任务/分步骤各一次），每次只关注当前未提交的部分；不对比基准分支、不探测
// upstream，已提交历史一概不算。
//
// 仓库清单不读状态文件——直接扫描 {root}/repo/ 一级子目录（.git 存在才算 git 仓库），
// 状态文件缺失/损坏不影响本功能；非 git 目录静默跳过。

// iwRepoDirPrefix 变更表 path 的仓库前缀（与 clone 落盘布局 repo/{name}/ 一致）。
const iwRepoDirPrefix = "repo"

// GitChanges POST /api/issueWorkspace/getGitChanges：列出全部仓库的未提交变更（扁平表，
// Path 带 repo/{name}/ 前缀，与 getFileTree 的 node.path 同构——前端复用同一套组树）。
// 与文件树同名单的忽略段（node_modules 等）不进变更表：树里看不见的文件，这里也不出现。
func (svc IssueWorkspace) GitChanges(req *types.IssueWorkspaceGitChangesRequest) (*types.IssueWorkspaceGitChangesResponseData, error) {
	root, err := issueWorkspaceFileRoot(req.BaseDir, req.IssueID)
	if err != nil {
		return nil, err
	}
	repoNames, err := issueWorkspaceRepoNames(root)
	if err != nil {
		return nil, err
	}
	data := &types.IssueWorkspaceGitChangesResponseData{Files: []types.IssueWorkspaceGitChangeFile{}}
	for _, name := range repoNames {
		repoAbs := filepath.Join(root, iwRepoDirPrefix, name)
		entries, err := gitutil.UncommittedChanges(repoAbs)
		if err != nil {
			return nil, errors.New("仓库 " + name + " 读取变更失败: " + err.Error())
		}
		for _, e := range entries {
			if issueWorkspaceHasIgnoredSegment(e.Path) {
				continue
			}
			data.Files = append(data.Files, types.IssueWorkspaceGitChangeFile{
				Path:   iwRepoDirPrefix + "/" + name + "/" + e.Path,
				Status: e.Status,
				Ins:    e.Ins,
				Del:    e.Del,
			})
		}
	}
	return data, nil
}

// FileDiff POST /api/issueWorkspace/getFileDiff：返回单文件未提交变更的前后内容对
// （OldContent=HEAD 版本，NewContent=工作区当前）。文本判定沿用 FileContent 范式
// （NUL/非 UTF-8 → binary；超上限 → tooLarge）；删除态工作区文件不存在按空串处理
// （EvalSymlinks 链要求存在，故不复用 issueWorkspaceFilePath，改用轻量段校验）。
func (svc IssueWorkspace) FileDiff(req *types.IssueWorkspaceFileDiffRequest) (*types.IssueWorkspaceFileDiffResponseData, error) {
	root, err := issueWorkspaceFileRoot(req.BaseDir, req.IssueID)
	if err != nil {
		return nil, err
	}
	repoName, rel, err := issueWorkspaceSplitRepoPath(req.Path)
	if err != nil {
		return nil, err
	}
	repoAbs := filepath.Join(root, iwRepoDirPrefix, repoName)
	if !gitutil.IsGitDir(repoAbs) {
		return nil, errors.New("仓库不存在: " + repoName)
	}

	// 状态与行数：从该仓库未提交变更里按 rel 对齐取；找不到 = 已提交或路径无效。
	entries, err := gitutil.UncommittedChanges(repoAbs)
	if err != nil {
		return nil, err
	}
	var status string
	var ins, del = -1, -1
	for _, e := range entries {
		if e.Path == rel {
			status, ins, del = e.Status, e.Ins, e.Del
			break
		}
	}
	if status == "" {
		return nil, errors.New("该文件没有未提交变更（可能刚提交过，请刷新变更列表）")
	}

	// old = HEAD 版本（untracked 恒空；其余 HEAD 无此文件 = 新增，同为空串）。
	var oldContent string
	if status != "U" {
		content, _, showErr := gitutil.ShowHeadFile(repoAbs, rel)
		if showErr != nil {
			return nil, showErr
		}
		oldContent = content
	}
	// new = 工作区当前内容（删除态文件不存在 → 空串）。
	newContent := ""
	if raw, readErr := os.ReadFile(filepath.Join(repoAbs, filepath.FromSlash(rel))); readErr == nil {
		newContent = string(raw)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}

	data := &types.IssueWorkspaceFileDiffResponseData{Status: status, Ins: ins, Del: del}
	if len(oldContent)+len(newContent) > iwFileMaxText { // 沿用文件预览 2MB 档（old+new 合计）
		data.Kind = types.IW_FILE_KIND_TOO_LARGE
		return data, nil
	}
	if strings.IndexByte(oldContent, 0) >= 0 || !utf8.ValidString(oldContent) ||
		strings.IndexByte(newContent, 0) >= 0 || !utf8.ValidString(newContent) {
		data.Kind = types.IW_FILE_KIND_BINARY
		return data, nil
	}
	data.Kind = types.IW_FILE_KIND_TEXT
	data.OldContent = oldContent
	data.NewContent = newContent
	return data, nil
}

// issueWorkspaceRepoNames 扫描 {root}/repo/ 下的一级子目录，返回其中的 git 仓库目录名
// （.git 存在才算；非 git 目录跳过）。repo 目录不存在返回空切片（未初始化语义，前端空态）。
func issueWorkspaceRepoNames(root string) ([]string, error) {
	repoRoot := filepath.Join(root, iwRepoDirPrefix)
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && gitutil.IsGitDir(filepath.Join(repoRoot, e.Name())) {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// issueWorkspaceSplitRepoPath 拆解变更表的 path（repo/{name}/{rel...}）：校验前缀、仓库名
// 非法段（.. 等）、rel 非空且无反斜杠/NUL/绝对路径形态，返回 (仓库名, 仓库内相对路径)。
func issueWorkspaceSplitRepoPath(path string) (string, string, error) {
	if strings.ContainsAny(path, "\x00\\") || filepath.IsAbs(path) {
		return "", "", errors.New("path 非法")
	}
	rest, ok := strings.CutPrefix(path, iwRepoDirPrefix+"/")
	if !ok {
		return "", "", errors.New("path 须为 repo/{仓库名}/ 前缀的工作空间相对路径")
	}
	name, rel, cut := strings.Cut(rest, "/")
	if !cut || name == "" || rel == "" {
		return "", "", errors.New("path 须为 repo/{仓库名}/ 前缀的工作空间相对路径")
	}
	if name == ".." || name == "." || strings.HasPrefix(rel, "../") || rel == ".." {
		return "", "", errors.New("path 非法")
	}
	if issueWorkspaceHasIgnoredSegment(path) {
		return "", "", errors.New("path 非法")
	}
	return name, rel, nil
}

// issueWorkspaceHasIgnoredSegment 判断正斜杠路径的任一段是否命中文件树忽略名单
// （变更表与文件树可见性对齐：树里看不见的文件，变更表也不出现）。
func issueWorkspaceHasIgnoredSegment(slashPath string) bool {
	for _, seg := range strings.Split(slashPath, "/") {
		if _, ignored := issueWorkspaceIgnoredNames[seg]; ignored {
			return true
		}
	}
	return false
}
