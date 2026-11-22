/*
core 进程管理：server.json 是常驻实例的簿记（core 启动写、退出删，不含
令牌），ensureCore 据此附着或拉起。bind 即仲裁：端口抢不上说明已有实例。
*/
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

/* serverInfo 与 core 写盘的 server.json 同构。 */
type serverInfo struct {
	Port    int    `json:"port"`
	Pid     int    `json:"pid"`
	Version string `json:"version"`
	DataDir string `json:"dataDir"`
}

type healthInfo struct {
	OK      bool   `json:"ok"`
	Boot    int64  `json:"boot"`
	Version string `json:"version"`
}

/*
rootDir 是 core 的应用根（ezharness.json/server.json 所在）：
EZHARNESS_ROOT > exe 同目录（便携/开发布局，目录里有 ezharness.json 或
server.json 即认定）> 用户配置目录（安装布局）。
*/
func rootDir() string {
	if v := strings.TrimSpace(os.Getenv("EZHARNESS_ROOT")); v != "" {
		return v
	}
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(dir, "ezharness.json")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "server.json")); err == nil {
			return dir
		}
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "ezharness")
	}
	return "."
}

/*
coreExe 定位 ezharness-core：EZHARNESS_CORE_EXE > exe 同目录 > 仓库开发
布局 release/dev。候选名两种都试（仓库约定 mac 下构建产物也带 .exe）。
*/
func coreExe() string {
	if v := strings.TrimSpace(os.Getenv("EZHARNESS_CORE_EXE")); v != "" {
		return v
	}
	names := []string{"ezharness-core.exe", "ezharness-core"}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, n := range names {
			if p := filepath.Join(dir, n); fileExists(p) {
				return p
			}
		}
		for _, n := range names {
			if p := filepath.Join(dir, "..", "..", "release", "dev", n); fileExists(p) {
				return p
			}
		}
	}
	return "ezharness-core"
}

/* fileExists 存在即真（不区分文件/目录，调用点都是可执行名）。 */
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

/* defaultDataDir rootDir 下 ezharness.json 的缺省数据目录（信息卡用）。 */
func defaultDataDir() string {
	return filepath.Join(rootDir(), "data")
}

/* readServerInfo 读实例簿记；无文件或损坏返回 nil。 */
func readServerInfo() *serverInfo {
	data, err := os.ReadFile(filepath.Join(rootDir(), "server.json"))
	if err != nil {
		return nil
	}
	var si serverInfo
	if json.Unmarshal(data, &si) != nil || si.Port <= 0 {
		return nil
	}
	return &si
}

/* probeHealth 短探活；超时/失败返回 nil。 */
func probeHealth(port int, timeoutMs int) *healthInfo {
	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}
	res, err := client.Get(localBase(port) + "/api/app/health")
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	var h healthInfo
	if json.NewDecoder(res.Body).Decode(&h) != nil || !h.OK {
		return nil
	}
	return &h
}

/* localBase 本机 core 地址（端口来自 server.json）。 */
func localBase(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

/* normalizeServer 把登录输入补成 origin：无协议默认 http、无端口默认 5260。 */
func normalizeServer(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("服务器地址为空")
	}
	hasScheme := strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
	if !hasScheme {
		if strings.Contains(s, "://") {
			return "", fmt.Errorf("只支持 http/https: %s", raw)
		}
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("地址格式不对: %s", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("只支持 http/https: %s", raw)
	}
	if !hasScheme && u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), "5260")
	}
	return u.Scheme + "://" + u.Host, nil
}

/* displayAddr 提示里省掉 http 前缀并把回环显示成 localhost。 */
func displayAddr(base string) string {
	if strings.HasPrefix(base, "https://") {
		return base
	}
	return strings.Replace(strings.TrimPrefix(base, "http://"), "127.0.0.1", "localhost", 1)
}

/*
resolveBase 决定 chat/tui 连哪台 core：--server 显式地址优先；否则
ensureCore 本机 core（默认值，无需用户输入）。
*/
func resolveBase(serverFlag string) string {
	if serverFlag != "" {
		base, err := normalizeServer(serverFlag)
		if err != nil {
			fatalf("%v", err)
		}
		return base
	}
	si := ensureCore()
	return localBase(si.Port)
}

/*
ensureCore 三入口命令共用的判断：已在跑就附着，没跑就拉起
（detached core，轮询就绪上限 10s）。返回实例簿记。
*/
func ensureCore() serverInfo {
	if si := readServerInfo(); si != nil {
		if h := probeHealth(si.Port, 800); h != nil {
			return *si
		}
	}
	root := rootDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		fatalf("应用根不可用 %s: %v", root, err)
	}
	fmt.Println("core 未运行，拉起中…")
	spawnCore(root, true)
	deadline := time.Now().Add(10 * time.Second)
	var si *serverInfo
	for si == nil {
		if si = readServerInfo(); si == nil {
			if time.Now().After(deadline) {
				fatalf("core 启动后未写出 server.json（日志见 %s）", filepath.Join(defaultDataDir(), "logs"))
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	for probeHealth(si.Port, 800) == nil {
		if time.Now().After(deadline) {
			fatalf("core 10 秒内未就绪（端口 %d，日志见 %s）", si.Port, filepath.Join(si.DataDir, "logs"))
		}
		time.Sleep(300 * time.Millisecond)
	}
	return *si
}

/*
spawnCore 启动 core：detached=true 后台常驻（三端退出不杀它），false 前
台跟随（Ctrl+C 一起退）。
*/
func spawnCore(root string, detached bool) {
	exe := coreExe()
	if _, err := os.Stat(exe); err != nil {
		fatalf("找不到 core: %s（先构建，或设 EZHARNESS_CORE_EXE）", exe)
	}
	cmd := exec.Command(exe, "--root", root)
	if detached {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
		cmd.SysProcAttr = detachAttr()
		if err := cmd.Start(); err != nil {
			fatalf("core 启动失败: %v", err)
		}
		_ = cmd.Process.Release()
		return
	}
	// 前台：stdio 直通，信号转发，core 退出本进程随之退
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fatalf("core 启动失败: %v", err)
	}
	forwardSignals(cmd.Process)
	if err := cmd.Wait(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			fatalf("core 异常退出: %v", err)
		}
	}
}

/*
stopInstance 停掉实例并等探活失败（最多 5 秒）。Unix 走 SIGTERM 优雅收尾，
Windows 直接 taskkill 进程树。
*/
func stopInstance(si *serverInfo) bool {
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", fmt.Sprint(si.Pid), "/T", "/F").Run()
	} else if p, err := os.FindProcess(si.Pid); err == nil {
		_ = p.Signal(sigterm())
	}
	for i := 0; i < 20; i++ {
		if probeHealth(si.Port, 300) == nil {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

/* cmdServer ezharness server [--detach]：前台启动（已在跑则先停再起）。 */
func cmdServer(rest []string) {
	detached := false
	for _, a := range rest {
		if a == "--detach" {
			detached = true
		} else {
			fatalf("未知参数 %s（可用：--detach）", a)
		}
	}
	si := readServerInfo()
	alive := si != nil && probeHealth(si.Port, 800) != nil
	if detached {
		if alive {
			fmt.Printf("core 已在运行（127.0.0.1:%d pid %d）\n", si.Port, si.Pid)
			return
		}
		/* ensureCore 自己会拉起并等就绪；再先 spawn 一次会双起进程（第二个 bind 失败） */
		si := ensureCore()
		fmt.Printf("core 已后台启动：127.0.0.1:%d（pid %d，数据目录 %s）\n", si.Port, si.Pid, si.DataDir)
		return
	}
	if alive {
		fmt.Printf("重启 core：停止旧实例（pid %d）…\n", si.Pid)
		if !stopInstance(si) {
			fatalf("旧实例未在 5 秒内退出（pid %d）", si.Pid)
		}
	}
	fmt.Println("core 前台运行（Ctrl+C 停止）")
	spawnCore(rootDir(), false)
}

/* cmdStatus 显示实例状态（未运行退出码 1）。 */
func cmdStatus() {
	si := readServerInfo()
	if si == nil || probeHealth(si.Port, 800) == nil {
		fmt.Println("ezharness  ○ 未运行")
		dataDir := defaultDataDir()
		if si != nil {
			dataDir = si.DataDir
		}
		fmt.Printf("  数据  %s\n", dataDir)
		fmt.Println("  提示  ezharness server 启动")
		os.Exit(1)
	}

	fmt.Println("ezharness  ● 运行中")
	fmt.Printf("  地址  127.0.0.1:%d\n", si.Port)
	fmt.Printf("  版本  %s\n", si.Version)
	fmt.Printf("  进程  %d\n", si.Pid)
	fmt.Printf("  数据  %s\n", si.DataDir)
	if b, err := ensureAuth(localBase(si.Port)).bootstrap(); err == nil {
		if b.Status != nil && b.Status.Model != "" {
			fmt.Printf("  模型  %s\n", b.Status.Model)
		}
		title := ""
		for _, br := range b.Branches {
			if br.ID == b.SessionID {
				title = br.Title
			}
		}
		line := fmt.Sprintf("%d 个", len(b.Branches))
		if title != "" {
			line += " · 当前 " + title
		}
		fmt.Printf("  分支  %s\n", line)
	}
}

/* cmdStop 停止常驻 core（Unix SIGTERM 优雅收尾，Windows taskkill）。 */
func cmdStop() {
	si := readServerInfo()
	if si == nil || probeHealth(si.Port, 300) == nil {
		fmt.Println("core 未在运行")
		return
	}
	if !stopInstance(si) {
		fatalf("core 未在 5 秒内退出（pid %d）", si.Pid)
	}
	fmt.Printf("core 已停止（pid %d）\n", si.Pid)
}

/* cmdLogs 跟随 core 日志：先回放最近 40 行，再持续跟随最新日志文件。 */
func cmdLogs() {
	si := ensureCore()
	logDir := filepath.Join(si.DataDir, "logs")
	if _, err := os.Stat(logDir); err != nil {
		fatalf("日志目录不存在: %s", logDir)
	}
	tailLogs(logDir)
}

/* cmdWeb 打开浏览器端。 */
func cmdWeb() {
	si := ensureCore()
	openURL(fmt.Sprintf("http://127.0.0.1:%d", si.Port))
}

/* cmdDesktop 拉起桌面端（自己做 ensureCore，这里只负责启动）。 */
func cmdDesktop() {
	exe, err := os.Executable()
	if err != nil {
		fatalf("定位自身失败: %v", err)
	}
	name := "ezharness-desktop"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	desktop := filepath.Join(filepath.Dir(exe), name)
	if !fileExists(desktop) {
		// dev 布局没有打包桌面端：源码在仓库 desktop/，直接用 Electron 跑
		devDir := filepath.Join(filepath.Dir(exe), "..", "..", "desktop")
		if !fileExists(filepath.Join(devDir, "node_modules", "electron")) {
			fatalf("找不到桌面端: %s（dev 下先在 desktop 里 npm install）", desktop)
		}
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/c", "npm", "run", "start")
		} else {
			cmd = exec.Command("npm", "run", "start")
		}
		cmd.Dir = devDir
		startDesktop(cmd)
		fmt.Println("桌面端已启动（开发布局）")
		return
	}
	startDesktop(exec.Command(desktop))
	fmt.Println("桌面端已启动")
}

/* startDesktop 脱离本进程后台拉起桌面端。 */
func startDesktop(cmd *exec.Cmd) {
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		fatalf("桌面端启动失败: %v", err)
	}
	_ = cmd.Process.Release()
}

/* openURL 用系统默认浏览器打开。 */
func openURL(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	_ = c.Start()
}
