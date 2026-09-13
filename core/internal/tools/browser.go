/*
共享浏览器工具组(browser_*):AI 与用户操控魔法看板里的同一批真实
Chromium 标签,全局共享(个人助手语义)。页面镜像到工作区抽屉·浏览
器页供用户实时共见;用户接管在抽屉页原生完成(标签条/地址栏直接操作)。
检索、查资料、操作网页用本组工具(与 terminal 互补:那是本机命令,
这是真实浏览器);截图经 image_loaded 标记转持久化图片消息(多模态
模型直接看图)。
依赖倒置:tools 只依赖 BrowserIO 接口,由 service.BrowserService 实现
(service 已 import tools,反向引用会循环)。
*/
package tools

import (
	"context"

	"github.com/xuanlv2002/ezloop/types"
)

/* BrowserIO 是共享浏览器服务的能力面(service.BrowserService 实现)。 */
type BrowserIO interface {
	/* StartBrowser 新建标签(描述必填)并可选导航 */
	StartBrowser(desc, url string, timeoutMs int) (string, error)
	/* NavigateBrowser 跳转并等加载 */
	NavigateBrowser(tabID, url string, timeoutMs int) (string, error)
	/* ClickBrowser 点击(selector 优先,否则视口截图坐标) */
	ClickBrowser(tabID, selector string, x, y int) (string, error)
	/* TypeBrowser 输入文本(selector 定位输入框,submit 回车提交) */
	TypeBrowser(tabID, selector, text string, submit bool) (string, error)
	/* PressBrowserKey 按键/组合键("Enter"、"Control+A") */
	PressBrowserKey(tabID, combo string) (string, error)
	/* ScrollBrowser 滚动(up|down) */
	ScrollBrowser(tabID, direction string, amountPx int) (string, error)
	/* ReadBrowser 读正文/链接清单 */
	ReadBrowser(tabID, mode string, chars int) (string, error)
	/* ScreenshotBrowser 截图(落盘+image_loaded 标记) */
	ScreenshotBrowser(tabID string, fullPage bool) (string, error)
	/* CloseBrowserTab 关闭标签 */
	CloseBrowserTab(tabID string) error
	/* ListBrowserTabsJSON 标签清单(JSON 文本) */
	ListBrowserTabsJSON() string
	/* ModelSeesImages 主模型是否多模态(截图返回形态裁决) */
	ModelSeesImages() bool
}

type browserStartArgs struct {
	Desc      string `json:"desc" desc:"标签描述/名称(如 查竞品、看文档),browser_list 与看板中展示"`
	URL       string `json:"url,omitempty" desc:"创建后立即打开的网址(可选)"`
	TimeoutMs int    `json:"timeoutMs,omitempty" desc:"导航等待上限毫秒,默认 20000;首次使用会自动下载 Chromium,此时给 600000"`
}

type browserTabArgs struct {
	TabID string `json:"tabId,omitempty" desc:"目标标签 id(browser_list 查看),省略=最近使用的标签"`
}

type browserNavigateArgs struct {
	TabID     string `json:"tabId,omitempty" desc:"目标标签 id,省略=最近使用的标签"`
	URL       string `json:"url" desc:"要打开的网址"`
	TimeoutMs int    `json:"timeoutMs,omitempty" desc:"导航等待上限毫秒,默认 20000"`
}

type browserClickArgs struct {
	TabID    string `json:"tabId,omitempty" desc:"目标标签 id,省略=最近使用的标签"`
	Selector string `json:"selector,omitempty" desc:"CSS 选择器(优先,等元素出现后点击,如 a.login、#search button)"`
	X        int    `json:"x,omitempty" desc:"点击的横坐标(无 selector 时用;视口截图的像素坐标,先 browser_screenshot 看图再定坐标)"`
	Y        int    `json:"y,omitempty" desc:"点击的纵坐标(同上)"`
}

type browserTypeArgs struct {
	TabID    string `json:"tabId,omitempty" desc:"目标标签 id,省略=最近使用的标签"`
	Selector string `json:"selector,omitempty" desc:"输入框的 CSS 选择器(省略=在当前焦点处输入)"`
	Text     string `json:"text" desc:"要输入的文本(支持中文)"`
	Submit   bool   `json:"submit,omitempty" desc:"输入后回车提交(搜索框/登录框常用)"`
}

type browserKeyArgs struct {
	TabID string `json:"tabId,omitempty" desc:"目标标签 id,省略=最近使用的标签"`
	Combo string `json:"combo" desc:"按键,如 Enter、Escape、Tab、ArrowDown、Control+A、Shift+ArrowDown"`
}

type browserScrollArgs struct {
	TabID     string `json:"tabId,omitempty" desc:"目标标签 id,省略=最近使用的标签"`
	Direction string `json:"direction" desc:"滚动方向:up 或 down"`
	AmountPx  int    `json:"amountPx,omitempty" desc:"滚动像素量,默认 600"`
}

type browserReadArgs struct {
	TabID string `json:"tabId,omitempty" desc:"目标标签 id,省略=最近使用的标签"`
	Mode  string `json:"mode,omitempty" desc:"text=页面正文(默认);links=链接清单(文字→地址)"`
	Chars int    `json:"chars,omitempty" desc:"返回字符数上限,默认 4000,上限 20000"`
}

type browserShotArgs struct {
	TabID    string `json:"tabId,omitempty" desc:"目标标签 id,省略=最近使用的标签"`
	FullPage bool   `json:"fullPage,omitempty" desc:"截整页(默认只截视口;坐标点击要用视口截图,整页图只用于观察)"`
}

type browserCloseArgs struct {
	TabID string `json:"tabId" desc:"要关闭的标签 id"`
}

/* SharedBrowser 构造 browser_* 工具组(b 为 nil 返回 nil,测试装配可不注入)。 */
func SharedBrowser(b BrowserIO) []types.Tool {
	if b == nil {
		return nil
	}
	return []types.Tool{
		types.NewTool("browser_start",
			"新建一个浏览器标签(真实 Chromium,画面镜像到看板·浏览器页供用户实时共见,用户可唤起大窗接管)。"+
				"desc 是标签描述,用于 browser_list 与看板展示;url 可选,带则创建后立即打开。"+
				"首次使用会自动下载 Chromium(约 150MB,timeoutMs 给 600000)。检索、查资料、操作网页用它。",
			func(ctx context.Context, in *browserStartArgs) (string, error) {
				return b.StartBrowser(in.Desc, in.URL, in.TimeoutMs)
			}),
		types.NewTool("browser_navigate",
			"在标签中打开新网址并等加载完成,返回页面标题。tabId 省略=最近使用的标签。",
			func(ctx context.Context, in *browserNavigateArgs) (string, error) {
				return b.NavigateBrowser(in.TabID, in.URL, in.TimeoutMs)
			}),
		types.NewTool("browser_click",
			"点击页面元素。优先给 CSS 选择器(等元素出现后点击,稳定);无法定位时先 browser_screenshot 看图,"+
				"再给视口截图中的像素坐标 x/y。",
			func(ctx context.Context, in *browserClickArgs) (string, error) {
				return b.ClickBrowser(in.TabID, in.Selector, in.X, in.Y)
			}),
		types.NewTool("browser_type",
			"在页面输入文本(支持中文)。selector 定位输入框(如 input#kw),省略则在当前焦点处输入;"+
				"submit=true 输入后回车提交(搜索/登录常用)。",
			func(ctx context.Context, in *browserTypeArgs) (string, error) {
				return b.TypeBrowser(in.TabID, in.Selector, in.Text, in.Submit)
			}),
		types.NewTool("browser_key",
			"按键或组合键:单键如 Enter、Escape、Tab、Backspace、ArrowDown;组合键如 Control+A、Alt+F4。",
			func(ctx context.Context, in *browserKeyArgs) (string, error) {
				return b.PressBrowserKey(in.TabID, in.Combo)
			}),
		types.NewTool("browser_scroll",
			"滚动页面(direction=up|down,amountPx 默认 600)。翻阅长文档、加载更多内容用。",
			func(ctx context.Context, in *browserScrollArgs) (string, error) {
				return b.ScrollBrowser(in.TabID, in.Direction, in.AmountPx)
			}),
		types.NewTool("browser_read",
			"读取页面内容:mode=text 返回页面正文(默认);mode=links 返回链接清单(文字→地址,适合找下一步入口)。"+
				"chars 控制返回上限(默认 4000)。",
			func(ctx context.Context, in *browserReadArgs) (string, error) {
				return b.ReadBrowser(in.TabID, in.Mode, in.Chars)
			}),
		types.NewTool("browser_screenshot",
			"截图当前页面(默认视口,fullPage=true 截整页)。多模态模型直接看图定位元素;"+
				"坐标点击(browser_click 的 x/y)以视口截图为准。",
			func(ctx context.Context, in *browserShotArgs) (string, error) {
				return b.ScreenshotBrowser(in.TabID, in.FullPage)
			}),
		types.NewTool("browser_list",
			"列出全部浏览器标签(id、名称、当前页、标题)。用户手动开的与 AI 新开的都在内,所有会话共享。",
			func(ctx context.Context, _ *struct{}) (string, error) {
				return b.ListBrowserTabsJSON(), nil
			}),
		types.NewTool("browser_close",
			"关闭一个浏览器标签(用完的页面及时关,释放资源)。",
			func(ctx context.Context, in *browserCloseArgs) (string, error) {
				if err := b.CloseBrowserTab(in.TabID); err != nil {
					return "", err
				}
				return "closed " + in.TabID, nil
			}),
	}
}
