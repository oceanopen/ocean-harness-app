package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"ocean-harness/src-server/internal/dal/enums"
	"ocean-harness/src-server/internal/dal/model"
	"ocean-harness/src-server/internal/dal/query"
	"ocean-harness/src-server/internal/dal/types"
	"ocean-harness/src-server/internal/gitutil"
)

// issueWorkspace archive：归档/取消工作空间（T3.2，docs/agent_dev_01_tasks.md §T3.2）。
// 归档与取消同为工程化确定性操作（不依赖 AI）：删除 {baseDir}/{issueId}/ 目录 + 流转
// issue 状态（archive→DONE / cancel→CANCELLED，父子解耦不级联子任务，见 project_issue.go
// 文件头注释）。两段式契约（类型注释见 types/issue_workspace.go）：force=false 仅安全
// 检查返回警告不执行；force=true 跳过检查直接执行。
//
// 同域文件分工：入口/校验在本文件；并发准入见 issue_workspace_registry.go；状态文件
// 读写见 issue_workspace_state.go；git 检查函数见 gitutil/status.go。

// Archive 归档/取消 issue 工作空间。执行序：入参校验 → issue 存在性 → 并发防护（init
// 进行中拒绝）→ force=false 安全检查 / force=true 删目录 + 事务流转状态（Move 范式）。
func (svc IssueWorkspace) Archive(req *types.IssueWorkspaceArchiveRequest) (*types.IssueWorkspaceArchiveResponseData, error) {
	if !filepath.IsAbs(req.BaseDir) {
		return nil, errors.New("baseDir 须为绝对路径")
	}
	if !issueWorkspaceValidIssueID(req.IssueID) {
		return nil, errors.New("issueId 非法")
	}
	var targetState enums.StateCode
	switch req.Action {
	case types.IW_ARCHIVE_ACTION_ARCHIVE:
		targetState = enums.STATE_CODE_DONE
	case types.IW_ARCHIVE_ACTION_CANCEL:
		targetState = enums.STATE_CODE_CANCELLED
	default:
		return nil, errors.New("action 须为 archive 或 cancel")
	}
	q := query.Use(svc.Orm)
	if _, err := q.ProjectIssue.WithContext(svc.Context).Where(q.ProjectIssue.ID.Eq(req.IssueID)).First(); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("issue 不存在")
		}
		return nil, err
	}
	// 并发防护：后台 goroutine 正在写 {issueId}/ 目录（clone 等），此刻删除会与其竞争。
	if issueWorkspaceActive(req.IssueID) {
		return nil, errors.New("工作空间初始化进行中，请完成后再归档/取消")
	}

	if !req.Force {
		warnings, err := issueWorkspaceArchiveWarnings(req.BaseDir, req.IssueID)
		if err != nil {
			return nil, err
		}
		return &types.IssueWorkspaceArchiveResponseData{Executed: false, Warnings: warnings}, nil
	}

	// 删目录（外部副作用不可回滚，置于事务前：失败即中止不流转状态；目录不存在返回 nil，
	// 天然幂等——未初始化/重复归档均安全）。终端会话已由前端在调本段前 ptyShutdownIssue。
	if err := os.RemoveAll(filepath.Join(req.BaseDir, req.IssueID)); err != nil {
		return nil, fmt.Errorf("删除工作空间目录失败: %w", err)
	}

	// 状态流转（Move 范式）：applyStateTransition + Save；同值 no-op（重复归档幂等）。
	// 父自动完成联动仅在「本次变为完成」时尝试（归档目标若是子任务且兄弟全完成）。
	issueSvc := ProjectIssue{Service: svc.Service}
	var issue *model.ProjectIssue
	if err := svc.Orm.Transaction(func(tx *gorm.DB) error {
		qtx := query.Use(tx)
		found, e := qtx.ProjectIssue.WithContext(svc.Context).Where(qtx.ProjectIssue.ID.Eq(req.IssueID)).First()
		if e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return errors.New("issue 不存在")
			}
			return e
		}
		issue = found
		wasCompleted := issue.CompletedAt != nil
		if e := issueSvc.applyStateTransition(issue, targetState); e != nil {
			return e
		}
		if e := qtx.ProjectIssue.WithContext(svc.Context).Save(issue); e != nil {
			return e
		}
		if !wasCompleted && issue.CompletedAt != nil {
			if e := issueSvc.maybeAutoCompleteParent(tx, issue); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &types.IssueWorkspaceArchiveResponseData{Executed: true, Warnings: []string{}}, nil
}

// issueWorkspaceArchiveWarnings 安全检查（force=false 段）：遍历状态文件 cloneRepos 步骤
// 的仓库清单，逐仓库探测未提交变更（git status --porcelain）与未推送提交（本地 ref 对照
// origin/{targetBranch}..HEAD，不联网 fetch；origin 引用不存在 = 分支从未推送）。检查失败的
// 仓库记警告不中止——force 段才是删除决策点，宁可多警告不可漏警告。状态文件缺失/损坏
// 返回空列表（无可检查项，未初始化工作空间可直接归档）。
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

// appendArchiveRepoWarnings 单仓库安全检查：目录缺失（未 clone 成功）跳过；未提交/未推送
// 各记一条警告（repoName 前缀区分多仓库）。
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
