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

/*
 * 工作空间 Git 变更（T5.1：文件工具「Git 变更」模式）。方法挂在 TaskWorkspace 上（同包多文件分工惯例，见 task_workspace.go 头注释）。
 * 口径定稿：只看未 commit 的变更（暂存区 + 工作区 vs HEAD）+ untracked 新文件——用户分步 commit（子任务/分步骤各一次），每次只关注当前未提交的部分；不对比基准分支、不探测 upstream，已提交历史一概不算。
 * 仓库清单不读状态文件——直接扫描 {root}/repo/ 一级子目录（.git 存在才算 git 仓库），状态文件缺失/损坏不影响本功能；非 git 目录静默跳过。
 */

// iwRepoDirPrefix 变更表 path 的仓库前缀（与 clone 落盘布局 repo/{name}/ 一致）。
const iwRepoDirPrefix = "repo"

/*
 * GitChanges POST /api/taskWorkspace/getGitChanges：列出查看根的未提交变更（扁平表）。
 * taskId 归属扫描 {root}/repo/ 多仓库，Path 带 repo/{name}/ 前缀（与 getFileTree 的
 * node.path 同构——前端复用同一套组树）；workspaceId 归属根即仓库本体，Path 为仓库内
 * 相对路径，IsRepo 探测失败/非 git 均返回空表 + IsGitRepo=false 不报错。
 * 与文件树同名单的忽略段（node_modules 等）不进变更表：树里看不见的文件，这里也不出现。
 */
func (svc TaskWorkspace) GitChanges(req *types.TaskWorkspaceGitChangesRequest) (*types.TaskWorkspaceGitChangesResponseData, error) {
	root, err := svc.taskWorkspaceViewRoot(req.TaskID, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if req.WorkspaceID != "" {
		return workspaceRootGitChanges(root)
	}
	repoNames, err := taskWorkspaceRepoNames(root)
	if err != nil {
		return nil, err
	}
	data := &types.TaskWorkspaceGitChangesResponseData{Files: []types.TaskWorkspaceGitChangeFile{}}
	for _, name := range repoNames {
		repoAbs := filepath.Join(root, iwRepoDirPrefix, name)
		entries, err := gitutil.UncommittedChanges(repoAbs)
		if err != nil {
			return nil, errors.New("仓库 " + name + " 读取变更失败: " + err.Error())
		}
		for _, e := range entries {
			if taskWorkspaceHasIgnoredSegment(e.Path) {
				continue
			}
			data.Files = append(data.Files, types.TaskWorkspaceGitChangeFile{
				Path:   iwRepoDirPrefix + "/" + name + "/" + e.Path,
				Status: e.Status,
				Ins:    e.Ins,
				Del:    e.Del,
			})
		}
	}
	return data, nil
}

/*
 * workspaceRootGitChanges workspaceId 归属的变更读取：根即 git 仓库（IsRepo 探测，
 * git 命令不存在等失败按非 git 处理），变更 Path 为仓库内相对路径（无 repo/ 前缀）。
 */
func workspaceRootGitChanges(root string) (*types.TaskWorkspaceGitChangesResponseData, error) {
	data := &types.TaskWorkspaceGitChangesResponseData{Files: []types.TaskWorkspaceGitChangeFile{}}
	if !gitutil.IsRepo(root) {
		return data, nil
	}
	data.IsGitRepo = true
	entries, err := gitutil.UncommittedChanges(root)
	if err != nil {
		return nil, errors.New("仓库读取变更失败: " + err.Error())
	}
	for _, e := range entries {
		if taskWorkspaceHasIgnoredSegment(e.Path) {
			continue
		}
		data.Files = append(data.Files, types.TaskWorkspaceGitChangeFile{
			Path:   e.Path,
			Status: e.Status,
			Ins:    e.Ins,
			Del:    e.Del,
		})
	}
	return data, nil
}

/*
 * FileDiff POST /api/taskWorkspace/getFileDiff：返回单文件未提交变更的前后内容对（OldContent=HEAD 版本，NewContent=工作区当前）。
 * 文本判定沿用 FileContent 范式（NUL/非 UTF-8 → binary；超上限 → tooLarge）；删除态工作区文件不存在按空串处理（EvalSymlinks 链要求存在，故不复用 taskWorkspaceFilePath，两归属均用轻量段校验——taskId 归属 taskWorkspaceSplitRepoPath，workspaceId 归属 taskWorkspaceValidateRelPath）。
 */
func (svc TaskWorkspace) FileDiff(req *types.TaskWorkspaceFileDiffRequest) (*types.TaskWorkspaceFileDiffResponseData, error) {
	root, err := svc.taskWorkspaceViewRoot(req.TaskID, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	var repoAbs, rel, missingMsg string
	if req.WorkspaceID != "" {
		if err := taskWorkspaceValidateRelPath(req.Path); err != nil {
			return nil, err
		}
		repoAbs, rel, missingMsg = root, req.Path, "目录不是 git 仓库"
	} else {
		repoName, r, splitErr := taskWorkspaceSplitRepoPath(req.Path)
		if splitErr != nil {
			return nil, splitErr
		}
		repoAbs, rel, missingMsg = filepath.Join(root, iwRepoDirPrefix, repoName), r, "仓库不存在: "+repoName
	}
	if !gitutil.IsGitDir(repoAbs) {
		return nil, errors.New(missingMsg)
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

	data := &types.TaskWorkspaceFileDiffResponseData{Status: status, Ins: ins, Del: del}
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

/*
 * taskWorkspaceRepoNames 扫描 {root}/repo/ 下的一级子目录，返回其中的 git 仓库目录名
 * （.git 存在才算；非 git 目录跳过）。repo 目录不存在返回空切片（未初始化语义，前端空态）。
 */
func taskWorkspaceRepoNames(root string) ([]string, error) {
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

/*
 * taskWorkspaceValidateRelPath workspaceId 归属 FileDiff 的 path 轻量校验（仓库内相对路径）：
 * 拒绝空串/绝对路径/NUL/反斜杠/Clean 后逃逸/忽略名单段（同 taskWorkspaceSplitRepoPath 的
 * 校验口径，仅少 repo/ 前缀拆解——删除态文件不存在，不做 EvalSymlinks 存在性链）。
 */
func taskWorkspaceValidateRelPath(path string) error {
	if path == "" || strings.ContainsAny(path, "\x00\\") || filepath.IsAbs(path) {
		return errors.New("path 非法")
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errors.New("path 非法")
	}
	if taskWorkspaceHasIgnoredSegment(filepath.ToSlash(clean)) {
		return errors.New("path 非法")
	}
	return nil
}

/*
 * taskWorkspaceSplitRepoPath 拆解变更表的 path（repo/{name}/{rel...}）：校验前缀、仓库名
 * 非法段（.. 等）、rel 非空且无反斜杠/NUL/绝对路径形态，返回 (仓库名, 仓库内相对路径)。
 */
func taskWorkspaceSplitRepoPath(path string) (string, string, error) {
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
	if taskWorkspaceHasIgnoredSegment(path) {
		return "", "", errors.New("path 非法")
	}
	return name, rel, nil
}

/*
 * taskWorkspaceHasIgnoredSegment 判断正斜杠路径的任一段是否命中文件树忽略名单
 * （变更表与文件树可见性对齐：树里看不见的文件，变更表也不出现）。
 */
func taskWorkspaceHasIgnoredSegment(slashPath string) bool {
	for _, seg := range strings.Split(slashPath, "/") {
		if _, ignored := taskWorkspaceIgnoredNames[seg]; ignored {
			return true
		}
	}
	return false
}
