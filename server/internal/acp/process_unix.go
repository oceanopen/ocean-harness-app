//go:build unix

package acp

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// processGroupAttr unix：独立进程组拉起，kill 才能覆盖 adapter 的全部后代
// （npx 会派生 node，工具进程还会再派生），杀整组不留孤儿占住 stdio。
func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup 对整个进程组发 SIGKILL。ESRCH = 组已不存在，归一为
// os.ErrProcessDone（幂等回收语义，acp-go osrun kill_unix 同款）。
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
