//go:build !windows

package service

import (
	"os/exec"
	"syscall"

	pty "github.com/aymanbagabas/go-pty"
)

/*
prepareScriptCommand 让定时脚本独占进程组：上下文取消时对负 PID 发
SIGKILL，shell 拉起的子进程一并结束；默认只杀 shell 会留下孤儿。
*/
func prepareScriptCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

/*
killTree 结束终端进程树。go-pty 在 Unix 启动 shell 时设 Setsid=true，
shell 是会话兼进程组首领（pgid == pid）：对负 PID 发 SIGKILL 才能把
shell 拉起的子进程一并收掉（只杀 shell 会留下后台任务与持有 PTY 的孙进程）。
*/
func killTree(cmd *pty.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}
