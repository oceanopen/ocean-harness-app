package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/gitutil"
)

/*
 * issueWorkspace archive：归档/取消任务运行目录。归档与取消同为工程化确定性操作（不依赖 AI）：
 * 删除 {baseDir}/{taskId}/ 目录（与状态流转解耦，无状态列）。
 * 两段式契约（类型注释见 types/issue_workspace.go）：force=false 仅安全检查返回警告不执行；force=true 跳过检查直接执行。
 * 同域文件分工：入口/校验在本文件；并发准入见 issue_workspace_registry.go；状态文件读写见 issue_workspace_state.go；git 检查函数见 gitutil/status.go。
 */

/*
 * Archive 归档/取消 issue 工作空间。执行序：入参校验 → 解析目录（含 issue 存在性）→ 并发防护
 * （init 进行中拒绝）→ force=false 安全检查 / force=true 删目录 + 事务流转状态（Move 范式）。
 */
func (svc IssueWorkspace) Archive(req *types.IssueWorkspaceArchiveRequest) (*types.IssueWorkspaceArchiveResponseData, error) {
	if !issueWorkspaceValidIssueID(req.IssueID) {
		return nil, errors.New("issueId 非法")
	}
	if req.Action != types.IW_ARCHIVE_ACTION_ARCHIVE && req.Action != types.IW_ARCHIVE_ACTION_CANCEL {
		return nil, errors.New("action 须为 archive 或 cancel")
	}
	baseDir, err := svc.resolveIssueBaseDir(req.IssueID)
	if err != nil {
		return nil, err
	}
	// 并发防护：后台 goroutine 正在写 {issueId}/ 目录（clone 等），此刻删除会与其竞争。
	if issueWorkspaceActive(req.IssueID) {
		return nil, errors.New("工作空间初始化进行中，请完成后再归档/取消")
	}

	if !req.Force {
		warnings, err := issueWorkspaceArchiveWarnings(baseDir, req.IssueID)
		if err != nil {
			return nil, err
		}
		return &types.IssueWorkspaceArchiveResponseData{Executed: false, Warnings: warnings}, nil
	}

	/*
	 * 删目录（外部副作用不可回滚；目录不存在返回 nil，天然幂等——未初始化/重复归档均安全）。
	 * 终端会话已由前端在调本段前 ptyShutdownIssue。
	 */
	if err := os.RemoveAll(filepath.Join(baseDir, req.IssueID)); err != nil {
		return nil, fmt.Errorf("删除任务运行目录失败: %w", err)
	}
	return &types.IssueWorkspaceArchiveResponseData{Executed: true, Warnings: []string{}}, nil
}

/*
 * issueWorkspaceArchiveWarnings 安全检查（force=false 段）：遍历状态文件 cloneRepos 步骤的仓库清单，逐仓库探测未提交变更（git status --porcelain）与未推送提交（本地 ref 对照 origin/{targetBranch}..HEAD，不联网 fetch；origin 引用不存在 = 分支从未推送）。
 * 检查失败的仓库记警告不中止——force 段才是删除决策点，宁可多警告不可漏警告。
 * 状态文件缺失/损坏返回空列表（无可检查项，未初始化工作空间可直接归档）。
 */
func issueWorkspaceArchiveWarnings(baseDir, issueID string) ([]string, error) {
	state, err := loadIssueWorkspaceState(baseDir, issueID)
	if err != nil || state == nil {
		return []string{}, nil
	}
	warnings := make([]string, 0)
	for _, step := range state.Steps {
		if step.Key != types.IW_STEP_KEY_CLONE_REPOS {
			continue
		}
		for _, repo := range step.Repos {
			warnings = appendArchiveRepoWarnings(warnings, baseDir, issueID, repo)
		}
	}
	return warnings, nil
}

/*
 * appendArchiveRepoWarnings 单仓库安全检查：目录缺失（未 clone 成功）跳过；未提交/未推送
 * 各记一条警告（repoName 前缀区分多仓库）。
 */
func appendArchiveRepoWarnings(warnings []string, baseDir, issueID string, repo *types.IssueWorkspaceRepoState) []string {
	dir := filepath.Join(baseDir, issueID, "repo", repo.Name)
	if !gitutil.IsRepo(dir) {
		return warnings
	}
	if dirty, e := gitutil.StatusPorcelain(dir); e != nil {
		warnings = append(warnings, fmt.Sprintf("%s：检查未提交变更失败（%v）", repo.Name, e))
	} else if n := len(nonEmptyLines(dirty)); n > 0 {
		warnings = append(warnings, fmt.Sprintf("%s：有 %d 处未提交变更", repo.Name, n))
	}
	remoteRef := "origin/" + repo.TargetBranch
	if !gitutil.RemoteRefExists(dir, remoteRef) {
		return append(warnings, fmt.Sprintf("%s：分支 %s 从未推送", repo.Name, repo.TargetBranch))
	}
	ahead, e := gitutil.LogAhead(dir, remoteRef, "HEAD")
	if e != nil {
		return append(warnings, fmt.Sprintf("%s：检查未推送提交失败（%v）", repo.Name, e))
	}
	if n := len(nonEmptyLines(ahead)); n > 0 {
		warnings = append(warnings, fmt.Sprintf("%s：有 %d 个未推送提交", repo.Name, n))
	}
	return warnings
}

// nonEmptyLines 多行输出按行拆分并滤空行（porcelain/log 每行一条变更/提交；空串返回 nil）。
func nonEmptyLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
