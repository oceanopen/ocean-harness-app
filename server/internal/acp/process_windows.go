//go:build windows

package acp

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// ProcessGroupAttr windows 无 POSIX 进程组；以新进程组标志创建（Ctrl-C 隔离），
// 整树终结走 taskkill /T。导出供 browser 域引擎子进程复用（单一 SSOT）。
func ProcessGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// KillProcessGroup 终结命令进程树（acp-go osrun kill_windows 同款语义）：
// taskkill /T /F 遍历树强杀（windows 无进程组信号语义）；Wait 已释放句柄后 PID
// 可能易主，先自开句柄钉住；taskkill 缺失时退回直接 kill。
//
// 不以 Signal(0) 探活早退：os.Process.Signal 在 cmd.Wait 之后恒返回 ErrProcessDone
// （done 短路），而 browser 域收敛序恰在 session.Close()（其内部已 Wait 收割）之后
// 补杀孙进程——早退会让树杀在该场景恒不执行。恒走 taskkill /T /F：父进程已死时
// 快照仍按 ParentProcessId 关联孤儿子进程、可被清树；PID 彻底消亡则幂等失败，退回
// Process.Kill 归一。自开句柄钉住期间 PID 不会复用，无误杀窗口。
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return reapedWindows(cmd.Process.Kill())
	}
	defer syscall.CloseHandle(handle)
	if exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run() == nil {
		return nil
	}
	return reapedWindows(cmd.Process.Kill())
}

// reapedWindows 把 Wait 后 Signal 的 EINVAL 归一为 os.ErrProcessDone。
func reapedWindows(err error) error {
	if errors.Is(err, syscall.EINVAL) {
		return os.ErrProcessDone
	}
	return err
}
