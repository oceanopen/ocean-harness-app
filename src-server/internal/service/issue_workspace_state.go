package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"we-claude-terminal/go-server/internal/dal/types"
)

// issueWorkspace 状态文件读写：文件（.workspace-init-state.json）是工作空间初始化的唯一持久真相，
// init 受理时同步生成、后台执行中逐步落盘、status 接口直接读它——Go 侧不落库。
//
// 写入用「临时文件 + os.Rename」原子替换：单写者（registry 准入控制保证同一 issue 至多一个写者
// goroutine）多读者（前端轮询 status）场景下，读者要么看到旧完整文件要么看到新完整文件，
// 永远读不到 truncate 后的半截内容；进程崩溃也只残留 .tmp（下次写入覆盖），目标文件始终完整。

const (
	issueWorkspaceStateFileName = ".workspace-init-state.json" // 位于 {baseDir}/{issueId}/ 下
	issueWorkspaceStateVersion  = 1                            // schema 版本（结构不兼容变更时递增）
)

// issueWorkspaceStatePath 返回状态文件路径（baseDir 须已校验为绝对路径）。
func issueWorkspaceStatePath(baseDir, issueID string) string {
	return filepath.Join(baseDir, issueID, issueWorkspaceStateFileName)
}

// loadIssueWorkspaceState 读状态文件：文件不存在返回 (nil, nil)（NOT_INITIALIZED 语义）；
// 存在但解析失败返回 error（CORRUPTED 语义，init 侧覆盖重建、status 侧原样上报）。
func loadIssueWorkspaceState(baseDir, issueID string) (*types.IssueWorkspaceState, error) {
	raw, err := os.ReadFile(issueWorkspaceStatePath(baseDir, issueID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var state types.IssueWorkspaceState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("状态文件解析失败: %w", err)
	}
	return &state, nil
}

// saveIssueWorkspaceState 原子写状态文件：先写同目录 .tmp 再 Rename 覆盖（同文件系统 rename 原子）。
// 幂等创建 {issueId} 目录（init 受理时的首个写入点，目录可能尚不存在）；updatedAt 由本函数统一刷新。
func saveIssueWorkspaceState(state *types.IssueWorkspaceState) error {
	dir := filepath.Join(state.BaseDir, state.IssueID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	state.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(dir, issueWorkspaceStateFileName)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}
