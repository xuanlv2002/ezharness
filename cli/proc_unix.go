//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

/* detachAttr 后台启动：独立进程组，脱离本进程的控制终端。 */
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

/* forwardSignals 把 Ctrl+C/SIGTERM 转发给前台 core，随其一起退。 */
func forwardSignals(p *os.Process) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		for range ch {
			_ = p.Signal(syscall.SIGTERM)
		}
	}()
}

/* sigterm 优雅停止信号。 */
func sigterm() os.Signal {
	return syscall.SIGTERM
}
