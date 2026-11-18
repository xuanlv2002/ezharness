/*
ezharness.exe 是唯一命令入口：无参打印信息卡；chat/tui 默认走
resolveBase()（读 server.json → 探活 → 附着或拉起），--server 指定时
直接连该地址。core 常驻单实例，本进程只是它的客户端（纯 HTTP，不
import core 内部包）。
*/
package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"
)

/* version 构建期注入（-X main.version；dev 构建为 dev）。 */
var version = "dev"

const usageText = `ezharness — 本机 AI 工作台入口

用法：ezharness [--server 地址] <命令> [参数]

  server [--detach]   启动 core（前台；--detach 后台常驻）
  status              实例状态（纯键值，供脚本）
  stop                停止常驻 core
  logs                跟随 core 日志
  web                 打开浏览器端（未运行则拉起 core）
  desktop             打开桌面端
  chat <一句话>        问一句（未运行则拉起 core）
  tui                 终端界面：分支 + 会话（未运行则拉起 core）
  help                本帮助
  -v, --version       版本号（单行）

--server <主机:端口>  chat/tui 连指定 core（默认本机自动发现；也可在
                      登录提示里填）`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		infoCard()
		return
	}
	server := ""
	if args[0] == "--server" || args[0] == "-s" {
		if len(args) < 3 {
			fatalf("用法：ezharness --server <主机:端口> <chat|tui> …")
		}
		server, args = args[1], args[2:]
	} else if strings.HasPrefix(args[0], "--server=") {
		if len(args) < 2 {
			fatalf("用法：ezharness --server=<主机:端口> <chat|tui> …")
		}
		server, args = strings.TrimPrefix(args[0], "--server="), args[1:]
	}
	cmd, rest := args[0], args[1:]
	if server != "" && cmd != "chat" && cmd != "tui" {
		fatalf("--server 只用于 chat / tui（本机管理命令走 server.json）")
	}
	switch cmd {
	case "-v", "--version":
		fmt.Println("ezharness " + version)
	case "help", "-h", "--help":
		fmt.Println(usageText)
	case "server":
		cmdServer(rest)
	case "status":
		cmdStatus()
	case "stop":
		cmdStop()
	case "logs":
		cmdLogs()
	case "web":
		cmdWeb()
	case "desktop":
		cmdDesktop()
	case "chat":
		cmdChat(rest, server)
	case "tui":
		cmdTUI(server)
	default:
		fmt.Printf("未知命令 %q\n\n%s\n", cmd, usageText)
		os.Exit(1)
	}
}

/*
infoCard 无参信息卡：幂等、无副作用、300ms 短探活。双击运行（stdin 非
终端）时打印后等一次回车，窗口不闪退。
*/
func infoCard() {
	si := readServerInfo()
	var state, dataDir string
	if si != nil {
		if h := probeHealth(si.Port, 300); h != nil {
			state = fmt.Sprintf("● 运行中 · 127.0.0.1:%d · pid %d · v%s", si.Port, si.Pid, h.Version)
		} else {
			state = fmt.Sprintf("○ 未运行（server.json 残留：端口 %d pid %d）", si.Port, si.Pid)
		}
		dataDir = si.DataDir
	} else {
		state = "○ 未运行（ezharness server 启动）"
		dataDir = defaultDataDir()
	}

	fmt.Printf("ezharness v%s\n", version)
	fmt.Printf("状态　%s\n", state)
	fmt.Printf("数据　%s\n", dataDir)
	/* 模型行尽力而为：已登录且 core 在跑才显示，失败静默 */
	if si != nil {
		if _, tok := loadSession(); tok != "" {
			cl := newClient(localBase(si.Port), tok)
			if b, err := cl.bootstrap(); err == nil && b.Status != nil && b.Status.Model != "" {
				fmt.Printf("模型　%s\n", b.Status.Model)
			}
		}
	}
	fmt.Println()
	fmt.Println("常用　tui 终端界面 · web 浏览器 · desktop 桌面端")
	fmt.Println("　　　chat \"…\" 问一句 · status 状态 · stop 停止 · help 全部命令")

	/* Windows 双击无参运行：控制台归本进程所有，退出即关窗——暂停一次防闪退。
	   管道/重定向输入会立即 EOF，脚本不会被卡住；终端里的其他平台不需要暂停 */
	if runtime.GOOS == "windows" {
		fmt.Print("\n按回车退出…")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
}

/* isTerminal 判断文件是否为终端（tui 只在真终端里跑；不引依赖，按字符设备探测）。 */
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

/* fatalf 统一报错退出。 */
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ezharness: "+format+"\n", args...)
	os.Exit(1)
}
