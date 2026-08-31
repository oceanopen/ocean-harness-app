package service

import (
	"testing"

	"go.uber.org/zap"

	"we-claude-terminal/go-server/internal/dal/types"
)

// TestIssueWorkspaceRunStepsPanicRecovery 验证步骤 runner 的 panic 被 issueWorkspaceRunSteps
// 顶层 recover 转为 FAILED 落盘，而非击穿进程（T1.2-T1.4 将注册子进程/文件写等真实 panic 面）。
func TestIssueWorkspaceRunStepsPanicRecovery(t *testing.T) {
	const stepKey = "panicTestStep"
	issueWorkspaceStepRunners[stepKey] = func(_ *types.IssueWorkspaceState, _ *types.IssueWorkspaceStep, _ *zap.Logger) error {
		panic("boom")
	}
	t.Cleanup(func() { delete(issueWorkspaceStepRunners, stepKey) })

	baseDir := t.TempDir()
	const issueID = "01111111-1111-7111-1111-111111111111"
	state := &types.IssueWorkspaceState{
		Version: issueWorkspaceStateVersion,
		IssueID: issueID,
		BaseDir: baseDir,
		Status:  types.IW_STATUS_RUNNING,
		Steps: []*types.IssueWorkspaceStep{
			{Key: stepKey, Title: "panic 测试步骤", Status: types.IW_STATUS_PENDING},
		},
	}

	issueWorkspaceRunSteps(state, zap.NewNop()) // 若 recover 失效，panic 会直接终止测试进程

	if state.Status != types.IW_STATUS_FAILED {
		t.Fatalf("status = %q, want FAILED", state.Status)
	}
	if state.Error == "" {
		t.Fatal("state.error 须记录 panic 原因")
	}
	loaded, err := loadIssueWorkspaceState(baseDir, issueID)
	if err != nil {
		t.Fatalf("读回状态文件失败: %v", err)
	}
	if loaded == nil || loaded.Status != types.IW_STATUS_FAILED || loaded.Error == "" {
		t.Fatalf("落盘状态异常: %+v", loaded)
	}
	if loaded.Steps[0].Status != types.IW_STATUS_FAILED {
		t.Fatalf("panic 步骤自身状态 = %q, want FAILED（与顶层结论一致，不悬挂 RUNNING）", loaded.Steps[0].Status)
	}
}
