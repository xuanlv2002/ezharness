/*
ezharness.exe 是唯一命令入口：无参直接进 tui；tui/chat 默认连本机 core
（读 server.json → 探活 → 附着或拉起），--server 可显式指定地址。core
常驻单实例，本进程只是它的客户端（纯 HTTP，不 import core 内部包）。
*/
package main

import (
	"fmt"
	"os"
	"strings"
)

/* version 构建期注入（-X main.version；dev 构建为 dev）。 */
var version = "dev"

const usageText = `ezharness — 本机 AI 工作台入口

用法：ezharness [--server 地址] <命令> [参数]

  （无参数直接进入 tui，已登录则直达会话）

  status              查看 core 状态
  server [--detach]   前台启动 core（已在跑则重启）；--detach 后台常驻
  stop                停止 core
  chat [一句话]        单轮问答：发一句，打印最终回复
  tui                 终端对话界面
  logs                跟随 core 日志
  web                 打开浏览器端
  desktop             打开桌面端
  help                本帮助
  -v, --version       版本号

--server <主机:端口>   tui / chat 连接指定 core（默认本机自动发现）`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		cmdTUI("")
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
