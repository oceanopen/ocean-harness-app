//go:build unix

package acp

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// ProcessGroupAttr unix：独立进程组拉起，kill 才能覆盖 adapter 的全部后代
// （npx 会派生 node，工具进程还会再派生），杀整组不留孤儿占住 stdio。
// 导出供 browser 域引擎子进程复用（单一 SSOT，勿在包外另立副本）。
func ProcessGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// KillProcessGroup 对整个进程组发 SIGKILL。ESRCH = 组已不存在，归一为
// os.ErrProcessDone（幂等回收语义，acp-go osrun kill_unix 同款）。
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
