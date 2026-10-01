//go:build windows

package acp

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// processGroupAttr windows 无 POSIX 进程组；以新进程组标志创建（Ctrl-C 隔离），
// 整树终结走 taskkill /T。
func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// killProcessGroup 终结命令进程树（acp-go osrun kill_windows 同款语义）：
// taskkill /T /F 遍历树强杀（windows 无进程组信号语义）；Wait 已释放句柄后 PID
// 可能易主，先自开句柄钉住；taskkill 缺失时退回直接 kill。
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return reapedWindows(cmd.Process.Kill())
	}
	defer syscall.CloseHandle(handle)
	if err := reapedWindows(cmd.Process.Signal(syscall.Signal(0))); errors.Is(err, os.ErrProcessDone) {
		return err
	}
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
