/*
窗口壳桥接：/api/window/* 供桌面端自绘标题栏调用（wails 资产域同源
或 Vite proxy 转发）。浏览器访问时无窗口，一律 503。窗口句柄由 main
侧装配（wails Window 的适配器），换代重启跨代保留。
*/
package controller

import (
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"sync"

	"github.com/gin-gonic/gin"

	"ezharness/internal/domain"
	"ezharness/internal/osfs"
)

/* WindowControl 是桌面窗口的最小控制面（main 侧适配原生窗口）。 */
type WindowControl interface {
	Minimise()
	ToggleMaximise()
	IsMaximised() bool
	Hide()                            // 最小化到托盘
	RequestQuit()                     // 真退出（收尾落盘后 Quit；关窗钩子放行）
	OpenAppWindow(path, title string) // 快应用独立子窗口
}

type WindowController struct {
	mu  sync.RWMutex
	win WindowControl
	Hub *domain.Hub // buildRouter 装配时注入（winCtl 创建早于 Hub，无法构造注入）
}

/* Set 注入当前窗口（openWindow 时）。 */
func (c *WindowController) Set(w WindowControl) {
	c.mu.Lock()
	c.win = w
	c.mu.Unlock()
}

func (c *WindowController) current() WindowControl {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.win
}

func (c *WindowController) Minimise(g *gin.Context) {
	if w := c.current(); w != nil {
		w.Minimise()
		g.Status(http.StatusNoContent)
		return
	}
	g.Status(http.StatusServiceUnavailable)
}

func (c *WindowController) ToggleMaximise(g *gin.Context) {
	if w := c.current(); w != nil {
		w.ToggleMaximise()
		g.JSON(http.StatusOK, gin.H{"maximized": w.IsMaximised()})
		return
	}
	g.Status(http.StatusServiceUnavailable)
}

func (c *WindowController) State(g *gin.Context) {
	if w := c.current(); w != nil {
		g.JSON(http.StatusOK, gin.H{"maximized": w.IsMaximised()})
		return
	}
	g.Status(http.StatusServiceUnavailable)
}

/*
Close POST /api/window/close：标题栏 X。已配置最小化到托盘 → 直接隐藏；
未配置 → 返回 prompt=true，由前端弹关闭询问（页面 modal，含「以后最小化
到托盘」勾选）。Alt+F4/任务栏关闭不经此端点，走窗口钩子（系统惯例退出）。
*/
func (c *WindowController) Close(g *gin.Context) {
	w := c.current()
	if w == nil {
		g.Status(http.StatusServiceUnavailable)
		return
	}
	if c.Hub.SettingsSnapshot().CloseToTray {
		w.Hide()
		g.JSON(http.StatusOK, gin.H{"hidden": true})
		return
	}
	g.JSON(http.StatusOK, gin.H{"prompt": true})
}

/*
	CloseDecision POST /api/window/close-decision：关闭询问的决定。

tray=本次最小化到托盘（remember 同时持久化，之后点 X 直接最小化）；
tray=false=退出。设置经 Hub（内存与磁盘同步，设置页同源联动）。
*/
func (c *WindowController) CloseDecision(g *gin.Context) {
	var body struct {
		Tray     bool `json:"tray"`
		Remember bool `json:"remember"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	w := c.current()
	if w == nil {
		g.Status(http.StatusServiceUnavailable)
		return
	}
	if body.Tray {
		if body.Remember && c.Hub != nil {
			if st := c.Hub.SettingsSnapshot(); !st.CloseToTray {
				st.CloseToTray = true
				_ = domain.SaveSettings(osfs.OS{}, st)
				c.Hub.ApplySettings(st)
			}
		}
		w.Hide()
		g.Status(http.StatusNoContent)
		return
	}
	w.RequestQuit()
	g.Status(http.StatusNoContent)
}

/* OpenApp POST /api/apps/open：桌面壳为快应用开子窗口（path 形如 /apps/x.html）。 */
func (c *WindowController) OpenApp(path, title string) bool {
	if w := c.current(); w != nil {
		w.OpenAppWindow(path, title)
		return true
	}
	return false
}

/*
OpenURL POST /api/window/open-url：用系统默认浏览器打开外链。
桌面壳 WebView 没有安全的"新窗口"语义（当前页导航会离开应用且无法返回），
外部链接一律由前端拦截后走这里；浏览器模式无需（前端直接 window.open）。
*/
func (c *WindowController) OpenURL(g *gin.Context) {
	var body struct {
		URL string `json:"url" binding:"required"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	u, err := url.Parse(body.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		g.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 http/https 链接"})
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", body.URL)
	case "darwin":
		cmd = exec.Command("open", body.URL)
	default:
		cmd = exec.Command("xdg-open", body.URL)
	}
	if err := cmd.Start(); err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	g.Status(http.StatusNoContent)
}

/*
OpenWeb POST /api/window/open-web：桌面壳为外链开 WebView 独立子窗口
（看板浏览器 iframe 被站点禁止内嵌时的兜底，能力完整）。无窗口（浏览器
模式）返回 503，前端退化为新标签页。
*/
func (c *WindowController) OpenWeb(g *gin.Context) {
	var body struct {
		URL string `json:"url" binding:"required"`
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	u, err := url.Parse(body.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		g.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 http/https 链接"})
		return
	}
	if w := c.current(); w != nil {
		w.OpenAppWindow(body.URL, u.Host)
		g.Status(http.StatusNoContent)
		return
	}
	g.Status(http.StatusServiceUnavailable)
}
