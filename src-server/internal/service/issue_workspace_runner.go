package service

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"ocean-harness/src-server/internal/dal/types"
)

// issueWorkspace 后台执行编排：init 受理方把合并后的状态（PENDING）连同进程级 logger 交给
// issueWorkspaceRunSteps，由其串行执行步骤、逐步落盘（纯状态文件驱动，不碰 DB 与请求 ctx）。
// 未注册实现的步骤置 SKIPPED 占位——新增步骤时在各自 step 文件内按 issueWorkspaceStepRunner
// 签名注册实现，编排逻辑不再改。

// issueWorkspaceStepRunner 单个初始化步骤的执行函数签名（T1.2/T1.3/T1.4 的实现按同签名注册）。
// 协议：返回 nil 即步骤正常收尾（编排置 SUCCESS）；步骤无事可做（降级跳过）时 runner 自行置
// step.Status = SKIPPED（可在 step.Message 附原因）并返回 nil，编排尊重该终态不再覆盖；
// 返回 error 则编排置 FAILED 并终止后续步骤。
type issueWorkspaceStepRunner func(state *types.IssueWorkspaceState, step *types.IssueWorkspaceStep, logger *zap.Logger) error

// issueWorkspaceStepRunners 步骤实现注册表：key → 执行函数。未注册的步骤置 SKIPPED。
var issueWorkspaceStepRunners = map[string]issueWorkspaceStepRunner{
	types.IW_STEP_KEY_CREATE_DIRS: issueWorkspaceRunCreateDirs,
}

// issueWorkspaceStepTitles 步骤骨架单一来源：顺序即固定执行顺序。
var issueWorkspaceStepTitles = []struct {
	Key   string
	Title string
}{
	{types.IW_STEP_KEY_CREATE_DIRS, "创建目录结构"},
	{types.IW_STEP_KEY_SSH_CONFIG, "生成 SSH Config"},
	{types.IW_STEP_KEY_CLONE_REPOS, "Clone 仓库与分支"},
}

// issueWorkspaceRunSteps 串行执行步骤（后台 goroutine 内调用，纯状态文件驱动：不碰 DB 与请求 ctx）。
// 非法状态：跳过（增量合并保留的既有成功结果）；未注册实现的步骤整体置 SKIPPED（含其仓库级子状态）；
// 每次状态变化即时落盘（前端轮询可见）；任一步骤失败置顶层 FAILED 并终止后续步骤。
// 顶层 recover：未恢复的 panic 在任意 goroutine 都会终止整个进程（Recovery 中间件只覆盖 HTTP
// handler 协程），T1.2-T1.4 将注册子进程/文件写等真实 panic 面——runner panic 统一转为 FAILED 落盘。
func issueWorkspaceRunSteps(state *types.IssueWorkspaceState, logger *zap.Logger) {
	defer func() {
		if r := recover(); r != nil {
			// 执行中步骤至多一个（单写者串行）；runner panic 时它停留在 RUNNING，
			// 补记 FAILED 使状态文件自洽（顶层 FAILED + 步骤 FAILED，而非步骤悬挂 RUNNING）。
			for _, step := range state.Steps {
				if step.Status == types.IW_STATUS_RUNNING {
					step.Status = types.IW_STATUS_FAILED
				}
			}
			state.Status = types.IW_STATUS_FAILED
			state.Error = fmt.Sprintf("初始化任务异常退出: %v", r)
			if err := saveIssueWorkspaceState(state); err != nil {
				logger.Error("[issueWorkspace] persist state after panic failed", zap.String("issueId", state.IssueID), zap.Error(err))
			}
			logger.Error("[issueWorkspace] step runner panicked", zap.String("issueId", state.IssueID), zap.Any("panic", r), zap.Stack("stack"))
		}
	}()
	persist := func() {
		if err := saveIssueWorkspaceState(state); err != nil {
			logger.Error("[issueWorkspace] persist state file failed", zap.String("issueId", state.IssueID), zap.Error(err))
		}
	}
	for _, step := range state.Steps {
		if step.Status != types.IW_STATUS_PENDING {
			continue
		}
		runner, ok := issueWorkspaceStepRunners[step.Key]
		if !ok {
			// 未注册实现的占位步骤：置 SKIPPED，待后续任务注册实现。
			step.Status = types.IW_STATUS_SKIPPED
			for _, repo := range step.Repos {
				repo.Status = types.IW_STATUS_SKIPPED
				repo.Message = "本期未实现，等待后续任务接入"
			}
			persist()
			continue
		}
		step.Status = types.IW_STATUS_RUNNING
		persist()
		if err := runner(state, step, logger); err != nil {
			step.Status = types.IW_STATUS_FAILED
			state.Status = types.IW_STATUS_FAILED
			state.Error = fmt.Sprintf("步骤「%s」失败: %v", step.Title, err)
			persist()
			return
		}
		// runner 正常返回默认置 SUCCESS；runner 显式置 SKIPPED（降级跳过）则尊重之。
		if step.Status != types.IW_STATUS_SKIPPED {
			step.Status = types.IW_STATUS_SUCCESS
		}
		persist()
	}
	state.Status = types.IW_STATUS_SUCCESS
	state.Error = ""
	persist()
}

// issueWorkspaceRunCreateDirs 创建工作空间目录骨架：{issueId}/.ssh 与 {issueId}/repo。
// issue 根目录由状态文件首次写入时创建（见 saveIssueWorkspaceState 的 MkdirAll）。
func issueWorkspaceRunCreateDirs(state *types.IssueWorkspaceState, _ *types.IssueWorkspaceStep, _ *zap.Logger) error {
	root := filepath.Join(state.BaseDir, state.IssueID)
	for _, sub := range []string{".ssh", "repo"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", sub, err)
		}
	}
	return nil
}
