package service

import "sync"

/*
 * taskWorkspace 准入控制（活跃任务登记）：同一 issue 至多一个后台写者。
 * 锁只保护本 map（纳秒级临界区），不保护文件——文件写的串行由「唯一写者 goroutine + 步骤 for 循环」
 * 构造保证。进程重启后 registry 随进程清空，status 侧以「文件 RUNNING 但无登记」判定 INTERRUPTED。
 */
var taskWorkspaceRegistry = struct {
	sync.Mutex
	tasks map[string]struct{}
}{tasks: map[string]struct{}{}}

// taskWorkspaceAcquire 登记活跃任务：登记成功返回 true；已存在（执行中重复触发）返回 false。
func taskWorkspaceAcquire(taskID string) bool {
	taskWorkspaceRegistry.Lock()
	defer taskWorkspaceRegistry.Unlock()
	if _, ok := taskWorkspaceRegistry.tasks[taskID]; ok {
		return false
	}
	taskWorkspaceRegistry.tasks[taskID] = struct{}{}
	return true
}

// taskWorkspaceRelease 归还写者名额（goroutine defer / init 失败路径调用）。
func taskWorkspaceRelease(taskID string) {
	taskWorkspaceRegistry.Lock()
	defer taskWorkspaceRegistry.Unlock()
	delete(taskWorkspaceRegistry.tasks, taskID)
}

// taskWorkspaceActive 查询是否存在活跃任务（INTERRUPTED 判定用）。
func taskWorkspaceActive(taskID string) bool {
	taskWorkspaceRegistry.Lock()
	defer taskWorkspaceRegistry.Unlock()
	_, ok := taskWorkspaceRegistry.tasks[taskID]
	return ok
}
