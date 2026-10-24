package service

import (
	"testing"

	"go.uber.org/zap"

	"ocean-harness/server/internal/dal/types"
)

/*
 * TestTaskWorkspaceRunStepsPanicRecovery 验证步骤 runner 的 panic 被 taskWorkspaceRunSteps
 * 顶层 recover 转为 FAILED 落盘，而非击穿进程（T1.2-T1.4 将注册子进程/文件写等真实 panic 面）。
 */
func TestTaskWorkspaceRunStepsPanicRecovery(t *testing.T) {
	const stepKey = "panicTestStep"
	taskWorkspaceStepRunners[stepKey] = func(_ *types.TaskWorkspaceState, _ *types.TaskWorkspaceStep, _ *zap.Logger) error {
		panic("boom")
	}
	t.Cleanup(func() { delete(taskWorkspaceStepRunners, stepKey) })

	baseDir := t.TempDir()
	const taskID = "01111111-1111-7111-1111-111111111111"
	state := &types.TaskWorkspaceState{
		Version: taskWorkspaceStateVersion,
		TaskID:  taskID,
		BaseDir: baseDir,
		Status:  types.IW_STATUS_RUNNING,
		Steps: []*types.TaskWorkspaceStep{
			{Key: stepKey, Title: "panic 测试步骤", Status: types.IW_STATUS_PENDING},
		},
	}

	taskWorkspaceRunSteps(state, zap.NewNop()) // 若 recover 失效，panic 会直接终止测试进程

	if state.Status != types.IW_STATUS_FAILED {
		t.Fatalf("status = %q, want FAILED", state.Status)
	}
	if state.Error == "" {
		t.Fatal("state.error 须记录 panic 原因")
	}
	loaded, err := loadTaskWorkspaceState(baseDir, taskID)
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
