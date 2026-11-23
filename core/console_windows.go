//go:build windows

package main

import "syscall"

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	user32               = syscall.NewLazyDLL("user32.dll")
	procAllocConsole     = kernel32.NewProc("AllocConsole")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
)

/*
hideConsole 给 core 分配一个隐藏控制台。desktop（CREATE_NO_WINDOW）与 CLI
（DETACHED_PROCESS）拉起的 core 都没有控制台，此时它 spawn 的 git、cmd、
taskkill 等控制台程序会各自新开一个可见控制台闪一下；分配之后子进程一律
附着在这个隐藏控制台上。已在终端里运行（自带控制台）时不动用户自己的窗口。
*/
func hideConsole() {
	if r, _, _ := procAllocConsole.Call(); r == 0 {
		return
	}
	if hwnd, _, _ := procGetConsoleWindow.Call(); hwnd != 0 {
		procShowWindow.Call(hwnd, 0) // SW_HIDE
	}
}
