//go:build windows

package main

import (
	"os"
	"syscall"
)

/* detachedProcess 脱离控制台：Go 的 syscall 包没有导出它（x/sys/windows 才有）。 */
const detachedProcess = 0x00000008

// detachAttr 后台启动：独立进程组 + 脱离控制台。脱离后 core 没有控制台，
// 由 core 自己分配隐藏控制台，它 spawn 的控制台程序（MCP server、taskkill）
// 才不会各自弹一个黑框。
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
	}
}

/* forwardSignals Windows 下无需转发：core 与本进程同控制台，Ctrl+C 同时送达。 */
func forwardSignals(*os.Process) {}

/* sigterm Windows 停止走 taskkill，这里不会被调用。 */
func sigterm() os.Signal { return os.Interrupt }
